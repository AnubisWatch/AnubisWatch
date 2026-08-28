package api

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// openapiYAML is the single source of truth for the documented API surface.
//
// It replaces three documents that had drifted apart: a hand-maintained
// docs/api/openapi.yaml that was never served (18 paths), a hardcoded JSON
// blob in rest.go that was (42 paths, declaring an "info.version" of 4.0.0
// that matched nothing), and a Go map that merged a few more paths into the
// blob at request time. None of them covered the full route table.
//
// TestOpenAPISpecCoversEveryRoute keeps this file honest.
//
//go:embed openapi.yaml
var openapiYAML []byte

// openapiJSON is the spec converted to JSON once at startup. Converting per
// request would re-parse ~1800 lines of YAML on every /api/openapi.json hit.
var openapiJSON []byte

func init() {
	var err error
	openapiJSON, err = yamlToJSON(openapiYAML)
	if err != nil {
		// The spec is embedded at build time, so a failure here means the
		// committed file is malformed — that must not reach a running server.
		panic(fmt.Sprintf("embedded openapi.yaml is not valid YAML: %v", err))
	}
}

// yamlToJSON converts a YAML document to indented JSON.
func yamlToJSON(in []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(in, &doc); err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// openAPISpec returns the parsed spec. Used by the drift test.
func openAPISpec() (map[string]any, error) {
	var spec map[string]any
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		return nil, err
	}
	return spec, nil
}
