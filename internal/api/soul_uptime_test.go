package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func soulUptimeServer(store *mockStorage) *RESTServer {
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      store,
		router:     router,
		auth:       &mockAuthenticator{},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("GET", "/api/v1/souls/:id/uptime", server.requireAuth(server.handleSoulUptime))
	return server
}

func getSoulUptime(t *testing.T, server *RESTServer, path string) (*httptest.ResponseRecorder, SoulUptimeSummary) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	var summary SoulUptimeSummary
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rec, summary
}

func addJudgments(store *mockStorage, soulID string, age time.Duration, status core.SoulStatus, n int, latency time.Duration) {
	at := time.Now().Add(-age)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d-%d", soulID, at.UnixNano(), i)
		store.judgments[id] = &core.Judgment{
			ID: id, SoulID: soulID, Status: status, Duration: latency,
			Timestamp: at.Add(-time.Duration(i) * time.Second),
		}
	}
}

func pct(w UptimeWindow) string {
	if w.UptimePercent == nil {
		return "null"
	}
	return fmt.Sprintf("%.2f", *w.UptimePercent)
}

// The dashboard computed 7- and 30-day uptime from the judgments endpoint,
// which returns at most 100 results of the last 24 hours, so those figures
// covered a day at most, and a soul with no checks showed 100%.
func TestSoulUptimeWindowsCoverTheirWholePeriod(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}
	addJudgments(store, "s1", time.Hour, core.SoulAlive, 3, 100*time.Millisecond)
	addJudgments(store, "s1", 2*time.Hour, core.SoulDead, 1, 500*time.Millisecond)
	addJudgments(store, "s1", 3*24*time.Hour, core.SoulDead, 4, time.Second)
	addJudgments(store, "s1", 20*24*time.Hour, core.SoulAlive, 2, 50*time.Millisecond)
	addJudgments(store, "s1", 40*24*time.Hour, core.SoulDead, 1, time.Second) // outside every window

	rec, got := getSoulUptime(t, soulUptimeServer(store), "/api/v1/souls/s1/uptime")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	want := map[string]struct {
		checks, up int
		pct        string
	}{
		"24h": {4, 3, "75.00"},
		"7d":  {8, 3, "37.50"},
		"30d": {10, 5, "50.00"},
	}
	for name, w := range want {
		g := got.Windows[name]
		if g.Checks != w.checks || g.Up != w.up || g.Failures != w.checks-w.up || pct(g) != w.pct {
			t.Errorf("%s = checks %d up %d failures %d uptime %s; want %d, %d, %d, %s",
				name, g.Checks, g.Up, g.Failures, pct(g), w.checks, w.up, w.checks-w.up, w.pct)
		}
	}
	if g := got.Windows["24h"].AvgLatencyMs; g != 200 {
		t.Errorf("24h avg latency = %v, want 200", g)
	}

	// Default series: the last 7 calendar days (UTC here), oldest first.
	points := got.Series.Points
	if got.Series.Range != "7d" || got.Series.StepSeconds != 86400 || len(points) != 7 {
		t.Fatalf("series = range %q, step %ds, %d points; want 7d, 86400s, 7", got.Series.Range, got.Series.StepSeconds, len(points))
	}
	threeDaysAgo := time.Now().UTC().Add(-3 * 24 * time.Hour).Format("2006-01-02")
	empty := 0
	for _, p := range points {
		day := p.Start.UTC().Format("2006-01-02")
		if day == threeDaysAgo && (p.Checks != 4 || pct(p.UptimeWindow) != "0.00") {
			t.Errorf("day %s = %d checks, uptime %s; want 4 checks at 0%%", day, p.Checks, pct(p.UptimeWindow))
		}
		if p.Checks == 0 {
			empty++
			if p.UptimePercent != nil {
				t.Errorf("day %s has no checks but uptime %s; want null", day, pct(p.UptimeWindow))
			}
		}
	}
	if empty < 4 {
		t.Errorf("only %d of 7 days are empty; checks landed on the wrong days", empty)
	}
	if !points[0].Start.Before(points[6].Start) {
		t.Errorf("series is not oldest first: %s .. %s", points[0].Start, points[6].Start)
	}
}

