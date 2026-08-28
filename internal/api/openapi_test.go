package api

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// routesNotInSpec are paths the router serves that are deliberately absent
// from the published API surface. Every entry needs a reason — this list is
// the escape hatch, so an unexplained addition is the thing to catch in
// review.
var routesNotInSpec = map[string]string{
	"GET /ws": "WebSocket upgrade, not an HTTP operation OpenAPI 3.0 can describe",
}

// prefixMountedRoutes are handled by a path-prefix branch in Router.ServeHTTP
// rather than a router.Handle registration, so scanning registrations cannot
// see them. They are documented and must stay documented.
var prefixMountedRoutes = []string{
	"GET /status/{slug}",
}

var handleRe = regexp.MustCompile(`s\.router\.Handle\("([A-Z]+)",\s*"([^"]+)"`)
var paramRe = regexp.MustCompile(`:(\w+)`)

// registeredRoutes reads the route table out of rest.go. Parsing the source is
// deliberate: building a RESTServer to enumerate routes would only report the
// routes that this test's particular config happens to enable (OIDC, metrics
// auth), and a route that disappears from the binary under some config is
// exactly the kind of thing that should still be documented.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("rest.go")
	if err != nil {
		t.Fatalf("reading rest.go: %v", err)
	}
	routes := map[string]bool{}
	for _, m := range handleRe.FindAllStringSubmatch(string(src), -1) {
		method, path := m[1], paramRe.ReplaceAllString(m[2], "{$1}")
		routes[method+" "+path] = true
	}
	if len(routes) < 50 {
		t.Fatalf("found only %d routes in rest.go; the scan regex is probably stale", len(routes))
	}
	return routes
}

func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	spec, err := openAPISpec()
	if err != nil {
		t.Fatalf("parsing embedded openapi.yaml: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("embedded openapi.yaml has no paths object")
	}
	ops := map[string]bool{}
	for path, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for method := range methods {
			switch method {
			case "get", "post", "put", "delete", "patch":
				ops[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	return ops
}

// The API used to be described by three documents that disagreed with each
// other and with the router. This test is what stops that from recurring: a
// route added without a spec entry fails the build.
func TestOpenAPISpecCoversEveryRoute(t *testing.T) {
	routes := registeredRoutes(t)
	ops := specOperations(t)

	var undocumented []string
	for route := range routes {
		if ops[route] {
			continue
		}
		if _, exempt := routesNotInSpec[route]; exempt {
			continue
		}
		undocumented = append(undocumented, route)
	}
	sort.Strings(undocumented)

	if len(undocumented) > 0 {
		t.Errorf("%d route(s) are registered but missing from internal/api/openapi.yaml:\n  %s\n\n"+
			"Add an operation for each, or — if the route is intentionally not part of the\n"+
			"published API — add it to routesNotInSpec with a reason.",
			len(undocumented), strings.Join(undocumented, "\n  "))
	}
}

// The reverse direction: the spec must not advertise endpoints that do not
// exist. A phantom operation sends client generators after a 404.
func TestOpenAPISpecHasNoPhantomOperations(t *testing.T) {
	routes := registeredRoutes(t)
	for _, r := range prefixMountedRoutes {
		routes[r] = true
	}
	ops := specOperations(t)

	var phantom []string
	for op := range ops {
		if !routes[op] {
			phantom = append(phantom, op)
		}
	}
	sort.Strings(phantom)

	if len(phantom) > 0 {
		t.Errorf("%d operation(s) are documented but not served:\n  %s\n\n"+
			"Remove them from internal/api/openapi.yaml, or — if the route is mounted by a\n"+
			"path prefix rather than router.Handle — add it to prefixMountedRoutes.",
			len(phantom), strings.Join(phantom, "\n  "))
	}
}

func TestOpenAPIExemptionsAreJustified(t *testing.T) {
	for route, reason := range routesNotInSpec {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("routesNotInSpec[%q] has no reason", route)
		}
	}
}

// The embedded YAML is converted to JSON once at init; assert the result is
// actually a usable OpenAPI document and not, say, an empty object.
func TestOpenAPIJSONIsValid(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal(openapiJSON, &spec); err != nil {
		t.Fatalf("openapiJSON is not valid JSON: %v", err)
	}
	if got := spec["openapi"]; got != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", got)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatal("converted spec has no paths")
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("converted spec has no components")
	}
	if _, ok := components["securitySchemes"]; !ok {
		t.Error("converted spec has no securitySchemes")
	}
}
