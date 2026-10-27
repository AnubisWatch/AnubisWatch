package cluster

// Regression: the Raft peer listener must reject clients that present no
// buildTLSPeerConfig + tls.Listen reject a client that presents NO
// certificate when VerifyPeers is true (drill scenario 5, next.md:165).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func tempCertFile(t *testing.T, der []byte, typ string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), typ+".pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRaftPeerTLSRejectsClientsWithoutCertificates(t *testing.T) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "drill-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caPath := tempCertFile(t, caDER, "CERTIFICATE")
	_ = caPath // CA file path for the config below

	srvKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "anubis-drill-peer"},
		DNSNames:     []string{"anubis-drill-peer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	srvDER, _ := x509.CreateCertificate(rand.Reader, srvTmpl, caTmpl, &srvKey.PublicKey, caKey)
	certFile := tempCertFile(t, srvDER, "CERTIFICATE")

	keyDER, _ := x509.MarshalECPrivateKey(srvKey)
	keyFile := filepath.Join(t.TempDir(), "key.pem")
	_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)

	cfg := &core.TLSPeerConfig{
		CertFile:    certFile,
		KeyFile:     keyFile,
		CAFile:      caPath,
		VerifyPeers: true,
	}
	tlsCfg, err := buildTLSPeerConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSPeerConfig: %v", err)
	}
	if tlsCfg == nil || tlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert", tlsCfg.ClientAuth)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type result struct{ err error }
	serverDone := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- result{err}
			return
		}
		buf := make([]byte, 16)
		_, rerr := conn.Read(buf) // drives the server-side handshake
		serverDone <- result{rerr}
	}()

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		RootCAs:    caPool,
		ServerName: "anubis-drill-peer",
		MinVersion: tls.VersionTLS12,
		// NO Certificates: the empty-client-cert case from the drill.
	})
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	clientErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := client.Read(buf)
		clientErr <- err
	}()

	select {
	case sr := <-serverDone:
		if sr.err == nil {
			t.Fatal("SERVER ACCEPTED a client with no certificate: mTLS enforcement is NOT active")
		}
		t.Logf("server rejected empty-cert client: %v", sr.err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server handshake result")
	}
}
