package journey

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyJSONIntegerPrecision(t *testing.T) {
	cases := []struct{ name, body, path, want string }{
		{"small_control", `{"id":42}`, "$.id", "42"},
		{"exact_boundary", `{"id":9007199254740992}`, "$.id", "9007199254740992"},
		{"above_boundary", `{"id":9007199254740993}`, "$.id", "9007199254740993"},
		{"negative", `{"id":-9007199254740993}`, "$.id", "-9007199254740993"},
		{"uint64_max", `{"id":18446744073709551615}`, "$.id", "18446744073709551615"},
		{"nested_object", `{"item":{"id":9007199254740993}}`, "$.item", `{"id":9007199254740993}`},
		{"array", `{"ids":[9007199254740993]}`, "$.ids", `[9007199254740993]`},
		{"fraction_control", `{"id":1.25}`, "$.id", "1.25"},
		{"exponent_control", `{"id":1e3}`, "$.id", "1000"},
		{"string_control", `{"id":"9007199254740993"}`, "$.id", "9007199254740993"},
		{"boolean_control", `{"id":true}`, "$.id", "true"},
		{"null_control", `{"id":null}`, "$.id", ""},
		{"trailing_json_invalid", `{"id":42} {"id":43}`, "$.id", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			e := NewExecutor(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			jctx := &JourneyContext{Variables: map[string]string{}, CookieJar: jar}
			step := core.JourneyStep{Name: "extract", Type: core.CheckHTTP, Target: srv.URL,
				HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Extract: map[string]core.ExtractionRule{"id": {From: "body", Path: tc.path}}}
			got := e.executeStep(ctx, jctx, step, 0)
			if got.Status != core.SoulAlive {
				t.Fatalf("setup HTTP step failed: %s", got.Message)
			}
			if got.Extracted["id"] != tc.want {
				t.Fatalf("FAIL: extracted id = %q, want %q", got.Extracted["id"], tc.want)
			}
		})
	}
}
