package graphv1

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"fmt"
)

// IdentityKey is a 65-byte uncompressed SEC1 encoded P-256 public key (0x04 || X || Y).
type IdentityKey [65]byte

var (
	ErrInvalidIdentityKeyLength = errors.New("identity key must be exactly 65 bytes")
	ErrInvalidIdentityKeyFormat = errors.New("identity key must start with uncompressed SEC1 prefix 0x04")
	ErrInvalidCurvePoint        = errors.New("identity key is not a valid P-256 curve point")
)

// IdentityKeyFromBytes constructs and validates an IdentityKey from a byte slice.
func IdentityKeyFromBytes(b []byte) (IdentityKey, error) {
	var key IdentityKey
	if len(b) != 65 {
		return key, fmt.Errorf("%w: got %d", ErrInvalidIdentityKeyLength, len(b))
	}
	if b[0] != 0x04 {
		return key, ErrInvalidIdentityKeyFormat
	}
	if _, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b); err != nil {
		return key, fmt.Errorf("%w: %v", ErrInvalidCurvePoint, err)
	}
	copy(key[:], b)
	return key, nil
}

// IdentityKeyFromHex constructs and validates an IdentityKey from a 130-character hex string.
func IdentityKeyFromHex(s string) (IdentityKey, error) {
	var key IdentityKey
	b, err := hex.DecodeString(s)
	if err != nil {
		return key, fmt.Errorf("decode hex identity key: %w", err)
	}
	return IdentityKeyFromBytes(b)
}

// IdentityKeyFromPublicKey extracts the uncompressed IdentityKey from an ECDSA P-256 public key.
func IdentityKeyFromPublicKey(pub *ecdsa.PublicKey) (IdentityKey, error) {
	var key IdentityKey
	if pub == nil {
		return key, errors.New("public key cannot be nil")
	}
	b, err := pub.Bytes()
	if err != nil {
		return key, fmt.Errorf("encode public key bytes: %w", err)
	}
	return IdentityKeyFromBytes(b)
}

// PublicKey parses the IdentityKey back into an *ecdsa.PublicKey.
func (k IdentityKey) PublicKey() (*ecdsa.PublicKey, error) {
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), k[:])
}

// Hex returns the 130-character lowercase hexadecimal representation of the key.
func (k IdentityKey) Hex() string {
	return hex.EncodeToString(k[:])
}

// Bytes returns a defensive copy of the 65-byte key.
func (k IdentityKey) Bytes() []byte {
	out := make([]byte, 65)
	copy(out, k[:])
	return out
}

// String returns a truncated hex representation suitable for human logging.
func (k IdentityKey) String() string {
	h := k.Hex()
	if len(h) <= 16 {
		return h
	}
	return h[:8] + "..." + h[len(h)-8:]
}
