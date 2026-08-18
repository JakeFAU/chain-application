package endorsementv1

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

const (
	PayloadVersionV1 uint64 = 1

	domainProposerEndorsementV1 = "attribution-chain:endorsement-proposal:v1"
	domainSubjectAcceptanceV1   = "attribution-chain:endorsement-acceptance:v1"
	domainRevocationV1          = "attribution-chain:endorsement-revocation:v1"

	minTopicLength    = 1
	maxTopicLength    = 128
	minClaimBodyBytes = 1
	maxClaimBodyBytes = 4096
	maxReasonLength   = 256
	minSignatureBytes = 8
	maxSignatureBytes = 72
)

type RevokerRole uint64

const (
	RevokerRoleProposer RevokerRole = 1
	RevokerRoleSubject  RevokerRole = 2
)

var (
	cborEncMode cbor.EncMode
	cborDecMode cbor.DecMode
)

func init() {
	encOptions := cbor.CoreDetEncOptions()
	encOptions.Sort = cbor.SortCanonical
	var err error
	cborEncMode, err = encOptions.EncMode()
	if err != nil {
		panic("initialize endorsement cbor enc mode: " + err.Error())
	}

	decOptions := cbor.DecOptions{
		DupMapKey:         cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:   4,
		MaxArrayElements:  16,
		MaxMapPairs:       16,
		IndefLength:       cbor.IndefLengthForbidden,
		TagsMd:            cbor.TagsForbidden,
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,
		UTF8:              cbor.UTF8RejectInvalid,
		FieldNameMatching: cbor.FieldNameMatchingCaseSensitive,
	}
	cborDecMode, err = decOptions.DecMode()
	if err != nil {
		panic("initialize endorsement cbor dec mode: " + err.Error())
	}
}

// EndorsementProposal represents an off-ledger endorsement proposal made by a proposer.
type EndorsementProposal struct {
	ProposerPublicKey *ecdsa.PublicKey
	SubjectPublicKey  *ecdsa.PublicKey
	Topic             string
	ClaimBodyBytes    []byte
	ProposedAtUnixMS  uint64
}

type proposalStatementWire struct {
	Version           uint64 `cbor:"0,keyasint"`
	ProposerPublicKey []byte `cbor:"1,keyasint"`
	SubjectPublicKey  []byte `cbor:"2,keyasint"`
	Topic             string `cbor:"3,keyasint"`
	ClaimBodyBytes    []byte `cbor:"4,keyasint"`
	ProposedAtUnixMS  uint64 `cbor:"5,keyasint"`
}

// NewProposal constructs and validates an endorsement proposal.
func NewProposal(
	proposer *ecdsa.PublicKey,
	subject *ecdsa.PublicKey,
	topic string,
	claimBody []byte,
	proposedAtUnixMS uint64,
) (*EndorsementProposal, error) {
	if proposer == nil || subject == nil {
		return nil, errors.New("proposer and subject public keys are required")
	}
	if len(topic) < minTopicLength || len(topic) > maxTopicLength {
		return nil, fmt.Errorf("topic length %d out of bounds [%d, %d]", len(topic), minTopicLength, maxTopicLength)
	}
	if len(claimBody) < minClaimBodyBytes || len(claimBody) > maxClaimBodyBytes {
		return nil, fmt.Errorf("claim body length %d out of bounds [%d, %d]", len(claimBody), minClaimBodyBytes, maxClaimBodyBytes)
	}
	return &EndorsementProposal{
		ProposerPublicKey: proposer,
		SubjectPublicKey:  subject,
		Topic:             topic,
		ClaimBodyBytes:    bytes.Clone(claimBody),
		ProposedAtUnixMS:  proposedAtUnixMS,
	}, nil
}

// Digest returns the domain-separated SHA-256 digest of the canonical proposal statement.
func (p *EndorsementProposal) Digest() [32]byte {
	wire := proposalStatementWire{
		Version:           PayloadVersionV1,
		ProposerPublicKey: encodePublicKey(p.ProposerPublicKey),
		SubjectPublicKey:  encodePublicKey(p.SubjectPublicKey),
		Topic:             p.Topic,
		ClaimBodyBytes:    p.ClaimBodyBytes,
		ProposedAtUnixMS:  p.ProposedAtUnixMS,
	}
	encoded, err := cborEncMode.Marshal(wire)
	if err != nil {
		panic("marshal proposal statement: " + err.Error())
	}
	return domainSeparatedDigest(domainProposerEndorsementV1, encoded)
}

