package main

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type recordingReplicator struct {
	inner *alertKeyFSMReplicator
	cmds  []core.FSMCommand
}

func (r *recordingReplicator) ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error) {
	r.cmds = append(r.cmds, cmd)
	return r.inner.ApplyMutation(cmd, timeout)
}

// Dashboard settings were saved with a local write, so in a cluster each node
// showed its own values.
func TestSettingsSaveReplicatesInClusterMode(t *testing.T) {
	db := newAlertKeyDB(t)
	rep := &recordingReplicator{inner: &alertKeyFSMReplicator{store: db}}
	adapter := &restStorageAdapter{store: db, replicator: rep}

	if err := adapter.SaveSystemConfig(context.Background(), "settings", []byte(`{"instance_name":"Prod"}`)); err != nil {
		t.Fatal(err)
	}
	if len(rep.cmds) != 1 || rep.cmds[0].Key != "system/config/settings" || rep.cmds[0].Op != core.FSMSet {
		t.Fatalf("replicated commands = %+v, want one set of system/config/settings", rep.cmds)
	}
	got, err := adapter.GetSystemConfig(context.Background(), "settings")
	if err != nil || string(got) != `{"instance_name":"Prod"}` {
		t.Fatalf("read back %q, %v", got, err)
	}

	standalone := &restStorageAdapter{store: db}
	if err := standalone.SaveSystemConfig(context.Background(), "settings", []byte(`{"instance_name":"Solo"}`)); err != nil {
		t.Fatal(err)
	}
	if len(rep.cmds) != 1 {
		t.Fatal("standalone save went through the replicator")
	}
}
