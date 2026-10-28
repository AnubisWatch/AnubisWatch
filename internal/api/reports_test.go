package api

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// roleAuthenticator issues one user per token: "role-<role>" in default,
// "other-ws" an admin of another workspace.
type roleAuthenticator struct{ *mockAuthenticator }

func (roleAuthenticator) Authenticate(token string) (*User, error) {
	if token == "other-ws" {
		return &User{ID: "u-other", Role: "admin", Workspace: "ws2"}, nil
	}
	if role, ok := strings.CutPrefix(token, "role-"); ok {
		return &User{ID: "u-" + role, Role: role, Workspace: "default"}, nil
	}
	return nil, http.ErrNoCookie
}

func reportServer(store *mockStorage, alert *mockAlertManager) *RESTServer {
	router := &Router{routes: make(map[string]map[string]Handler)}
	s := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      store,
		router:     router,
		auth:       roleAuthenticator{&mockAuthenticator{}},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	if alert != nil {
		s.alert = alert
	}
	router.Handle("GET", "/api/v1/souls/:id/judgments/export", s.requireRole(s.handleExportJudgments, "judgments:read"))
	router.Handle("GET", "/api/v1/reports/sla", s.requireRole(s.handleSLAReport, "judgments:read"))
	router.Handle("GET", "/api/v1/reports/sla.csv", s.requireRole(s.handleSLAReportCSV, "judgments:read"))
	return s
}

func reportGet(s *RESTServer, token, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// reportFixture: soul "web" in default with a check per minute over the two
// hours [base, base+2h), the first 6 failing; soul "theirs" in ws2.
func reportFixture() (*mockStorage, time.Time) {
	store := newMockStorage()
	base := time.Now().UTC().Truncate(time.Hour).Add(-6 * time.Hour)
	store.souls["web"] = &core.Soul{ID: "web", Name: "=web", Type: core.CheckHTTP, WorkspaceID: "default"}
	store.souls["db"] = &core.Soul{ID: "db", Name: "db", Type: core.CheckTCP, WorkspaceID: "default"}
	store.souls["theirs"] = &core.Soul{ID: "theirs", Name: "theirs", WorkspaceID: "ws2"}
	for i := 0; i < 120; i++ {
		status := core.SoulAlive
		if i < 6 {
			status = core.SoulDead
		}
		for _, soul := range []string{"web", "theirs"} {
			id := fmt.Sprintf("%s-%03d", soul, i)
			store.judgments[id] = &core.Judgment{ID: id, SoulID: soul, Status: status, Duration: time.Duration(10+i) * time.Millisecond,
				Timestamp: base.Add(time.Duration(i) * time.Minute), Message: "ok"}
		}
	}
	return store, base
}

func rangeQuery(from, to time.Time) string {
	return "from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339))
}

func TestSLAReportPerSoulAndWorkspace(t *testing.T) {
	store, base := reportFixture()
	resolved := base.Add(20 * time.Minute)
	alert := &mockAlertManager{incidents: []*core.Incident{
		{ID: "i1", SoulID: "web", WorkspaceID: "default", StartedAt: base.Add(time.Minute), ResolvedAt: &resolved},
		{ID: "i2", SoulID: "theirs", WorkspaceID: "ws2", StartedAt: base.Add(time.Minute)},
	}}
	// A maintenance window over the second hour excludes it.
	store.windows["m"] = &core.MaintenanceWindow{ID: "m", Enabled: true, WorkspaceID: "default", SoulIDs: []string{"web"},
		StartTime: base.Add(time.Hour), EndTime: base.Add(2 * time.Hour)}
	s := reportServer(store, alert)

	rec := reportGet(s, "role-viewer", "/api/v1/reports/sla?"+rangeQuery(base, base.Add(2*time.Hour))+"&target=99")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp SLAResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Souls) != 2 {
		t.Fatalf("souls = %d, want only this workspace's two", len(resp.Souls))
	}
	var web, db = resp.Souls[1], resp.Souls[0] // sorted by name: "=web" sorts first
	if web.SoulID != "web" {
		web, db = db, web
	}
	if web.Checks != 60 || web.Failed != 6 || *web.AvailabilityPercent != 90 || web.DowntimeSeconds != 360 || web.ExcludedSeconds != 3600 {
		t.Fatalf("web = %+v", web)
	}
	if web.Incidents != 1 || web.MTTRSeconds == nil || *web.MTTRSeconds != 1140 {
		t.Fatalf("web incidents = %d mttr %v", web.Incidents, web.MTTRSeconds)
	}
	if web.ErrorBudget == nil || web.ErrorBudget.Met || web.ErrorBudget.AllowedDowntimeSeconds != 36 {
		t.Fatalf("web budget = %+v", web.ErrorBudget)
	}
	if db.Checks != 0 || db.AvailabilityPercent != nil || db.NoDataSeconds != 7200 {
		t.Fatalf("db = %+v", db)
	}
	if resp.Summary.Checks != 60 || resp.Summary.Incidents != 1 || resp.Summary.PeriodSeconds != 14400 {
		t.Fatalf("summary = %+v", resp.Summary)
	}

	// One soul, without a target.
	rec = reportGet(s, "role-viewer", "/api/v1/reports/sla?soul_id=web&"+rangeQuery(base, base.Add(time.Hour)))
	resp = SLAResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || len(resp.Souls) != 1 || resp.Summary.ErrorBudget != nil || resp.Summary.Incidents != 1 {
		t.Fatalf("single soul: %d %+v", rec.Code, resp)
	}
}