// Sign signs the proposal using the proposer's private key.
func (p *EndorsementProposal) Sign(proposerKey *ecdsa.PrivateKey) ([]byte, error) {
	if proposerKey == nil {
		return nil, errors.New("private key cannot be nil")
	}
	digest := p.Digest()
	return ecdsa.SignASN1(rand.Reader, proposerKey, digest[:])
}

// VerifySignature verifies the proposer's signature over this proposal.
func (p *EndorsementProposal) VerifySignature(signatureBytes []byte) error {
	if len(signatureBytes) < minSignatureBytes || len(signatureBytes) > maxSignatureBytes {
		return fmt.Errorf("signature length %d out of bounds [%d, %d]", len(signatureBytes), minSignatureBytes, maxSignatureBytes)
	}
	digest := p.Digest()
	if !ecdsa.VerifyASN1(p.ProposerPublicKey, digest[:], signatureBytes) {
		return errors.New("proposer signature verification failed")
	}
	return nil
}

// EndorsementAcceptance represents the subject's acceptance of an endorsement proposal.
type EndorsementAcceptance struct {
	ProposalDigest   [32]byte
	AcceptedAtUnixMS uint64
}

type acceptanceStatementWire struct {
	Version          uint64 `cbor:"0,keyasint"`
	ProposalDigest   []byte `cbor:"1,keyasint"`
	AcceptedAtUnixMS uint64 `cbor:"2,keyasint"`
}

// NewAcceptance creates a new acceptance statement.
func NewAcceptance(proposalDigest [32]byte, acceptedAtUnixMS uint64) (*EndorsementAcceptance, error) {
	return &EndorsementAcceptance{
		ProposalDigest:   proposalDigest,
		AcceptedAtUnixMS: acceptedAtUnixMS,
	}, nil
}

// Digest returns the domain-separated SHA-256 digest of the acceptance statement.
func (a *EndorsementAcceptance) Digest() [32]byte {
	wire := acceptanceStatementWire{
		Version:          PayloadVersionV1,
		ProposalDigest:   a.ProposalDigest[:],
		AcceptedAtUnixMS: a.AcceptedAtUnixMS,
	}
	encoded, err := cborEncMode.Marshal(wire)
	if err != nil {
		panic("marshal acceptance statement: " + err.Error())
	}
	return domainSeparatedDigest(domainSubjectAcceptanceV1, encoded)
}

// Sign signs the acceptance statement using the subject's private key.
func (a *EndorsementAcceptance) Sign(subjectKey *ecdsa.PrivateKey) ([]byte, error) {
	if subjectKey == nil {
		return nil, errors.New("private key cannot be nil")
	}
	digest := a.Digest()
	return ecdsa.SignASN1(rand.Reader, subjectKey, digest[:])
}

// VerifySignature verifies the subject's signature over this acceptance statement.
func (a *EndorsementAcceptance) VerifySignature(subjectPublicKey *ecdsa.PublicKey, signatureBytes []byte) error {
	if len(signatureBytes) < minSignatureBytes || len(signatureBytes) > maxSignatureBytes {
		return fmt.Errorf("signature length %d out of bounds [%d, %d]", len(signatureBytes), minSignatureBytes, maxSignatureBytes)
	}
	digest := a.Digest()
	if !ecdsa.VerifyASN1(subjectPublicKey, digest[:], signatureBytes) {
		return errors.New("subject signature verification failed")
	}
	return nil
}

// EndorsementAcceptedPayload represents the complete on-ledger accepted endorsement payload.
type EndorsementAcceptedPayload struct {
	ProposerPublicKey *ecdsa.PublicKey
	SubjectPublicKey  *ecdsa.PublicKey
	Topic             string
	ClaimBodyBytes    []byte
	ProposedAtUnixMS  uint64
	ProposerSignature []byte
	AcceptedAtUnixMS  uint64
	SubjectSignature  []byte
}

type acceptedPayloadWire struct {
	Version           uint64 `cbor:"0,keyasint"`
	ProposerPublicKey []byte `cbor:"1,keyasint"`
	SubjectPublicKey  []byte `cbor:"2,keyasint"`
	Topic             string `cbor:"3,keyasint"`
	ClaimBodyBytes    []byte `cbor:"4,keyasint"`
	ProposedAtUnixMS  uint64 `cbor:"5,keyasint"`
	ProposerSignature []byte `cbor:"6,keyasint"`
	AcceptedAtUnixMS  uint64 `cbor:"7,keyasint"`
	SubjectSignature  []byte `cbor:"8,keyasint"`
}

