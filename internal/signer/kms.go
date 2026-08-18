package signer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
)

var kmsKeyVersionPattern = regexp.MustCompile(`^projects/[a-z0-9-]+/locations/[a-z0-9-]+/keyRings/[a-zA-Z0-9_-]+/cryptoKeys/[a-zA-Z0-9_-]+/cryptoKeyVersions/[0-9]+$`)

// KMSSigner signs digests using Google Cloud Key Management Service (KMS).
type KMSSigner struct {
	client     *kms.KeyManagementClient
	keyVersion string
}

var _ Signer = (*KMSSigner)(nil)

// NewKMSSigner creates a KMSSigner for the specified GCP KMS cryptoKeyVersion resource name.
func NewKMSSigner(client *kms.KeyManagementClient, keyVersionResourceName string) (*KMSSigner, error) {
	if client == nil {
		return nil, errors.New("kms client cannot be nil")
	}
	if !kmsKeyVersionPattern.MatchString(keyVersionResourceName) {
		return nil, fmt.Errorf("invalid KMS key version resource name: %q", keyVersionResourceName)
	}
	return &KMSSigner{
		client:     client,
		keyVersion: keyVersionResourceName,
	}, nil
}

// Sign calls Cloud KMS AsymmetricSign to generate an ECDSA P-256 SHA-256 signature.
func (s *KMSSigner) Sign(ctx context.Context, digest []byte) ([]byte, error) {
	if len(digest) == 0 {
		return nil, errors.New("digest cannot be empty")
	}
	hash := digest
	if len(digest) != 32 {
		h := sha256.Sum256(digest)
		hash = h[:]
	}

	req := &kmspb.AsymmetricSignRequest{
		Name: s.keyVersion,
		Digest: &kmspb.Digest{
			Digest: &kmspb.Digest_Sha256{
				Sha256: hash,
			},
		},
	}

	resp, err := s.client.AsymmetricSign(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("kms asymmetric sign: %w", err)
	}
	if len(resp.Signature) == 0 {
		return nil, errors.New("kms returned empty signature")
	}
	return resp.Signature, nil
}

// KeyReference returns the full GCP resource name of the KMS key version.
func (s *KMSSigner) KeyReference() string {
	return s.keyVersion
}

// Algorithm returns "ecdsa-p256-sha256".
func (s *KMSSigner) Algorithm() string {
	return AlgorithmECDSAP256SHA256
}

// Close closes the underlying KMS client.
func (s *KMSSigner) Close() error {
	return s.client.Close()
}
