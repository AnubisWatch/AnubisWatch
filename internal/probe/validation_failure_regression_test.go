package probe

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type rejectingChecker struct{ judged chan struct{} }

func (c *rejectingChecker) Type() core.CheckType { return core.CheckType("rejecting") }
func (c *rejectingChecker) Validate(*core.Soul) error {
	return errors.New("target IP \"127.0.0.1\" is blocked")
}
func (c *rejectingChecker) Judge(context.Context, *core.Soul) (*core.Judgment, error) {
	close(c.judged)
	return &core.Judgment{Status: core.SoulAlive}, nil
}

// A soul whose target fails validation (typically SSRF policy) used to be
// skipped without a judgment, leaving it "unknown" forever with no alert.
func TestScheduledCheckRecordsDeadJudgmentWhenValidationFails(t *testing.T) {
	checker := &rejectingChecker{judged: make(chan struct{})}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	store := &recordingProbeStorage{souls: map[string]*core.Soul{}}
	engine := newTestEngine(t, EngineOptions{Registry: registry, Store: store})

	engine.AssignSouls([]*core.Soul{{
		ID: "blocked", Name: "blocked", Type: checker.Type(),
		Weight: core.Duration{Duration: time.Hour},
	}})

	deadline := time.Now().Add(2 * time.Second)
	for store.latestJudgment() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	j := store.latestJudgment()
	if j == nil {
		t.Fatal("no judgment recorded for a soul that failed validation")
	}
	if j.Status != core.SoulDead || !strings.Contains(j.Message, "blocked") {
		t.Fatalf("judgment = %s %q, want dead with the validation error", j.Status, j.Message)
	}
	select {
	case <-checker.judged:
		t.Fatal("Judge ran for a soul that failed validation")
	default:
	}
}

func TestForceCheckReturnsDeadJudgmentWhenValidationFails(t *testing.T) {
	checker := &rejectingChecker{judged: make(chan struct{})}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	store := &recordingProbeStorage{souls: map[string]*core.Soul{}}
	engine := newTestEngine(t, EngineOptions{Registry: registry, Store: store})
	engine.AssignSouls([]*core.Soul{{
		ID: "blocked", Name: "blocked", Type: checker.Type(),
		Weight: core.Duration{Duration: time.Hour},
	}})

	j, err := engine.ForceCheck("blocked")
	if err != nil {
		t.Fatalf("ForceCheck error = %v, want a dead judgment", err)
	}
	if j.Status != core.SoulDead || !strings.Contains(j.Message, "blocked") {
		t.Fatalf("judgment = %s %q, want dead with the validation error", j.Status, j.Message)
	}
}

func TestConfigureDefaultValidator(t *testing.T) {
	prev := DefaultValidator.Load()
	t.Cleanup(func() { DefaultValidator.Store(prev) })
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "")

	if err := ConfigureDefaultValidator(false, nil); err != nil {
		t.Fatal(err)
	}
	err := ValidateAddress("127.0.0.1:80")
	if err == nil || !strings.Contains(err.Error(), "security.allow_private_targets") {
		t.Fatalf("default policy error = %v, want blocked with config hint", err)
	}

	if err := ConfigureDefaultValidator(true, nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddress("127.0.0.1:80"); err != nil {
		t.Fatalf("allow_private_targets: %v", err)
	}
	// Metadata / link-local stay blocked even when private targets are allowed,
	// and get no misleading hint.
	err = ValidateAddress("169.254.169.254:80")
	if err == nil || strings.Contains(err.Error(), "allow_private_targets") {
		t.Fatalf("metadata endpoint error = %v, want blocked without hint", err)
	}

	if err := ConfigureDefaultValidator(false, []string{"10.20.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddress("10.20.1.5:22"); err != nil {
		t.Fatalf("allowed network: %v", err)
	}
	if err := ValidateAddress("10.21.1.5:22"); err == nil {
		t.Fatal("address outside allowed_networks was accepted")
	}

	if err := ConfigureDefaultValidator(false, []string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
}

func TestPrivateHintOnlyForPrivateRanges(t *testing.T) {
	if privateHint(net.ParseIP("192.168.1.1")) == "" {
		t.Error("no hint for 192.168.1.1")
	}
	if privateHint(net.ParseIP("169.254.169.254")) != "" {
		t.Error("hint offered for hard-blocked metadata IP")
	}
}