// NewAcceptedPayload constructs a new accepted endorsement payload.
func NewAcceptedPayload(
	proposer *ecdsa.PublicKey,
	subject *ecdsa.PublicKey,
	topic string,
	claimBody []byte,
	proposedAt uint64,
	proposerSig []byte,
	acceptedAt uint64,
	subjectSig []byte,
) (*EndorsementAcceptedPayload, error) {
	if proposer == nil || subject == nil {
		return nil, errors.New("proposer and subject public keys are required")
	}
	if len(topic) < minTopicLength || len(topic) > maxTopicLength {
		return nil, fmt.Errorf("topic length %d out of bounds", len(topic))
	}
	if len(claimBody) < minClaimBodyBytes || len(claimBody) > maxClaimBodyBytes {
		return nil, fmt.Errorf("claim body length %d out of bounds", len(claimBody))
	}
	if len(proposerSig) < minSignatureBytes || len(proposerSig) > maxSignatureBytes {
		return nil, fmt.Errorf("proposer signature length %d out of bounds", len(proposerSig))
	}
	if len(subjectSig) < minSignatureBytes || len(subjectSig) > maxSignatureBytes {
		return nil, fmt.Errorf("subject signature length %d out of bounds", len(subjectSig))
	}

	return &EndorsementAcceptedPayload{
		ProposerPublicKey: proposer,
		SubjectPublicKey:  subject,
		Topic:             topic,
		ClaimBodyBytes:    bytes.Clone(claimBody),
		ProposedAtUnixMS:  proposedAt,
		ProposerSignature: bytes.Clone(proposerSig),
		AcceptedAtUnixMS:  acceptedAt,
		SubjectSignature:  bytes.Clone(subjectSig),
	}, nil
}

// Encode serializes the accepted payload to canonical CBOR.
func (p *EndorsementAcceptedPayload) Encode() ([]byte, error) {
	wire := acceptedPayloadWire{
		Version:           PayloadVersionV1,
		ProposerPublicKey: encodePublicKey(p.ProposerPublicKey),
		SubjectPublicKey:  encodePublicKey(p.SubjectPublicKey),
		Topic:             p.Topic,
		ClaimBodyBytes:    p.ClaimBodyBytes,
		ProposedAtUnixMS:  p.ProposedAtUnixMS,
		ProposerSignature: p.ProposerSignature,
		AcceptedAtUnixMS:  p.AcceptedAtUnixMS,
		SubjectSignature:  p.SubjectSignature,
	}
	return cborEncMode.Marshal(wire)
}

// DecodeAcceptedPayload parses and structurally validates canonical CBOR bytes for an accepted endorsement payload.
func DecodeAcceptedPayload(encoded []byte) (*EndorsementAcceptedPayload, error) {
	var wire acceptedPayloadWire
	if err := cborDecMode.Unmarshal(encoded, &wire); err != nil {
		return nil, fmt.Errorf("decode accepted payload: %w", err)
	}
	if wire.Version != PayloadVersionV1 {
		return nil, fmt.Errorf("unsupported payload version %d", wire.Version)
	}

	proposerPub, err := decodePublicKey(wire.ProposerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode proposer public key: %w", err)
	}
	subjectPub, err := decodePublicKey(wire.SubjectPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode subject public key: %w", err)
	}

	return NewAcceptedPayload(
		proposerPub,
		subjectPub,
		wire.Topic,
		wire.ClaimBodyBytes,
		wire.ProposedAtUnixMS,
		wire.ProposerSignature,
		wire.AcceptedAtUnixMS,
		wire.SubjectSignature,
	)
}

// Verify validates both the proposer signature and the subject acceptance signature.
func (p *EndorsementAcceptedPayload) Verify() error {
	proposal, err := NewProposal(
		p.ProposerPublicKey,
		p.SubjectPublicKey,
		p.Topic,
		p.ClaimBodyBytes,
		p.ProposedAtUnixMS,
	)
	if err != nil {
		return fmt.Errorf("reconstruct proposal: %w", err)
	}

	if err := proposal.VerifySignature(p.ProposerSignature); err != nil {
		return fmt.Errorf("verify proposer signature: %w", err)
	}

	acceptance, err := NewAcceptance(proposal.Digest(), p.AcceptedAtUnixMS)
	if err != nil {
		return fmt.Errorf("reconstruct acceptance: %w", err)
	}

	if err := acceptance.VerifySignature(p.SubjectPublicKey, p.SubjectSignature); err != nil {
		return fmt.Errorf("verify subject signature: %w", err)
	}

	return nil
}