func TestSLAReportValidationAndIsolation(t *testing.T) {
	store, base := reportFixture()
	s := reportServer(store, nil)
	cases := []struct {
		name, token, query string
		want               int
	}{
		{"no token", "", "", http.StatusUnauthorized},
		{"role without permissions", "role-guest", "", http.StatusForbidden},
		{"empty role", "role-", "", http.StatusForbidden},
		{"viewer", "role-viewer", "", http.StatusOK},
		{"api key role", "role-api", "", http.StatusOK},
		{"soul of another workspace", "role-admin", "soul_id=theirs", http.StatusNotFound},
		{"other workspace cannot read ours", "other-ws", "soul_id=web", http.StatusNotFound},
		{"bad from", "role-admin", "from=yesterday", http.StatusBadRequest},
		{"inverted", "role-admin", rangeQuery(base, base.Add(-time.Hour)), http.StatusBadRequest},
		{"too long", "role-admin", "from=2000-01-01", http.StatusBadRequest},
		{"target above 100", "role-admin", "target=101", http.StatusBadRequest},
		{"target zero", "role-admin", "target=0", http.StatusBadRequest},
		{"target not a number", "role-admin", "target=abc", http.StatusBadRequest},
		{"date range", "role-admin", "from=" + base.AddDate(0, 0, -3).Format("2006-01-02"), http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/api/v1/reports/sla?", "/api/v1/reports/sla.csv?"} {
				if rec := reportGet(s, tc.token, path+tc.query); rec.Code != tc.want {
					t.Fatalf("GET %s%s = %d, want %d: %s", path, tc.query, rec.Code, tc.want, rec.Body)
				}
			}
		})
	}
}

func TestSLAReportClipsToRetention(t *testing.T) {
	store, _ := reportFixture()
	s := reportServer(store, nil)
	s.SetRetentionPolicy(core.RetentionConfig{Raw: core.Duration{Duration: 24 * time.Hour}, Hour: core.Duration{Duration: 48 * time.Hour}, Day: "72h"})
	rec := reportGet(s, "role-viewer", "/api/v1/reports/sla?from="+time.Now().AddDate(0, 0, -10).Format("2006-01-02"))
	var resp SLAResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || !resp.Clipped || resp.RetentionHorizon == nil || !resp.From.Equal(*resp.RetentionHorizon) ||
		time.Since(resp.From) > 73*time.Hour {
		t.Fatalf("clip: %d %+v", rec.Code, resp)
	}
	s.SetRetentionPolicy(core.RetentionConfig{Day: "unlimited"})
	rec = reportGet(s, "role-viewer", "/api/v1/reports/sla?from="+time.Now().AddDate(0, 0, -10).Format("2006-01-02"))
	resp = SLAResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Clipped || resp.RetentionHorizon != nil {
		t.Fatalf("unlimited retention clipped: %+v", resp)
	}
}

