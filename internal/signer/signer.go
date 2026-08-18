package signer

import "context"

const (
	// AlgorithmECDSAP256SHA256 is the standard signature algorithm for ledger admission.
	AlgorithmECDSAP256SHA256 = "ecdsa-p256-sha256"
)

// Signer signs a digest or preimage using a managed or software key.
type Signer interface {
	// Sign signs the provided digest or payload bytes and returns the ASN.1 DER signature.
	Sign(ctx context.Context, digest []byte) ([]byte, error)

	// KeyReference returns the canonical URI or identifier of the key version.
	KeyReference() string

	// Algorithm returns the signature algorithm identifier.
	Algorithm() string
}
