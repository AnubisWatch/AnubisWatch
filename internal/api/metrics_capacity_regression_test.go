package api

import (
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestMetricsEnumeratesBeyondTenThousandSouls(t *testing.T) {
	for _, count := range []int{0, 1, 10000, 10001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			souls := make([]*core.Soul, count)
			for i := range souls {
				souls[i] = &core.Soul{ID: fmt.Sprintf("soul-%05d", i), WorkspaceID: "default"}
			}
			store := &workspaceAwareMetricsStorage{mockStorage: newMockStorage(), soulsByWorkspace: map[string][]*core.Soul{"default": souls}}
			server := &RESTServer{store: store}
			for _, scope := range [][]string{nil, {"default"}} {
				got, err := server.metricsSouls(scope...)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != count {
					t.Errorf("EXPECTED: all %d souls in metrics; ACTUAL: %d; PROBLEM CONFIRMED", count, len(got))
				}
			}
		})
	}
}
