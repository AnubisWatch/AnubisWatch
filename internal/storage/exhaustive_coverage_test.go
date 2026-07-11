package storage

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type failReader struct{ after int }

func (r *failReader) Read(p []byte) (int, error) {
	if r.after <= 0 {
		return 0, errors.New("injected random failure")
	}
	n := len(p)
	if n > r.after {
		n = r.after
	}
	for i := 0; i < n; i++ {
		p[i] = byte(i)
	}
	r.after -= n
	if n < len(p) {
		return n, errors.New("injected random failure")
	}
	return n, nil
}

func requireError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
}

func putRaw(t *testing.T, db *CobaltDB, key string, value []byte) {
	t.Helper()
	if err := db.Put(key, value); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func TestEncryptionDefensivePaths(t *testing.T) {
	if _, err := newEncryptor(""); err == nil {
		t.Fatal("empty key accepted")
	}
	e, err := newEncryptor("key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.buildCipher([]byte("bad")); err == nil {
		t.Fatal("invalid AES key accepted")
	}
	for _, data := range [][]byte{nil, make([]byte, encryptionSaltLen), make([]byte, encryptionSaltLen+12)} {
		if _, err := e.decrypt(data); err == nil {
			t.Fatalf("decrypt(%d bytes) unexpectedly succeeded", len(data))
		}
	}
	old := crand.Reader
	t.Cleanup(func() { crand.Reader = old })
	crand.Reader = &failReader{}
	if _, err := e.encrypt([]byte("x")); err == nil || !strings.Contains(err.Error(), "salt") {
		t.Fatalf("salt failure = %v", err)
	}
	crand.Reader = &failReader{after: encryptionSaltLen}
	if _, err := e.encrypt([]byte("x")); err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("nonce failure = %v", err)
	}
}

