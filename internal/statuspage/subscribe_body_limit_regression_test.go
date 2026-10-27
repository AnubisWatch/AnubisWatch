package statuspage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type subscriptionRecorder struct {
	*mockRepository
	saved []*core.StatusPageSubscription
}

func (r *subscriptionRecorder) SaveSubscription(sub *core.StatusPageSubscription) error {
	r.saved = append(r.saved, sub)
	return nil
}

// SubscribeHandler is a public, unauthenticated POST. Its body must be bounded
// (as the password POST in ServeHTTP is): otherwise one request can make the
// server buffer and persist an arbitrarily large "email".
func TestSubscribeRejectsOversizedBody(t *testing.T) {
	post := func(email string) (*httptest.ResponseRecorder, *subscriptionRecorder) {
		repo := &subscriptionRecorder{mockRepository: &mockRepository{}}
		body, _ := json.Marshal(map[string]string{"page_id": "test", "type": "email", "email": email})
		req := httptest.NewRequest(http.MethodPost, "/status/subscribe", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		enableTestSubscriptions(NewHandler(repo)).SubscribeHandler(w, req)
		return w, repo
	}

	// Control: a normal subscription is accepted and stored.
	w, repo := post("user@example.com")
	if w.Code != http.StatusAccepted || len(repo.saved) != 1 || repo.saved[0].Email != "user@example.com" {
		t.Fatalf("control: code=%d saved=%d", w.Code, len(repo.saved))
	}

	// A multi-megabyte body is refused and nothing is stored.
	w, repo = post(strings.Repeat("a", 4<<20) + "@example.com")
	if len(repo.saved) != 0 {
		t.Errorf("oversized subscription persisted (%d bytes of email)", len(repo.saved[0].Email))
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 400 or 413", w.Code)
	}
}
