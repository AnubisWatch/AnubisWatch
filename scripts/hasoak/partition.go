//go:build unix

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// Every node runs as its own uid in -partition mode so iptables can tell a
// node's outbound loopback traffic apart (all nodes share 127.0.0.1).
const partitionUIDBase = 61000

func partitionUID(index int) uint32 { return uint32(partitionUIDBase + index) }

// partitionRules isolates one node's Raft traffic in both directions while its
// HTTP port stays reachable, so the soak can still talk to the cut-off node:
// nothing may reach its Raft port, and nothing it sends leaves it except
// answers from its own HTTP port.
func partitionRules(uid uint32, raftPort, httpPort int) [][]string {
	return [][]string{
		{"INPUT", "-i", "lo", "-p", "tcp", "--dport", strconv.Itoa(raftPort), "-j", "DROP"},
		{"OUTPUT", "-o", "lo", "-p", "tcp", "-m", "owner", "--uid-owner", strconv.FormatUint(uint64(uid), 10),
			"!", "--sport", strconv.Itoa(httpPort), "-j", "DROP"},
	}
}

func iptables(op string, rule []string) error {
	args := append([]string{"-w", op, rule[0]}, rule[1:]...)
	if out, err := exec.CommandContext(context.Background(), "iptables", args...).CombinedOutput(); err != nil { // #nosec G204 -- fixed arguments built from this harness's own ports
		return fmt.Errorf("iptables %v: %w: %s", args, err, out)
	}
	return nil
}

// partition cuts the node off from its peers; heal restores it. Healing
// ignores a rule that is already gone so cleanup can always run.
func (n *node) partition() error {
	applied := 0
	for _, r := range partitionRules(n.uid, n.raftPort, n.httpPort) {
		if err := iptables("-I", r); err != nil {
			for _, done := range partitionRules(n.uid, n.raftPort, n.httpPort)[:applied] {
				_ = iptables("-D", done)
			}
			return err
		}
		applied++
	}
	return nil
}

func (n *node) heal() {
	for _, r := range partitionRules(n.uid, n.raftPort, n.httpPort) {
		_ = iptables("-D", r)
	}
}

// ownNodeDir hands a node's directory to its uid and opens the run directory so
// that uid can read the binary and its config.
func ownNodeDir(root, dir string, uid uint32) error {
	if err := os.Chmod(root, 0o755); err != nil { // #nosec G302 -- the run directory holds throwaway soak data
		return err
	}
	return filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, int(uid), int(uid)) // #nosec G122 -- the directory was created moments ago by this harness and nothing else writes to it
	})
}

func credential(uid uint32) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
}
