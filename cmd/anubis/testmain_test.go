package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testRootEnv passes the package's temporary root to helper subprocesses.
const testRootEnv = "ANUBIS_TEST_ROOT"

// TestMain gives the package one temporary root and removes it when the run
// ends. It replaces an init() that created a data directory per process and
// never removed it.
//
// Helper subprocesses (GO_WANT_HELPER_PROCESS=1) re-run this binary and
// usually leave through os.Exit, which skips every deferred cleanup,
// t.TempDir's included. They therefore create nothing outside the parent's
// root: their fresh data directory and their temp dir both live under it, so
// the parent's removal covers whatever they leave behind.
func TestMain(m *testing.M) {
	os.Exit(runPackageTests(m))
}

func runPackageTests(m *testing.M) int {
	if root := os.Getenv(testRootEnv); root != "" && os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmp := filepath.Join(root, "helper-tmp")
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "helper temp dir: %v\n", err)
			return 1
		}
		// os.TempDir, and with it t.TempDir and os.MkdirTemp, reads these.
		for _, env := range []string{"TMPDIR", "TMP", "TEMP"} {
			os.Setenv(env, tmp)
		}
		// Like the parent, a helper starts with an empty data directory of
		// its own, whatever ANUBIS_DATA_DIR the calling test had set.
		data, err := os.MkdirTemp(tmp, "data-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper data dir: %v\n", err)
			return 1
		}
		testDataDir = data
		os.Setenv("ANUBIS_DATA_DIR", data)
		return m.Run()
	}

	root, err := os.MkdirTemp("", "anubis-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "test root: %v\n", err)
		return 1
	}
	defer os.RemoveAll(root)

	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "test data dir: %v\n", err)
		return 1
	}
	testDataDir = data
	// Always set, so a test that unsets ANUBIS_DATA_DIR still falls back to
	// testDataDir rather than the real system data directory.
	os.Setenv("ANUBIS_DATA_DIR", data)
	os.Setenv(testRootEnv, root)
	// Keep client commands from reading the developer's own config file or
	// `anubis login` token. Tests that need either set these themselves.
	if os.Getenv("ANUBIS_CONFIG") == "" {
		os.Setenv("ANUBIS_CONFIG", filepath.Join(root, "no-config.json"))
	}
	os.Setenv("ANUBIS_TOKEN_FILE", filepath.Join(root, "token"))
	return m.Run()
}
