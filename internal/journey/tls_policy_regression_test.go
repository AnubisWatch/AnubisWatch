package journey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

func TestJourneyHonorsProcessTLSPolicy(t *testing.T) {
	if err := probe.ConfigureDefaultValidator(true, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = probe.ConfigureDefaultValidator(false, nil) })
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer server.Close()
	executor := NewExecutor(nil, newTestLogger())
	executor.SetRunSaver(func(context.Context, *core.JourneyRun) error { return nil })
	journey := &core.JourneyConfig{ID: "tls-policy", WorkspaceID: "default", Steps: []core.JourneyStep{{Type: core.CheckHTTP, Target: server.URL, Timeout: core.Duration{Duration: time.Second}, HTTP: &core.HTTPConfig{InsecureSkipVerify: true}}}}
	for _, tc := range []struct {
		name  string
		allow bool
		want  core.SoulStatus
	}{{"secure default", false, core.SoulDead}, {"explicit master override", true, core.SoulAlive}, {"override disabled again", false, core.SoulDead}} {
		t.Run(tc.name, func(t *testing.T) {
			executor.SetAllowInsecureSkipVerify(tc.allow)
			run, err := executor.RunOnce(context.Background(), journey)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != tc.want {
				t.Fatalf("status=%s, want %s", run.Status, tc.want)
			}
			if !journey.Steps[0].HTTP.InsecureSkipVerify {
				t.Fatal("policy mutated caller configuration")
			}
		})
	}
}
