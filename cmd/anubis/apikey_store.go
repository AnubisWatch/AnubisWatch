package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// API keys live under the workspace prefix like every other workspace
// resource: {workspace}/apikeys/{id} holds the record (secret hash only) and
// {workspace}/apikey-usage/{id} the throttled last-use time, kept apart so a
// usage write can never undo a revocation. In cluster mode both are written
// through Raft via applyMutation, the same path status pages and maintenance
// windows take, so every node authenticates and revokes the same keys.

func apiKeyRecordKey(workspace, id string) string {
	return fmt.Sprintf("%s/apikeys/%s", workspace, id)
}

func apiKeyUsageKey(workspace, id string) string {
	return fmt.Sprintf("%s/apikey-usage/%s", workspace, id)
}

func validAPIKeyStorageID(id string) bool {
	_, err := core.ParseAPIKey(core.APIKeyPrefix + id + "_x")
	return err == nil
}

func (a *restStorageAdapter) putReplicated(key string, value []byte) error {
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: value})
	}
	return a.store.Put(key, value)
}

// SaveAPIKey implements auth.APIKeyStore.
func (a *restStorageAdapter) SaveAPIKey(key *core.APIKey) error {
	if key == nil || !validAPIKeyStorageID(key.ID) || key.WorkspaceID == "" || strings.Contains(key.WorkspaceID, "/") {
		return &core.ValidationError{Field: "api_key", Message: "invalid API key record"}
	}
	stored := *key
	stored.LastUsedAt = nil // kept in the usage record
	data, err := json.Marshal(&stored)
	if err != nil {
		return err
	}
	return a.putReplicated(apiKeyRecordKey(key.WorkspaceID, key.ID), data)
}

// GetAPIKey implements auth.APIKeyStore. Workspaces are few, so the id is
// looked up in each rather than kept in a secondary index that would also
// need rebuilding on every node.
func (a *restStorageAdapter) GetAPIKey(id string) (*core.APIKey, error) {
	if !validAPIKeyStorageID(id) {
		return nil, &core.NotFoundError{Entity: "api_key", ID: id}
	}
	workspaces := a.store.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}
	for _, ws := range workspaces {
		raw, err := a.store.Get(apiKeyRecordKey(ws, id))
		if err != nil {
			var nf *core.NotFoundError
			if errors.As(err, &nf) {
				continue
			}
			return nil, err
		}
		var key core.APIKey
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, fmt.Errorf("decode API key %s: %w", id, err)
		}
		if key.ID != id || key.WorkspaceID != ws {
			continue
		}
		a.attachAPIKeyUsage(&key)
		return &key, nil
	}
	return nil, &core.NotFoundError{Entity: "api_key", ID: id}
}

// ListAPIKeys implements auth.APIKeyStore.
func (a *restStorageAdapter) ListAPIKeys(workspace string) ([]*core.APIKey, error) {
	if workspace == "" || strings.Contains(workspace, "/") {
		return []*core.APIKey{}, nil
	}
	results, err := a.store.PrefixScan(workspace + "/apikeys/")
	if err != nil {
		return nil, err
	}
	keys := make([]*core.APIKey, 0, len(results))
	for _, raw := range results {
		if raw == nil {
			continue
		}
		var key core.APIKey
		if err := json.Unmarshal(raw, &key); err != nil || key.WorkspaceID != workspace {
			continue
		}
		a.attachAPIKeyUsage(&key)
		keys = append(keys, &key)
	}
	return keys, nil
}

// RecordAPIKeyUse implements auth.APIKeyStore.
func (a *restStorageAdapter) RecordAPIKeyUse(key *core.APIKey, at time.Time) error {
	if key == nil || !validAPIKeyStorageID(key.ID) {
		return nil
	}
	data, err := json.Marshal(at.UTC())
	if err != nil {
		return err
	}
	return a.putReplicated(apiKeyUsageKey(key.WorkspaceID, key.ID), data)
}

func (a *restStorageAdapter) attachAPIKeyUsage(key *core.APIKey) {
	raw, err := a.store.Get(apiKeyUsageKey(key.WorkspaceID, key.ID))
	if err != nil {
		return
	}
	var at time.Time
	if json.Unmarshal(raw, &at) == nil && !at.IsZero() {
		key.LastUsedAt = &at
	}
}
