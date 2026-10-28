package alert

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The methods in this file let a clustered node bring its in-memory alert state
// in line with records another node wrote through Raft. They update memory
// only: the record is already in this node's storage, so persisting it again
// would turn every replicated write into a second one.

// LoadChannel installs a replicated alert channel.
func (m *Manager) LoadChannel(channel *core.AlertChannel) {
	if channel == nil || channel.ID == "" {
		return
	}
	m.mu.Lock()
	m.channels[channel.ID] = channel
	m.mu.Unlock()
}

// UnloadChannel forgets a channel deleted on another node.
func (m *Manager) UnloadChannel(id string) {
	m.mu.Lock()
	delete(m.channels, id)
	m.mu.Unlock()
}

// LoadRule installs a replicated alert rule.
func (m *Manager) LoadRule(rule *core.AlertRule) {
	if rule == nil || rule.ID == "" {
		return
	}
	m.mu.Lock()
	m.rules[rule.ID] = rule
	m.mu.Unlock()
}

// UnloadRule forgets a rule deleted on another node.
func (m *Manager) UnloadRule(id string) {
	m.mu.Lock()
	delete(m.rules, id)
	m.mu.Unlock()
}

// LoadIncident installs an incident opened or changed on another node and
// tells the incident hook, so live clients of this node see it too.
func (m *Manager) LoadIncident(incident *core.Incident) {
	if incident == nil || incident.ID == "" {
		return
	}
	m.mu.Lock()
	m.incidents[incident.ID] = incident
	notify := m.incidentNotifier(incident)
	m.mu.Unlock()
	notify()
}

// Resync reloads channels, rules and active incidents from storage, replacing
// the channel and rule sets. A clustered node calls it after a snapshot
// replaced its keyspace, which no per-record update describes.
func (m *Manager) Resync() {
	if m.storage == nil {
		return
	}
	channels, chErr := m.storage.ListChannels("")
	rules, ruleErr := m.storage.ListRules("")
	incidents, incErr := m.storage.ListActiveIncidents()

	m.mu.Lock()
	defer m.mu.Unlock()
	if chErr == nil {
		m.channels = make(map[string]*core.AlertChannel, len(channels))
		for _, ch := range channels {
			m.channels[ch.ID] = ch
		}
	} else {
		m.logger.Warn("Failed to reload channels", "error", chErr)
	}
	if ruleErr == nil {
		m.rules = make(map[string]*core.AlertRule, len(rules))
		for _, rule := range rules {
			m.rules[rule.ID] = rule
		}
	} else {
		m.logger.Warn("Failed to reload rules", "error", ruleErr)
	}
	if incErr == nil {
		for _, incident := range incidents {
			m.incidents[incident.ID] = incident
		}
	} else {
		m.logger.Warn("Failed to reload active incidents", "error", incErr)
	}
	m.loadDedupState()
}

// DedupStateStore is implemented by alert storage that shares notification
// dedup state between nodes. Rules are evaluated only on the node that owns a
// soul, so the record of when a (rule, soul, status) alert was last sent lives
// in that node's memory; when ownership moves, the new owner would see no
// record and send the alert again inside the cooldown. With a store that
// implements this, every new dedup record (an alert about to be sent) and
// every release (an alert that could not be delivered) is shared, and
// LoadDedupState applies records shared by other nodes. Storage that does not
// implement it keeps dedup state node-local, as a standalone node needs.
type DedupStateStore interface {
	SaveDedupState(workspaceID string, entry *core.AlertHistoryEntry) error
	DeleteDedupState(workspaceID, key string) error
	ListDedupState() ([]*core.AlertHistoryEntry, error)
}

func dedupKey(ruleID, soulID string, status core.SoulStatus) string {
	return fmt.Sprintf("dedup:%s:%s:%s", ruleID, soulID, status)
}

// dedupStatus is the status component of an event's dedup key: the soul
// status for rule alerts, the kind for flap notifications, so a "flapping
// started" notice neither suppresses nor is suppressed by a status alert.
func dedupStatus(event *core.AlertEvent) core.SoulStatus {
	if event.Kind != "" {
		return core.SoulStatus(event.Kind)
	}
	return event.Status
}

// persistDedup shares the dedup record isDuplicate just created or renewed for
// event.
func (m *Manager) persistDedup(event *core.AlertEvent) {
	store, ok := m.storage.(DedupStateStore)
	if !ok {
		return
	}
	key := dedupKey(event.RuleID, event.SoulID, dedupStatus(event))
	m.history.Mu.Lock()
	entry, exists := m.history.Entries[key]
	var snapshot core.AlertHistoryEntry
	if exists {
		snapshot = *entry
	}
	m.history.Mu.Unlock()
	if !exists {
		return
	}
	if err := store.SaveDedupState(defaultWorkspace(event.WorkspaceID), &snapshot); err != nil {
		m.logger.Warn("Failed to share dedup state", "key", key, "error", err)
	}
}

