package core

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// docs/CONFIGURATION.md ("Extraction Rule") and the shipped
// configs/anubis.example.yaml write extraction rules with json_path,
// header_name and cookie_name. ExtractionRule's own key is "path", and the
// YAML decoder drops unknown keys without a word, so a journey written exactly
// as documented loaded with an empty Path: the rule extracted nothing and every
// later step that interpolated the variable ran with it unset.
func TestExtractionRuleYAMLAcceptsDocumentedKeys(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want ExtractionRule
	}{
		{"native path key (control)", "from: body\npath: \"$.token\"\n", ExtractionRule{From: "body", Path: "$.token"}},
		{"native path with regex (control)", "from: header\npath: X-Request-Id\nregex: \"id-(\\\\d+)\"\n",
			ExtractionRule{From: "header", Path: "X-Request-Id", Regex: `id-(\d+)`}},
		{"documented json_path", "from: body\njson_path: \"$.csrf_token\"\n", ExtractionRule{From: "body", Path: "$.csrf_token"}},
		{"documented header_name", "from: header\nheader_name: X-Token\n", ExtractionRule{From: "header", Path: "X-Token"}},
		{"documented cookie_name", "from: cookie\ncookie_name: session\n", ExtractionRule{From: "cookie", Path: "session"}},
		{"documented key with regex", "from: header\nheader_name: Location\nregex: \"/items/(\\\\d+)\"\n",
			ExtractionRule{From: "header", Path: "Location", Regex: `/items/(\d+)`}},
		{"path wins over an alias", "from: body\npath: \"$.a\"\njson_path: \"$.b\"\n", ExtractionRule{From: "body", Path: "$.a"}},
		{"alias for another source is not used", "from: header\njson_path: \"$.x\"\n", ExtractionRule{From: "header"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got ExtractionRule
			if err := yaml.Unmarshal([]byte(tc.yaml), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}

	// The same rules inside a whole journey step, as the shipped example has them.
	var step JourneyStep
	doc := "name: login\ntype: http\ntarget: https://x\nextract:\n  csrf_token:\n    from: body\n    json_path: \"$.csrf_token\"\n"
	if err := yaml.Unmarshal([]byte(doc), &step); err != nil {
		t.Fatal(err)
	}
	if rule := step.Extract["csrf_token"]; rule.Path != "$.csrf_token" {
		t.Errorf("documented step extraction rule = %+v, want Path $.csrf_token", rule)
	}
}
