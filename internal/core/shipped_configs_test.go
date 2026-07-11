package core

import (
	"path/filepath"
	"testing"
)

// TestContainerConfigValidates guards the config baked into the Docker image
// (configs/container.anubis.json), which the image runs by default via
// `serve --single`. It must load and validate when given the environment the
// container runtime supplies (an admin password). This is a regression test
// for a crash-loop where the baked config declared environment "production"
// with TLS disabled — a combination validate() rejects — so every
// `docker run` of the image exited immediately on startup.
func TestContainerConfigValidates(t *testing.T) {
	// The container/compose runtime supplies the admin password; local auth
	// requires it. TLS is terminated upstream (environment "production-proxied").
	t.Setenv("ANUBIS_ADMIN_PASSWORD", "Str0ng-Passw0rd!42")

	path := filepath.Join("..", "..", "configs", "container.anubis.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("baked container config failed to load/validate: %v", err)
	}

	// Guard the specific invariant: a plaintext listener must not be labelled
	// the exact "production" environment, or the production TLS gate would
	// reject it at startup.
	if !cfg.Server.TLS.Enabled && cfg.Environment == "production" {
		t.Fatalf("container config would crash-loop: environment=%q with TLS disabled", cfg.Environment)
	}
}
