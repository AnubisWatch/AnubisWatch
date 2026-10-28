package statuspage

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type rssLinksRepository struct {
	mockRepository
	customDomain string
}

func (r *rssLinksRepository) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	return &core.StatusPage{
		ID:           "page-1",
		Name:         "Operations",
		Slug:         slug,
		CustomDomain: r.customDomain,
		Visibility:   core.VisibilityPublic,
	}, nil
}

func (r *rssLinksRepository) GetIncidentsByPage(string) ([]core.Incident, error) {
	return []core.Incident{{
		ID:        "incident-1",
		SoulID:    "soul-1",
		Status:    core.IncidentOpen,
		StartedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}}, nil
}

func TestRSSLinksUseReachableStatusPageURL(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestURL     string
		forwardedProto string
		customDomain   string
		wantPageLink   string
		wantItemLink   string
	}{
		{
			name:         "HTTPS origin",
			requestURL:   "https://monitor.example/status/ordinary/feed.xml",
			wantPageLink: "https://monitor.example/status/ordinary",
			wantItemLink: "https://monitor.example/status/ordinary/incidents/incident-1",
		},
		{
			name:         "HTTP origin with port",
			requestURL:   "http://localhost:8080/status/ordinary/feed.xml",
			wantPageLink: "http://localhost:8080/status/ordinary",
			wantItemLink: "http://localhost:8080/status/ordinary/incidents/incident-1",
		},
		{
			name:           "proxied HTTPS origin",
			requestURL:     "http://monitor.example/status/ordinary/feed.xml",
			forwardedProto: "https",
			wantPageLink:   "https://monitor.example/status/ordinary",
			wantItemLink:   "https://monitor.example/status/ordinary/incidents/incident-1",
		},
		{
			name:         "custom domain control",
			requestURL:   "https://monitor.example/status/ordinary/feed.xml",
			customDomain: "status.customer.example",
			wantPageLink: "https://status.customer.example",
			wantItemLink: "https://status.customer.example/incidents/incident-1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandler(&rssLinksRepository{customDomain: tc.customDomain})
			req := httptest.NewRequest(http.MethodGet, tc.requestURL, nil)
			if tc.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			}
			rec := httptest.NewRecorder()
			handler.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("feed status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var feed struct {
				Channel struct {
					Link  string `xml:"link"`
					Items []struct {
						Link string `xml:"link"`
					} `xml:"item"`
				} `xml:"channel"`
			}
			if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
				t.Fatalf("feed is not valid XML: %v\n%s", err, rec.Body.String())
			}
			if feed.Channel.Link != tc.wantPageLink {
				t.Fatalf("channel link = %q, want %q", feed.Channel.Link, tc.wantPageLink)
			}
			if len(feed.Channel.Items) != 1 {
				t.Fatalf("feed item count = %d, want 1", len(feed.Channel.Items))
			}
			if got := feed.Channel.Items[0].Link; got != tc.wantItemLink {
				t.Fatalf("item link = %q, want %q", got, tc.wantItemLink)
			}
		})
	}
}
