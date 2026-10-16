package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// loginCommand signs in against the running server and stores the session
// token so other client commands (judge, status, export, …) can use it
// without ANUBIS_API_TOKEN.
//
//	anubis login [--email EMAIL] [--password-stdin]
//
// The password comes from --password-stdin, ANUBIS_PASSWORD, or a prompt.
func loginCommand() {
	email := ""
	passwordFromStdin := false
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--email" || arg == "-e":
			if i+1 < len(args) {
				email = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--email="):
			email = strings.TrimPrefix(arg, "--email=")
		case arg == "--password-stdin":
			passwordFromStdin = true
		case arg == "--help" || arg == "-h":
			fmt.Println(`Usage: anubis login [--email EMAIL] [--password-stdin]

Signs in to the AnubisWatch server and saves the session token for other
commands. The server address comes from ANUBIS_HOST/ANUBIS_PORT or the
config file. The password is read from --password-stdin, ANUBIS_PASSWORD,
or an interactive prompt.`)
			return
		}
	}

	reader := bufio.NewReader(os.Stdin)
	if email == "" {
		email = os.Getenv("ANUBIS_EMAIL")
	}
	if email == "" {
		if cfg := loadCLIConfig(); cfg != nil {
			email = cfg.Auth.Local.AdminEmail
		}
	}
	if email == "" && !passwordFromStdin {
		fmt.Print("Email: ")
		line, _ := reader.ReadString('\n')
		email = strings.TrimSpace(line)
	}
	if email == "" {
		fmt.Fprintln(os.Stderr, "Error: email is required (--email)")
		os.Exit(1)
	}

	var password string
	switch {
	case passwordFromStdin:
		data, err := io.ReadAll(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		password = strings.TrimRight(string(data), "\r\n")
	case os.Getenv("ANUBIS_PASSWORD") != "":
		password = os.Getenv("ANUBIS_PASSWORD")
	default:
		password = promptPassword(reader, fmt.Sprintf("Password for %s: ", email))
	}

	apiURL := getAPIURL()
	token, err := apiLogin(apiURL, email, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path := tokenFilePath()
	if err := saveToken(path, token); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving token: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Logged in to %s as %s (token saved to %s)\n", apiURL, email, path)
}

// logoutCommand ends the saved session and removes the token file.
func logoutCommand() {
	path := tokenFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Not logged in.")
		return
	}
	token := strings.TrimSpace(string(data))
	if resp, err := httpPost(getAPIURL()+"/api/v1/auth/logout", "application/json", nil, token); err == nil {
		resp.Body.Close()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Error removing token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Logged out")
}

func apiLogin(apiURL, email, password string) (string, error) {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return "", err
	}
	resp, err := httpPost(apiURL+"/api/v1/auth/login", "application/json", body, "")
	if err != nil {
		return "", fmt.Errorf("connecting to %s: %w", apiURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return "", fmt.Errorf("login failed: %s", e.Error)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "auth_token" && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", errors.New("login succeeded but the server returned no session token")
}

func saveToken(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// promptPassword reads a line with terminal echo disabled where stty is
// available, falling back to a visible prompt otherwise.
func promptPassword(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	if runtime.GOOS != "windows" {
		stty := exec.CommandContext(context.Background(), "stty", "-echo")
		stty.Stdin = os.Stdin
		if stty.Run() == nil {
			defer func() {
				restore := exec.CommandContext(context.Background(), "stty", "echo")
				restore.Stdin = os.Stdin
				_ = restore.Run()
				fmt.Println()
			}()
		}
	}
	line, _ := reader.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}
