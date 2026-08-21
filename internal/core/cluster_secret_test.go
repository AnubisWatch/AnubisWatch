package core

import (
	"strings"
	"testing"
)

// K9: gossip falls back to accepting unsigned messages when no cluster secret
// is configured, so anyone who can reach the gossip port could inject peers.
// Clustering must refuse to start rather than run unauthenticated.
func TestValidate_ClusterSecretRequiredWhenClusteringEnabled(t *testing.T) {
	tests := []struct {
		name      string
		necro     NecropolisConfig
		wantError bool
	}{
		{
			name:      "gossip discovery without a secret is rejected",
			necro:     NecropolisConfig{Enabled: true, Discovery: DiscoveryConfig{Mode: "mdns"}},
			wantError: true,
		},
		{
			name:      "whitespace-only secret is rejected",
			necro:     NecropolisConfig{Enabled: true, ClusterSecret: "   ", Discovery: DiscoveryConfig{Mode: "gossip"}},
			wantError: true,
		},
		{
			name:      "gossip discovery with a secret is accepted",
			necro:     NecropolisConfig{Enabled: true, ClusterSecret: "shared-secret", Discovery: DiscoveryConfig{Mode: "mdns"}},
			wantError: false,
		},
		{
			name:      "manual discovery never gossips, so no secret is needed",
			necro:     NecropolisConfig{Enabled: true, Discovery: DiscoveryConfig{Mode: "manual"}},
			wantError: false,
		},
		{
			// setDefaults fills an unset mode with "mdns", so the default
			// clustered deployment gossips and does need a secret.
			name:      "unset discovery mode defaults to mdns and requires a secret",
			necro:     NecropolisConfig{Enabled: true},
			wantError: true,
		},
		{
			name:      "single-node never gossips, so no secret is needed",
			necro:     NecropolisConfig{Enabled: true, SingleNode: true, Discovery: DiscoveryConfig{Mode: "mdns"}},
			wantError: false,
		},
		{
			name:      "clustering disabled needs no secret",
			necro:     NecropolisConfig{Enabled: false, Discovery: DiscoveryConfig{Mode: "mdns"}},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Config
			c.Necropolis = tt.necro

			err := c.validate()
			gotSecretError := err != nil && strings.Contains(err.Error(), "cluster_secret")

			if tt.wantError && !gotSecretError {
				t.Fatalf("expected a cluster_secret validation error, got %v", err)
			}
			if !tt.wantError && gotSecretError {
				t.Fatalf("unexpected cluster_secret validation error: %v", err)
			}
		})
	}
}
