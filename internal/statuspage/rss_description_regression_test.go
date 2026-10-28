package statuspage

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type rssDescriptionRepository struct {
	mockRepository
	soulName string
}

func (r *rssDescriptionRepository) GetSoul(id string) (*core.Soul, error) {
	return &core.Soul{ID: id, Name: r.soulName, Enabled: true}, nil
}

func (r *rssDescriptionRepository) GetIncidentsByPage(string) ([]core.Incident, error) {
	return []core.Incident{{
		ID:        "incident-1",
		SoulID:    "soul-1",
		Status:    core.IncidentOpen,
		StartedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}}, nil
}

func TestRSSDescriptionPreservesSoulName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		soulName string
	}{
		{name: "ampersand", soulName: "API & Edge"},
		{name: "angle brackets", soulName: "<Gateway>"},
		{name: "CDATA terminator", soulName: "Worker ]]> Queue"},
		{name: "quotes", soulName: `Primary "API" 'edge'`},
		{name: "plain text control", soulName: "API Edge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandler(&rssDescriptionRepository{soulName: tc.soulName})
			req := httptest.NewRequest(http.MethodGet, "/status/test/feed.xml", nil)
			rec := httptest.NewRecorder()
			handler.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("feed status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var feed struct {
				Channel struct {
					Items []struct {
						Description string `xml:"description"`
					} `xml:"item"`
				} `xml:"channel"`
			}
			if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
				t.Fatalf("feed is not valid XML: %v\n%s", err, rec.Body.String())
			}
			if len(feed.Channel.Items) != 1 {
				t.Fatalf("feed item count = %d, want 1", len(feed.Channel.Items))
			}
			want := "Incident on " + tc.soulName + ": open"
			if got := feed.Channel.Items[0].Description; got != want {
				t.Fatalf("parsed RSS description = %q, want %q", got, want)
			}
		})
	}
}
