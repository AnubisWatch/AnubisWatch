package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestCreateSoulRejectsImpossibleHTTPStatusCodes(t *testing.T) {
	for _, status := range []int{-1, 0, 99, 600, 999} {
		t.Run(fmt.Sprintf("invalid_%d", status), func(t *testing.T) {
			_, storage, probe, router := newSoulCreateServer()
			body := fmt.Sprintf(`{"name":"invalid-status","type":"http","target":"https://example.com","http":{"valid_status":[%d]}}`, status)
			response := postSoul(router, body)
			if response.Code != http.StatusBadRequest {
				if response.Code == http.StatusCreated && len(storage.souls) == 1 && len(probe.souls) == 1 {
					t.Fatalf("impossible HTTP status %d was accepted, persisted, and scheduled", status)
				}
				t.Fatalf("impossible HTTP status %d returned %d, want 400: %s", status, response.Code, response.Body.String())
			}
		})
	}

	for _, status := range []int{100, 200, 599} {
		t.Run(fmt.Sprintf("valid_%d", status), func(t *testing.T) {
			_, _, _, router := newSoulCreateServer()
			body := fmt.Sprintf(`{"name":"valid-status","type":"http","target":"https://example.com","http":{"valid_status":[%d]}}`, status)
			response := postSoul(router, body)
			if response.Code != http.StatusCreated {
				t.Fatalf("valid HTTP status %d returned %d: %s", status, response.Code, response.Body.String())
			}
		})
	}
}
