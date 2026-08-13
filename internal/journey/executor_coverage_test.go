package journey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// TestMain sets ANUBIS_SSRF_ALLOW_PRIVATE before any probe-package init
// so that httptest.NewServer targets (127.0.0.1) pass SSRF validation.
// Without this, every test would need its own per-test reset, which races
// when tests run in parallel (ResetDefaultForTest mutates a package-level
// var). Setting it once here is safe because the entire journey package
// test suite needs private-network access.
func TestMain(m *testing.M) {
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	probe.ResetDefaultForTest()
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// RunOnce — the manual one-shot entry point. Previously untested.
// ---------------------------------------------------------------------------

func TestRunOnce_NilJourney(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	_, err := executor.RunOnce(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil journey")
	}
}

func TestRunOnce_NoSteps(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "empty",
		Name:        "Empty",
		WorkspaceID: "default",
		Steps:       []core.JourneyStep{},
	}

	_, err := executor.RunOnce(context.Background(), j)
	if err == nil {
		t.Fatal("expected error for journey with no steps")
	}
}

func TestRunOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "manual-run",
		Name:        "Manual Run",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "fetch",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
				},
			},
		},
	}

	run, err := executor.RunOnce(context.Background(), j)
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if run == nil {
		t.Fatal("RunOnce returned nil run")
	}
	if run.JourneyID != j.ID {
		t.Errorf("JourneyID = %q, want %q", run.JourneyID, j.ID)
	}
	if run.Status != core.SoulAlive {
		t.Errorf("Status = %q, want %q", run.Status, core.SoulAlive)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("Steps len = %d, want 1", len(run.Steps))
	}
	if run.Steps[0].Status != core.SoulAlive {
		t.Errorf("Step 0 status = %q, want %q", run.Steps[0].Status, core.SoulAlive)
	}
	if run.Duration <= 0 {
		t.Error("Duration should be positive")
	}
	if run.CompletedAt < run.StartedAt {
		t.Error("CompletedAt should be >= StartedAt")
	}
	if run.JackalID != "local" {
		t.Errorf("JackalID = %q, want %q", run.JackalID, "local")
	}
	if run.Region != "default" {
		t.Errorf("Region = %q, want %q", run.Region, "default")
	}
}

func TestRunOnce_StepFailure_NoContinue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:                "fail-journey",
		Name:              "Fail",
		WorkspaceID:       "default",
		ContinueOnFailure: false,
		Steps: []core.JourneyStep{
			{
				Name:   "bad",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
			{
				Name:   "never-reached",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
		},
	}

	run, err := executor.RunOnce(context.Background(), j)
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if run.Status != core.SoulDead {
		t.Errorf("Status = %q, want %q", run.Status, core.SoulDead)
	}
	// Should stop after the first failure since ContinueOnFailure is false.
	if len(run.Steps) != 1 {
		t.Errorf("Steps len = %d, want 1 (stopped early)", len(run.Steps))
	}
}

func TestRunOnce_StepFailure_WithContinue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:                "continue-journey",
		Name:              "Continue",
		WorkspaceID:       "default",
		ContinueOnFailure: true,
		Steps: []core.JourneyStep{
			{
				Name:   "fail1",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
			{
				Name:   "fail2",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
		},
	}

	run, err := executor.RunOnce(context.Background(), j)
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if run.Status != core.SoulDead {
		t.Errorf("Status = %q, want %q", run.Status, core.SoulDead)
	}
	// Both steps should execute since ContinueOnFailure is true.
	if len(run.Steps) != 2 {
		t.Errorf("Steps len = %d, want 2 (continue on failure)", len(run.Steps))
	}
}

// ---------------------------------------------------------------------------
// Variable propagation through steps via Extract + interpolation.
// ---------------------------------------------------------------------------

