package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Journey NoCtx wrappers

func (db *CobaltDB) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	// O(1) lookup via secondary index. db.mu.RLock pairs with
	// the writer in SaveJourney (storage.go) and rebuildSecondaryIndexes
	// (engine.go) — both take db.mu.Lock when mutating the map.
	db.mu.RLock()
	workspaceID, ok := db.journeyIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "journey", ID: id}
	}

	key := fmt.Sprintf("%s/journeys/%s", workspaceID, id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var journey core.JourneyConfig
	if err := json.Unmarshal(data, &journey); err != nil {
		return nil, err
	}

	return &journey, nil
}

func (db *CobaltDB) ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error) {
	journeys, err := db.ListJourneys(context.Background(), workspace)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(journeys) {
		return []*core.JourneyConfig{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(journeys) {
		end = len(journeys)
	}
	return journeys[offset:end], nil
}

func (db *CobaltDB) SaveJourneyNoCtx(journey *core.JourneyConfig) error {
	return db.SaveJourney(context.Background(), journey)
}

func (db *CobaltDB) DeleteJourneyNoCtx(id string) error {
	journey, err := db.GetJourneyNoCtx(id)
	if err != nil {
		return err
	}
	return db.DeleteJourney(context.Background(), defaultWorkspace(journey.WorkspaceID), id)
}
