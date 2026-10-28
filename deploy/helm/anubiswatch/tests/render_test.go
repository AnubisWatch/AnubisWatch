package chart_test

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"gopkg.in/yaml.v3"
)

func chartPath(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is not installed; run this target in the chart-validation job")
	}
	_, name, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate chart tests")
	}
	return filepath.Dir(filepath.Dir(name))
}
func render(t *testing.T, overrides ...string) ([]map[string]any, error) {
	t.Helper()
	args := []string{"template", "audit", chartPath(t), "--namespace", "audit", "--set", "secrets.adminPassword=AuditOnly123456!"}
	for _, override := range overrides {
		args = append(args, "--set", override)
	}
	output, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("helm template: %w: %s", err, output)
	}
	var docs []map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(output))
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}
func secureHA(overrides ...string) []string {
	return append([]string{"config.necropolis.enabled=true", "secrets.clusterSecret=chart-test-cluster-secret", "cluster.raftTLSSecret=chart-test-raft-tls", "image.tag=reviewed-ha-candidate"}, overrides...)
}
func document(t *testing.T, docs []map[string]any, kind string) map[string]any {
	t.Helper()
	for _, doc := range docs {
		if doc["kind"] == kind {
			return doc
		}
	}
	t.Fatalf("missing %s", kind)
	return nil
}
func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", value)
	}
	return result
}
func list(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("expected list, got %T", value)
	}
	return result
}
func config(t *testing.T, raw any) *core.Config {
	t.Helper()
	text, ok := raw.(string)
	if !ok {
		t.Fatalf("expected config string, got %T", raw)
	}
	var cfg core.Config
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

func TestStandaloneConfigurationPreserved(t *testing.T) {
	docs, err := render(t, "config.server.metricsAuth=true", "config.server.port=8181", "service.grpcPort=0", "config.server.trustedProxies={192.0.2.1}", "config.storage.path=/data")
	if err != nil {
		t.Fatal(err)
	}
	deployment := document(t, docs, "Deployment")
	if object(t, deployment["spec"])["strategy"].(map[string]any)["type"] != "Recreate" {
		t.Fatal("standalone lost exclusive-writer rollout strategy")
	}
	data := object(t, document(t, docs, "ConfigMap")["data"])
	if len(data) != 1 {
		t.Fatalf("standalone has %d configs", len(data))
	}
	cfg := config(t, data["anubis.yaml"])
	if cfg.Necropolis.Enabled || cfg.Server.Port != 8181 || !cfg.Server.MetricsAuth || cfg.Server.GRPCPort != 0 || cfg.Storage.Path != "/data" || len(cfg.Server.TrustedProxies) != 1 || cfg.Server.TrustedProxies[0] != "192.0.2.1" {
		t.Fatalf("standalone configuration changed: %+v", cfg)
	}
	for _, doc := range docs {
		if doc["kind"] == "StatefulSet" {
			t.Fatal("standalone produced StatefulSet")
		}
	}
}

func TestHAStaticMembershipAndMutualTLS(t *testing.T) {
	for _, tc := range []struct {
		name, fullname, domain string
		overrides              []string
	}{
		{"default DNS", "audit-anubiswatch", "cluster.local", nil},
		{"custom DNS", "custom-watch", "internal.test", []string{"fullnameOverride=custom-watch", "cluster.domain=internal.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs, err := render(t, secureHA(tc.overrides...)...)
			if err != nil {
				t.Fatal(err)
			}
			data := object(t, document(t, docs, "ConfigMap")["data"])
			if len(data) != 3 {
				t.Fatalf("want 3 configs, got %d", len(data))
			}
			for ordinal := 0; ordinal < 3; ordinal++ {
				cfg := config(t, data[fmt.Sprintf("anubis-%d.yaml", ordinal)])
				id := fmt.Sprintf("%s-%d", tc.fullname, ordinal)
				address := fmt.Sprintf("%s.%s-headless.audit.svc.%s:7946", id, tc.fullname, tc.domain)
				raft := cfg.Necropolis.Raft
				if !cfg.Necropolis.Enabled || cfg.Necropolis.NodeName != id || raft.NodeID != id || cfg.Necropolis.AdvertiseAddr != address || raft.AdvertiseAddr != address || raft.Bootstrap || len(raft.Join) != 0 || cfg.Necropolis.Discovery.Mode != "manual" {
					t.Fatalf("unsafe node configuration: %+v", cfg.Necropolis)
				}
				if len(raft.Peers) != 2 {
					t.Fatalf("voter %s has %d peers", id, len(raft.Peers))
				}
				seen := map[string]bool{id: true}
				for _, peer := range raft.Peers {
					if seen[peer.ID] || peer.Role != core.RoleVoter {
						t.Fatalf("duplicate/self/non-voter peer: %+v", peer)
					}
					seen[peer.ID] = true
					if !strings.HasSuffix(peer.Address, fmt.Sprintf(".%s-headless.audit.svc.%s:7946", tc.fullname, tc.domain)) {
						t.Fatalf("wrong peer DNS: %s", peer.Address)
					}
				}
				if raft.TLS == nil || !raft.TLS.VerifyPeers || !raft.TLS.RequireClientCert || raft.TLS.CertFile != "/etc/anubis/raft-tls/tls.crt" || raft.TLS.KeyFile != "/etc/anubis/raft-tls/tls.key" || raft.TLS.CAFile != "/etc/anubis/raft-tls/ca.crt" {
					t.Fatalf("peer mTLS is not fail-closed: %+v", raft.TLS)
				}
				if !cfg.Server.MetricsAuth || cfg.Auth.Type != "local" {
					t.Fatal("HA lost protected metrics/local auth")
				}
			}
			sts := document(t, docs, "StatefulSet")
			spec := object(t, sts["spec"])
			if spec["replicas"] != 3 || spec["podManagementPolicy"] != "Parallel" {
				t.Fatal("quorum formation policy changed")
			}
			pod := object(t, object(t, spec["template"])["spec"])
			if pod["automountServiceAccountToken"] != false {
				t.Fatal("unneeded Kubernetes API token mounted")
			}
			container := object(t, list(t, pod["containers"])[0])
			security := object(t, container["securityContext"])
			if security["runAsNonRoot"] != true || security["readOnlyRootFilesystem"] != true {
				t.Fatal("cluster lost container isolation")
			}
			env := list(t, container["env"])
			nodeAt, configAt := -1, -1
			for i, item := range env {
				v := object(t, item)
				if v["name"] == "ANUBIS_NODE_ID" {
					nodeAt = i
				}
				if v["name"] == "ANUBIS_CONFIG" {
					configAt = i
					if v["value"] != "/etc/anubis/$(ANUBIS_NODE_ID).yaml" {
						t.Fatal("CLI config differs from server config")
					}
				}
			}
			if nodeAt < 0 || configAt <= nodeAt {
				t.Fatal("dependent config env expansion must follow node ID")
			}
			mounts := map[string]bool{}
			for _, item := range list(t, container["volumeMounts"]) {
				m := object(t, item)
				mounts[fmt.Sprint(m["name"])] = true
				if m["name"] == "raft-tls" && m["readOnly"] != true {
					t.Fatal("peer key mount is writable")
				}
			}
			for _, name := range []string{"data", "runtime", "tmp", "raft-tls", "config"} {
				if !mounts[name] {
					t.Fatalf("missing mount %s", name)
				}
			}
			anti := object(t, object(t, pod["affinity"])["podAntiAffinity"])
			required := list(t, anti["requiredDuringSchedulingIgnoredDuringExecution"])
			if len(required) != 1 || object(t, required[0])["topologyKey"] != "kubernetes.io/hostname" {
				t.Fatal("voters can share a default failure domain")
			}
			if len(list(t, spec["volumeClaimTemplates"])) != 1 {
				t.Fatal("voters must own separate data claims")
			}
		})
	}
}

func TestHARejectsUnsafeConfigurations(t *testing.T) {
	for _, tc := range []struct{ name, override, want string }{
		{"no peer certificate", "cluster.raftTLSSecret=", "cluster.raftTLSSecret"},
		{"missing cluster secret", "secrets.clusterSecret=", "secrets.clusterSecret"},
		{"two voters", "statefulSet.replicas=2", "statefulSet.replicas must be 3"},
		{"four voters", "statefulSet.replicas=4", "statefulSet.replicas must be 3"},
		{"ephemeral data", "persistence.enabled=false", "persistence.enabled"},
		{"autoscaling", "autoscaling.enabled=true", "autoscaling.enabled"},
		{"ordered startup", "statefulSet.podManagementPolicy=OrderedReady", "Parallel"},
		{"unvalidated auth", "config.auth.type=oidc", "config.auth.type must be local"},
		{"implicit old image", "image.tag=", "explicit image.tag"},
		{"latest image", "image.tag=latest", "explicit image.tag"},
		{"invalid digest", "image.digest=sha256:bad", "image.digest"},
		{"invalid domain", "cluster.domain=not_a_domain", "cluster.domain"},
		{"oversized DNS label", "fullnameOverride=" + strings.Repeat("a", 55), "54 characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := render(t, secureHA(tc.override)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want rejection containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestImmutableImageReference(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	docs, err := render(t, secureHA("image.tag=", "image.digest="+digest)...)
	if err != nil {
		t.Fatal(err)
	}
	pod := object(t, object(t, object(t, document(t, docs, "StatefulSet")["spec"])["template"])["spec"])
	if got := object(t, list(t, pod["containers"])[0])["image"]; got != "ghcr.io/anubiswatch/anubiswatch@"+digest {
		t.Fatalf("digest was not used: %v", got)
	}
}

func TestStandaloneConfigurationChangesRestartPod(t *testing.T) {
	original, err := render(t)
	if err != nil {
		t.Fatal(err)
	}
	originalPod := object(t, document(t, original, "Deployment")["spec"])["template"]
	control, err := render(t, "image.tag=other-reviewed-image")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(originalPod, object(t, document(t, control, "Deployment")["spec"])["template"]) {
		t.Fatal("invalid control: changing the image must change the pod template")
	}
	for _, override := range []string{"config.server.metricsAuth=false", "secrets.adminPassword=OtherAudit123456!"} {
		changed, err := render(t, override)
		if err != nil {
			t.Fatal(err)
		}
		changedPod := object(t, document(t, changed, "Deployment")["spec"])["template"]
		if reflect.DeepEqual(originalPod, changedPod) {
			t.Errorf("EXPECTED: %s changes pod template; ACTUAL: unchanged; PROBLEM CONFIRMED", override)
		}
	}
}

// With secrets.existingSecret the chart creates no Secret and every
// credential env var reads the operator's Secret, so no password passes
// through Helm values.
func TestExistingSecretReplacesChartSecret(t *testing.T) {
	docs, err := render(t, "secrets.existingSecret=ops-managed", "config.storage.encryption.enabled=true")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if doc["kind"] == "Secret" {
			t.Fatalf("chart rendered its own Secret %v despite existingSecret", object(t, doc["metadata"])["name"])
		}
	}
	spec := object(t, object(t, object(t, document(t, docs, "Deployment")["spec"])["template"])["spec"])
	container := object(t, list(t, spec["containers"])[0])
	refs := map[string]string{}
	for _, raw := range list(t, container["env"]) {
		env := object(t, raw)
		if from, ok := env["valueFrom"].(map[string]any); ok {
			if ref, ok := from["secretKeyRef"].(map[string]any); ok {
				refs[env["name"].(string)] = ref["name"].(string)
			}
		}
	}
	for _, name := range []string{"ANUBIS_ADMIN_PASSWORD", "ANUBIS_ENCRYPTION_KEY"} {
		if refs[name] != "ops-managed" {
			t.Errorf("%s reads Secret %q, want ops-managed", name, refs[name])
		}
	}
	if _, ok := refs["ANUBIS_CLUSTER_SECRET"]; ok {
		t.Error("standalone pod reads cluster-secret")
	}
}

// The self-monitoring rules target only this release's main Service, and the
// leader rule exists only in cluster mode.
func TestPrometheusRuleSelfMonitoring(t *testing.T) {
	alerts := func(docs []map[string]any) map[string]string {
		rule := document(t, docs, "PrometheusRule")
		out := map[string]string{}
		for _, g := range list(t, object(t, rule["spec"])["groups"]) {
			for _, r := range list(t, object(t, g)["rules"]) {
				rr := object(t, r)
				out[rr["alert"].(string)] = rr["expr"].(string)
			}
		}
		return out
	}
	docs, err := render(t, "monitoring.enabled=true", "monitoring.prometheusRule.enabled=true")
	if err != nil {
		t.Fatal(err)
	}
	got := alerts(docs)
	for _, name := range []string{"AnubisWatchDown", "AnubisWatchSchedulerStalled", "AnubisWatchReplicationDropping"} {
		if !strings.Contains(got[name], `service="audit-anubiswatch"`) {
			t.Errorf("%s expr %q does not select the release's Service", name, got[name])
		}
	}
	if _, ok := got["AnubisWatchNoLeader"]; ok {
		t.Error("standalone release has a cluster-leader alert")
	}

	docs, err = render(t, secureHA("monitoring.enabled=true", "monitoring.prometheusRule.enabled=true")...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := alerts(docs)["AnubisWatchNoLeader"]; !ok {
		t.Error("cluster release lacks AnubisWatchNoLeader")
	}
}
