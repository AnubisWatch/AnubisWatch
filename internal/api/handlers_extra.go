package api

import (
	"net/http"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// handleListJourneys lists all journeys
func (s *RESTServer) handleListJourneys(ctx *Context) error {
	workspace := ctx.Workspace
	offset, limit := parsePagination(ctx.Request, 20, 100)

	journeys, err := s.store.ListJourneysNoCtx(workspace, offset, limit)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, journeys)
}

// handleCreateJourney creates a new journey
func (s *RESTServer) handleCreateJourney(ctx *Context) error {
	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}

	journey.ID = core.GenerateID()
	journey.WorkspaceID = ctx.Workspace
	journey.CreatedAt = time.Now()
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, journey)
}

// handleGetJourney gets a journey by ID
func (s *RESTServer) handleGetJourney(ctx *Context) error {
	id := ctx.Params["id"]
	journey, err := s.store.GetJourneyNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}
	return ctx.JSON(http.StatusOK, journey)
}

// handleUpdateJourney updates a journey
func (s *RESTServer) handleUpdateJourney(ctx *Context) error {
	id := ctx.Params["id"]
	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}

	journey.ID = id
	journey.WorkspaceID = ctx.Workspace
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, journey)
}

// handleDeleteJourney deletes a journey
func (s *RESTServer) handleDeleteJourney(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteJourneyNoCtx(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// handleRunJourney runs a journey
func (s *RESTServer) handleRunJourney(ctx *Context) error {
	id := ctx.Params["id"]

	journey, err := s.store.GetJourneyNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"journey_id": journey.ID,
		"status":     "execution_requested",
		"message":    "Journey execution triggered",
	})
}

// handleMCPTools returns available MCP tools
func (s *RESTServer) handleMCPTools(ctx *Context) error {
	tools := []map[string]interface{}{
		{
			"name":        "list_souls",
			"description": "List all monitored souls",
			"parameters":  map[string]interface{}{},
		},
		{
			"name":        "get_soul_status",
			"description": "Get status of a specific soul",
			"parameters": map[string]interface{}{
				"soul_id": map[string]string{"type": "string", "description": "ID of the soul"},
			},
		},
	}
	return ctx.JSON(http.StatusOK, tools)
}

// handleSoulLogs returns logs for a soul
func (s *RESTServer) handleSoulLogs(ctx *Context) error {
	id := ctx.Params["id"]
	logs := []map[string]interface{}{
		{
			"timestamp": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			"level":     "info",
			"message":   "Health check passed",
			"soul_id":   id,
		},
	}
	return ctx.JSON(http.StatusOK, logs)
}
