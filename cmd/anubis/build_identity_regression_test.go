package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRuntimeMetricsUsesLinkedBuildIdentity(t *testing.T) {
	old := Version
	Version = "candidate-audit-identity"
	defer func() { Version = old }()
	port := freeLoopbackPort(t)
	config := writeServerConfig(t, t.TempDir(), port, "audit@example.test", "Fixture-Password47!")
	s := bootAuthServer(t, config, port)
	defer s.stop(t)
	response, err := http.Get(s.baseURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !strings.Contains(string(raw), "anubis_memory_alloc_bytes") {
		t.Fatal("invalid metrics control")
	}
	fmt.Println("CONTROL EXPECTED: metrics functional; ACTUAL: HTTP200 and memory metric")
	want := `version="` + Version + `"`
	fmt.Printf("EXPECTED: %s in build metric; ACTUAL: matched=%v\n", want, strings.Contains(string(raw), want))
	if !strings.Contains(string(raw), want) {
		t.Fatal("PROBLEM CONFIRMED")
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
}
