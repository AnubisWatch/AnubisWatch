package core

import (
	"strings"
	"testing"
)

func TestParseULIDRejectsNonzeroPaddingBits(t *testing.T) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for i, suffix := range alphabet {
		input := strings.Repeat("0", 25) + string(suffix)
		got, err := ParseULID(input)
		if i%4 != 0 {
			if err == nil {
				t.Errorf("ParseULID(%q) accepted nonzero padding bits", input)
			}
			continue
		}
		if err != nil || got.String() != strings.ToLower(input) {
			t.Errorf("ParseULID(%q) = %v, %v", input, got, err)
		}
	}
}