// EndorsementRevocation represents an off-ledger revocation request.
type EndorsementRevocation struct {
	TargetRecordDigest [32]byte
	RevokerPublicKey   *ecdsa.PublicKey
	Role               RevokerRole
	RevokedAtUnixMS    uint64
	Reason             string
}

type revocationStatementWire struct {
	Version            uint64 `cbor:"0,keyasint"`
	TargetRecordDigest []byte `cbor:"1,keyasint"`
	RevokerPublicKey   []byte `cbor:"2,keyasint"`
	Role               uint64 `cbor:"3,keyasint"`
	RevokedAtUnixMS    uint64 `cbor:"4,keyasint"`
	Reason             string `cbor:"5,keyasint"`
}

// NewRevocation creates a new revocation statement.
func NewRevocation(
	targetDigest [32]byte,
	revokerPub *ecdsa.PublicKey,
	role RevokerRole,
	revokedAt uint64,
	reason string,
) (*EndorsementRevocation, error) {
	if revokerPub == nil {
		return nil, errors.New("revoker public key is required")
	}
	if role != RevokerRoleProposer && role != RevokerRoleSubject {
		return nil, fmt.Errorf("invalid revoker role: %d", role)
	}
	if len(reason) > maxReasonLength {
		return nil, fmt.Errorf("reason length %d exceeds max %d", len(reason), maxReasonLength)
	}
	return &EndorsementRevocation{
		TargetRecordDigest: targetDigest,
		RevokerPublicKey:   revokerPub,
		Role:               role,
		RevokedAtUnixMS:    revokedAt,
		Reason:             reason,
	}, nil
}

// Digest computes the domain-separated digest of the revocation statement.
func (r *EndorsementRevocation) Digest() [32]byte {
	wire := revocationStatementWire{
		Version:            PayloadVersionV1,
		TargetRecordDigest: r.TargetRecordDigest[:],
		RevokerPublicKey:   encodePublicKey(r.RevokerPublicKey),
		Role:               uint64(r.Role),
		RevokedAtUnixMS:    r.RevokedAtUnixMS,
		Reason:             r.Reason,
	}
	encoded, err := cborEncMode.Marshal(wire)
	if err != nil {
		panic("marshal revocation statement: " + err.Error())
	}
	return domainSeparatedDigest(domainRevocationV1, encoded)
}

// Sign signs the revocation using the revoker's private key.
func (r *EndorsementRevocation) Sign(revokerKey *ecdsa.PrivateKey) ([]byte, error) {
	if revokerKey == nil {
		return nil, errors.New("private key cannot be nil")
	}
	digest := r.Digest()
	return ecdsa.SignASN1(rand.Reader, revokerKey, digest[:])
}

// VerifySignature verifies the revoker's signature.
func (r *EndorsementRevocation) VerifySignature(signatureBytes []byte) error {
	if len(signatureBytes) < minSignatureBytes || len(signatureBytes) > maxSignatureBytes {
		return fmt.Errorf("signature length %d out of bounds", len(signatureBytes))
	}
	digest := r.Digest()
	if !ecdsa.VerifyASN1(r.RevokerPublicKey, digest[:], signatureBytes) {
		return errors.New("revoker signature verification failed")
	}
	return nil
}

// EndorsementRevokedPayload represents the complete on-ledger revocation event payload.
type EndorsementRevokedPayload struct {
	TargetRecordDigest [32]byte
	RevokerPublicKey   *ecdsa.PublicKey
	Role               RevokerRole
	RevokedAtUnixMS    uint64
	Reason             string
	RevokerSignature   []byte
}

type revokedPayloadWire struct {
	Version            uint64 `cbor:"0,keyasint"`
	TargetRecordDigest []byte `cbor:"1,keyasint"`
	RevokerPublicKey   []byte `cbor:"2,keyasint"`
	Role               uint64 `cbor:"3,keyasint"`
	RevokedAtUnixMS    uint64 `cbor:"4,keyasint"`
	Reason             string `cbor:"5,keyasint"`
	RevokerSignature   []byte `cbor:"6,keyasint"`
}

