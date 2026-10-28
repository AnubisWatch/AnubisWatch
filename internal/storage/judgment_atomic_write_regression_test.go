package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func judgmentRollupCount(t *testing.T, db *CobaltDB, j *core.Judgment, resolution TimeResolution) int {
	t.Helper()
	key := fmt.Sprintf("%s/ts/%s/%s/%d", j.WorkspaceID, j.SoulID, resolution, truncateToResolution(j.Timestamp, resolution).Unix())
	b, err := db.Get(key)
	if err != nil {
		var missing *core.NotFoundError
		if !errors.As(err, &missing) {
			t.Fatal(err)
		}
		return 0
	}
	var summary JudgmentSummary
	if err := json.Unmarshal(b, &summary); err != nil {
		t.Fatal(err)
	}
	return summary.Count
}

func TestJudgmentCompoundWriteFailureIsAtomic(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, seeded := range []bool{false, true} {
			for _, failure := range []string{"summary", "index", "short-write", "sync"} {
				t.Run(fmt.Sprintf("encrypted=%v/seeded=%v/%s", encrypted, seeded, failure), func(t *testing.T) {
					cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: encrypted, Key: "compound-write-test-key"}}
					db, err := NewEngine(cfg, newTestLogger())
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = db.Close() }()
					ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
					at := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
					wantBefore := 0
					if seeded {
						if err := ts.SaveJudgment(context.Background(), &core.Judgment{ID: "seed", WorkspaceID: "tenant", SoulID: "soul", Timestamp: at, Status: core.SoulAlive}); err != nil {
							t.Fatal(err)
						}
						wantBefore = 1
					}
					j := &core.Judgment{ID: "failed", WorkspaceID: "tenant", SoulID: "soul", Timestamp: at.Add(time.Second), Status: core.SoulDead}
					oldWrite, oldSync := walWriteSeam, walSyncSeam
					defer func() { walWriteSeam, walSyncSeam = oldWrite, oldSync }()
					injected := false
					walWriteSeam = func(f *os.File, b []byte) (int, error) {
						if failure == "sync" || injected {
							return oldWrite(f, b)
						}
						var entry walEntry
						if err := json.Unmarshal(b[4:], &entry); err != nil {
							return 0, err
						}
						var values map[string][]byte
						if err := json.Unmarshal(entry.Value, &values); err != nil {
							return 0, err
						}
						target := "tenant/judgment-idx/failed"
						if failure == "summary" {
							target = fmt.Sprintf("tenant/ts/soul/1min/%d", at.Unix())
						}
						if _, exists := values[target]; !exists {
							return oldWrite(f, b)
						}
						injected = true
						if failure == "short-write" {
							n, _ := f.Write(b[:len(b)/2])
							return n, io.ErrShortWrite
						}
						return 0, errors.New("compound write fault")
					}
					walSyncSeam = func(f *os.File) error {
						if failure == "sync" && !injected {
							injected = true
							return errors.New("compound fsync fault")
						}
						return oldSync(f)
					}
					writeErr := ts.SaveJudgment(context.Background(), j)
					walWriteSeam, walSyncSeam = oldWrite, oldSync
					if writeErr == nil || !injected {
						t.Fatalf("fault must be exercised and returned: injected=%v err=%v", injected, writeErr)
					}
					assertAbsent := func() {
						if _, err := db.GetJudgment(context.Background(), "tenant", "soul", j.Timestamp); err == nil {
							t.Fatal("failed write exposed primary record")
						}
						if _, err := db.Get("tenant/judgment-idx/failed"); err == nil {
							t.Fatal("failed write exposed index")
						}
						for _, res := range []TimeResolution{Resolution1Min, Resolution1Hour, Resolution1Day} {
							if count := judgmentRollupCount(t, db, j, res); count != wantBefore {
								t.Fatalf("failed write changed %s count=%d want=%d", res, count, wantBefore)
							}
						}
					}
					assertAbsent()
					db.mu.RLock()
					_, cached := db.judgmentIndex[j.ID]
					db.mu.RUnlock()
					if cached {
						t.Fatal("failed write published in-memory lookup index")
					}
					if !seeded {
						if _, marked := ts.liveMarked.Load(liveSinceKey("tenant", "soul")); marked {
							t.Fatal("failed write published live-since cache")
						}
					}
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
					db, err = NewEngine(cfg, newTestLogger())
					if err != nil {
						t.Fatal(err)
					}
					assertAbsent()
					ts = NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
					if err := ts.SaveJudgment(context.Background(), j); err != nil {
						t.Fatal("retry failed:", err)
					}
					if got, err := db.GetJudgmentNoCtx(j.ID); err != nil || got.ID != j.ID {
						t.Fatalf("retry lookup failed: err=%v", err)
					}
					for _, res := range []TimeResolution{Resolution1Min, Resolution1Hour, Resolution1Day} {
						if count := judgmentRollupCount(t, db, j, res); count != wantBefore+1 {
							t.Fatalf("retry double counted %s: %d", res, count)
						}
					}
				})
			}
		}
	}
}

func TestJudgmentSavesUseOneDurableFrame(t *testing.T) {
	for _, rollups := range []bool{false, true} {
		t.Run(fmt.Sprint(rollups), func(t *testing.T) {
			db := newTestDB(t)
			defer db.Close()
			ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
			write, sync := walWriteSeam, walSyncSeam
			defer func() { walWriteSeam, walSyncSeam = write, sync }()
			writes, syncs := 0, 0
			walWriteSeam = func(f *os.File, b []byte) (int, error) { writes++; return write(f, b) }
			walSyncSeam = func(f *os.File) error { syncs++; return sync(f) }
			for i := 0; i < 20; i++ {
				j := &core.Judgment{ID: fmt.Sprint(i), SoulID: "soul", Timestamp: time.Unix(1_700_000_000+int64(i), 0), Status: core.SoulAlive}
				var err error
				if rollups {
					err = ts.SaveJudgment(context.Background(), j)
				} else {
					err = db.SaveJudgment(context.Background(), j)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if writes != 20 || syncs != 20 {
				t.Fatalf("each logical commit requires one frame/fsync: writes=%d syncs=%d", writes, syncs)
			}
		})
	}
}

func TestRawJudgmentIndexFailureLeavesNoPrimary(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	j := &core.Judgment{ID: "raw-index", SoulID: "soul", Timestamp: time.Unix(1_700_000_000, 0)}
	write := walWriteSeam
	defer func() { walWriteSeam = write }()
	injected := false
	walWriteSeam = func(f *os.File, b []byte) (int, error) {
		var entry walEntry
		if err := json.Unmarshal(b[4:], &entry); err != nil {
			return 0, err
		}
		hit := entry.Key == "default/judgment-idx/raw-index"
		if entry.Op == "BATCH" {
			var values map[string][]byte
			if err := json.Unmarshal(entry.Value, &values); err != nil {
				return 0, err
			}
			_, hit = values["default/judgment-idx/raw-index"]
		}
		if hit {
			injected = true
			return 0, errors.New("index unavailable")
		}
		return write(f, b)
	}
	err := db.SaveJudgment(context.Background(), j)
	walWriteSeam = write
	if err == nil || !injected {
		t.Fatalf("index fault not propagated: injected=%v err=%v", injected, err)
	}
	if _, err := db.GetJudgment(context.Background(), "default", j.SoulID, j.Timestamp); err == nil {
		t.Fatal("index failure left an independently committed primary")
	}
}