func TestRunOnce_VariablePropagation(t *testing.T) {
	var extractedVal atomic.Value // string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"secret123"}`))
			return
		}
		// page2 — verify we received the propagated variable in headers.
		extractedVal.Store(r.Header.Get("X-Token"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "var-propagation",
		Name:        "Var Propagation",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "extract",
				Type:   core.CheckHTTP,
				Target: srv.URL + "/page1",
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
				},
				Extract: map[string]core.ExtractionRule{
					"auth_token": {From: "body", Path: "$.token"},
				},
			},
			{
				Name:   "use-var",
				Type:   core.CheckHTTP,
				Target: srv.URL + "/page2",
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
					Headers:     map[string]string{"X-Token": "${auth_token}"},
				},
			},
		},
	}

	run, err := executor.RunOnce(context.Background(), j)
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if run.Status != core.SoulAlive {
		t.Fatalf("Status = %q, want %q", run.Status, core.SoulAlive)
	}
	// Verify the JSONPath extraction populated the variable.
	v, ok := run.Variables["auth_token"]
	if !ok {
		t.Fatal("auth_token not in run.Variables")
	}
	if v != "secret123" {
		t.Errorf("auth_token = %q, want %q", v, "secret123")
	}
	// Verify the header was sent on the second request.
	sent, _ := extractedVal.Load().(string)
	if sent != "secret123" {
		t.Errorf("X-Token header on page2 = %q, want %q", sent, "secret123")
	}
}

// ---------------------------------------------------------------------------
// Dedup logic — when the same extracted values appear across runs, the
// scheduled execution path (allowDedupSkip=true) returns nil to suppress
// redundant saves.
// ---------------------------------------------------------------------------

func TestRunOnce_DedupHashUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value":"same"}`))
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "dedup-journey",
		Name:        "Dedup",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "fetch",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
					JSONPath:    map[string]string{"val": "$.value"},
				},
				Extract: map[string]core.ExtractionRule{
					"val": {From: "body", Path: "$.value"},
				},
			},
		},
	}

	// First manual run — should succeed and store a dedup hash.
	_, err := executor.RunOnce(context.Background(), j)
	if err != nil {
		t.Fatalf("first RunOnce err = %v", err)
	}

	executor.mu.RLock()
	hash, hashExists := executor.lastHash[j.ID]
	executor.mu.RUnlock()

	if !hashExists {
		t.Fatal("dedup hash not set after RunOnce")
	}
	if hash == "" {
		t.Error("dedup hash is empty")
	}
}

// ---------------------------------------------------------------------------
// executeJourney — the scheduled path with dedup. Calling it twice with
// identical extracted values should return nil on the second call.
// ---------------------------------------------------------------------------

func TestExecuteJourney_DedupSkipsIdenticalRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "sched-dedup",
		Name:        "Sched Dedup",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "fetch",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
					JSONPath:    map[string]string{"status": "$.status"},
				},
				Extract: map[string]core.ExtractionRule{
					"status": {From: "body", Path: "$.status"},
				},
			},
		},
	}

	ctx := context.Background()

	// First scheduled run populates the dedup hash.
	first := executor.executeJourneyRun(ctx, j, true)
	if first == nil {
		t.Fatal("first scheduled run returned nil (expected a run)")
	}

	// Second scheduled run with identical extracted values should be deduped.
	second := executor.executeJourneyRun(ctx, j, true)
	if second != nil {
		t.Error("second scheduled run with identical values should return nil (deduped)")
	}
}

func TestExecuteJourney_NoDedupWithoutExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "no-extract",
		Name:        "No Extract",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "fetch",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
				},
			},
		},
	}

	ctx := context.Background()

	// Without extracted values, dedup hash is empty → both runs should return.
	first := executor.executeJourneyRun(ctx, j, true)
	if first == nil {
		t.Fatal("first run returned nil without extraction")
	}
	second := executor.executeJourneyRun(ctx, j, true)
	if second == nil {
		t.Fatal("second run returned nil without extraction (should not dedup)")
	}
}

// ---------------------------------------------------------------------------
// runJourneyLoop — verify the background loop fires at the configured
// interval and stops on context cancellation.
// ---------------------------------------------------------------------------

func TestRunJourneyLoop_FiresOnInterval(t *testing.T) {
	var hitCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "loop-journey",
		Name:        "Loop",
		WorkspaceID: "default",
		Steps: []core.JourneyStep{
			{
				Name:   "hit",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
		},
	}

	// Use a short interval so the test completes quickly.
	j.Weight = core.Duration{Duration: 100 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	run := &journeyRun{cancel: cancel}
	executor.running[j.ID] = run

	go executor.runJourneyLoop(ctx, j, run)

	// Wait for at least 2 executions (immediate + at least 1 tick).
	time.Sleep(350 * time.Millisecond)
	cancel()

	count := hitCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 hits, got %d", count)
	}
}

