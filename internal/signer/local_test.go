package signer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestLocalSignerGeneratesValidSignature(t *testing.T) {
	t.Parallel()

	const keyRef = "local:p256:v1"
	localSigner, err := GenerateLocalSigner(keyRef)
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	if got := localSigner.KeyReference(); got != keyRef {
		t.Fatalf("KeyReference() = %q, want %q", got, keyRef)
	}
	if got := localSigner.Algorithm(); got != AlgorithmECDSAP256SHA256 {
		t.Fatalf("Algorithm() = %q, want %q", got, AlgorithmECDSAP256SHA256)
	}

	digest := sha256.Sum256([]byte("test event payload"))
	sig, err := localSigner.Sign(context.Background(), digest[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if len(sig) < 8 || len(sig) > 72 {
		t.Fatalf("signature length = %d, want between 8 and 72", len(sig))
	}

	if !ecdsa.VerifyASN1(localSigner.PublicKey(), digest[:], sig) {
		t.Fatal("ecdsa.VerifyASN1 returned false for generated signature")
	}
}

func TestLocalSignerSignsArbitraryLengthPayloads(t *testing.T) {
	t.Parallel()

	localSigner, err := GenerateLocalSigner("local:p256:v1")
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	rawPayload := []byte("arbitrary-length-unhashed-preimage-data-here")
	sig, err := localSigner.Sign(context.Background(), rawPayload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	expectedDigest := sha256.Sum256(rawPayload)
	if !ecdsa.VerifyASN1(localSigner.PublicKey(), expectedDigest[:], sig) {
		t.Fatal("ecdsa.VerifyASN1 returned false for hashed payload signature")
	}
}

func TestLocalSignerRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	t.Run("nil private key", func(t *testing.T) {
		t.Parallel()
		if _, err := NewLocalSigner(nil, "local:p256:v1"); err == nil {
			t.Fatal("NewLocalSigner(nil) error = nil, want error")
		}
	})

	t.Run("non-P256 curve", func(t *testing.T) {
		t.Parallel()
		key384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey P384: %v", err)
		}
		if _, err := NewLocalSigner(key384, "local:p384:v1"); err == nil {
			t.Fatal("NewLocalSigner(P384) error = nil, want error")
		}
	})

	t.Run("empty key reference", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey P256: %v", err)
		}
		if _, err := NewLocalSigner(key, ""); err == nil {
			t.Fatal("NewLocalSigner(empty ref) error = nil, want error")
		}
	})

	t.Run("non-printable key reference", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey P256: %v", err)
		}
		if _, err := NewLocalSigner(key, "key\x00ref"); err == nil {
			t.Fatal("NewLocalSigner(non-printable ref) error = nil, want error")
		}
	})

	t.Run("oversized key reference", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey P256: %v", err)
		}
		oversized := strings.Repeat("a", 513)
		if _, err := NewLocalSigner(key, oversized); err == nil {
			t.Fatal("NewLocalSigner(oversized ref) error = nil, want error")
		}
	})

	t.Run("empty digest to sign", func(t *testing.T) {
		t.Parallel()
		s, err := GenerateLocalSigner("local:p256:v1")
		if err != nil {
			t.Fatalf("GenerateLocalSigner: %v", err)
		}
		if _, err := s.Sign(context.Background(), nil); err == nil {
			t.Fatal("Sign(nil) error = nil, want error")
		}
	})
}
