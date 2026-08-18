package signer

import (
	"context"
	"testing"
)

func TestNewKMSSignerRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		const keyVersion = "projects/attribution-chain-505000/locations/us-east1/keyRings/attribution-chain-system/cryptoKeys/system-signing/cryptoKeyVersions/1"
		if _, err := NewKMSSigner(nil, keyVersion); err == nil {
			t.Fatal("NewKMSSigner(nil client) error = nil, want error")
		}
	})

	t.Run("invalid key version format", func(t *testing.T) {
		t.Parallel()
		invalidNames := []string{
			"",
			"not-a-resource-name",
			"projects/attribution-chain-505000/locations/us-east1/keyRings/ring/cryptoKeys/key", // missing cryptoKeyVersions/N
			"projects//locations/us-east1/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/1",
		}
		for _, name := range invalidNames {
			if _, err := NewKMSSigner(nil, name); err == nil {
				t.Fatalf("NewKMSSigner(nil, %q) error = nil, want error", name)
			}
		}
	})
}

func TestKMSSignerValidResourceName(t *testing.T) {
	t.Parallel()

	const validKeyVersion = "projects/attribution-chain-505000/locations/us-east1/keyRings/attribution-chain-system/cryptoKeys/system-signing/cryptoKeyVersions/1"
	if !kmsKeyVersionPattern.MatchString(validKeyVersion) {
		t.Fatalf("kmsKeyVersionPattern failed to match valid resource name %q", validKeyVersion)
	}
}

func TestKMSSignerSignRejectsEmptyDigest(t *testing.T) {
	t.Parallel()

	signer := &KMSSigner{
		keyVersion: "projects/attribution-chain-505000/locations/us-east1/keyRings/attribution-chain-system/cryptoKeys/system-signing/cryptoKeyVersions/1",
	}
	if _, err := signer.Sign(context.Background(), nil); err == nil {
		t.Fatal("Sign(nil) error = nil, want error")
	}
}
