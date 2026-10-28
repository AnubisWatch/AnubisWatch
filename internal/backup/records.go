package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Records that describe this node or its Raft state rather than the user's
// data. Restoring them onto another node would give it the wrong identity.
var nodeLocalPrefixes = []string{"raft/", "system/cluster/", "system/jackals/"}

// History collections, the second key segment under a workspace. They are
// backed up only with --include-history.
var historyCollections = map[string]bool{
	"ts":           true,
	"journey-runs": true,
}

// Raw judgments travel in the typed Judgments section instead, which honours
// the --include-history day window; SaveJudgment rebuilds their index.
var typedCollections = map[string]bool{
	"judgments":    true,
	"judgment-idx": true,
}

// collectRecords returns every stored record worth backing up, and false when
// the store cannot list raw records.
func (m *Manager) collectRecords(includeHistory bool) (map[string][]byte, bool, error) {
	src, ok := m.storage.(RecordSource)
	if !ok {
		return nil, false, nil
	}
	all, err := src.PrefixScan("")
	if err != nil {
		return nil, false, err
	}
	records := make(map[string][]byte, len(all))
	for key, value := range all {
		if isNodeLocalKey(key) || isTypedKey(key) {
			continue
		}
		if !includeHistory && isHistoryKey(key) {
			continue
		}
		records[key] = value
	}
	return records, true, nil
}

func isNodeLocalKey(key string) bool {
	for _, p := range nodeLocalPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func isTypedKey(key string) bool {
	parts := strings.SplitN(key, "/", 3)
	return len(parts) == 3 && parts[0] != "system" && typedCollections[parts[1]]
}

func isHistoryKey(key string) bool {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) < 3 || parts[0] == "system" {
		return false
	}
	if parts[1] == "alerts" && parts[2] == "events" {
		return true
	}
	return historyCollections[parts[1]]
}

// recordCategory maps a record key to the restore option that covers it.
func recordCategory(key string) string {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) < 2 || parts[0] == "system" {
		return "system"
	}
	switch parts[1] {
	case "souls", "ts":
		return "souls"
	case "channels", "rules", "alerts", "verdicts":
		return "alerts"
	case "statuspages":
		return "statuspages"
	case "journeys", "journey-runs":
		return "journeys"
	}
	return "other"
}

// restoreRecords writes the raw records of an archive into the store. It
// returns the number of records that could not be written, so a restore
// that only partly succeeded can be reported to the operator.
func (m *Manager) restoreRecords(sink RecordSink, records map[string][]byte, opts RestoreOptions) (int, error) {
	include := map[string]bool{
		"souls":       opts.IncludeSouls,
		"alerts":      opts.IncludeAlerts,
		"statuspages": opts.IncludeStatusPages,
		"journeys":    opts.IncludeJourneys,
		"system":      opts.IncludeSystemConfig,
		"other":       opts.IncludeWorkspaces,
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	restored := 0
	failed := 0
	for _, key := range keys {
		if !include[recordCategory(key)] || isNodeLocalKey(key) {
			continue
		}
		if err := sink.Put(key, records[key]); err != nil {
			m.logger.Warn("Failed to restore record", "key", key, "error", err)
			if !opts.ContinueOnError {
				return failed, fmt.Errorf("failed to restore record %s: %w", key, err)
			}
			failed++
			continue
		}
		restored++
	}
	m.logger.Info("Restored records", "count", restored)
	return failed, nil
}

// JudgmentSink is implemented by stores that can restore judgment history.
type JudgmentSink interface {
	SaveJudgment(ctx context.Context, j *core.Judgment) error
}

// restoreJudgments restores the judgment history of an archive. It returns
// the number of judgments that could not be written.
func (m *Manager) restoreJudgments(ctx context.Context, storage RestoreStorage, judgments []*core.Judgment, opts RestoreOptions) (int, error) {
	failed := 0
	if len(judgments) > 0 && opts.IncludeSouls {
		sink, ok := storage.(JudgmentSink)
		if !ok {
			return 0, fmt.Errorf("store cannot restore judgment history")
		}
		for _, j := range judgments {
			if err := sink.SaveJudgment(ctx, j); err != nil {
				m.logger.Warn("Failed to restore judgment", "id", j.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore judgment %s: %w", j.ID, err)
				}
				failed++
			}
		}
		m.logger.Info("Restored judgments", "count", len(judgments))
	}
	return failed, nil
}

// authState is the part of sessions.json that is backed up: accounts and the
// admin password, not live sessions, reset tokens or lockouts.
type authState struct {
	Users             json.RawMessage `json:"users,omitempty"`
	AdminEmail        string          `json:"admin_email,omitempty"`
	AdminPasswordHash string          `json:"admin_password_hash,omitempty"`
	AdminSeedHash     string          `json:"admin_seed_hash,omitempty"`
}

func (m *Manager) collectAuth() (json.RawMessage, error) {
	data, err := os.ReadFile(filepath.Join(m.dataDir, sessionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state authState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse %s: %w", sessionsFile, err)
	}
	return json.Marshal(state)
}

// restoreAuth replaces sessions.json with the backed-up accounts. Existing
// login sessions end, which is what a restore should do.
func (m *Manager) restoreAuth(raw json.RawMessage) error {
	var state authState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.dataDir, sessionsFile)
	// The account state is secret, so it is written through a private
	// temporary file of its own: a leftover "sessions.json.tmp" from an
	// earlier run is not ours, so it is neither written through nor
	// changed in any way.
	tmp, err := os.CreateTemp(m.dataDir, sessionsFile+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
