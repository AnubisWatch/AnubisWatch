package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// initInteractive runs interactive configuration wizard
func initInteractive() {
	configPath := "../anubis.json"
	// Check for --output flag
	for i, arg := range os.Args {
		if (arg == "--output" || arg == "-o") && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			break
		}
	}
	initInteractiveWithPath(configPath)
}

// initInteractiveWithPath runs interactive wizard with specific config path
func initInteractiveWithPath(configPath string) {
	fmt.Print(`
╔════════════════════════════════════════════════════════════════╗
║                                                                ║
║   ⚖️  AnubisWatch — The Judgment Never Sleeps                  ║
║                                                                ║
║   Interactive Setup Wizard                                     ║
║                                                                ║
╚════════════════════════════════════════════════════════════════╝
`)

	reader := bufio.NewReader(os.Stdin)

	// Detect available ports
	defaultHTTPPort := findAvailablePort(8080)
	raftPort := findAvailablePort(7946)

	fmt.Println("📡 Server Configuration")
	fmt.Println("────────────────────────")

	// HTTP Port
	httpPort := askInt(reader, "HTTP Server Port", defaultHTTPPort)
	if httpPort == 0 {
		httpPort = 8080
	}

	// Host
	host := askString(reader, "Bind Host", "0.0.0.0")

	// TLS
	enableTLS := askBool(reader, "Enable TLS/HTTPS", false)
	tlsAuto := false
	tlsCert := ""
	tlsKey := ""
	acmeEmail := ""

	if enableTLS {
		tlsAuto = askBool(reader, "Use Let's Encrypt (Auto HTTPS)", true)
		if tlsAuto {
			acmeEmail = askString(reader, "ACME Email (for Let's Encrypt)", "")
		} else {
			tlsCert = askString(reader, "TLS Certificate Path", "")
			tlsKey = askString(reader, "TLS Private Key Path", "")
		}
	}

	fmt.Println()
	fmt.Println("🔐 Authentication")
	fmt.Println("─────────────────")

	// Admin credentials
	adminEmail := askString(reader, "Admin Email", "admin@anubis.watch")
	adminPass := askPassword(reader, "Admin Password")
	if adminPass == "" {
		adminPass = generateSecurePassword()
		fmt.Printf("   Generated password: %s\n", adminPass)
	}

	fmt.Println()
	fmt.Println("💾 Storage")
	fmt.Println("──────────")

	// Data directory
	defaultDataDir := getDefaultDataDir()
	dataDir := askString(reader, "Data Directory", defaultDataDir)

	// Retention
	retentionDays := askInt(reader, "Data Retention (days)", 90)

	// Encryption
	enableEncryption := askBool(reader, "Enable Encryption", false)
	encryptionKey := ""
	if enableEncryption {
		encryptionKey = askPassword(reader, "Encryption Key")
		if encryptionKey == "" {
			encryptionKey = generateSecurePassword()
			fmt.Printf("   Generated key: %s\n", encryptionKey)
		}
	}

	fmt.Println()
	fmt.Println("🏛️  Cluster (Necropolis)")
	fmt.Println("─────────────────────────")

	enableCluster := askBool(reader, "Enable Cluster Mode", false)
	nodeName := "jackal-1"
	region := "default"
	clusterSecret := ""
	bootstrap := true

	if enableCluster {
		nodeName = askString(reader, "Node Name", "jackal-"+randomSuffix())
		region = askString(reader, "Region", "default")
		raftPort = askInt(reader, "Raft Port", raftPort)
		bootstrap = askBool(reader, "Bootstrap Cluster (first node)", true)
		if !bootstrap {
			fmt.Println("   You'll need to join an existing cluster with 'anubis summon'")
		}
		clusterSecret = askPassword(reader, "Cluster Secret (optional)")
	}

	fmt.Println()
	fmt.Println("📊 Dashboard")
	fmt.Println("───────────")

	enableDashboard := askBool(reader, "Enable Dashboard", true)
	dashboardTheme := "dark"
	if enableDashboard {
		theme := askChoice(reader, "Theme", []string{"dark", "light", "auto"}, "dark")
		dashboardTheme = theme
	}

	fmt.Println()
	fmt.Println("📝 Logging")
	fmt.Println("─────────")

	logLevel := askChoice(reader, "Log Level", []string{"debug", "info", "warn", "error"}, "info")
	logFormat := askChoice(reader, "Log Format", []string{"json", "text"}, "json")

	// Generate config
	config := generateConfig(ConfigOptions{
		Host:             host,
		HTTPPort:         httpPort,
		EnableTLS:        enableTLS,
		TLSAuto:          tlsAuto,
		TLSCert:          tlsCert,
		TLSKey:           tlsKey,
		ACMEEmail:        acmeEmail,
		AdminEmail:       adminEmail,
		AdminPassword:    adminPass,
		DataDir:          dataDir,
		RetentionDays:    retentionDays,
		EnableEncryption: enableEncryption,
		EncryptionKey:    encryptionKey,
		EnableCluster:    enableCluster,
		NodeName:         nodeName,
		Region:           region,
		RaftPort:         raftPort,
		Bootstrap:        bootstrap,
		ClusterSecret:    clusterSecret,
		EnableDashboard:  enableDashboard,
		DashboardTheme:   dashboardTheme,
		LogLevel:         logLevel,
		LogFormat:        logFormat,
	})

	// Write config file
	configPath = "anubis.json"
	if _, err := os.Stat(configPath); err == nil {
		overwrite := askBool(reader, fmt.Sprintf("%s already exists. Overwrite?", configPath), false)
		if !overwrite {
			configPath = askString(reader, "New config filename", "anubis-new.json")
		}
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ✅ Configuration Complete!                                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📄 Config file: %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Review the configuration in %s\n", configPath)
	fmt.Println("  2. Run 'anubis serve' to start the server")
	fmt.Printf("  3. Access the dashboard at http://localhost:%d\n", httpPort)
	fmt.Println()
	fmt.Printf("   Login: %s / %s\n", adminEmail, maskPassword(adminPass))
	if enableCluster {
		fmt.Println()
		fmt.Println("🔗 Cluster Information:")
		fmt.Printf("   Node: %s\n", nodeName)
		fmt.Printf("   Region: %s\n", region)
		fmt.Printf("   Raft Port: %d\n", raftPort)
	}
	fmt.Println()
}

// ConfigOptions holds configuration options
type ConfigOptions struct {
	Host             string
	HTTPPort         int
	EnableTLS        bool
	TLSAuto          bool
	TLSCert          string
	TLSKey           string
	ACMEEmail        string
	AdminEmail       string
	AdminPassword    string
	DataDir          string
	RetentionDays    int
	EnableEncryption bool
	EncryptionKey    string
	EnableCluster    bool
	NodeName         string
	Region           string
	RaftPort         int
	Bootstrap        bool
	ClusterSecret    string
	EnableDashboard  bool
	DashboardTheme   string
	LogLevel         string
	LogFormat        string
}

// generateConfig generates the JSON config content
func generateConfig(opts ConfigOptions) string {
	tlsConfig := "false"
	if opts.EnableTLS {
		if opts.TLSAuto {
			tlsConfig = fmt.Sprintf(`{
      "enabled": true,
      "auto_cert": true,
      "acme_email": %q
    }`, opts.ACMEEmail)
		} else {
			tlsConfig = fmt.Sprintf(`{
      "enabled": true,
      "cert": %q,
      "key": %q
    }`, opts.TLSCert, opts.TLSKey)
		}
	}

	encryptionConfig := "false"
	if opts.EnableEncryption {
		encryptionConfig = fmt.Sprintf(`{
      "enabled": true,
      "key": %q
    }`, opts.EncryptionKey)
	}

	clusterConfig := "false"
	if opts.EnableCluster {
		clusterConfig = fmt.Sprintf(`{
      "enabled": true,
      "node_name": %q,
      "region": %q,
      "cluster_secret": %q,
      "raft": {
        "bootstrap": %t,
        "bind_addr": "0.0.0.0:%d"
      }
    }`, opts.NodeName, opts.Region, opts.ClusterSecret, opts.Bootstrap, opts.RaftPort)
	}

	dashboardConfig := "false"
	if opts.EnableDashboard {
		dashboardConfig = fmt.Sprintf(`{
      "enabled": true,
      "theme": %q
    }`, opts.DashboardTheme)
	}

	return fmt.Sprintf(`{
  "server": {
    "host": %q,
    "port": %d,
    "tls": %s
  },
  "storage": {
    "path": %q,
    "retention_days": %d,
    "encryption": %s
  },
  "auth": {
    "enabled": true,
    "type": "local",
    "local": {
      "admin_email": %q,
      "admin_password": %q
    }
  },
  "necropolis": %s,
  "dashboard": %s,
  "logging": {
    "level": %q,
    "format": %q
  }
}
`, opts.Host, opts.HTTPPort, tlsConfig, opts.DataDir, opts.RetentionDays,
		encryptionConfig, opts.AdminEmail, opts.AdminPassword,
		clusterConfig, dashboardConfig, opts.LogLevel, opts.LogFormat)
}

// Interactive helpers

func askString(reader *bufio.Reader, prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("   %s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("   %s: ", prompt)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func askInt(reader *bufio.Reader, prompt string, defaultVal int) int {
	fmt.Printf("   %s [%d]: ", prompt, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(input)
	if err != nil {
		fmt.Println("   Invalid number, using default")
		return defaultVal
	}
	return val
}

func askBool(reader *bufio.Reader, prompt string, defaultVal bool) bool {
	defaultStr := "n"
	if defaultVal {
		defaultStr = "y"
	}
	fmt.Printf("   %s [y/N] [%s]: ", prompt, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultVal
	}
	return input == "y" || input == "yes"
}

func askChoice(reader *bufio.Reader, prompt string, choices []string, defaultVal string) string {
	fmt.Printf("   %s [", prompt)
	for i, c := range choices {
		if i > 0 {
			fmt.Print("/")
		}
		if c == defaultVal {
			fmt.Printf("%s", strings.ToUpper(c))
		} else {
			fmt.Printf("%s", c)
		}
	}
	fmt.Printf("]: ")
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultVal
	}
	for _, c := range choices {
		if strings.HasPrefix(strings.ToLower(c), input) {
			return c
		}
	}
	return defaultVal
}

func askPassword(reader *bufio.Reader, prompt string) string {
	fmt.Printf("   %s (leave empty for auto-generate): ", prompt)
	// Note: In production, use terminal.ReadPassword for hidden input
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// Utility functions

func findAvailablePort(startPort int) int {
	for port := startPort; port < startPort+100; port++ {
		if !isPortInUse(port) {
			return port
		}
	}
	return startPort
}

func isPortInUse(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

func getDefaultDataDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("LOCALAPPDATA")
		}
		if appData == "" {
			return `C:\ProgramData\AnubisWatch`
		}
		return filepath.Join(appData, "AnubisWatch")
	case "darwin":
		home := os.Getenv("HOME")
		return filepath.Join(home, "Library", "Application Support", "AnubisWatch")
	default: // Linux and others
		if os.Getuid() == 0 {
			return "/var/lib/anubis"
		}
		home := os.Getenv("HOME")
		return filepath.Join(home, ".anubis")
	}
}

func generateSecurePassword() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	length := 16
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		time.Sleep(1 * time.Nanosecond)
	}
	return string(result)
}

func randomSuffix() string {
	return fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
}

func maskPassword(pass string) string {
	if len(pass) <= 4 {
		return "****"
	}
	return pass[:2] + strings.Repeat("*", len(pass)-4) + pass[len(pass)-2:]
}
