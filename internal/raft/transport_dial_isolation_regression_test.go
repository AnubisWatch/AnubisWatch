package raft

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func dialIsolationTLS(t *testing.T) (tls.Certificate, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local raft regression"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func TestTCPTransportDialDoesNotOwnPool(t *testing.T) {
	for _, scenario := range []string{"unrelated vote", "stop", "unregister", "changed address", "replacement connection"} {
		t.Run(scenario, func(t *testing.T) {
			cert, clientTLS := dialIsolationTLS(t)
			healthy, err := NewTCPTransport("127.0.0.1:0", "", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			healthy.RegisterHandler("PreVote", func(_ interface{}, ch chan interface{}) { ch <- &core.PreVoteResponse{VoteGranted: true} })
			if err := healthy.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { healthy.Stop() })
			arrived, release := make(chan struct{}), make(chan struct{})
			var arriveOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			blocked, err := NewTCPTransport("127.0.0.1:0", "", &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				arriveOnce.Do(func() { close(arrived) })
				<-release
				return &cert, nil
			}}, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			if err := blocked.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { blocked.Stop() })
			client, err := NewTCPTransport("127.0.0.1:0", "", clientTLS, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { client.Stop() })
			client.RegisterPeer("healthy", healthy.listener.Addr().String())
			client.RegisterPeer("blocked", blocked.listener.Addr().String())
			// Control: the verified TLS connection and real vote exchange are healthy.
			vote := func() error { _, err := client.SendPreVote("healthy", &core.PreVoteRequest{Term: 2}); return err }
			if err := vote(); err != nil {
				t.Fatal("control:", err)
			}
			type outcome struct {
				conn net.Conn
				err  error
			}
			result := make(chan outcome, 1)
			go func() { conn, err := client.getPooledConnection("blocked", "blocked"); result <- outcome{conn, err} }()
			select {
			case <-arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("dial never reached gated TLS handshake")
			}
			// The handshake is provably in flight. Pool ownership must be available
			// now, without waiting for any timer or releasing the stalled peer.
			if !client.connMu.TryLock() {
				unblock()
				<-result
				t.Fatal("unrelated TLS dial owns global pool lock")
			}
			client.connMu.Unlock()
			var replacement net.Conn
			switch scenario {
			case "unrelated vote":
				done := make(chan error, 1)
				go func() { done <- vote() }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("healthy vote blocked by unrelated handshake")
				}
			case "stop":
				client.Stop()
			case "unregister":
				client.UnregisterPeer("blocked")
			case "changed address":
				client.RegisterPeer("blocked", healthy.listener.Addr().String())
			case "replacement connection":
				var other net.Conn
				replacement, other = net.Pipe()
				defer other.Close()
				client.connMu.Lock()
				client.connections["blocked"] = replacement
				client.connMu.Unlock()
			}
			unblock()
			var got outcome
			select {
			case got = <-result:
			case <-time.After(3 * time.Second):
				t.Fatal("released dial did not complete")
			}
			client.connMu.Lock()
			pooled := client.connections["blocked"]
			client.connMu.Unlock()
			switch scenario {
			case "unrelated vote":
				if got.err != nil || got.conn == nil || pooled != got.conn {
					t.Fatalf("healthy dial not published: %v", got.err)
				}
			case "replacement connection":
				if got.err != nil || got.conn != replacement || pooled != replacement {
					t.Fatalf("late dial replaced current connection: %v", got.err)
				}
			default:
				if got.err == nil || got.conn != nil || pooled != nil {
					t.Fatalf("stale dial published after %s: conn=%v pooled=%v err=%v", scenario, got.conn, pooled, got.err)
				}
			}
		})
	}
}
