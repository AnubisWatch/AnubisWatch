package probe

import "testing"

func TestExtractJSONPathPreservesIntegerPrecision(t *testing.T) {
	for _, tc := range []struct {
		body, want string
		found      bool
	}{
		{`{"id":9007199254740993}`, "9007199254740993", true},
		{`{"id":18446744073709551615}`, "18446744073709551615", true},
		{`{"id":-9007199254740993}`, "-9007199254740993", true},
		{`{"id":42}`, "42", true},
		{`{"id":1.5}`, "1.5", true},
		{`{"id":1.0}`, "1", true},
		{`{"id":1e3}`, "1000", true},
		{`{"id":""}`, "", true},
		{`{"id":true}`, "true", true},
		{`{"id":null}`, "null", true},
		{`{"id":{"n":9007199254740993}}`, `{"n":9007199254740993}`, true},
		{`{}`, "", false},
		{`{"id":1} {}`, "", false},
		{`{"id":1} invalid`, "", false},
		{``, "", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			got, found := extractJSONPathValue([]byte(tc.body), "$.id")
			if got != tc.want || found != tc.found {
				t.Fatalf("got (%q, %t), want (%q, %t)", got, found, tc.want, tc.found)
			}
		})
	}
}
