package core

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"time"
)

// ULID is a 16-byte Universally Unique Lexicographically Sortable Identifier.
// Format: 48-bit timestamp (milliseconds) + 80-bit randomness
// Encoded as 26 characters using Crockford's Base32.
type ULID [16]byte

// ZeroULID is the zero ULID value.
var ZeroULID ULID

// String returns the ULID as a 26-character string.
func (u ULID) String() string {
	return strings.ToLower(ulidEncode(u[:]))
}

// Time returns the timestamp component as time.Time.
func (u ULID) Time() time.Time {
	ms := binary.BigEndian.Uint64(append([]byte{0, 0}, u[:6]...))
	return time.Unix(0, int64(ms)*1e6).UTC()
}

// MarshalText implements encoding.TextMarshaler.
func (u ULID) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *ULID) UnmarshalText(text []byte) error {
	parsed, err := ParseULID(string(text))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// MarshalJSON implements json.Marshaler.
func (u ULID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + u.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (u *ULID) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("invalid ULID JSON")
	}
	parsed, err := ParseULID(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// Compare returns an integer comparing two ULIDs lexicographically.
func (u ULID) Compare(other ULID) int {
	for i := 0; i < 16; i++ {
		if u[i] < other[i] {
			return -1
		}
		if u[i] > other[i] {
			return 1
		}
	}
	return 0
}

// GenerateULID creates a new ULID with the current timestamp.
func GenerateULID() (ULID, error) {
	return GenerateULIDAt(time.Now().UTC())
}

// GenerateULIDAt creates a new ULID with the specified timestamp.
func GenerateULIDAt(t time.Time) (ULID, error) {
	var u ULID
	ms := uint64(t.UnixNano()) / 1e6

	// Encode timestamp (48 bits)
	binary.BigEndian.PutUint64(u[:8], ms)

	// Encode randomness (80 bits)
	if _, err := io.ReadFull(rand.Reader, u[6:]); err != nil {
		return ZeroULID, err
	}

	return u, nil
}

// MustGenerateULID generates a ULID, panicking on error.
func MustGenerateULID() ULID {
	u, err := GenerateULID()
	if err != nil {
		panic(err)
	}
	return u
}

// ParseULID parses a ULID string.
func ParseULID(s string) (ULID, error) {
	s = strings.ToUpper(s)
	if len(s) != 26 {
		return ZeroULID, errors.New("invalid ULID length")
	}

	decoded, err := ulidDecode(s)
	if err != nil {
		return ZeroULID, err
	}

	var u ULID
	copy(u[:], decoded)
	return u, nil
}

// Crockford's Base32 alphabet (lowercase for encoding, uppercase accepted for decoding)
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var ulidDecodeTable = func() map[byte]byte {
	table := make(map[byte]byte)
	for i := 0; i < len(ulidAlphabet); i++ {
		c := ulidAlphabet[i]
		table[c] = byte(i)
		// Accept lowercase equivalents
		if c >= 'A' && c <= 'Z' {
			table[c+32] = byte(i)
		}
	}
	// Accept I, L, O as aliases for 1, 1, 0 (per Crockford)
	table['I'] = table['1']
	table['i'] = table['1']
	table['L'] = table['1']
	table['l'] = table['1']
	table['O'] = table['0']
	table['o'] = table['0']
	return table
}()

// ulidEncode encodes 16 bytes to 26 Crockford Base32 characters.
func ulidEncode(src []byte) string {
	if len(src) != 16 {
		panic("invalid ULID length")
	}

	// Use standard base32 encoding with our custom alphabet
	enc := base32.NewEncoding(ulidAlphabet).WithPadding(base32.NoPadding)
	return enc.EncodeToString(src)
}

// ulidDecode decodes 26 Crockford Base32 characters to 16 bytes.
func ulidDecode(src string) ([]byte, error) {
	// Use standard base32 decoding
	enc := base32.NewEncoding(ulidAlphabet).WithPadding(base32.NoPadding)
	decoded, err := enc.DecodeString(src)
	if err != nil {
		return nil, err
	}
	if len(decoded) != 16 {
		return nil, errors.New("invalid decoded ULID length")
	}
	return decoded, nil
}

// GenerateID generates a new ULID string (convenience function).
func GenerateID() string {
	return MustGenerateULID().String()
}
