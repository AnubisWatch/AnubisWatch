package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
)

// adminCommand handles operator-only maintenance that works on the data
// directory directly.
//
//	anubis admin reset-password [--email EMAIL] [--password-stdin] [--force]
func adminCommand() {
	if len(os.Args) < 3 || os.Args[2] != "reset-password" {
		fmt.Println(`Usage: anubis admin reset-password [--email EMAIL] [--password-stdin] [--force]

Sets the local admin password in the data directory. Stop the server first:
a running server would overwrite the change. All sessions are revoked.
The email defaults to auth.local.admin_email from the config file.
Cluster authentication uses replicated state; this local-file command refuses
cluster configurations and stored consensus state. Restore quorum and use
authenticated password rotation.`)
		if len(os.Args) >= 3 && os.Args[2] != "--help" && os.Args[2] != "-h" {
			os.Exit(1)
		}
		return
	}
	if err := adminResetPassword(os.Args[3:], os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func adminResetPassword(args []string, stdin io.Reader) error {
	email := ""
	fromStdin := false
	force := false
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--email" || arg == "-e":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return fmt.Errorf("%s requires an email address", arg)
			}
			email = args[i+1]
			i++
		case strings.HasPrefix(arg, "--email="):
			email = strings.TrimPrefix(arg, "--email=")
			if email == "" {
				return fmt.Errorf("--email requires an email address")
			}
		case arg == "--password-stdin":
			fromStdin = true
		case arg == "--force":
			force = true
		default:
			return fmt.Errorf("unknown option %q", arg)
		}
	}

	cfg, err := loadLocalCLIConfig()
	if err != nil {
		return err
	}
	dataDir := dataDirForConfig(cfg)
	sharedAuthorityError := fmt.Errorf("admin reset-password only supports standalone local authentication; cluster credentials use replicated state: restore quorum and use the authenticated change-password workflow")
	if cfg != nil {
		n := cfg.Necropolis
		if n.Enabled || n.SingleNode || n.Raft.Bootstrap || len(n.Raft.Peers) > 0 || len(n.Raft.Join) > 0 {
			return sharedAuthorityError
		}
	}
	if email == "" && cfg != nil {
		email = cfg.Auth.Local.AdminEmail
	}
	if email == "" {
		return fmt.Errorf("admin email is required (--email)")
	}

	if !force {
		if err := probeServerHealth(getAPIURL()); err == nil {
			return fmt.Errorf("the server at %s is running; stop it first (or pass --force if that is a different instance)", getAPIURL())
		}
	}

	// Clustering may have been enabled by serve-time flags rather than the
	// saved config. Inspect persisted markers before touching local auth, and
	// hold the data-directory lock through the credential write.
	if _, err := os.Stat(filepath.Join(dataDir, "wal.log")); err == nil {
		store, err := openLocalStorageWithConfig(cfg)
		if err != nil {
			return fmt.Errorf("cannot inspect authentication authority: %w", err)
		}
		defer store.Close()
		for _, prefix := range []string{"raft/", "system/auth/"} {
			keys, err := store.List(prefix)
			if err != nil {
				return fmt.Errorf("cannot inspect authentication authority: %w", err)
			}
			if len(keys) != 0 {
				return sharedAuthorityError
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect authentication authority: %w", err)
	}

	reader := bufio.NewReader(stdin)
	var password string
	if fromStdin {
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		password = strings.TrimRight(string(data), "\r\n")
	} else {
		password = promptPassword(reader, fmt.Sprintf("New password for %s: ", email))
		if confirm := promptPassword(reader, "Repeat new password: "); confirm != password {
			return fmt.Errorf("passwords do not match")
		}
	}

	// Same file the server uses: <storage.path>/sessions.json.
	sessionPath := filepath.Join(dataDir, "sessions.json")
	if err := auth.ResetLocalAdminPassword(sessionPath, email, password); err != nil {
		return err
	}
	fmt.Printf("✓ Password for %s updated in %s; all sessions were revoked.\n", email, sessionPath)
	return nil
}
