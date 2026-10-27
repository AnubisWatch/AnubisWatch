package alert

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type channelTypeFilterRecorder struct {
	calls int
}

func (d *channelTypeFilterRecorder) Send(context.Context, *core.AlertEvent, *core.AlertChannel) error {
	d.calls++
	return nil
}

func (*channelTypeFilterRecorder) Validate(map[string]any) error { return nil }

// A channel's "type" filter selects the destination currently being evaluated.
// Alert events are created before a destination is chosen, so dispatch must put
// the channel type on its private event copy before applying channel filters.
func TestChannelTypeFilterUsesDestinationType(t *testing.T) {
	tests := []struct {
		name   string
		filter core.AlertFilter
		want   int
	}{
		{
			name:   "status filter control reaches dispatcher",
			filter: core.AlertFilter{Field: "status", Operator: "eq", Value: "dead"},
			want:   1,
		},
		{
			name:   "nonmatching type control remains filtered",
			filter: core.AlertFilter{Field: "type", Operator: "eq", Value: "email"},
			want:   0,
		},
		{
			name:   "matching destination type reaches dispatcher",
			filter: core.AlertFilter{Field: "type", Operator: "eq", Value: "slack"},
			want:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher := &channelTypeFilterRecorder{}
			manager := NewManager(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			manager.dispatchers[core.ChannelSlack] = dispatcher
			manager.channels["slack-oncall"] = &core.AlertChannel{
				ID:          "slack-oncall",
				Name:        "Slack on-call",
				Type:        core.ChannelSlack,
				Enabled:     true,
				WorkspaceID: "default",
				Filters:     []core.AlertFilter{tc.filter},
			}

			manager.dispatch(&core.AlertEvent{
				ID:          "event-1",
				SoulID:      "soul-1",
				WorkspaceID: "default",
				Status:      core.SoulDead,
				Severity:    core.SeverityCritical,
			})

			if dispatcher.calls != tc.want {
				t.Fatalf("dispatcher calls = %d, want %d", dispatcher.calls, tc.want)
			}
		})
	}
}
