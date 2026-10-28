package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestPartialRestoreReportsCountAndPreservesAccounts(t *testing.T) {
	for _, failed := range []int{0, 1, 2} {
		name := []string{"success", "one missing record", "two missing records"}[failed]
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			sessions := writeExistingSessions(t, dir)
			m := NewManager(nil, dir, newTestLogger())
			path := filepath.Join(t.TempDir(), "partial.json")
			records := map[string][]byte{"default/dashboards/d1": []byte(`{"id":"d1"}`), "default/dashboards/d2": []byte(`{"id":"d2"}`)}
			if failed == 1 {
				delete(records, "default/dashboards/d2")
			}
			writeAuthOrderingBackup(t, m, path, backedUpAuthJSON, records, nil)
			store := &authOrderStore{}
			if failed > 0 {
				store.putErr = errors.New("disk full")
			}
			opts := DefaultRestoreOptions()
			opts.ContinueOnError = true
			err := m.Restore(context.Background(), store, path, opts)
			if failed == 0 {
				if err != nil {
					t.Fatal(err)
				}
				if len(store.puts) != 2 {
					t.Fatal("normal restore omitted records")
				}
				return
			}
			var partial *PartialRestoreError
			if !errors.As(err, &partial) || partial.FailedRecords != failed {
				t.Fatalf("expected partial restore with %d failures, got %v", failed, err)
			}
			assertSessionsUnchanged(t, sessions)
		})
	}
}