func TestNewEngineFilesystemFailuresAndNilLogger(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEngine(core.StorageConfig{Path: filepath.Join(file, "child")}, nil); err == nil {
		t.Fatal("MkdirAll failure not returned")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "wal.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEngine(core.StorageConfig{Path: dir}, nil); err == nil {
		t.Fatal("WAL open failure not returned")
	}
	db, err := NewEngine(core.StorageConfig{Path: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	db.data.mu.Lock()
	db.data.root = nil
	db.data.mu.Unlock()
	requireError(t, db.Ping())
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	requireError(t, db.Ping())
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClosedDatabaseAllPublicErrorPaths(t *testing.T) {
	db := newTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requireError(t, db.Put("x", []byte("x")))
	requireError(t, db.Delete("x"))
	requireError(t, db.Checkpoint())
	requireError(t, db.DeletePrefix("x"))
	_, err := db.Get("x")
	requireError(t, err)
	_, err = db.List("x")
	requireError(t, err)
	_, err = db.PrefixScan("x")
	requireError(t, err)
	_, err = db.RangeScan("a", "z")
	requireError(t, err)
	_, err = db.GetVerdict(ctx, "", "x")
	requireError(t, err)
	_, err = db.ListVerdicts(ctx, "", "", 0)
	requireError(t, err)
	_, err = db.GetActiveVerdicts(ctx, "", "x")
	requireError(t, err)
	_, err = db.GetJourney(ctx, "", "x")
	requireError(t, err)
	_, err = db.ListJourneys(ctx, "")
	requireError(t, err)
	requireError(t, db.DeleteJourney(ctx, "", "x"))
	_, err = db.QueryJourneyRuns(ctx, "", "x", 0)
	requireError(t, err)
	_, err = db.GetJourneyRun(ctx, "", "x", "x")
	requireError(t, err)
	_, err = db.GetChannel(ctx, "x")
	requireError(t, err)
	_, err = db.ListChannels(ctx, "")
	requireError(t, err)
	_, err = db.ListJudgments(ctx, "", time.Time{}, time.Now(), 0)
	requireError(t, err)
	_, err = db.GetRule(ctx, "x")
	requireError(t, err)
	_, err = db.ListRules(ctx, "")
	requireError(t, err)
	_, err = db.ListJackals(ctx)
	requireError(t, err)
	_, _, err = db.GetRaftState(ctx)
	requireError(t, err)
	_, _, err = db.GetRaftLogEntry(ctx, 1)
	requireError(t, err)
	_, err = db.GetAlertChannel("x", "")
	requireError(t, err)
	_, err = db.ListAlertChannels("")
	requireError(t, err)
	requireError(t, db.DeleteAlertChannel("x", ""))
	_, err = db.GetAlertRule("x", "")
	requireError(t, err)
	_, err = db.ListAlertRules("")
	requireError(t, err)
	requireError(t, db.DeleteAlertRule("x", ""))
	_, err = db.ListAlertEvents("x", 0)
	requireError(t, err)
	_, err = db.ListActiveIncidents()
	requireError(t, err)
	_, err = db.GetStatusPageByDomain("x")
	requireError(t, err)
	_, err = db.GetStatusPageBySlug("x")
	requireError(t, err)
	_, err = db.ListStatusPages()
	requireError(t, err)
	_, err = db.GetSubscriptionsByPage("x")
	requireError(t, err)
	_, err = db.GetUptimeHistory("x", 1)
	requireError(t, err)
	_, err = db.ListDashboards()
	requireError(t, err)
	_, err = db.ListMaintenanceWindows()
	requireError(t, err)
	_, err = db.GetSoul(ctx, "", "x")
	requireError(t, err)
	_, err = db.ListSouls(ctx, "", 0, 1)
	requireError(t, err)
	_, err = db.GetWorkspace(ctx, "x")
	requireError(t, err)
	_, err = db.ListWorkspaces(ctx)
	requireError(t, err)
	_, err = db.GetSoulJudgments("x", 1)
	requireError(t, err)
}

func TestMalformedStoredJSONPaths(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()
	bad := []byte("{")
	keys := []string{
		"default/verdicts/bad", "default/journeys/bad", "default/journey-runs/j/1",
		"default/channels/bad", "default/judgments/s/1", "default/rules/bad",
		"system/jackals/bad", "default/alerts/channels/bad", "default/alerts/rules/bad",
		"default/alerts/events/s/1/bad", "default/alerts/incidents/bad",
		"default/statuspages/bad", "default/statuspages/subscriptions/bad",
		"default/dashboards/bad", "default/maintenance/bad", "default/souls/bad",
		"workspaces/bad", "default/ts/s/1min/1", "raft/snapshot-meta", "raft/state", "raft/log/99",
	}
	for _, key := range keys {
		putRaw(t, db, key, bad)
	}
	db.mu.Lock()
	db.journeyIndex["bad"] = "default"
	db.soulIndex["bad"] = "default"
	db.incidentIndex["bad"] = "default"
	db.statusPageIndex["bad"] = "default"
	db.dashboardIndex["bad"] = "default"
	db.maintenanceIndex["bad"] = "default"
	db.workspaceIndex["default"] = struct{}{}
	db.workspaceOrder = []string{"default"}
	db.mu.Unlock()
	_, err := db.GetVerdict(ctx, "", "bad")
	requireError(t, err)
	_, err = db.GetJourney(ctx, "", "bad")
	requireError(t, err)
	_, err = db.GetJourneyNoCtx("bad")
	requireError(t, err)
	_, err = db.GetJourneyRun(ctx, "", "j", "none")
	requireError(t, err)
	_, err = db.GetChannel(ctx, "bad")
	requireError(t, err)
	_, err = db.GetRule(ctx, "bad")
	requireError(t, err)
	_, err = db.GetAlertChannel("bad", "")
	requireError(t, err)
	_, err = db.GetAlertRule("bad", "")
	requireError(t, err)
	_, err = db.GetIncident("bad")
	requireError(t, err)
	_, err = db.GetStatusPage("bad")
	requireError(t, err)
	_, err = db.GetDashboard("bad")
	requireError(t, err)
	_, err = db.GetMaintenanceWindow("bad")
	requireError(t, err)
	_, err = db.GetSoul(ctx, "", "bad")
	requireError(t, err)
	_, err = db.GetSoulNoCtx("bad")
	requireError(t, err)
	_, err = db.GetWorkspace(ctx, "bad")
	requireError(t, err)
	_, _, err = db.GetRaftState(ctx)
	requireError(t, err)
	_, _, err = db.GetRaftLogEntry(ctx, 99)
	requireError(t, err)
	store := NewCobaltDBLogStore(db)
	var log core.RaftLogEntry
	requireError(t, store.GetLog(99, &log))
	snaps := NewCobaltDBSnapshotStore(db)
	_, err = snaps.List()
	requireError(t, err)
	// List methods skip malformed records rather than failing the entire page.
	_, _ = db.ListVerdicts(ctx, "", "", 0)
	_, _ = db.GetActiveVerdicts(ctx, "", "s")
	_, _ = db.ListJourneys(ctx, "")
	_, _ = db.QueryJourneyRuns(ctx, "", "j", 0)
	_, _ = db.ListChannels(ctx, "")
	_, _ = db.ListJudgments(ctx, "", time.Time{}, time.Now().Add(time.Hour), 0)
	_, _ = db.ListRules(ctx, "")
	_, _ = db.ListJackals(ctx)
	_, _ = db.ListAlertChannels("")
	_, _ = db.ListAlertRules("")
	_, _ = db.ListAlertEvents("s", 0)
	_, _ = db.ListActiveIncidents()
	_, _ = db.ListStatusPages()
	_, _ = db.GetSubscriptionsByPage("x")
	_, _ = db.GetUptimeHistory("s", 1)
	_, _ = db.ListDashboards()
	_, _ = db.ListMaintenanceWindows()
	_, _ = db.ListSouls(ctx, "", 0, 0)
	_, _ = db.ListWorkspaces(ctx)
	_, _ = db.GetSoulJudgments("s", 0)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	_, _ = ts.QuerySummaries(ctx, "", "s", Resolution1Min, time.Unix(0, 0), time.Now())
	_ = ts.updateSummary(ctx, &core.Judgment{SoulID: "s", Timestamp: time.Unix(1, 0), Status: core.SoulAlive}, Resolution1Min)
	_ = ts.aggregateAndSave("default", "s", Resolution1Min, time.Unix(1, 0), []*JudgmentSummary{{Count: 1, SuccessCount: 1}})
}

func TestManualTreeDefensiveTraversal(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	leaf := &btreeNode{isLeaf: true, keys: []string{"m"}, values: [][]byte{[]byte("v")}}
	db.data.mu.Lock()
	db.data.root = &btreeNode{keys: []string{"a"}, children: nil}
	db.data.mu.Unlock()
	all, err := db.data.scanAllNodes()
	if err != nil || len(all) != 0 {
		t.Fatalf("scan=%v,%v", all, err)
	}
	_, err = db.Get("z")
	requireError(t, err)
	keys, err := db.List("z")
	if err != nil || len(keys) != 0 {
		t.Fatalf("List=%v,%v", keys, err)
	}
	vals, err := db.PrefixScan("z")
	if err != nil || len(vals) != 0 {
		t.Fatalf("PrefixScan=%v,%v", vals, err)
	}
	vals, err = db.RangeScan("z", "zz")
	if err != nil || len(vals) != 0 {
		t.Fatalf("RangeScan=%v,%v", vals, err)
	}
	db.data.mu.Lock()
	db.data.root = nil
	db.data.mu.Unlock()
	keys, _ = db.List("x")
	vals, _ = db.PrefixScan("x")
	rangeVals, _ := db.RangeScan("a", "z")
	if len(keys)+len(vals)+len(rangeVals) != 0 {
		t.Fatal("nil root returned data")
	}
	db.data.mu.Lock()
	db.data.root = &btreeNode{keys: []string{"a"}, children: []*btreeNode{leaf}}
	db.data.mu.Unlock()
	_, _ = db.List("z")
	_, _ = db.PrefixScan("z")
	_, _ = db.RangeScan("z", "zz")
	db.data.mu.Lock()
	db.data.root = &btreeNode{keys: []string{"z"}, children: []*btreeNode{leaf}}
	db.data.mu.Unlock()
	_, _ = db.List("a")
	_, _ = db.PrefixScan("a")
	_, _ = db.RangeScan("a", "z")
}

func TestWALFaultInjection(t *testing.T) {
	dir := t.TempDir()
	w, err := newWAL(filepath.Join(dir, "wal"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}
	requireError(t, w.Append("x", []byte("x")))
	requireError(t, w.AppendDelete("x"))
	requireError(t, w.Truncate())
	w2, err := newWAL(filepath.Join(dir, "wal2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w2.path+".compact", 0o700); err != nil {
		t.Fatal(err)
	}
	requireError(t, w2.rewrite(nil))
	_ = w2.Close()

	db := newTestDB(t)
	defer db.Close()
	_ = db.wal.file.Close()
	requireError(t, db.Put("x", []byte("x")))
	requireError(t, db.Delete("x"))
}

func TestCheckpointBranchesAndMaybeCheckpoint(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	db.data.mu.Lock()
	db.data.root = &btreeNode{keys: []string{"x"}, children: nil}
	db.data.mu.Unlock()
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	// Restore a valid leaf after exercising the childless-internal-node guard.
	db.data.mu.Lock()
	db.data.root = &btreeNode{isLeaf: true}
	db.data.mu.Unlock()
	putRaw(t, db, "live", []byte("v"))
	db.checkpointBaseMu.Lock()
	db.checkpointBaseSz = minWALCheckpointBytes
	db.checkpointBaseMu.Unlock()
	db.maybeCheckpoint()
	db.wal.mu.Lock()
	db.wal.size = 2*minWALCheckpointBytes + 1
	db.wal.mu.Unlock()
	db.maybeCheckpoint()
}

func TestNoCtxJourneyAndIndexRebuildEdges(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	if _, err := db.GetJourneyNoCtx("missing"); err == nil {
		t.Fatal("expected not found")
	}
	if err := db.DeleteJourneyNoCtx("missing"); err == nil {
		t.Fatal("expected not found")
	}
	j := &core.JourneyConfig{ID: "j", WorkspaceID: "ws"}
	if err := db.SaveJourneyNoCtx(j); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListJourneysNoCtx("ws", -1, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v,%v", list, err)
	}
	list, err = db.ListJourneysNoCtx("ws", 10, 1)
	if err != nil || len(list) != 0 {
		t.Fatalf("list=%v,%v", list, err)
	}
	if err := db.DeleteJourneyNoCtx("j"); err != nil {
		t.Fatal(err)
	}

	entries := map[string][]byte{
		"orphan": []byte("x"), "ws/type": []byte("x"),
		"ws/souls/s": []byte("x"), "ws/judgment-idx/j": []byte("x"),
		"ws/channels/c": []byte("x"), "ws/rules/r": []byte("x"), "ws/journeys/j": []byte("x"),
		"ws/alerts/channels/ac": []byte("x"), "ws/alerts/rules/ar": []byte("x"),
		"ws/alerts/incidents/i": []byte("x"), "ws/alerts/events/s/1/e": []byte("x"),
		"ws/alerts/events/short": []byte("x"), "ws/statuspages/p": []byte("x"),
		"ws/statuspages/subscriptions/sub": []byte("x"), "ws/dashboards/d": []byte("x"),
		"ws/maintenance/m": []byte("x"),
	}
	for k, v := range entries {
		putRaw(t, db, k, v)
	}
	if err := db.rebuildSecondaryIndexes(); err != nil {
		t.Fatal(err)
	}
	db.mu.Lock()
	db.recordWorkspaceLocked("")
	db.recordWorkspaceLocked("ws")
	db.mu.Unlock()
}

func TestRaftSnapshotAndStoreErrorPaths(t *testing.T) {
	db := newTestDB(t)
	store := NewCobaltDBLogStore(db)
	if first, err := store.FirstIndex(); err != nil || first != 0 {
		t.Fatalf("first=%d,%v", first, err)
	}
	if last, err := store.LastIndex(); err != nil || last != 0 {
		t.Fatalf("last=%d,%v", last, err)
	}
	putRaw(t, db, "raft/log/not-a-number", []byte(`{}`))
	_, _ = store.FirstIndex()
	_, _ = store.LastIndex()
	putRaw(t, db, "raft/log/7", []byte(`{"term":2,"type":1,"data":"%%%"}`))
	var log core.RaftLogEntry
	if err := store.GetLog(7, &log); err != nil {
		t.Fatal(err)
	}
	snaps := NewCobaltDBSnapshotStore(db)
	sink, err := snaps.Create(1, 2, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if sink.ID() == "" {
		t.Fatal("missing ID")
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("x")); err == nil {
		t.Fatal("write to closed sink succeeded")
	}
	source, err := snaps.Open("ignored")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(source)
	if err != nil || string(got) != "abc" {
		t.Fatalf("read=%q,%v", got, err)
	}
	_ = source.Close()
	sink2, _ := snaps.Create(1, 1, 1, nil)
	_ = sink2.Cancel()
	if _, err := sink2.Write(nil); err == nil {
		t.Fatal("cancelled sink writable")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = store.FirstIndex()
	requireError(t, err)
	_, err = store.LastIndex()
	requireError(t, err)
	requireError(t, store.StoreLogs([]core.RaftLogEntry{{Index: 1}}))
	requireError(t, store.DeleteRange(1, 1))
	sink3, _ := snaps.Create(1, 1, 1, nil)
	_, _ = sink3.Write([]byte("x"))
	requireError(t, sink3.Close())
}

func TestTimeSeriesAdditionalEdges(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	ts.StopCompaction()
	ts.StartCompaction()
	ts.StartCompaction()
	ts.StopCompaction()
	if got := weightedPercentile([]weightedLatency{{value: 1, count: 1}}, 2, 1); got != 1 {
		t.Fatalf("fallback=%v", got)
	}
	ctx := context.Background()
	putRaw(t, db, "default/ts/s/1min/1", []byte("null"))
	purity, err := ts.GetPurityFromSummaries(ctx, "", "s", 100000*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_ = purity
	if err := ts.compactToResolution(ResolutionRaw, Resolution1Min, 0); err != nil {
		t.Fatal(err)
	}
	putRaw(t, db, "default/ts/s/raw/bad", []byte(`{}`))
	putRaw(t, db, "default/ts/s/raw/1", []byte("{"))
	putRaw(t, db, "default/ts/s/raw/2", []byte(`{"count":1,"success_count":1}`))
	if err := ts.compactToResolution(ResolutionRaw, Resolution1Min, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	// A zero-count source drives the no-weight branch; the resulting NaN uptime
	// is intentionally rejected by encoding/json and is the observable error.
	if err := ts.aggregateAndSave("default", "zero", Resolution1Min, time.Unix(0, 0), []*JudgmentSummary{{}}); err == nil {
		t.Fatal("expected zero-count aggregate marshal error")
	}
}

func TestRetentionAdditionalEdges(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	rm := NewRetentionManager(db, core.RetentionConfig{}, t.TempDir(), newTestLogger())
	old := time.Now().Add(-48 * time.Hour)
	putRaw(t, db, "default/judgments/bad", []byte("x"))
	putRaw(t, db, "default/judgments/s/not-time", []byte("x"))
	putRaw(t, db, "default/judgments/s/"+strconv.FormatInt(old.UnixNano(), 10), []byte("{"))
	putRaw(t, db, "default/ts/short", []byte("x"))
	putRaw(t, db, "default/ts/s/1min/not-time", []byte("x"))
	putRaw(t, db, "default/ts/s/5min/1", []byte("x"))
	if err := rm.purgeRawData(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := rm.purgeSummaries("1min", time.Now()); err != nil {
		t.Fatal(err)
	}
	stats, err := rm.GetStorageStats(context.Background())
	if err != nil || stats == nil {
		t.Fatalf("stats=%v,%v", stats, err)
	}
	badRM := NewRetentionManager(db, core.RetentionConfig{}, filepath.Join(t.TempDir(), "missing"), newTestLogger())
	if _, err := badRM.getDiskUsage(); err == nil {
		t.Fatal("missing path accepted")
	}
	if _, err := badRM.GetStorageStats(context.Background()); err != nil {
		t.Fatal(err)
	}
	rm.Start()
	rm.Stop()
}

func TestWriteEntryAndDecryptCorruption(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wal")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := writeEntryLocked(f, walEntry{Key: "x"}); err == nil {
		t.Fatal("closed file write succeeded")
	}
	db, err := NewEngine(core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: true, Key: "key"}}, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.data.mu.Lock()
	_ = db.data.insert("bad", bytes.Repeat([]byte{1}, 64))
	db.data.mu.Unlock()
	_, err = db.Get("bad")
	requireError(t, err)
}
