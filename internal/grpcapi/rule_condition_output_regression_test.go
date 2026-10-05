package grpcapi

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestRuleToPBPreservesPrimaryCondition(t *testing.T) {
	if ruleToPB(nil) != nil || channelToPB(nil) != nil {
		t.Fatal("nil input")
	}
	for _, cs := range [][]core.AlertCondition{nil, {}, {{Type: "threshold"}}, {{Type: "consecutive_failures"}, {Type: "threshold"}}, {{Type: "status_change"}}} {
		p := ruleToPB(&core.AlertRule{ID: "r", Name: "Fixture", Channels: []string{"one", "two"}, Conditions: cs, Enabled: true})
		expected := ""
		if len(cs) > 0 {
			expected = cs[0].Type
		}
		if p.ConditionType != expected || p.ChannelId != "one" || p.Id != "r" || !p.Enabled {
			t.Fatal("conversion")
		}
	}

}
