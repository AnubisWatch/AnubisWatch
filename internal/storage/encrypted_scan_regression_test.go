package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const encryptedScanTestKey = "encrypted-scan-regression-key-32-bytes"

func newEncryptedScanTestDB(t *testing.T) *CobaltDB {
	t.Helper()

	db, err := NewEngine(core.StorageConfig{
		Path: t.TempDir(),
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     encryptedScanTestKey,
		},
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func mustPutJSON(t *testing.T, db *CobaltDB, key string, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", key, err)
	}
	if err := db.Put(key, data); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
}

func TestEncryptedScansReturnPlaintext(t *testing.T) {
	db := newEncryptedScanTestDB(t)

	values := map[string]string{
		"scan/a": "alpha",
		"scan/b": "bravo",
		"scan/c": "charlie",
	}
	for key, value := range values {
		if err := db.Put(key, []byte(value)); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}

	prefixResults, err := db.PrefixScan("scan/")
	if err != nil {
		t.Fatalf("PrefixScan: %v", err)
	}
	if len(prefixResults) != len(values) {
		t.Fatalf("PrefixScan returned %d values, want %d", len(prefixResults), len(values))
	}
	for key, want := range values {
		if got := string(prefixResults[key]); got != want {
			t.Errorf("PrefixScan[%q] = %q, want %q", key, got, want)
		}
	}

	rangeResults, err := db.RangeScan("scan/a", "scan/c")
	if err != nil {
		t.Fatalf("RangeScan: %v", err)
	}
	if len(rangeResults) != 2 {
		t.Fatalf("RangeScan returned %d values, want 2", len(rangeResults))
	}
	for _, key := range []string{"scan/a", "scan/b"} {
		if got := string(rangeResults[key]); got != values[key] {
			t.Errorf("RangeScan[%q] = %q, want %q", key, got, values[key])
		}
	}
}

func TestEncryptedScansFailClosedOnCorruptCiphertext(t *testing.T) {
	newCorruptDB := func(t *testing.T) *CobaltDB {
		t.Helper()
		db := newEncryptedScanTestDB(t)
		if err := db.Put("scan/a-good", []byte("good")); err != nil {
			t.Fatalf("Put good value: %v", err)
		}
		ciphertext, err := db.encryptor.encrypt([]byte("corrupt me"))
		if err != nil {
			t.Fatalf("encrypt corrupt fixture: %v", err)
		}
		ciphertext[len(ciphertext)-1] ^= 0xff
		db.data.mu.Lock()
		err = db.data.insert("scan/b-corrupt", ciphertext)
		db.data.mu.Unlock()
		if err != nil {
			t.Fatalf("insert corrupt fixture: %v", err)
		}
		return db
	}

	t.Run("prefix", func(t *testing.T) {
		db := newCorruptDB(t)
		results, err := db.PrefixScan("scan/")
		if err == nil {
			t.Fatal("PrefixScan accepted corrupt ciphertext")
		}
		if results != nil {
			t.Fatalf("PrefixScan returned a partial result: %#v", results)
		}
		if !strings.Contains(err.Error(), "scan/b-corrupt") {
			t.Fatalf("PrefixScan error %q does not identify the corrupt key", err)
		}
	})

	t.Run("range", func(t *testing.T) {
		db := newCorruptDB(t)
		results, err := db.RangeScan("scan/", "scan0")
		if err == nil {
			t.Fatal("RangeScan accepted corrupt ciphertext")
		}
		if results != nil {
			t.Fatalf("RangeScan returned a partial result: %#v", results)
		}
		if !strings.Contains(err.Error(), "scan/b-corrupt") {
			t.Fatalf("RangeScan error %q does not identify the corrupt key", err)
		}
	})
}

func TestEncryptedDecodedScanConsumers(t *testing.T) {
	db := newEncryptedScanTestDB(t)
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	now := time.Now().UTC().Truncate(time.Second)

	workspace := &core.Workspace{ID: "workspace-1", Name: "Workspace", Slug: "workspace", Status: core.WorkspaceActive}
	if err := db.SaveWorkspace(ctx, workspace); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}
	soul := &core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "API", Type: core.CheckHTTP, Target: "https://example.com", Enabled: true}
	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}
	judgment := &core.Judgment{ID: "judgment-1", SoulID: soul.ID, WorkspaceID: "default", Status: core.SoulAlive, Timestamp: now, Duration: 25 * time.Millisecond}
	if err := db.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
	verdict := &core.Verdict{ID: "verdict-1", WorkspaceID: "default", SoulID: soul.ID, Status: core.VerdictActive, Severity: core.SeverityWarning, FiredAt: now}
	if err := db.SaveVerdict(ctx, verdict); err != nil {
		t.Fatalf("SaveVerdict: %v", err)
	}
	journey := &core.JourneyConfig{ID: "journey-1", WorkspaceID: "default", Name: "Checkout", Enabled: true}
	if err := db.SaveJourney(ctx, journey); err != nil {
		t.Fatalf("SaveJourney: %v", err)
	}
	journeyRun := &core.JourneyRun{ID: "run-1", JourneyID: journey.ID, WorkspaceID: "default", StartedAt: now.UnixMilli(), Status: core.SoulAlive}
	if err := db.SaveJourneyRun(ctx, journeyRun); err != nil {
		t.Fatalf("SaveJourneyRun: %v", err)
	}
	channelConfig := &core.ChannelConfig{Name: "webhook-1", Type: "webhook"}
	if err := db.SaveChannel(ctx, channelConfig); err != nil {
		t.Fatalf("SaveChannel: %v", err)
	}
	ruleConfig := &core.AlertRule{ID: "rule-config-1", Name: "Rule Config", WorkspaceID: "default", Enabled: true}
	if err := db.SaveRule(ctx, ruleConfig); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	if err := db.SaveJackal(ctx, "jackal-1", "127.0.0.1:7946", "local"); err != nil {
		t.Fatalf("SaveJackal: %v", err)
	}
	alertChannel := &core.AlertChannel{ID: "alert-channel-1", Name: "Webhook", Type: core.ChannelWebHook, Enabled: true, WorkspaceID: "default"}
	if err := db.SaveAlertChannel(alertChannel); err != nil {
		t.Fatalf("SaveAlertChannel: %v", err)
	}
	alertRule := &core.AlertRule{ID: "alert-rule-1", Name: "Failures", WorkspaceID: "default", Enabled: true}
	if err := db.SaveAlertRule(alertRule); err != nil {
		t.Fatalf("SaveAlertRule: %v", err)
	}
	alertEvent := &core.AlertEvent{ID: "alert-event-1", SoulID: soul.ID, SoulName: soul.Name, WorkspaceID: "default", Status: core.SoulDead, Severity: core.SeverityCritical, Timestamp: now}
	if err := db.SaveAlertEvent(alertEvent); err != nil {
		t.Fatalf("SaveAlertEvent: %v", err)
	}
	incident := &core.Incident{ID: "incident-1", SoulID: soul.ID, SoulName: soul.Name, WorkspaceID: "default", Status: core.IncidentOpen, Severity: core.SeverityCritical, StartedAt: now}
	if err := db.SaveIncident(incident); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}
	page := &core.StatusPage{ID: "status-page-1", WorkspaceID: "default", Name: "Status", Slug: "status", CustomDomain: "status.example.com", Enabled: true}
	if err := db.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage: %v", err)
	}
	subscription := &core.StatusPageSubscription{ID: "subscription-1", PageID: page.ID, Email: "operator@example.com", Type: "email", Confirmed: true, SubscribedAt: now}
	if err := db.SaveStatusPageSubscription(subscription); err != nil {
		t.Fatalf("SaveStatusPageSubscription: %v", err)
	}
	dashboard := &core.CustomDashboard{ID: "dashboard-1", WorkspaceID: "default", Name: "Operations", RefreshSec: 30}
	if err := db.SaveDashboard(dashboard); err != nil {
		t.Fatalf("SaveDashboard: %v", err)
	}
	maintenance := &core.MaintenanceWindow{ID: "maintenance-1", WorkspaceID: "default", Name: "Upgrade", StartTime: now, EndTime: now.Add(time.Hour), Enabled: true}
	if err := db.SaveMaintenanceWindow(maintenance); err != nil {
		t.Fatalf("SaveMaintenanceWindow: %v", err)
	}

	t.Run("ListSouls", func(t *testing.T) {
		items, err := db.ListSouls(ctx, "default", 0, 10)
		if err != nil || len(items) != 1 || items[0].ID != soul.ID {
			t.Fatalf("ListSouls = %#v, %v", items, err)
		}
	})
	t.Run("ListWorkspaces", func(t *testing.T) {
		items, err := db.ListWorkspaces(ctx)
		if err != nil || len(items) != 1 || items[0].ID != workspace.ID {
			t.Fatalf("ListWorkspaces = %#v, %v", items, err)
		}
	})
	t.Run("ListVerdicts", func(t *testing.T) {
		items, err := db.ListVerdicts(ctx, "default", "", 10)
		if err != nil || len(items) != 1 || items[0].ID != verdict.ID {
			t.Fatalf("ListVerdicts = %#v, %v", items, err)
		}
	})
	t.Run("GetActiveVerdicts", func(t *testing.T) {
		items, err := db.GetActiveVerdicts(ctx, "default", soul.ID)
		if err != nil || len(items) != 1 || items[0].ID != verdict.ID {
			t.Fatalf("GetActiveVerdicts = %#v, %v", items, err)
		}
	})
	t.Run("ListJourneys", func(t *testing.T) {
		items, err := db.ListJourneys(ctx, "default")
		if err != nil || len(items) != 1 || items[0].ID != journey.ID {
			t.Fatalf("ListJourneys = %#v, %v", items, err)
		}
	})
	t.Run("QueryJourneyRuns", func(t *testing.T) {
		items, err := db.QueryJourneyRuns(ctx, "default", journey.ID, 10)
		if err != nil || len(items) != 1 || items[0].ID != journeyRun.ID {
			t.Fatalf("QueryJourneyRuns = %#v, %v", items, err)
		}
	})
	t.Run("GetJourneyRun", func(t *testing.T) {
		item, err := db.GetJourneyRun(ctx, "default", journey.ID, journeyRun.ID)
		if err != nil || item.ID != journeyRun.ID {
			t.Fatalf("GetJourneyRun = %#v, %v", item, err)
		}
	})
	t.Run("ListChannels", func(t *testing.T) {
		items, err := db.ListChannels(ctx, "default")
		if err != nil || len(items) != 1 || items[0].Name != channelConfig.Name {
			t.Fatalf("ListChannels = %#v, %v", items, err)
		}
	})
	t.Run("ListJudgments", func(t *testing.T) {
		items, err := db.ListJudgments(ctx, soul.ID, now.Add(-time.Minute), now.Add(time.Minute), 10)
		if err != nil || len(items) != 1 || items[0].ID != judgment.ID {
			t.Fatalf("ListJudgments = %#v, %v", items, err)
		}
	})
	t.Run("QueryJudgments", func(t *testing.T) {
		items, err := db.QueryJudgments(ctx, "default", soul.ID, now.Add(-time.Minute), now.Add(time.Minute), 10)
		if err != nil || len(items) != 1 || items[0].ID != judgment.ID {
			t.Fatalf("QueryJudgments = %#v, %v", items, err)
		}
	})
	t.Run("GetLatestJudgment", func(t *testing.T) {
		item, err := db.GetLatestJudgment(ctx, "default", soul.ID)
		if err != nil || item.ID != judgment.ID {
			t.Fatalf("GetLatestJudgment = %#v, %v", item, err)
		}
	})
	t.Run("GetSoulJudgments", func(t *testing.T) {
		items, err := db.GetSoulJudgments(soul.ID, 10)
		if err != nil || len(items) != 1 || items[0].ID != judgment.ID {
			t.Fatalf("GetSoulJudgments = %#v, %v", items, err)
		}
	})
	t.Run("ListRules", func(t *testing.T) {
		items, err := db.ListRules(ctx, "default")
		if err != nil || len(items) != 1 || items[0].ID != ruleConfig.ID {
			t.Fatalf("ListRules = %#v, %v", items, err)
		}
	})
	t.Run("ListJackals", func(t *testing.T) {
		items, err := db.ListJackals(ctx)
		if err != nil || items["jackal-1"]["region"] != "local" {
			t.Fatalf("ListJackals = %#v, %v", items, err)
		}
	})
	t.Run("ListAlertChannels", func(t *testing.T) {
		items, err := db.ListAlertChannels("default")
		if err != nil || len(items) != 1 || items[0].ID != alertChannel.ID {
			t.Fatalf("ListAlertChannels = %#v, %v", items, err)
		}
	})
	t.Run("ListAlertRules", func(t *testing.T) {
		items, err := db.ListAlertRules("default")
		if err != nil || len(items) != 1 || items[0].ID != alertRule.ID {
			t.Fatalf("ListAlertRules = %#v, %v", items, err)
		}
	})
	t.Run("ListAlertEvents", func(t *testing.T) {
		items, err := db.ListAlertEvents(soul.ID, 10)
		if err != nil || len(items) != 1 || items[0].ID != alertEvent.ID {
			t.Fatalf("ListAlertEvents = %#v, %v", items, err)
		}
	})
	t.Run("ListActiveIncidents", func(t *testing.T) {
		items, err := db.ListActiveIncidents()
		if err != nil || len(items) != 1 || items[0].ID != incident.ID {
			t.Fatalf("ListActiveIncidents = %#v, %v", items, err)
		}
	})
	t.Run("ListStatusPages", func(t *testing.T) {
		items, err := db.ListStatusPages()
		if err != nil || len(items) != 1 || items[0].ID != page.ID {
			t.Fatalf("ListStatusPages = %#v, %v", items, err)
		}
	})
	t.Run("GetSubscriptionsByPage", func(t *testing.T) {
		items, err := db.GetSubscriptionsByPage(page.ID)
		if err != nil || len(items) != 1 || items[0].ID != subscription.ID {
			t.Fatalf("GetSubscriptionsByPage = %#v, %v", items, err)
		}
	})
	t.Run("GetUptimeHistory", func(t *testing.T) {
		items, err := db.GetUptimeHistory(soul.ID, 1)
		if err != nil || len(items) != 1 || items[0].Uptime != 100 {
			t.Fatalf("GetUptimeHistory = %#v, %v", items, err)
		}
	})
	t.Run("ListDashboards", func(t *testing.T) {
		items, err := db.ListDashboards()
		if err != nil || len(items) != 1 || items[0].ID != dashboard.ID {
			t.Fatalf("ListDashboards = %#v, %v", items, err)
		}
	})
	t.Run("ListMaintenanceWindows", func(t *testing.T) {
		items, err := db.ListMaintenanceWindows()
		if err != nil || len(items) != 1 || items[0].ID != maintenance.ID {
			t.Fatalf("ListMaintenanceWindows = %#v, %v", items, err)
		}
	})
}

