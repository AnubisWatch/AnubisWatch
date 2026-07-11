package cluster

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

func writeTestPeerCertificate(t *testing.T) (certFile, keyFile string, certPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "cluster-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, certPEM
}

func TestBuildTLSPeerConfigCompleteModes(t *testing.T) {
	certFile, keyFile, caPEM := writeTestPeerCertificate(t)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		cfg  core.TLSPeerConfig
		auth tls.ClientAuthType
	}{
		{"certificate only", core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile}, tls.NoClientCert},
		{"verify with CA", core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, CAFile: caFile, VerifyPeers: true}, tls.RequireAndVerifyClientCert},
		{"require with CA", core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, CAFile: caFile, RequireClientCert: true}, tls.RequireAnyClientCert},
		{"require without CA", core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, RequireClientCert: true}, tls.RequireAnyClientCert},
		{"verify without CA", core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, VerifyPeers: true}, tls.RequireAnyClientCert},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildTLSPeerConfig(&tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got.Certificates) != 1 || got.ClientAuth != tc.auth {
				t.Fatalf("unexpected TLS config: %#v", got)
			}
		})
	}

	missingCA := filepath.Join(t.TempDir(), "missing.pem")
	if _, err := buildTLSPeerConfig(&core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, CAFile: missingCA}); err == nil {
		t.Fatal("expected missing CA error")
	}
	badCA := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(badCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildTLSPeerConfig(&core.TLSPeerConfig{CertFile: certFile, KeyFile: keyFile, CAFile: badCA}); err == nil {
		t.Fatal("expected invalid CA error")
	}
}

func TestDistributorSelectionAndLoopEdgePaths(t *testing.T) {
	d := NewDistributor("self", "east", DistributionStrategy(99), newTestLogger())
	candidates := []*NodeLoad{
		{NodeID: "west", Region: "west", Healthy: true, SoulCount: 3},
		{NodeID: "west-light", Region: "west", Healthy: true, SoulCount: 1},
	}
	for _, node := range candidates {
		d.nodeLoads[node.NodeID] = node
	}
	if got := d.selectNodeForSoul(&core.Soul{ID: "s"}); got != "west-light" {
		t.Fatalf("default strategy selected %q", got)
	}
	if got := d.selectRoundRobin(nil); got != "" {
		t.Fatalf("empty round robin = %q", got)
	}
	if got := d.selectLoadBased(nil); got != "" {
		t.Fatalf("empty load selection = %q", got)
	}
	if got := d.selectRegionAware(candidates, &core.Soul{}); got != "west-light" {
		t.Fatalf("region fallback = %q", got)
	}
	if got := d.selectHashBased(nil, "key"); got != "" {
		t.Fatalf("empty hash selection = %q", got)
	}

	loop := NewDistributor("self", "east", StrategyRoundRobin, newTestLogger())
	loop.wg.Add(1)
	go loop.rebalanceLoop(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	close(loop.stopCh)
	loop.wg.Wait()
}

func TestDistributorRebalanceSkipsUnhealthyAndCallsBack(t *testing.T) {
	d := NewDistributor("self", "east", StrategyRoundRobin, newTestLogger())
	d.rebalanceThreshold = .1
	d.nodeLoads["over"] = &NodeLoad{NodeID: "over", Healthy: true, SoulCount: 4}
	d.nodeLoads["under"] = &NodeLoad{NodeID: "under", Healthy: true, SoulCount: 0}
	d.nodeLoads["dead"] = &NodeLoad{NodeID: "dead", Healthy: false, SoulCount: 100}
	for i := 0; i < 4; i++ {
		d.soulMap[string(rune('a'+i))] = "over"
	}
	called := make(chan []SoulMove, 1)
	d.SetCallbacks(nil, nil, func(m []SoulMove) { called <- m })
	d.checkAndRebalance()
	select {
	case moves := <-called:
		if len(moves) == 0 {
			t.Fatal("expected rebalance moves")
		}
	case <-time.After(time.Second):
		t.Fatal("rebalance callback not called")
	}
}

func TestManagerAdditionalStartAndGetterPaths(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	cfg := newTestRaftConfig()
	cfg.Bootstrap = true
	cfg.BindAddr = "bad address"
	m, err := NewManager(core.NecropolisConfig{Raft: cfg}, db, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("expected invalid bind address error")
	}

	singleCfg := newTestRaftConfig()
	single, err := NewManager(core.NecropolisConfig{SingleNode: true, Raft: singleCfg}, db, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !single.IsClustered() {
		t.Fatal("single-node manager must be clustered")
	}

	runningCfg := newTestRaftConfig()
	runningCfg.Bootstrap = true
	running, err := NewManager(core.NecropolisConfig{Raft: runningCfg}, db, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer running.Stop(context.Background())
	_ = running.IsLeader()
	_ = running.Leader()
	if running.GetStatus().State == "" {
		t.Fatal("running manager status has no state")
	}
}
