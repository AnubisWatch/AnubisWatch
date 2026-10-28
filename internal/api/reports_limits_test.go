package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// endlessStore streams judgments until the callback fails or ctx ends, and
// blocks SLA collection until ctx ends.
type endlessStore struct {
	*mockStorage
	collected chan struct{}
}

func (e *endlessStore) StreamJudgments(ctx context.Context, _, soulID string, start, _ time.Time, limit int, fn func(*core.Judgment) error) (int, bool, error) {
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return i, false, err
		}
		if limit > 0 && i >= limit {
			return i, true, nil
		}
		j := &core.Judgment{ID: fmt.Sprintf("j%09d", i), SoulID: soulID, Status: core.SoulAlive,
			Timestamp: start.Add(time.Duration(i) * time.Millisecond), Message: strings.Repeat("x", 200)}
		if err := fn(j); err != nil {
			return i, false, err
		}
	}
}

func (e *endlessStore) CollectSLA(ctx context.Context, _, _ string, _, _ time.Time, _ []core.TimeRange, _, _ time.Time) (*sla.Tally, error) {
	if e.collected != nil {
		close(e.collected)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestExportConcurrencyIsLimited(t *testing.T) {
	store, _ := reportFixture()
	s := reportServer(store, nil)
	path := "/api/v1/souls/web/judgments/export?limit=1"

	cases := []struct {
		name  string
		hold  map[string]int // slots taken before the request
		token string
		want  int
	}{
		{"free", nil, "role-viewer", http.StatusOK},
		{"user at its limit", map[string]int{"u-viewer": maxExportsPerUser}, "role-viewer", http.StatusTooManyRequests},
		{"another user is not affected", map[string]int{"u-viewer": maxExportsPerUser}, "role-admin", http.StatusOK},
		{"server at its limit", map[string]int{"a": 2, "b": 2, "c": 2, "d": 2}, "role-admin", http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for user, n := range tc.hold {
				for i := 0; i < n; i++ {
					if !s.exports.acquire(user) {
						t.Fatalf("could not pre-acquire for %s", user)
					}
				}
			}
			rec := reportGet(s, tc.token, path)
			for user, n := range tc.hold {
				for i := 0; i < n; i++ {
					s.exports.release(user)
				}
			}
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
		})
	}
	if s.exports.total != 0 || len(s.exports.byUser) != 0 {
		t.Fatalf("slots leaked: total %d, %v", s.exports.total, s.exports.byUser)
	}
}

// A client that stops reading is cut off after the idle timeout and its slot
// is freed, instead of holding both for the whole export window.
func TestExportStalledReaderIsCutOff(t *testing.T) {
	prev := exportIdleTimeout
	exportIdleTimeout = 200 * time.Millisecond
	defer func() { exportIdleTimeout = prev }()

	store, _ := reportFixture()
	es := &endlessStore{mockStorage: store}
	s := reportServer(store, nil)
	s.store = es
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { done <- struct{}{} }()
		s.router.ServeHTTP(w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/souls/web/judgments/export?limit=1000000", nil)
	req.Header.Set("Authorization", "Bearer role-viewer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() // never read: the client stalls
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the export kept running against a stalled reader")
	}
	if s.exports.total != 0 {
		t.Fatalf("slot not released: %d", s.exports.total)
	}
}

func TestSLAReportStopsAtItsTimeBudget(t *testing.T) {
	prev := slaReportBudget
	slaReportBudget = 50 * time.Millisecond
	defer func() { slaReportBudget = prev }()

	store, _ := reportFixture()
	s := reportServer(store, nil)
	s.store = &endlessStore{mockStorage: store}
	start := time.Now()
	rec := reportGet(s, "role-viewer", "/api/v1/reports/sla")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "time budget") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("budget not enforced promptly")
	}

	// A client that disconnects cancels the collection.
	slaReportBudget = time.Hour
	collected := make(chan struct{})
	s.store = &endlessStore{mockStorage: store, collected: collected}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/v1/reports/sla?soul_id=web", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer role-viewer")
	finished := make(chan struct{})
	go func() {
		s.router.ServeHTTP(httptest.NewRecorder(), req)
		close(finished)
	}()
	<-collected
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("report kept running after the client left")
	}
}
