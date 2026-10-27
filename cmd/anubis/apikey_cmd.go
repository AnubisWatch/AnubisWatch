package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// #nosec G101 -- help text; "anb_..." is a placeholder, not a credential.
const apikeyUsage = `Usage: anubis apikey <command> [options]

Manage API keys for automation. Keys are managed with an interactive session
(run "anubis login" first); an API key cannot list, create or revoke keys.

Commands:
  list                                List the workspace's API keys
  create --name N [--role R] [--expires-in-days D]
                                      Create a key and print it ONCE
  revoke <id>                         Revoke a key immediately

Use a key with any client command or HTTP/gRPC client:
  ANUBIS_API_TOKEN=anb_... anubis judge
  curl -H "Authorization: Bearer anb_..." https://host/api/v1/souls
`

func apikeyCommand() {
	if err := runAPIKeyCommand(os.Args[2:], getAPIURL(), getAPIToken(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAPIKeyCommand(args []string, apiURL, token string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, apikeyUsage)
		return nil
	}
	if token == "" {
		return errors.New("not logged in; run `anubis login` first")
	}
	switch args[0] {
	case "list", "ls":
		var keys []struct {
			ID         string     `json:"id"`
			Name       string     `json:"name"`
			Prefix     string     `json:"prefix"`
			Role       string     `json:"role"`
			Status     string     `json:"status"`
			ExpiresAt  *time.Time `json:"expires_at"`
			LastUsedAt *time.Time `json:"last_used_at"`
		}
		if err := apiKeyCall(http.MethodGet, apiURL+"/api/v1/api-keys", token, nil, &keys); err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Fprintln(out, "No API keys.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tROLE\tSTATUS\tEXPIRES\tLAST USED")
		for _, k := range keys {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", k.ID, k.Name, k.Role, k.Status, formatOptionalTime(k.ExpiresAt, "never"), formatOptionalTime(k.LastUsedAt, "never"))
		}
		return tw.Flush()
	case "create":
		fs := flag.NewFlagSet("apikey create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		name := fs.String("name", "", "key name")
		role := fs.String("role", "", "role (default: yours)")
		days := fs.Int("expires-in-days", 0, "expiry in days (0 = never, max 366)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*name) == "" {
			return errors.New("--name is required")
		}
		body, _ := json.Marshal(map[string]any{"name": *name, "role": *role, "expires_in_days": *days})
		var created struct {
			ID   string `json:"id"`
			Role string `json:"role"`
			Key  string `json:"key"`
		}
		if err := apiKeyCall(http.MethodPost, apiURL+"/api/v1/api-keys", token, body, &created); err != nil {
			return err
		}
		fmt.Fprintf(out, "Created API key %s (role %s). Store it now; it is not shown again:\n\n%s\n", created.ID, created.Role, created.Key)
		return nil
	case "revoke", "rm":
		if len(args) != 2 || args[1] == "" {
			return errors.New("usage: anubis apikey revoke <id>")
		}
		if err := apiKeyCall(http.MethodDelete, apiURL+"/api/v1/api-keys/"+url.PathEscape(args[1]), token, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(out, "Revoked API key %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown apikey command %q (list, create, revoke)", args[0])
	}
}

func formatOptionalTime(t *time.Time, empty string) string {
	if t == nil || t.IsZero() {
		return empty
	}
	return t.Local().Format("2006-01-02 15:04")
}

func apiKeyCall(method, target, token string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("server answered %d: %s", resp.StatusCode, e.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
