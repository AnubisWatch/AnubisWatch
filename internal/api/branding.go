package api

import (
	"net/http"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// SetBranding installs dashboard.branding, which GET /api/v1/branding serves.
// Call it before the server starts.
func (s *RESTServer) SetBranding(b core.DashboardBranding) {
	s.branding = b
}

// handleBranding serves the dashboard's title, logo and default theme. It is
// public because the login page shows them; branding holds nothing secret.
func (s *RESTServer) handleBranding(ctx *Context) error {
	b := s.branding
	if b.Title == "" {
		b.Title = "AnubisWatch"
	}
	if b.Theme == "" {
		b.Theme = "auto"
	}
	ctx.Response.Header().Set("Cache-Control", "no-cache")
	return ctx.JSON(http.StatusOK, b)
}
