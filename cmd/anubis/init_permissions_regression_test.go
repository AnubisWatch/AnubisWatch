//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInitConfigRestrictsCredentialPermissions(t *testing.T) {
	oldMask := syscall.Umask(0022)
	defer syscall.Umask(oldMask)
	path := filepath.Join(t.TempDir(), "anubis.json")
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	initSimpleWithPath(path)
	os.Stdout = original
	output.Close()
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		t.Fatal("invalid config control")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CONTROL EXPECTED: config exists; ACTUAL: %d bytes\n", len(raw))
	fmt.Printf("EXPECTED: credential file mode0600; ACTUAL: %04o\n", info.Mode().Perm())
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("PROBLEM CONFIRMED")
	}
	fmt.Println("FIX VERIFIED")
}
