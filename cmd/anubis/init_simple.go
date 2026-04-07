package main

import (
	"fmt"
	"os"
)

// initSimpleWithPath creates simple config at specific path
func initSimpleWithPath(configPath string) {
	opts := ConfigOptions{
		Host:            "0.0.0.0",
		HTTPPort:        findAvailablePort(8080),
		EnableTLS:       false,
		AdminEmail:      "admin@anubis.watch",
		AdminPassword:   "admin",
		DataDir:         getDefaultDataDir(),
		RetentionDays:   90,
		EnableDashboard: true,
		DashboardTheme:  "dark",
		LogLevel:        "info",
		LogFormat:       "json",
	}

	config := generateConfig(opts)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Created config: %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  ANUBIS_CONFIG=%s anubis serve\n", configPath)
	fmt.Println("     OR")
	fmt.Println("  anubis serve   (if in same directory)")
	fmt.Println()
	fmt.Printf("Dashboard: http://localhost:%d\n", opts.HTTPPort)
	fmt.Println("Login: admin@anubis.watch / admin")
}