// The range selector picks the series: 12 five-minute buckets for 1h, 24
// hours for 24h, 7 or 30 days. Each check lands in the bucket containing it.
func TestSoulUptimeSeriesFollowsTheRange(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}
	now := time.Now()
	ages := []time.Duration{2 * time.Minute, 40 * time.Minute, 5 * time.Hour, 3 * 24 * time.Hour, 20 * 24 * time.Hour}
	for i, age := range ages {
		id := fmt.Sprintf("j%d", i)
		store.judgments[id] = &core.Judgment{ID: id, SoulID: "s1", Status: core.SoulAlive, Timestamp: now.Add(-age)}
	}
	server := soulUptimeServer(store)

	cases := []struct {
		rng      string
		step     time.Duration
		buckets  int
		inSeries int // checks whose bucket falls in the range
	}{
		{"1h", 5 * time.Minute, 12, 2},
		{"24h", time.Hour, 24, 3},
		{"7d", 24 * time.Hour, 7, 4},
		{"30d", 24 * time.Hour, 30, 5},
	}
	for _, tc := range cases {
		rec, got := getSoulUptime(t, server, "/api/v1/souls/s1/uptime?range="+tc.rng+"&tz_offset_minutes=330")
		if rec.Code != http.StatusOK {
			t.Fatalf("range %s: status %d", tc.rng, rec.Code)
		}
		s := got.Series
		if s.Range != tc.rng || s.StepSeconds != int(tc.step/time.Second) || len(s.Points) != tc.buckets {
			t.Fatalf("range %s: got %q, %ds, %d points", tc.rng, s.Range, s.StepSeconds, len(s.Points))
		}
		total := 0
		for i, p := range s.Points {
			total += p.Checks
			if i > 0 && p.Start.Sub(s.Points[i-1].Start) != tc.step {
				t.Fatalf("range %s: point %d starts %s after the previous, want %s", tc.rng, i, p.Start.Sub(s.Points[i-1].Start), tc.step)
			}
			for _, age := range ages {
				at := now.Add(-age)
				if !at.Before(p.Start) && at.Before(p.Start.Add(tc.step)) && p.Checks == 0 {
					t.Errorf("range %s: check at %s missing from its bucket %s", tc.rng, at, p.Start)
				}
			}
		}
		if total != tc.inSeries {
			t.Errorf("range %s: %d checks in the series, want %d", tc.rng, total, tc.inSeries)
		}
		if last := s.Points[len(s.Points)-1]; !now.Before(last.Start.Add(tc.step)) || now.Before(last.Start) {
			t.Errorf("range %s: last bucket %s does not contain now", tc.rng, last.Start)
		}
		// Buckets align to the caller's zone (UTC+5:30 here, so hour
		// boundaries fall at :30 UTC).
		if l := s.Points[0].Start.In(time.FixedZone("", 330*60)); l.Second() != 0 || (tc.step >= time.Hour && l.Minute() != 0) {
			t.Errorf("range %s: first bucket %s is not aligned in the caller's zone", tc.rng, l)
		}
	}
}

func TestSoulUptimeWithoutChecksIsNull(t *testing.T) {
	store := newMockStorage()
	store.souls["new"] = &core.Soul{ID: "new", Name: "new", WorkspaceID: "default"}

	_, got := getSoulUptime(t, soulUptimeServer(store), "/api/v1/souls/new/uptime")
	for _, name := range []string{"24h", "7d", "30d"} {
		if w := got.Windows[name]; w.Checks != 0 || w.UptimePercent != nil {
			t.Errorf("%s for a soul with no checks = %d checks, uptime %s; want 0 and null", name, w.Checks, pct(w))
		}
	}
}

