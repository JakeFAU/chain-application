package signer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	minKeyReferenceLength = 1
	maxKeyReferenceLength = 512
)

// LocalSigner signs payloads using an in-memory ECDSA P-256 private key.
type LocalSigner struct {
	key          *ecdsa.PrivateKey
	keyReference string
}

var _ Signer = (*LocalSigner)(nil)

// NewLocalSigner creates a new LocalSigner with the provided ECDSA P-256 private key.
func NewLocalSigner(key *ecdsa.PrivateKey, keyReference string) (*LocalSigner, error) {
	if key == nil {
		return nil, errors.New("private key cannot be nil")
	}
	if key.Curve != elliptic.P256() {
		return nil, errors.New("only P-256 curve is supported")
	}
	if err := validateKeyReference(keyReference); err != nil {
		return nil, err
	}
	return &LocalSigner{
		key:          key,
		keyReference: keyReference,
	}, nil
}

// GenerateLocalSigner creates a fresh ephemeral ECDSA P-256 key for testing and local development.
func GenerateLocalSigner(keyReference string) (*LocalSigner, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA P-256 key: %w", err)
	}
	return NewLocalSigner(key, keyReference)
}

// Sign computes the ASN.1 DER ECDSA signature over the provided SHA-256 digest or payload.
func (s *LocalSigner) Sign(_ context.Context, digest []byte) ([]byte, error) {
	if len(digest) == 0 {
		return nil, errors.New("digest cannot be empty")
	}
	hash := digest
	if len(digest) != 32 {
		h := sha256.Sum256(digest)
		hash = h[:]
	}
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, hash)
	if err != nil {
		return nil, fmt.Errorf("sign ASN.1 DER: %w", err)
	}
	return sig, nil
}

// KeyReference returns the canonical identifier for the signing key.
func (s *LocalSigner) KeyReference() string {
	return s.keyReference
}

// Algorithm returns "ecdsa-p256-sha256".
func (s *LocalSigner) Algorithm() string {
	return AlgorithmECDSAP256SHA256
}

// PublicKey returns the public key associated with this signer.
func (s *LocalSigner) PublicKey() *ecdsa.PublicKey {
	return &s.key.PublicKey
}

func validateKeyReference(ref string) error {
	if len(ref) < minKeyReferenceLength || len(ref) > maxKeyReferenceLength {
		return fmt.Errorf("key reference length %d is out of bounds [%d, %d]", len(ref), minKeyReferenceLength, maxKeyReferenceLength)
	}
	for i := 0; i < len(ref); i++ {
		b := ref[i]
		if b < 0x20 || b > 0x7e {
			return fmt.Errorf("key reference contains non-printable character 0x%02x", b)
		}
	}
	return nil
}
