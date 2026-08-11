package ledgerv1

import (
	"errors"
	"strings"
	"testing"
)

type codecTestWire struct {
	Value uint64 `cbor:"0,keyasint"`
}

func TestDecodeCanonicalMapAcceptsExactEncoding(t *testing.T) {
	t.Parallel()

	encoded := []byte{0xa1, 0x00, 0x01}
	var decoded codecTestWire
	if err := decodeCanonicalMap(encoded, len(encoded), []uint64{0}, &decoded, "test"); err != nil {
		t.Fatalf("decodeCanonicalMap: %v", err)
	}
	if decoded.Value != 1 {
		t.Fatalf("value = %d, want 1", decoded.Value)
	}
}

func TestDecodeCanonicalMapRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		encoded []byte
		maxSize int
		want    error
	}{
		{
			name:    "non-shortest integer",
			encoded: []byte{0xa1, 0x00, 0x18, 0x01},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "indefinite map",
			encoded: []byte{0xbf, 0x00, 0x01, 0xff},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "duplicate key",
			encoded: []byte{0xa2, 0x00, 0x01, 0x00, 0x02},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "tag",
			encoded: []byte{0xd9, 0xd9, 0xf7, 0xa1, 0x00, 0x01},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "missing key",
			encoded: []byte{0xa0},
			maxSize: 32,
			want:    ErrSchemaViolation,
		},
		{
			name:    "unknown key",
			encoded: []byte{0xa1, 0x01, 0x6e, 0x70, 0x72, 0x69, 0x76, 0x61, 0x74, 0x65, 0x2d, 0x6d, 0x61, 0x72, 0x6b, 0x65, 0x72},
			maxSize: 32,
			want:    ErrSchemaViolation,
		},
		{
			name:    "outer slice exceeds limit",
			encoded: []byte{0xa1, 0x00, 0x01},
			maxSize: 2,
			want:    ErrOversizedInput,
		},
		{
			name:    "truncated CBOR",
			encoded: []byte{0xa1, 0x00},
			maxSize: 32,
			want:    ErrMalformedCBOR,
		},
		{
			name:    "out-of-order map",
			encoded: []byte{0xa2, 0x01, 0x01, 0x00, 0x01},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "text key",
			encoded: []byte{0xa1, 0x61, 0x30, 0x01},
			maxSize: 32,
			want:    ErrSchemaViolation,
		},
		{
			name:    "invalid UTF-8",
			encoded: []byte{0xa1, 0x00, 0x61, 0xff},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "undefined",
			encoded: []byte{0xa1, 0x00, 0xf7},
			maxSize: 32,
			want:    ErrNonConformingCBOR,
		},
		{
			name:    "float for unsigned integer",
			encoded: []byte{0xa1, 0x00, 0xf9, 0x3c, 0x00},
			maxSize: 32,
			want:    ErrSchemaViolation,
		},
		{
			name:    "trailing bytes",
			encoded: []byte{0xa1, 0x00, 0x01, 0x00},
			maxSize: 32,
			want:    ErrMalformedCBOR,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoded codecTestWire
			err := decodeCanonicalMap(test.encoded, test.maxSize, []uint64{0}, &decoded, "test")
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), "private-marker") {
				t.Fatalf("error text leaked private marker: %q", err)
			}
		})
	}
}
