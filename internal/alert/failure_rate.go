package alert

import (
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// JudgmentHistoryStore supplies persisted check results, including healthy
// checks. Alert events alone cannot be used as an error-rate denominator.
type JudgmentHistoryStore interface {
	ListAlertJudgments(workspaceID, soulID string, start, end time.Time) ([]*core.Judgment, error)
}

type rateSample struct {
	at     time.Time
	status core.SoulStatus
	id     string
}

func rateWindow(cond core.AlertCondition) time.Duration {
	if cond.Window.Duration > 0 {
		return cond.Window.Duration
	}
	return 5 * time.Minute
}
func maxRateWindow(conditions []core.AlertCondition) time.Duration {
	var window time.Duration
	for _, cond := range conditions {
		if (cond.Type == "failure_rate" || cond.Type == "percentage") && rateWindow(cond) > window {
			window = rateWindow(cond)
		}
		if nested := maxRateWindow(cond.SubConditions); nested > window {
			window = nested
		}
	}
	return window
}
func rateKey(j *core.Judgment) string { return defaultWorkspace(j.WorkspaceID) + "/" + j.SoulID }
func judgmentTime(j *core.Judgment) time.Time {
	if !j.Timestamp.IsZero() {
		return j.Timestamp
	}
	return time.Now()
}

// Keep the standalone/in-memory seam useful without retaining judgments beyond
// any configured rate window. Production uses persisted history across restarts.
func (m *Manager) noteRateJudgment(j *core.Judgment, rules []*core.AlertRule) {
	if _, persisted := m.storage.(JudgmentHistoryStore); persisted {
		return
	}
	var window time.Duration
	for _, rule := range rules {
		if rule.Enabled {
			if w := maxRateWindow(rule.Conditions); w > window {
				window = w
			}
		}
	}
	key := rateKey(j)
	m.mu.Lock()
	defer m.mu.Unlock()
	if window == 0 {
		delete(m.rateSamples, key)
		return
	}
	if m.rateSamples == nil {
		m.rateSamples = make(map[string][]rateSample)
	}
	at := judgmentTime(j)
	cutoff := at.Add(-window)
	old := m.rateSamples[key]
	kept := old[:0]
	for _, sample := range old {
		if !sample.at.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	for _, sample := range kept {
		if j.ID != "" && sample.id == j.ID {
			m.rateSamples[key] = kept
			return
		}
	}
	m.rateSamples[key] = append(kept, rateSample{at: at, status: j.Status, id: j.ID})
}

func (m *Manager) failureRateCondition(cond core.AlertCondition, judgment *core.Judgment) bool {
	end := judgmentTime(judgment)
	start := end.Add(-rateWindow(cond))
	samples := m.windowSamples(cond, judgment, end)
	total, failed := 0, 0
	for _, sample := range samples {
		if sample.at.Before(start) || sample.at.After(end) {
			continue
		}
		switch sample.status {
		case core.SoulAlive, core.SoulDegraded:
			total++
		case core.SoulDead:
			total++
			failed++
		}
	}
	if total == 0 || failed == 0 {
		return false
	}
	if cond.Threshold == 0 {
		return true
	}
	return float64(failed)*100/float64(total) >= float64(cond.Threshold)
}

// windowSamples collects the check results of a windowed-rate condition's
// evaluation window, from persisted history when available and from the
// in-memory rate buffer otherwise. end is the caller's evaluation instant:
// the failing judgment itself is always included as a sample at exactly end,
// because the probe callback may run before its result is persisted, and a
// sample timestamped after end would otherwise be filtered out by callers.
func (m *Manager) windowSamples(cond core.AlertCondition, judgment *core.Judgment, end time.Time) []rateSample {
	start := end.Add(-rateWindow(cond))
	var samples []rateSample
	if store, ok := m.storage.(JudgmentHistoryStore); ok {
		history, err := store.ListAlertJudgments(defaultWorkspace(judgment.WorkspaceID), judgment.SoulID, start, end)
		if err != nil {
			m.logger.Warn("Failed to read failure-rate history", "soul_id", judgment.SoulID, "error", err)
			return nil
		}
		currentIncluded := false
		for _, j := range history {
			if j == nil || j.SoulID != judgment.SoulID || defaultWorkspace(j.WorkspaceID) != defaultWorkspace(judgment.WorkspaceID) {
				continue
			}
			if (judgment.ID != "" && j.ID == judgment.ID) || (!judgment.Timestamp.IsZero() && j.Timestamp.Equal(judgment.Timestamp) && j.Status == judgment.Status) {
				currentIncluded = true
			}
			samples = append(samples, rateSample{at: j.Timestamp, status: j.Status, id: j.ID})
		}
		// The probe callback may precede persistence of its current result.
		if !currentIncluded {
			samples = append(samples, rateSample{at: end, status: judgment.Status, id: judgment.ID})
		}
	} else {
		m.mu.RLock()
		samples = append(samples, m.rateSamples[rateKey(judgment)]...)
		m.mu.RUnlock()
		if len(samples) == 0 {
			samples = []rateSample{{at: end, status: judgment.Status}}
		}
	}
	return samples
}

// percentageCondition evaluates the documented "percentage" condition type
// (docs/CONFIGURATION.md): a percentage metric over a window, compared with
// operator and threshold. failure_rate and uptime_percent are computed over
// the condition's window from check history; packet_loss_percent is the
// current judgment's measured loss. A metric this engine cannot measure
// never fires, matching evaluateCondition's treatment of unknown metrics.
func (m *Manager) percentageCondition(cond core.AlertCondition, judgment *core.Judgment) bool {
	var value float64
	switch cond.Metric {
	case "", "failure_rate", "uptime_percent":
		end := judgmentTime(judgment)
		start := end.Add(-rateWindow(cond))
		total, failed := 0, 0
		for _, sample := range m.windowSamples(cond, judgment, end) {
			if sample.at.Before(start) || sample.at.After(end) {
				continue
			}
			switch sample.status {
			case core.SoulAlive, core.SoulDegraded:
				total++
			case core.SoulDead:
				total++
				failed++
			}
		}
		if total == 0 {
			return false
		}
		failurePct := float64(failed) * 100 / float64(total)
		if cond.Metric == "uptime_percent" {
			value = 100 - failurePct
		} else {
			value = failurePct
		}
	case "packet_loss_percent":
		if judgment.Details == nil {
			return false
		}
		value = judgment.Details.PacketLoss
	default:
		return false
	}
	threshold, _ := cond.Value.(float64)
	if threshold == 0 {
		threshold = float64(cond.Threshold)
	}
	return m.compareFloatValue(value, cond.Operator, threshold)
}