func TestEncryptedTimeSeriesScanConsumers(t *testing.T) {
	db := newEncryptedScanTestDB(t)
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	now := time.Now().UTC().Truncate(time.Second)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	judgment := &core.Judgment{ID: "timeseries-judgment", SoulID: "timeseries-soul", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: now, Duration: 20 * time.Millisecond}
	if err := ts.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
	summaries, err := ts.QuerySummaries(ctx, "default", judgment.SoulID, Resolution1Min, now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil || len(summaries) != 1 || summaries[0].SoulID != judgment.SoulID {
		t.Fatalf("QuerySummaries = %#v, %v", summaries, err)
	}

	oldBucket := now.Add(-10 * time.Minute).Truncate(time.Minute)
	source := &JudgmentSummary{
		SoulID:        judgment.SoulID,
		WorkspaceID:   "default",
		Resolution:    string(Resolution1Min),
		BucketTime:    oldBucket,
		Count:         2,
		SuccessCount:  2,
		MinLatency:    10,
		MaxLatency:    20,
		AvgLatency:    15,
		UptimePercent: 100,
	}
	realSourceKey := "default/ts/timeseries-soul/1min/" + formatUnix(oldBucket.Unix())
	mustPutJSON(t, db, realSourceKey, source)

	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Nanosecond); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}
	compacted, err := ts.QuerySummaries(ctx, "default", judgment.SoulID, Resolution5Min, oldBucket.Add(-5*time.Minute), oldBucket.Add(5*time.Minute))
	if err != nil || len(compacted) == 0 {
		t.Fatalf("compacted QuerySummaries = %#v, %v", compacted, err)
	}
}

