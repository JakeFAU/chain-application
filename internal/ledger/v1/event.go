package ledgerv1

import "bytes"

type StructuralEvent struct {
	bytes                   []byte
	digest                  Digest
	ledgerID                LedgerID
	sequence                uint64
	previousRecordDigest    Digest
	hasPreviousRecordDigest bool
	admittedAtUnixMS        uint64
	kind                    EventKind
	payloadVersion          uint64
	payloadBytes            []byte
}

type eventBodyWire struct {
	ProtocolVersion      uint64 `cbor:"0,keyasint"`
	LedgerID             []byte `cbor:"1,keyasint"`
	Sequence             uint64 `cbor:"2,keyasint"`
	PreviousRecordDigest []byte `cbor:"3,keyasint"`
	AdmittedAtUnixMS     uint64 `cbor:"4,keyasint"`
	EventKind            uint64 `cbor:"5,keyasint"`
	PayloadVersion       uint64 `cbor:"6,keyasint"`
	PayloadBytes         []byte `cbor:"7,keyasint"`
}

// NewEvent constructs and structurally validates an arbitrary event body envelope.
func NewEvent(
	ledgerID LedgerID,
	sequence uint64,
	previousRecordDigest *Digest,
	admittedAtUnixMS uint64,
	kind EventKind,
	payloadVersion uint64,
	payloadBytes []byte,
) (StructuralEvent, error) {
	var prevBytes []byte
	if previousRecordDigest != nil {
		prevBytes = bytes.Clone(previousRecordDigest[:])
	}
	wire := eventBodyWire{
		ProtocolVersion:      protocolVersionV1,
		LedgerID:             bytes.Clone(ledgerID[:]),
		Sequence:             sequence,
		PreviousRecordDigest: prevBytes,
		AdmittedAtUnixMS:     admittedAtUnixMS,
		EventKind:            uint64(kind),
		PayloadVersion:       payloadVersion,
		PayloadBytes:         bytes.Clone(payloadBytes),
	}

	encoded, err := encodeCanonical(wire, maxEventBodyBytes, stageEventBody)
	if err != nil {
		return StructuralEvent{}, err
	}
	return ValidateEventStructure(encoded)
}

func NewGenesisEvent(ledgerID LedgerID, admittedAtUnixMS uint64) (StructuralEvent, error) {
	payloadBytes := []byte(ledgerInitializedPayloadCBOR)
	if err := validateLedgerInitializedPayload(payloadBytes); err != nil {
		return StructuralEvent{}, err
	}
	return NewEvent(
		ledgerID,
		1,
		nil,
		admittedAtUnixMS,
		EventKindLedgerInitialized,
		ledgerInitializedPayloadVersionV1,
		payloadBytes,
	)
}

func ValidateEventStructure(encoded []byte) (StructuralEvent, error) {
	var wire eventBodyWire
	if err := decodeCanonicalMap(
		encoded,
		maxEventBodyBytes,
		[]uint64{
			eventBodyKeyProtocolVersion,
			eventBodyKeyLedgerID,
			eventBodyKeySequence,
			eventBodyKeyPreviousRecordDigest,
			eventBodyKeyAdmittedAtUnixMS,
			eventBodyKeyEventKind,
			eventBodyKeyPayloadVersion,
			eventBodyKeyPayloadBytes,
		},
		&wire,
		stageEventBody,
	); err != nil {
		return StructuralEvent{}, err
	}
	if err := validateEventBodyWire(wire); err != nil {
		return StructuralEvent{}, wrapValidationError(stageEventBody, err)
	}

	event := StructuralEvent{
		bytes:            bytes.Clone(encoded),
		digest:           digestEventBody(encoded),
		sequence:         wire.Sequence,
		admittedAtUnixMS: wire.AdmittedAtUnixMS,
		kind:             EventKind(wire.EventKind),
		payloadVersion:   wire.PayloadVersion,
		payloadBytes:     bytes.Clone(wire.PayloadBytes),
	}
	copy(event.ledgerID[:], wire.LedgerID)
	if wire.PreviousRecordDigest != nil {
		event.hasPreviousRecordDigest = true
		copy(event.previousRecordDigest[:], wire.PreviousRecordDigest)
	}
	return event, nil
}

func ValidateEventSemantics(event StructuralEvent) error {
	if len(event.bytes) == 0 {
		return wrapValidationError(stageEventBody, ErrSchemaViolation)
	}
	validated, err := ValidateEventStructure(event.bytes)
	if err != nil {
		return err
	}

	if validated.kind != EventKindLedgerInitialized ||
		validated.payloadVersion != ledgerInitializedPayloadVersionV1 {
		return wrapValidationError(stageEventBody, ErrUnsupportedEvent)
	}
	if validated.sequence != 1 || validated.hasPreviousRecordDigest {
		return wrapValidationError(stageEventBody, ErrInvalidSequence)
	}
	if err := validateLedgerInitializedPayload(validated.payloadBytes); err != nil {
		return err
	}
	return nil
}

func (event StructuralEvent) Bytes() []byte {
	return bytes.Clone(event.bytes)
}

func (event StructuralEvent) Digest() Digest {
	return event.digest
}

func (event StructuralEvent) LedgerID() LedgerID {
	return event.ledgerID
}

func (event StructuralEvent) Sequence() uint64 {
	return event.sequence
}

func (event StructuralEvent) PreviousRecordDigest() (Digest, bool) {
	return event.previousRecordDigest, event.hasPreviousRecordDigest
}

func (event StructuralEvent) AdmittedAtUnixMS() uint64 {
	return event.admittedAtUnixMS
}

func (event StructuralEvent) Kind() EventKind {
	return event.kind
}

func (event StructuralEvent) PayloadVersion() uint64 {
	return event.payloadVersion
}

func (event StructuralEvent) PayloadBytes() []byte {
	return bytes.Clone(event.payloadBytes)
}

func validateEventBodyWire(wire eventBodyWire) error {
	if wire.ProtocolVersion != protocolVersionV1 {
		return ErrUnsupportedVersion
	}
	if len(wire.LedgerID) != ledgerIDBytes {
		return ErrSchemaViolation
	}
	if wire.Sequence == 0 {
		return ErrInvalidSequence
	}
	if wire.PreviousRecordDigest != nil && len(wire.PreviousRecordDigest) != digestBytes {
		return ErrSchemaViolation
	}
	if wire.EventKind == 0 || wire.PayloadVersion == 0 {
		return ErrSchemaViolation
	}
	if len(wire.PayloadBytes) == 0 || len(wire.PayloadBytes) > maxPayloadBytes {
		return ErrSchemaViolation
	}
	return nil
}

func validateLedgerInitializedPayload(payloadBytes []byte) error {
	var payload struct{}
	if err := decodeCanonicalMap(
		payloadBytes,
		maxPayloadBytes,
		[]uint64{},
		&payload,
		stagePayload,
	); err != nil {
		return err
	}
	if !bytes.Equal(payloadBytes, []byte(ledgerInitializedPayloadCBOR)) {
		return wrapValidationError(stagePayload, ErrSchemaViolation)
	}
	return nil
}

func digestEventBody(eventBodyBytes []byte) Digest {
	return domainSeparatedDigest(domainEventDigestV1, eventBodyBytes)
}
