package backup

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

var auditTarWriteError = errors.New("injected tar write failure")

type auditTarWriter struct {
	phase              string
	writes, zeroBlocks int
	failed             bool
}

func (w *auditTarWriter) Write(p []byte) (int, error) {
	w.writes++
	zero := len(p) > 0 && bytes.Equal(p, make([]byte, len(p)))
	if len(p) == 512 && zero {
		w.zeroBlocks++
	}
	fail := (w.phase == "trailer" && len(p) == 512 && zero) || (w.phase == "second-trailer" && w.zeroBlocks == 2) || (w.phase == "header" && w.writes == 1) || (w.phase == "payload" && len(p) > 0 && p[0] == '{') || (w.phase == "padding" && len(p) < 512 && zero)
	if fail {
		w.failed = true
		return 0, auditTarWriteError
	}
	return len(p), nil
}
func auditTarCloseCase(t *testing.T, phase string) {
	t.Helper()
	m := NewManager(&mockStorage{}, t.TempDir(), newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	if phase == "control" {
		var output bytes.Buffer
		if err := m.ExportToTar(context.Background(), &output, Options{}); err != nil {
			t.Fatal(err)
		}
		if output.Len() < 1024 || !bytes.Equal(output.Bytes()[output.Len()-1024:], make([]byte, 1024)) {
			t.Fatal("missing trailer")
		}
		t.Log("control EXPECTED: complete trailer ACTUAL: complete trailer")
		return
	}
	writer := &auditTarWriter{phase: phase}
	err := m.ExportToTar(context.Background(), writer, Options{})
	t.Logf("%s EXPECTED: injected tar write failure ACTUAL: %v", phase, err)
	if !writer.failed {
		t.Fatal("injection did not execute")
	}
	if !errors.Is(err, auditTarWriteError) {
		t.Fatal("PROBLEM CONFIRMED")
	}
}
func TestAuditBackupTarClose(t *testing.T) {
	for _, phase := range []string{"control", "trailer", "header", "payload", "second-trailer"} {
		t.Run(phase, func(t *testing.T) { auditTarCloseCase(t, phase) })
	}
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