func TestEncryptedKeyOnlyScanConsumers(t *testing.T) {
	db := newEncryptedScanTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	logStore := NewCobaltDBLogStore(db)
	if err := logStore.StoreLogs([]core.RaftLogEntry{{Index: 3, Term: 1}, {Index: 7, Term: 1}}); err != nil {
		t.Fatalf("StoreLogs: %v", err)
	}
	first, err := logStore.FirstIndex()
	if err != nil || first != 3 {
		t.Fatalf("FirstIndex = %d, %v", first, err)
	}
	last, err := logStore.LastIndex()
	if err != nil || last != 7 {
		t.Fatalf("LastIndex = %d, %v", last, err)
	}

	oldJudgment := &core.Judgment{ID: "old-judgment", SoulID: "retention-soul", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: now.Add(-48 * time.Hour)}
	if err := db.SaveJudgment(context.Background(), oldJudgment); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
	oldSummaryKey := "default/ts/retention-soul/1min/" + formatUnix(now.Add(-48*time.Hour).Unix())
	mustPutJSON(t, db, oldSummaryKey, &JudgmentSummary{SoulID: "retention-soul", WorkspaceID: "default", Resolution: string(Resolution1Min), Count: 1})

	rm := NewRetentionManager(db, core.RetentionConfig{}, t.TempDir(), newTestLogger())
	stats, err := rm.GetStorageStats(context.Background())
	if err != nil || stats.TotalKeys == 0 || stats.TotalSize == 0 {
		t.Fatalf("GetStorageStats = %#v, %v", stats, err)
	}
	if err := rm.purgeRawData(now.Add(-24 * time.Hour)); err != nil {
		t.Fatalf("purgeRawData: %v", err)
	}
	if _, err := db.Get("default/judgments/retention-soul/" + formatUnix(oldJudgment.Timestamp.UnixNano())); err == nil {
		t.Fatal("purgeRawData left the old judgment")
	}
	if err := rm.purgeSummaries("1min", now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("purgeSummaries: %v", err)
	}
	if _, err := db.Get(oldSummaryKey); err == nil {
		t.Fatal("purgeSummaries left the old summary")
	}

	if err := db.ApplyFSMCommand(&core.FSMCommand{Op: core.FSMSet, Key: "default/souls/fsm-1", Value: []byte(`{"id":"fsm-1"}`)}); err != nil {
		t.Fatalf("ApplyFSMCommand set: %v", err)
	}
	if err := db.ApplyFSMCommand(&core.FSMCommand{Op: core.FSMDeletePrefix, Key: "default/souls/fsm"}); err != nil {
		t.Fatalf("ApplyFSMCommand delete prefix: %v", err)
	}
}

func TestEncryptedDecodedConsumerPropagatesScanFailure(t *testing.T) {
	db := newEncryptedScanTestDB(t)
	if err := db.SaveSoul(context.Background(), &core.Soul{ID: "good", WorkspaceID: "default", Name: "Good", Type: core.CheckHTTP}); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}

	ciphertext, err := db.encryptor.encrypt([]byte(`{"id":"bad"}`))
	if err != nil {
		t.Fatalf("encrypt corrupt fixture: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	db.data.mu.Lock()
	err = db.data.insert("default/souls/bad", ciphertext)
	db.data.mu.Unlock()
	if err != nil {
		t.Fatalf("insert corrupt fixture: %v", err)
	}

	items, err := db.ListSouls(context.Background(), "default", 0, 10)
	if err == nil {
		t.Fatalf("ListSouls accepted corrupt ciphertext: %#v", items)
	}
	if items != nil {
		t.Fatalf("ListSouls returned partial results: %#v", items)
	}
	if !strings.Contains(err.Error(), "default/souls/bad") {
		t.Fatalf("ListSouls error %q does not identify the corrupt key", err)
	}
}

func formatUnix(value int64) string {
	return strconv.FormatInt(value, 10)
}
