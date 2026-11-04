//go:build unix

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPartitionRulesCutRaftTrafficOnly(t *testing.T) {
	got := partitionRules(61001, 7001, 8001)
	want := [][]string{
		{"INPUT", "-i", "lo", "-p", "tcp", "--dport", "7001", "-j", "DROP"},
		{"OUTPUT", "-o", "lo", "-p", "tcp", "-m", "owner", "--uid-owner", "61001", "!", "--sport", "8001", "-j", "DROP"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
}

func TestPartitionUIDsAreDistinct(t *testing.T) {
	seen := map[uint32]bool{}
	for i := 0; i < 3; i++ {
		u := partitionUID(i)
		if u == 0 || seen[u] {
			t.Fatalf("uid %d for node %d is root or repeated", u, i)
		}
		seen[u] = true
	}
}

func TestCheckTraversable(t *testing.T) {
	// Build the tree under /var/tmp: t.TempDir may sit inside a private home
	// directory, which would make the "open" case fail for the right reason.
	base, err := os.MkdirTemp("/var/tmp", "hasoak-test-")
	if err != nil {
		t.Skipf("/var/tmp is not writable: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Chmod(base, 0o755); err != nil { // #nosec G302 -- test directory
		t.Fatal(err)
	}
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o755); err != nil { // #nosec G301 -- test directory
		t.Fatal(err)
	}
	if err := checkTraversable(open); err != nil {
		t.Fatalf("a world-searchable path must pass: %v", err)
	}

	private := filepath.Join(base, "private")
	inner := filepath.Join(private, "run")
	if err := os.MkdirAll(inner, 0o755); err != nil { // #nosec G301 -- test directory
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o750); err != nil { // #nosec G302 -- test directory
		t.Fatal(err)
	}
	err = checkTraversable(inner)
	if err == nil || !strings.Contains(err.Error(), private) {
		t.Fatalf("a private ancestor must be named in the error, got %v", err)
	}
}