// NewRevokedPayload creates a new revoked payload.
func NewRevokedPayload(
	targetDigest [32]byte,
	revokerPub *ecdsa.PublicKey,
	role RevokerRole,
	revokedAt uint64,
	reason string,
	signatureBytes []byte,
) (*EndorsementRevokedPayload, error) {
	if revokerPub == nil {
		return nil, errors.New("revoker public key is required")
	}
	if role != RevokerRoleProposer && role != RevokerRoleSubject {
		return nil, fmt.Errorf("invalid revoker role: %d", role)
	}
	if len(reason) > maxReasonLength {
		return nil, fmt.Errorf("reason length %d exceeds max %d", len(reason), maxReasonLength)
	}
	if len(signatureBytes) < minSignatureBytes || len(signatureBytes) > maxSignatureBytes {
		return nil, fmt.Errorf("signature length %d out of bounds", len(signatureBytes))
	}
	return &EndorsementRevokedPayload{
		TargetRecordDigest: targetDigest,
		RevokerPublicKey:   revokerPub,
		Role:               role,
		RevokedAtUnixMS:    revokedAt,
		Reason:             reason,
		RevokerSignature:   bytes.Clone(signatureBytes),
	}, nil
}

// Encode serializes the revoked payload to canonical CBOR.
func (r *EndorsementRevokedPayload) Encode() ([]byte, error) {
	wire := revokedPayloadWire{
		Version:            PayloadVersionV1,
		TargetRecordDigest: r.TargetRecordDigest[:],
		RevokerPublicKey:   encodePublicKey(r.RevokerPublicKey),
		Role:               uint64(r.Role),
		RevokedAtUnixMS:    r.RevokedAtUnixMS,
		Reason:             r.Reason,
		RevokerSignature:   r.RevokerSignature,
	}
	return cborEncMode.Marshal(wire)
}

// DecodeRevokedPayload parses and structurally validates canonical CBOR bytes for a revocation payload.
func DecodeRevokedPayload(encoded []byte) (*EndorsementRevokedPayload, error) {
	var wire revokedPayloadWire
	if err := cborDecMode.Unmarshal(encoded, &wire); err != nil {
		return nil, fmt.Errorf("decode revoked payload: %w", err)
	}
	if wire.Version != PayloadVersionV1 {
		return nil, fmt.Errorf("unsupported payload version %d", wire.Version)
	}
	if len(wire.TargetRecordDigest) != 32 {
		return nil, errors.New("target record digest must be 32 bytes")
	}
	var targetDigest [32]byte
	copy(targetDigest[:], wire.TargetRecordDigest)

	revokerPub, err := decodePublicKey(wire.RevokerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode revoker public key: %w", err)
	}

	return NewRevokedPayload(
		targetDigest,
		revokerPub,
		RevokerRole(wire.Role),
		wire.RevokedAtUnixMS,
		wire.Reason,
		wire.RevokerSignature,
	)
}

// Verify validates the revoker signature.
func (r *EndorsementRevokedPayload) Verify() error {
	revocation, err := NewRevocation(
		r.TargetRecordDigest,
		r.RevokerPublicKey,
		r.Role,
		r.RevokedAtUnixMS,
		r.Reason,
	)
	if err != nil {
		return fmt.Errorf("reconstruct revocation: %w", err)
	}
	return revocation.VerifySignature(r.RevokerSignature)
}

func encodePublicKey(pub *ecdsa.PublicKey) []byte {
	if pub == nil {
		return nil
	}
	bytes, err := pub.Bytes()
	if err != nil {
		panic("encode ecdsa public key: " + err.Error())
	}
	return bytes
}

func decodePublicKey(b []byte) (*ecdsa.PublicKey, error) {
	if len(b) == 0 {
		return nil, errors.New("empty public key bytes")
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b)
	if err != nil {
		return nil, fmt.Errorf("parse uncompressed P-256 public key: %w", err)
	}
	return pub, nil
}

func domainSeparatedDigest(domain string, body []byte) [32]byte {
	preimage := make([]byte, 0, len(domain)+1+len(body))
	preimage = append(preimage, domain...)
	preimage = append(preimage, 0)
	preimage = append(preimage, body...)
	return sha256.Sum256(preimage)
}
