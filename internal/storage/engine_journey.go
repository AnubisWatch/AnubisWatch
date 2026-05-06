package storage

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Journey NoCtx wrappers

func (db *CobaltDB) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	results, err := db.PrefixScan("")
	if err != nil {
		return nil, err
	}

	for key, data := range results {
		if !strings.HasSuffix(key, "/journeys/"+id) {
			continue
		}
		var journey core.JourneyConfig
		if err := json.Unmarshal(data, &journey); err != nil {
			continue
		}
		return &journey, nil
	}

	return nil, &core.NotFoundError{Entity: "journey", ID: id}
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
