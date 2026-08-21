package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func summonNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis summon <address> [--name <node-id>] [--region <region>]\n")
		os.Exit(1)
	}

	addr := os.Args[2]
	nodeID := ""
	region := "default"

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				nodeID = os.Args[i+1]
				i++
			}
		case "--region", "-r":
			if i+1 < len(os.Args) {
				region = os.Args[i+1]
				i++
			}
		}
	}

	// Generate node ID from address if not provided
	if nodeID == "" {
		nodeID = strings.ReplaceAll(addr, ":", "-")
		nodeID = strings.ReplaceAll(nodeID, ".", "-")
	}

	fmt.Printf("⚖️  Summoning Jackal at %s...\n", addr)

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	if token != "" {
		reqBody := fmt.Sprintf(`{"node_id": "%s", "address": "%s", "region": "%s"}`, nodeID, addr, region)
		resp, err := httpPost(apiURL+"/api/v1/cluster/nodes", "application/json", []byte(reqBody), token)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated) {
			resp.Body.Close()
			fmt.Printf("✓ Node '%s' added to cluster successfully via API\n", nodeID)
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// Fall back to direct storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SaveJackal(ctx, nodeID, addr, region); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Node '%s' added to cluster configuration\n", nodeID)
	fmt.Println("\nNote: Cluster changes require a server restart to take full effect.")
}

func banishNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis banish <node-id>\n")
		os.Exit(1)
	}

	nodeID := os.Args[2]
	fmt.Printf("⚖️  Banishing Jackal %s...\n", nodeID)

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	if token != "" {
		req, err := http.NewRequestWithContext(context.Background(), "DELETE", apiURL+"/api/v1/cluster/nodes/"+nodeID, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent) {
				resp.Body.Close()
				fmt.Printf("✓ Node '%s' removed from cluster via API\n", nodeID)
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}

	// Fall back to direct storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Delete the node from storage
	key := fmt.Sprintf("system/jackals/%s", nodeID)
	if err := store.Delete(key); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Node '%s' removed from cluster configuration\n", nodeID)
	fmt.Println("\nNote: Cluster changes require a server restart to take full effect.")
}

func showCluster() {
	fmt.Println("⚖️  AnubisWatch Necropolis — Cluster Status")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	var clusterData map[string]interface{}
	useAPI := false

	if token != "" {
		resp, err := httpGet(apiURL+"/api/v1/cluster/status", token)
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&clusterData); err == nil {
				useAPI = true
			}
			resp.Body.Close()
		}
	}

	if useAPI {
		// Display API data
		isClustered, _ := clusterData["is_clustered"].(bool)
		nodeID, _ := clusterData["node_id"].(string)
		state, _ := clusterData["state"].(string)
		leader, _ := clusterData["leader"].(string)
		term, _ := clusterData["term"].(float64)
		peerCount, _ := clusterData["peer_count"].(float64)

		fmt.Printf("%-16s %s\n", "Raft State:", state)
		fmt.Printf("%-16s %s\n", "Current Node:", nodeID)
		fmt.Printf("%-16s %s\n", "Current Leader:", leader)
		fmt.Printf("%-16s %.0f\n", "Term:", term)
		fmt.Printf("%-16s %.0f\n", "Peer Count:", peerCount)
		fmt.Printf("%-16s %v\n", "Clustered:", isClustered)
	} else {
		// Fall back to storage
		store, err := openLocalStorage()
		if err != nil {
			fmt.Println("Raft State:     Standalone")
			fmt.Println("Current Leader: this node")
			fmt.Println("Term:           1")
			fmt.Println("Nodes:          1")
			fmt.Println()
			fmt.Println("Jackals:")
			fmt.Println("  ID              Region    Address           Role")
			fmt.Println("  ──────────────  ────────  ────────────────  ────────")
			fmt.Println("  (this node)     default   localhost:7946    Pharaoh")
			return
		}
		defer store.Close()

		ctx := context.Background()
		jackals, err := store.ListJackals(ctx)
		if err != nil {
			jackals = make(map[string]map[string]string)
		}

		// Get Raft state if available
		currentTerm, votedFor, _ := store.GetRaftState(ctx)
		if currentTerm == 0 {
			currentTerm = 1
		}

		nodeCount := len(jackals) + 1 // +1 for this node

		fmt.Printf("%-16s %s\n", "Raft State:", "Standalone")
		fmt.Printf("%-16s %d\n", "Term:", currentTerm)
		fmt.Printf("%-16s %d\n", "Nodes:", nodeCount)
		if votedFor != "" {
			fmt.Printf("%-16s %s\n", "Voted For:", votedFor)
		}
		fmt.Println()
		fmt.Println("Jackals:")
		fmt.Println("  ID              Region    Address           Role")
		fmt.Println("  ──────────────  ────────  ────────────────  ────────")

		// Show this node
		fmt.Printf("  %-14s  %-8s  %-16s  %-8s\n", "(this node)", "default", "localhost:7946", "Pharaoh")

		// Show other nodes
		for id, node := range jackals {
			region := node["region"]
			if region == "" {
				region = "default"
			}
			address := node["address"]
			if address == "" {
				address = "unknown"
			}
			fmt.Printf("  %-14s  %-8s  %-16s  %-8s\n", truncate(id, 14), region, address, "Jackal")
		}
	}

	fmt.Println()
	fmt.Println("Use 'anubis summon <address>' to add nodes to the cluster.")
}
