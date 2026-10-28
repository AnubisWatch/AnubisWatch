package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckQuotaCount(t *testing.T) {
	cases := []struct {
		name           string
		limit, current int
		refused        bool
	}{
		{"unlimited when 0", 0, 1000, false},
		{"unlimited when negative", -1, 1000, false},
		{"under the limit", 3, 2, false},
		{"at the limit", 3, 3, true},
		{"over the limit after lowering", 3, 5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckQuotaCount("ws", QuotaMaxSouls, tc.limit, tc.current)
			qe, refused := AsQuotaExceeded(err)
			if refused != tc.refused {
				t.Fatalf("refused = %v (%v), want %v", refused, err, tc.refused)
			}
			if refused {
				if qe.Quota != QuotaMaxSouls || qe.Limit != tc.limit || qe.Current != tc.current {
					t.Fatalf("error = %+v", qe)
				}
				msg := err.Error()
				if !strings.Contains(msg, QuotaMaxSouls) || !strings.Contains(msg, fmt.Sprintf("of %d", tc.limit)) {
					t.Fatalf("message %q does not name the quota and limit", msg)
				}
			}
		})
	}
}

func TestCheckQuotaInterval(t *testing.T) {
	cases := []struct {
		name            string
		floor, interval time.Duration
		refused         bool
	}{
		{"no floor", 0, time.Second, false},
		{"unset interval", 30 * time.Second, 0, false},
		{"at the floor", 30 * time.Second, 30 * time.Second, false},
		{"below the floor", 30 * time.Second, 10 * time.Second, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckQuotaInterval("ws", tc.floor, tc.interval)
			if _, refused := AsQuotaExceeded(err); refused != tc.refused {
				t.Fatalf("refused = %v (%v), want %v", refused, err, tc.refused)
			}
			if tc.refused && !strings.Contains(err.Error(), QuotaCheckIntervalMin) {
				t.Fatalf("message %q does not name the quota", err)
			}
		})
	}
}

func TestAsQuotaExceededUnwraps(t *testing.T) {
	wrapped := fmt.Errorf("save channel: %w", &QuotaExceededError{Quota: QuotaMaxAlertChannels})
	if qe, ok := AsQuotaExceeded(wrapped); !ok || qe.Quota != QuotaMaxAlertChannels {
		t.Fatalf("wrapped quota error not found: %v", wrapped)
	}
	if _, ok := AsQuotaExceeded(errors.New("disk full")); ok {
		t.Fatal("plain error reported as a quota refusal")
	}
	if _, ok := AsQuotaExceeded(nil); ok {
		t.Fatal("nil reported as a quota refusal")
	}
}

func TestCapRetention(t *testing.T) {
	week := 7 * 24 * time.Hour
	cases := []struct {
		name    string
		in      RetentionConfig
		days    int
		want    RetentionConfig
		changed bool
	}{
		{"no cap", RetentionConfig{Raw: Duration{time.Hour}, Day: "unlimited"}, 0,
			RetentionConfig{Raw: Duration{time.Hour}, Day: "unlimited"}, false},
		{"caps forever and long tiers", RetentionConfig{Raw: Duration{48 * time.Hour}, Minute: Duration{30 * 24 * time.Hour}, Day: "unlimited"}, 7,
			RetentionConfig{Raw: Duration{48 * time.Hour}, Minute: Duration{week}, FiveMin: Duration{week}, Hour: Duration{week}, Day: week.String()}, true},
		{"already within the cap", RetentionConfig{Raw: Duration{time.Hour}, Minute: Duration{time.Hour}, FiveMin: Duration{time.Hour}, Hour: Duration{time.Hour}, Day: "24h0m0s"}, 7,
			RetentionConfig{Raw: Duration{time.Hour}, Minute: Duration{time.Hour}, FiveMin: Duration{time.Hour}, Hour: Duration{time.Hour}, Day: "24h0m0s"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			if changed := CapRetention(&got, tc.days); changed != tc.changed {
				t.Fatalf("changed = %v, want %v", changed, tc.changed)
			}
			if got != tc.want {
				t.Fatalf("retention = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Old configs that set max_team_members still load; quotas are not defaulted.
func TestQuotaConfigLoadsDeprecatedTeamMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.json")
	body := `{"storage":{"path":"/tmp/x"},"tenants":{"enabled":true,"default_quotas":{"max_team_members":25,"max_souls":3}}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	q := cfg.Tenants.DefaultQuotas
	if q.MaxTeamMembers != 25 || q.MaxSouls != 3 || q.MaxJourneys != 0 || q.MaxAlertChannels != 0 || q.RetentionDays != 0 {
		t.Fatalf("quotas after load = %+v", q)
	}
}