// sharedHistoryKey reports whether a history record is shared across the
// cluster: notification dedup records and channel rate-limit windows.
func sharedHistoryKey(key string) bool {
	return strings.HasPrefix(key, "dedup:") || strings.HasPrefix(key, "ratelimit:")
}

// persistHistoryEntry shares a dedup or rate-limit record when the storage
// supports it (cluster mode); standalone it is a no-op.
func (m *Manager) persistHistoryEntry(workspaceID string, entry core.AlertHistoryEntry) {
	store, ok := m.storage.(DedupStateStore)
	if !ok || entry.Key == "" {
		return
	}
	if err := store.SaveDedupState(defaultWorkspace(workspaceID), &entry); err != nil {
		m.logger.Warn("Failed to share alert history state", "key", entry.Key, "error", err)
	}
}

// loadDedupState merges the shared dedup records from storage.
func (m *Manager) loadDedupState() {
	store, ok := m.storage.(DedupStateStore)
	if !ok {
		return
	}
	entries, err := store.ListDedupState()
	if err != nil {
		m.logger.Warn("Failed to load shared dedup state", "error", err)
		return
	}
	for _, entry := range entries {
		m.LoadDedupState(entry)
	}
}

// LoadDedupState installs a dedup record shared by another node. A record
// older than the one this node holds is ignored, so a late replica cannot
// shorten a cooldown.
func (m *Manager) LoadDedupState(entry *core.AlertHistoryEntry) {
	if entry == nil || !sharedHistoryKey(entry.Key) {
		return
	}
	m.history.Mu.Lock()
	defer m.history.Mu.Unlock()
	if existing, ok := m.history.Entries[entry.Key]; ok {
		if existing.LastSent.After(entry.LastSent) {
			return
		}
		if existing.LastSent.Before(entry.LastSent) {
			delete(m.dedupClaims, entry.Key)
		}
	}
	copied := *entry
	m.history.Entries[entry.Key] = &copied
}

// UnloadDedupState forgets a dedup record another node released.
func (m *Manager) UnloadDedupState(key string) {
	m.history.Mu.Lock()
	delete(m.history.Entries, key)
	delete(m.dedupClaims, key)
	m.history.Mu.Unlock()
}

// SeedFromJudgments initializes missing per-soul evaluation state from recent
// judgments in any order, preserving live state already held here. Ownership
// acquisition uses SeedOwnedSoul to replace state from an earlier tenure.
func (m *Manager) SeedFromJudgments(soulID string, judgments []*core.Judgment) {
	m.seedFromJudgments(soulID, judgments, false)
}

// SeedOwnedSoul reconstructs evaluation state when this node acquires a soul,
// including a return after another owner observed a recovery or new outage.
// Call before scheduling the acquired soul. Shared notification cooldowns and
// channel rate limits are retained.
func (m *Manager) SeedOwnedSoul(soulID string, judgments []*core.Judgment) {
	m.seedFromJudgments(soulID, judgments, true)
}

func (m *Manager) seedFromJudgments(soulID string, judgments []*core.Judgment, replace bool) {
	if soulID == "" {
		return
	}
	ordered := make([]*core.Judgment, 0, len(judgments))
	for _, j := range judgments {
		if j != nil {
			ordered = append(ordered, j)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Timestamp.Before(ordered[j].Timestamp) })

	m.mu.Lock()
	defer m.mu.Unlock()
	if replace {
		delete(m.statusSince, soulID)
		for ruleID := range m.rules {
			delete(m.failureStreak, ruleID+":"+soulID)
		}
	}
	if len(ordered) == 0 {
		return
	}

	latest := ordered[len(ordered)-1]
	streak := 0
	since := latest.Timestamp
	for i := len(ordered) - 1; i >= 0 && ordered[i].Status == latest.Status; i-- {
		since = ordered[i].Timestamp
		if latest.Status == core.SoulDead {
			streak++
		}
	}
	if streak > 1_000_000 {
		streak = 1_000_000
	}

	if m.statusSince == nil {
		m.statusSince = make(map[string]statusHold)
	}
	if _, ok := m.statusSince[soulID]; !ok && !since.IsZero() {
		m.statusSince[soulID] = statusHold{Status: latest.Status, Since: since}
	}
	if streak == 0 {
		return
	}
	if m.failureStreak == nil {
		m.failureStreak = make(map[string]int)
	}
	for ruleID := range m.rules {
		key := ruleID + ":" + soulID
		if _, ok := m.failureStreak[key]; !ok {
			m.failureStreak[key] = streak
		}
	}
}
