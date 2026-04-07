package storage

import (
	"context"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Journey NoCtx wrappers

func (db *CobaltDB) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	return db.GetJourney(context.Background(), "default", id)
}

func (db *CobaltDB) ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error) {
	return db.ListJourneys(context.Background(), workspace)
}

func (db *CobaltDB) SaveJourneyNoCtx(journey *core.JourneyConfig) error {
	return db.SaveJourney(context.Background(), journey)
}

func (db *CobaltDB) DeleteJourneyNoCtx(id string) error {
	return db.DeleteJourney(context.Background(), "default", id)
}
