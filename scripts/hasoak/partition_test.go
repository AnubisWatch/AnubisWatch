//go:build unix

package main

import (
	"reflect"
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
