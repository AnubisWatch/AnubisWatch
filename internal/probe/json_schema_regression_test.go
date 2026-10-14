package probe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A check configured with a JSON schema must not report a response healthy when the
// response violates that schema. The validator implements type/required/properties/
// enum and used to ignore every other keyword, and matchesType used to accept any
// unrecognized type name, so such a schema validated anything: the check could not
// fail and would never alert.

const schemaRegressionBody = `{"status":"ok","count":0,"name":"service"}`

func schemaJudgment(t *testing.T, schema string) *core.Judgment {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, schemaRegressionBody)
	}))
	t.Cleanup(srv.Close)

	soul := &core.Soul{
		ID: "schema-regression-soul", Name: "schema-check", Target: srv.URL,
		HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, JSONSchema: schema},
	}

	judgment, err := NewHTTPChecker().Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("judge failed: %v", err)
	}
	return judgment
}

func TestJSONSchemaAssertionIsNotVacuous(t *testing.T) {
	for _, tc := range []struct{ name, schema, why string }{
		{
			"unimplemented keyword",
			`{"type":"object","properties":{"count":{"type":"integer","minimum":1}}}`,
			"count=0 violates minimum:1",
		},
		{
			"unimplemented keyword nested under properties",
			`{"type":"object","properties":{"status":{"type":"string","pattern":"^ok$"},"count":{"type":"integer","minimum":5}}}`,
			"count=0 violates the nested minimum:5",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := schemaJudgment(t, tc.schema)
			if j.Status == core.SoulAlive {
				t.Errorf("the response was reported healthy although %s (message=%q)", tc.why, j.Message)
			}
		})
	}
}

// TestJSONSchemaAssertionStillEvaluatesSupportedSchemas is the neighbouring
// control: schemas the validator implements keep working in both directions.
func TestJSONSchemaAssertionStillEvaluatesSupportedSchemas(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		want   core.SoulStatus
	}{
		{
			"conforming response",
			`{"type":"object","required":["status"],"properties":{"status":{"type":"string"},"count":{"type":"integer"}}}`,
			core.SoulAlive,
		},
		{
			"missing required field",
			`{"type":"object","required":["absent_field"]}`,
			core.SoulDead,
		},
		{
			"wrong type",
			`{"type":"object","properties":{"status":{"type":"integer"}}}`,
			core.SoulDead,
		},
		{
			"enum mismatch",
			`{"type":"object","properties":{"status":{"type":"string","enum":["down"]}}}`,
			core.SoulDead,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := schemaJudgment(t, tc.schema).Status; got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}
