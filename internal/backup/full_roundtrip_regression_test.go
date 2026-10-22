package backup

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func openRoundtripStore(t *testing.T, dir string) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(core.StorageConfig{Path: dir}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return db
}

// Backups used to cover only souls, channels, rules, status pages, journeys
// and a few config keys. Dashboards, maintenance windows, incidents, user
// accounts and (despite --include-history) judgments never came back.
func TestBackupRestoreRoundTripsEverything(t *testing.T) {
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	srcDir := t.TempDir()
	src := openRoundtripStore(t, srcDir)

	soul := &core.Soul{ID: "s1", WorkspaceID: "default", Name: "API", Type: core.CheckHTTP, Target: "https://example.com", Enabled: true}
	if err := src.SaveSoul(ctx, soul); err != nil {
		t.Fatal(err)
	}
	if err := src.SaveJudgment(ctx, &core.Judgment{ID: "j1", SoulID: "s1", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{
		"default/dashboards/d1":        `{"id":"d1"}`,
		"default/maintenance/m1":       `{"id":"m1"}`,
		"default/alerts/incidents/i1":  `{"id":"i1"}`,
		"default/ts/s1/1day/1":         `{"n":1}`,
		"raft/stable/term":             `7`,
		"system/jackals/other-node-id": `{}`,
	}
	for k, v := range extra {
		if err := src.Put(k, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	sessions := `{"tokens":{"live-token":{"user_id":"u1"}},"users":{"admin@x":{"id":"u1","email":"admin@x"}},"admin_password_hash":"$2a$12$hash"}`
	if err := os.WriteFile(filepath.Join(srcDir, "sessions.json"), []byte(sessions), 0600); err != nil {
		t.Fatal(err)
	}

	m := NewManager(src, srcDir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	b, path, err := m.Create(ctx, Options{IncludeJudgments: true, JudgmentDays: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Version != backupVersionRecords {
		t.Fatalf("backup version = %q, want %q", b.Version, backupVersionRecords)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	dst := openRoundtripStore(t, dstDir)
	rm := NewManager(nil, dstDir, newTestLogger())
	if err := rm.Restore(ctx, dst, path, DefaultRestoreOptions()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	dst = openRoundtripStore(t, dstDir)
	defer dst.Close()
	for _, k := range []string{"default/dashboards/d1", "default/maintenance/m1", "default/alerts/incidents/i1", "default/ts/s1/1day/1"} {
		if _, err := dst.Get(k); err != nil {
			t.Errorf("record %s missing after restore: %v", k, err)
		}
	}
	for _, k := range []string{"raft/stable/term", "system/jackals/other-node-id"} {
		if _, err := dst.Get(k); err == nil {
			t.Errorf("node-local record %s was restored onto another node", k)
		}
	}
	if got, err := dst.GetSoulNoCtx("s1"); err != nil || got.Name != "API" {
		t.Errorf("soul after restore = %v, %v", got, err)
	}
	js, err := dst.ListJudgments(ctx, "s1", time.Now().Add(-24*time.Hour), time.Now(), 0)
	if err != nil || len(js) != 1 {
		t.Errorf("judgments after restore = %d, %v; want 1", len(js), err)
	}

	data, err := os.ReadFile(filepath.Join(dstDir, "sessions.json"))
	if err != nil {
		t.Fatalf("sessions.json not restored: %v", err)
	}
	if strings.Contains(string(data), "live-token") {
		t.Error("restored sessions.json carries live login tokens")
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil || state["users"] == nil || state["admin_password_hash"] == nil {
		t.Errorf("restored sessions.json = %s, want users and admin hash", data)
	}
}

func TestBackupWithoutHistorySkipsHistoryRecords(t *testing.T) {
	dir := t.TempDir()
	src := openRoundtripStore(t, dir)
	defer src.Close()
	for _, k := range []string{"default/ts/s1/1day/1", "default/journey-runs/j/1", "default/alerts/events/s1/1/e", "default/dashboards/d1"} {
		if err := src.Put(k, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(src, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	b, _, err := m.Create(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Data.Records) != 1 || b.Data.Records["default/dashboards/d1"] == nil {
		t.Fatalf("records = %v, want only the dashboard", keysOf(b.Data.Records))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