func TestRunJourneyLoop_StopsOnCancel(t *testing.T) {
	var hitCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	j := &core.JourneyConfig{
		ID:          "cancel-journey",
		Name:        "Cancel",
		WorkspaceID: "default",
		Weight:      core.Duration{Duration: 100 * time.Millisecond},
		Steps: []core.JourneyStep{
			{
				Name:   "hit",
				Type:   core.CheckHTTP,
				Target: srv.URL,
				HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &journeyRun{cancel: cancel}
	executor.running[j.ID] = run

	go executor.runJourneyLoop(ctx, j, run)

	time.Sleep(250 * time.Millisecond)
	cancel()
	countAfterCancel := hitCount.Load()

	// Wait a bit more — no new hits should arrive.
	time.Sleep(250 * time.Millisecond)
	countFinal := hitCount.Load()

	if countFinal > countAfterCancel+1 {
		t.Errorf("loop kept firing after cancel: after=%d, final=%d", countAfterCancel, countFinal)
	}
}

// ---------------------------------------------------------------------------
// retryWithBackoff
// ---------------------------------------------------------------------------

func TestRetryWithBackoff_SuccessOnFirstTry(t *testing.T) {
	calls := 0
	err := retryWithBackoff(context.Background(), 3, 10*time.Millisecond, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetryWithBackoff_RetriesAndSucceeds(t *testing.T) {
	var calls atomic.Int64
	err := retryWithBackoff(context.Background(), 3, 5*time.Millisecond, func() error {
		if calls.Add(1) < 3 {
			return http.ErrServerClosed // any non-nil error
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestRetryWithBackoff_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int64
	err := retryWithBackoff(context.Background(), 2, 1*time.Millisecond, func() error {
		calls.Add(1)
		return http.ErrServerClosed
	})
	if err == nil {
		t.Fatal("expected err after exhausting retries")
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestRetryWithBackoff_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int64
	// Cancel on the second call to ensure the retry loop notices.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := retryWithBackoff(ctx, 5, 50*time.Millisecond, func() error {
		calls.Add(1)
		return http.ErrServerClosed
	})
	if err == nil {
		t.Fatal("expected context cancellation err")
	}
	// Should not have retried all 5 times.
	if calls.Load() >= 5 {
		t.Errorf("calls = %d, expected fewer than 5 (context cancelled)", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// interpolateVariables — ${var} replacement in targets, headers, bodies.
// ---------------------------------------------------------------------------

func TestInterpolateVariables_BasicReplacement(t *testing.T) {
	executor := NewExecutor(nil, newTestLogger())

	vars := map[string]string{"host": "example.com", "port": "8080"}

	got := executor.interpolateVariables("https://${host}:${port}/api", vars)
	want := "https://example.com:8080/api"
	if got != want {
		t.Errorf("interpolateVariables = %q, want %q", got, want)
	}
}

func TestInterpolateVariables_NoMatch(t *testing.T) {
	executor := NewExecutor(nil, newTestLogger())

	got := executor.interpolateVariables("https://literal.example.com", map[string]string{})
	if got != "https://literal.example.com" {
		t.Errorf("interpolateVariables = %q, want unchanged", got)
	}
}

func TestInterpolateVariables_MultipleOccurrences(t *testing.T) {
	executor := NewExecutor(nil, newTestLogger())

	vars := map[string]string{"env": "prod"}
	got := executor.interpolateVariables("${env}-${env}-${env}", vars)
	if got != "prod-prod-prod" {
		t.Errorf("interpolateVariables = %q, want %q", got, "prod-prod-prod")
	}
}

// ---------------------------------------------------------------------------
// NewExecutorWithNodeID — defaults for empty values.
// ---------------------------------------------------------------------------

func TestNewExecutorWithNodeID_Defaults(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutorWithNodeID(db, newTestLogger(), "", "")
	if executor.nodeID != "local" {
		t.Errorf("nodeID = %q, want %q", executor.nodeID, "local")
	}
	if executor.region != "default" {
		t.Errorf("region = %q, want %q", executor.region, "default")
	}
}

func TestNewExecutorWithNodeID_CustomValues(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutorWithNodeID(db, newTestLogger(), "jackal-1", "us-east-1")
	if executor.nodeID != "jackal-1" {
		t.Errorf("nodeID = %q, want %q", executor.nodeID, "jackal-1")
	}
	if executor.region != "us-east-1" {
		t.Errorf("region = %q, want %q", executor.region, "us-east-1")
	}
}