func TestSoulUptimeDaysFollowTheCallersTimeZone(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}
	at := time.Now().Add(-time.Hour)
	store.judgments["j"] = &core.Judgment{ID: "j", SoulID: "s1", Status: core.SoulAlive, Timestamp: at}
	server := soulUptimeServer(store)

	for _, offset := range []int{-600, 0, 330, 840} {
		_, got := getSoulUptime(t, server, fmt.Sprintf("/api/v1/souls/s1/uptime?tz_offset_minutes=%d", offset))
		zone := time.FixedZone("", offset*60)
		wantDate := at.In(zone).Format("2006-01-02")
		// The check lands in the bucket for its local calendar day — usually
		// today, but yesterday when the test runs within an hour after local
		// midnight; the test used to assume the last bucket and failed then.
		found := false
		for _, p := range got.Series.Points {
			if p.Start.In(zone).Format("2006-01-02") != wantDate {
				if p.Checks != 0 {
					t.Errorf("offset %d: day %s has %d checks; the only check is on %s", offset, p.Start.In(zone).Format("2006-01-02"), p.Checks, wantDate)
				}
				continue
			}
			found = true
			if p.Checks != 1 {
				t.Errorf("offset %d: day %s has %d checks; want 1", offset, wantDate, p.Checks)
			}
			if l := p.Start.In(zone); l.Hour() != 0 || l.Minute() != 0 {
				t.Errorf("offset %d: day bucket starts at %s, not local midnight", offset, l)
			}
		}
		if !found {
			t.Errorf("offset %d: no bucket for %s", offset, wantDate)
		}
	}
}

func TestSoulUptimeRejectsOtherWorkspacesAndBadOffsets(t *testing.T) {
	store := newMockStorage()
	store.souls["theirs"] = &core.Soul{ID: "theirs", Name: "theirs", WorkspaceID: "other"}
	store.souls["ours"] = &core.Soul{ID: "ours", Name: "ours", WorkspaceID: "default"}
	server := soulUptimeServer(store)

	cases := map[string]int{
		"/api/v1/souls/theirs/uptime":                     http.StatusForbidden,
		"/api/v1/souls/missing/uptime":                    http.StatusNotFound,
		"/api/v1/souls/ours/uptime?tz_offset_minutes=abc": http.StatusBadRequest,
		"/api/v1/souls/ours/uptime?tz_offset_minutes=900": http.StatusBadRequest,
		"/api/v1/souls/ours/uptime?range=90d":             http.StatusBadRequest,
	}
	for path, want := range cases {
		if rec, _ := getSoulUptime(t, server, path); rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

// tallyingStore is a store that can count checks itself, as the production
// storage does from its hourly summaries.
type tallyingStore struct {
	*mockStorage
	ranges    []core.TimeRange
	listCalls int
}

func (s *tallyingStore) TallyChecks(soulID string, ranges []core.TimeRange) ([]core.CheckTally, error) {
	s.ranges = ranges
	out := make([]core.CheckTally, len(ranges))
	for i := range out {
		out[i] = core.CheckTally{Checks: 10 * (i + 1), Up: 9 * (i + 1), LatencySumMs: float64(100 * (i + 1))}
	}
	return out, nil
}

func (s *tallyingStore) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	s.listCalls++
	return s.mockStorage.ListJudgmentsNoCtx(soulID, start, end, limit)
}

// A store that tallies is asked for every window and bucket at once, and
// the month of raw judgments is never loaded.
func TestSoulUptimeUsesStoreTallies(t *testing.T) {
	base := newMockStorage()
	base.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}
	store := &tallyingStore{mockStorage: base}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080}, authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store: store, router: router, auth: &mockAuthenticator{}, logger: newTestLogger(), cluster: &mockClusterManager{},
	}
	router.Handle("GET", "/api/v1/souls/:id/uptime", server.requireAuth(server.handleSoulUptime))

	rec, got := getSoulUptime(t, server, "/api/v1/souls/s1/uptime?range=24h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if store.listCalls != 0 {
		t.Fatalf("raw judgments listed %d times although the store tallies", store.listCalls)
	}
	if len(store.ranges) != 3+24 {
		t.Fatalf("ranges asked = %d, want 3 windows + 24 hourly buckets", len(store.ranges))
	}
	if w := store.ranges[2]; w.End.Sub(w.Start) < 30*24*time.Hour {
		t.Fatalf("30d window range spans %s", w.End.Sub(w.Start))
	}
	// Windows come from tallies 0..2, buckets from the rest, in order.
	if g := got.Windows["30d"]; g.Checks != 30 || g.Up != 27 || pct(g) != "90.00" || g.AvgLatencyMs != 10 {
		t.Fatalf("30d window = %+v (uptime %s), want the third tally", g, pct(g))
	}
	if p := got.Series.Points[0]; p.Checks != 40 || !p.Start.Equal(store.ranges[3].Start) {
		t.Fatalf("first bucket = %d checks at %s, want the fourth tally at %s", p.Checks, p.Start, store.ranges[3].Start)
	}
}