func TestSLAReportCSVIsInjectionSafe(t *testing.T) {
	store, base := reportFixture()
	s := reportServer(store, nil)
	rec := reportGet(s, "role-viewer", "/api/v1/reports/sla.csv?"+rangeQuery(base, base.Add(2*time.Hour))+"&target=99.9")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="anubis-sla-`) || !strings.HasSuffix(cd, `.csv"`) {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "soul_id" || len(rows[0]) != len(slaCSVHeader) {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][1] != "'=web" {
		t.Fatalf("soul name cell = %q, want it neutralised", rows[1][1])
	}
	if last := rows[3]; last[0] != "" || last[1] != "(total)" {
		t.Fatalf("total row = %v", last)
	}
}

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"plain":                  "plain",
		"=HYPERLINK(\"x\")":      "'=HYPERLINK(\"x\")",
		"+1+1":                   "'+1+1",
		"-2":                     "'-2",
		"@SUM(A1)":               "'@SUM(A1)",
		"\t=1":                   "'\t=1",
		"\r=1":                   "'\r=1",
		"a=b":                    "a=b",
		"connection refused: =x": "connection refused: =x",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExportJudgments(t *testing.T) {
	store, base := reportFixture()
	store.judgments["web-000"].Message = "=cmd|' /C calc'!A0"
	store.judgments["web-001"].Message = "@evil"
	s := reportServer(store, nil)

	t.Run("csv", func(t *testing.T) {
		rec := reportGet(s, "role-viewer", "/api/v1/souls/web/judgments/export?"+rangeQuery(base, base.Add(time.Hour)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		cd := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(cd, `attachment; filename="anubis-judgments-web-`) || !strings.HasSuffix(cd, `.csv"`) {
			t.Fatalf("Content-Disposition = %q", cd)
		}
		rows, err := csv.NewReader(rec.Body).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 61 || strings.Join(rows[0], ",") != strings.Join(exportCSVHeader, ",") {
			t.Fatalf("%d rows, header %v", len(rows), rows[0])
		}
		if rows[1][8] != "'=cmd|' /C calc'!A0" || rows[2][8] != "'@evil" || rows[3][8] != "ok" {
			t.Fatalf("messages not neutralised: %q %q %q", rows[1][8], rows[2][8], rows[3][8])
		}
		if rows[1][3] != "dead" || rows[1][4] != "10" {
			t.Fatalf("first row = %v", rows[1])
		}
		for i := 2; i < len(rows); i++ {
			if rows[i][0] <= rows[i-1][0] {
				t.Fatalf("rows not oldest first at %d", i)
			}
		}
		if rec.Result().Trailer.Get("X-Export-Truncated") != "false" || rec.Result().Trailer.Get("X-Export-Rows") != "60" {
			t.Fatalf("trailers = %v", rec.Result().Trailer)
		}
	})

	t.Run("ndjson limited", func(t *testing.T) {
		rec := reportGet(s, "role-viewer", "/api/v1/souls/web/judgments/export?format=ndjson&limit=25&"+rangeQuery(base, base.Add(2*time.Hour)))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-ndjson" {
			t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
		}
		sc := bufio.NewScanner(rec.Body)
		lines := 0
		for sc.Scan() {
			var r exportRecord
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil || r.SoulID != "web" {
				t.Fatalf("line %d: %v %+v", lines, err, r)
			}
			lines++
		}
		if lines != 25 || rec.Result().Trailer.Get("X-Export-Truncated") != "true" {
			t.Fatalf("lines = %d truncated = %q", lines, rec.Result().Trailer.Get("X-Export-Truncated"))
		}
	})

	t.Run("clipped to raw retention", func(t *testing.T) {
		s := reportServer(store, nil)
		s.SetRetentionPolicy(core.RetentionConfig{Raw: core.Duration{Duration: time.Since(base.Add(90 * time.Minute))}})
		rec := reportGet(s, "role-viewer", "/api/v1/souls/web/judgments/export?"+rangeQuery(base, base.Add(2*time.Hour)))
		rows, _ := csv.NewReader(rec.Body).ReadAll()
		if rec.Header().Get("X-Export-Clipped") != "true" || len(rows) > 31 || len(rows) < 29 {
			t.Fatalf("clipped=%q rows=%d", rec.Header().Get("X-Export-Clipped"), len(rows))
		}
	})

	cases := []struct {
		name, token, path string
		want              int
	}{
		{"no token", "", "/api/v1/souls/web/judgments/export", http.StatusUnauthorized},
		{"role without judgments:read", "role-guest", "/api/v1/souls/web/judgments/export", http.StatusForbidden},
		{"other workspace's soul", "role-admin", "/api/v1/souls/theirs/judgments/export", http.StatusNotFound},
		{"our soul from another workspace", "other-ws", "/api/v1/souls/web/judgments/export", http.StatusNotFound},
		{"missing soul", "role-admin", "/api/v1/souls/nope/judgments/export", http.StatusNotFound},
		{"bad format", "role-admin", "/api/v1/souls/web/judgments/export?format=xlsx", http.StatusBadRequest},
		{"zero limit", "role-admin", "/api/v1/souls/web/judgments/export?limit=0", http.StatusBadRequest},
		{"huge limit", "role-admin", "/api/v1/souls/web/judgments/export?limit=999999999", http.StatusBadRequest},
		{"bad to", "role-admin", "/api/v1/souls/web/judgments/export?to=soon", http.StatusBadRequest},
		{"editor", "role-editor", "/api/v1/souls/web/judgments/export", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := reportGet(s, tc.token, tc.path); rec.Code != tc.want {
				t.Fatalf("GET %s as %q = %d, want %d", tc.path, tc.token, rec.Code, tc.want)
			}
		})
	}
}

func TestAttachmentFilenameIsSanitised(t *testing.T) {
	rec := httptest.NewRecorder()
	attachment(rec, "a\"b\r\nc/../d.csv")
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="a_b_c_.._d.csv"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	rec = httptest.NewRecorder()
	attachment(rec, "///")
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="export"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}
