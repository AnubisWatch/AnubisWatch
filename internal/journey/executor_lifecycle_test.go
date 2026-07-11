package journey

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestRemoveRunRequiresCurrentToken(t *testing.T) {
	executor := NewExecutor(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, oldCancel := context.WithCancel(context.Background())
	_, newCancel := context.WithCancel(context.Background())
	oldRun := &journeyRun{cancel: oldCancel}
	newRun := &journeyRun{cancel: newCancel}
	executor.running["journey"] = newRun

	executor.removeRun("journey", oldRun)
	if executor.running["journey"] != newRun {
		t.Fatal("an old journey loop removed the replacement run")
	}

	executor.removeRun("journey", newRun)
	if _, exists := executor.running["journey"]; exists {
		t.Fatal("current journey run did not remove itself")
	}
}
