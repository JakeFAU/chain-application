package graphv1

import (
	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

// AttestationEdge represents an admitted endorsement relationship between two cryptographic identities.
type AttestationEdge struct {
	recordDigest           ledgerv1.Digest
	sequence               uint64
	proposer               IdentityKey
	subject                IdentityKey
	topic                  string
	claimBody              []byte
	proposedAtUnixMS       uint64
	acceptedAtUnixMS       uint64
	isActive               bool
	revokedAtUnixMS        *uint64
	revocationRecordDigest *ledgerv1.Digest
	revocationReason       string
	revokerRole            endorsementv1.RevokerRole
}

// NewAttestationEdge constructs an active attestation edge admitted at a specific ledger record.
func NewAttestationEdge(
	recordDigest ledgerv1.Digest,
	sequence uint64,
	proposer IdentityKey,
	subject IdentityKey,
	topic string,
	claimBody []byte,
	proposedAtUnixMS uint64,
	acceptedAtUnixMS uint64,
) *AttestationEdge {
	claimCopy := make([]byte, len(claimBody))
	copy(claimCopy, claimBody)

	return &AttestationEdge{
		recordDigest:     recordDigest,
		sequence:         sequence,
		proposer:         proposer,
		subject:          subject,
		topic:            topic,
		claimBody:        claimCopy,
		proposedAtUnixMS: proposedAtUnixMS,
		acceptedAtUnixMS: acceptedAtUnixMS,
		isActive:         true,
	}
}

// RecordDigest returns the digest of the ledger record where this edge was admitted.
func (e *AttestationEdge) RecordDigest() ledgerv1.Digest {
	return e.recordDigest
}

// Sequence returns the ledger sequence number of admission.
func (e *AttestationEdge) Sequence() uint64 {
	return e.sequence
}

// Proposer returns the identity key of the endorsing party.
func (e *AttestationEdge) Proposer() IdentityKey {
	return e.proposer
}

// Subject returns the identity key of the endorsed subject.
func (e *AttestationEdge) Subject() IdentityKey {
	return e.subject
}

// Topic returns the normalized domain topic of the attestation.
func (e *AttestationEdge) Topic() string {
	return e.topic
}

// ClaimBody returns a defensive copy of the raw claim payload bytes.
func (e *AttestationEdge) ClaimBody() []byte {
	out := make([]byte, len(e.claimBody))
	copy(out, e.claimBody)
	return out
}

// ProposedAtUnixMS returns the timestamp when the proposer signed the proposal.
func (e *AttestationEdge) ProposedAtUnixMS() uint64 {
	return e.proposedAtUnixMS
}

// AcceptedAtUnixMS returns the timestamp when the subject accepted the endorsement.
func (e *AttestationEdge) AcceptedAtUnixMS() uint64 {
	return e.acceptedAtUnixMS
}

// IsActive returns whether this endorsement is currently active (unrevoked).
func (e *AttestationEdge) IsActive() bool {
	return e.isActive
}

// RevokedAtUnixMS returns the timestamp of revocation, if revoked.
func (e *AttestationEdge) RevokedAtUnixMS() (uint64, bool) {
	if e.revokedAtUnixMS == nil {
		return 0, false
	}
	return *e.revokedAtUnixMS, true
}

// RevocationRecordDigest returns the ledger record digest of the revocation event, if revoked.
func (e *AttestationEdge) RevocationRecordDigest() (ledgerv1.Digest, bool) {
	if e.revocationRecordDigest == nil {
		return ledgerv1.Digest{}, false
	}
	return *e.revocationRecordDigest, true
}

// RevocationReason returns the optional note attached upon revocation.
func (e *AttestationEdge) RevocationReason() string {
	return e.revocationReason
}

// RevokerRole returns the role of the revoking party (proposer vs subject).
func (e *AttestationEdge) RevokerRole() endorsementv1.RevokerRole {
	return e.revokerRole
}
