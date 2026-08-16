package ledgerv1

import "bytes"

type StructuralRecord struct {
	bytes              []byte
	recordBodyBytes    []byte
	recordDigest       Digest
	event              StructuralEvent
	signerKeyReference string
	signatureBytes     []byte
}

type recordBodyWire struct {
	RecordVersion        uint64 `cbor:"0,keyasint"`
	EventBodyBytes       []byte `cbor:"1,keyasint"`
	EventDigestAlgorithm string `cbor:"2,keyasint"`
	EventDigest          []byte `cbor:"3,keyasint"`
	SignerKeyReference   string `cbor:"4,keyasint"`
	SignatureAlgorithm   string `cbor:"5,keyasint"`
	SignatureEncoding    string `cbor:"6,keyasint"`
	SignatureBytes       []byte `cbor:"7,keyasint"`
}

type ledgerRecordWire struct {
	RecordVersion         uint64 `cbor:"0,keyasint"`
	RecordBodyBytes       []byte `cbor:"1,keyasint"`
	RecordDigestAlgorithm string `cbor:"2,keyasint"`
	RecordDigest          []byte `cbor:"3,keyasint"`
}

func NewRecord(event StructuralEvent, signerKeyReference string, signatureBytes []byte) (StructuralRecord, error) {
	if len(event.bytes) == 0 {
		return StructuralRecord{}, wrapValidationError(stageEventBody, ErrSchemaViolation)
	}
	validatedEvent, err := ValidateEventStructure(event.bytes)
	if err != nil {
		return StructuralRecord{}, err
	}
	if err := validateSignerKeyReference(signerKeyReference); err != nil {
		return StructuralRecord{}, wrapValidationError(stageRecordBody, err)
	}
	if !isValidSignatureLength(signatureBytes) {
		return StructuralRecord{}, wrapValidationError(stageRecordBody, ErrSchemaViolation)
	}

	eventDigest := validatedEvent.Digest()
	recordBodyBytes, err := encodeCanonical(recordBodyWire{
		RecordVersion:        recordVersionV1,
		EventBodyBytes:       validatedEvent.Bytes(),
		EventDigestAlgorithm: digestAlgorithmSHA256,
		EventDigest:          bytes.Clone(eventDigest[:]),
		SignerKeyReference:   signerKeyReference,
		SignatureAlgorithm:   signatureAlgorithmP256,
		SignatureEncoding:    signatureEncodingASN1DER,
		SignatureBytes:       bytes.Clone(signatureBytes),
	}, maxRecordBodyBytes, stageRecordBody)
	if err != nil {
		return StructuralRecord{}, err
	}
	recordDigest := digestRecordBody(recordBodyBytes)
	encoded, err := encodeCanonical(ledgerRecordWire{
		RecordVersion:         recordVersionV1,
		RecordBodyBytes:       recordBodyBytes,
		RecordDigestAlgorithm: digestAlgorithmSHA256,
		RecordDigest:          bytes.Clone(recordDigest[:]),
	}, maxLedgerRecordBytes, stageLedgerRecord)
	if err != nil {
		return StructuralRecord{}, err
	}
	return ValidateRecordStructure(encoded)
}

func ValidateRecordStructure(encoded []byte) (StructuralRecord, error) {
	var outer ledgerRecordWire
	if err := decodeCanonicalMap(
		encoded,
		maxLedgerRecordBytes,
		[]uint64{
			ledgerRecordKeyVersion,
			ledgerRecordKeyRecordBodyBytes,
			ledgerRecordKeyDigestAlgorithm,
			ledgerRecordKeyDigest,
		},
		&outer,
		stageLedgerRecord,
	); err != nil {
		return StructuralRecord{}, err
	}
	if err := validateLedgerRecordWire(outer); err != nil {
		return StructuralRecord{}, wrapValidationError(stageLedgerRecord, err)
	}

	var body recordBodyWire
	if err := decodeCanonicalMap(
		outer.RecordBodyBytes,
		maxRecordBodyBytes,
		[]uint64{
			recordBodyKeyVersion,
			recordBodyKeyEventBodyBytes,
			recordBodyKeyEventDigestAlgorithm,
			recordBodyKeyEventDigest,
			recordBodyKeySignerKeyReference,
			recordBodyKeySignatureAlgorithm,
			recordBodyKeySignatureEncoding,
			recordBodyKeySignatureBytes,
		},
		&body,
		stageRecordBody,
	); err != nil {
		return StructuralRecord{}, err
	}
	if err := validateRecordBodyWire(body); err != nil {
		return StructuralRecord{}, wrapValidationError(stageRecordBody, err)
	}
	if outer.RecordVersion != body.RecordVersion {
		return StructuralRecord{}, wrapValidationError(stageLedgerRecord, ErrSchemaViolation)
	}

	recordDigest := digestRecordBody(outer.RecordBodyBytes)
	if !bytes.Equal(recordDigest[:], outer.RecordDigest) {
		return StructuralRecord{}, wrapValidationError(stageLedgerRecord, ErrDigestMismatch)
	}
	event, err := ValidateEventStructure(body.EventBodyBytes)
	if err != nil {
		return StructuralRecord{}, err
	}
	eventDigest := event.Digest()
	if !bytes.Equal(eventDigest[:], body.EventDigest) {
		return StructuralRecord{}, wrapValidationError(stageRecordBody, ErrDigestMismatch)
	}

	return StructuralRecord{
		bytes:              bytes.Clone(encoded),
		recordBodyBytes:    bytes.Clone(outer.RecordBodyBytes),
		recordDigest:       recordDigest,
		event:              event,
		signerKeyReference: body.SignerKeyReference,
		signatureBytes:     bytes.Clone(body.SignatureBytes),
	}, nil
}

func (record StructuralRecord) Bytes() []byte {
	return bytes.Clone(record.bytes)
}

func (record StructuralRecord) RecordBodyBytes() []byte {
	return bytes.Clone(record.recordBodyBytes)
}

func (record StructuralRecord) RecordDigest() Digest {
	return record.recordDigest
}

func (record StructuralRecord) Event() StructuralEvent {
	return record.event
}

func (record StructuralRecord) SignerKeyReference() string {
	return record.signerKeyReference
}

func (record StructuralRecord) SignatureBytes() []byte {
	return bytes.Clone(record.signatureBytes)
}

func (record StructuralRecord) SignatureStatus() SignatureStatus {
	return SignatureStatusUnverified
}

func validateLedgerRecordWire(wire ledgerRecordWire) error {
	if wire.RecordVersion != recordVersionV1 {
		return ErrUnsupportedVersion
	}
	if len(wire.RecordBodyBytes) == 0 || len(wire.RecordBodyBytes) > maxRecordBodyBytes {
		return ErrSchemaViolation
	}
	if wire.RecordDigestAlgorithm != digestAlgorithmSHA256 || len(wire.RecordDigest) != digestBytes {
		return ErrSchemaViolation
	}
	return nil
}

func validateRecordBodyWire(wire recordBodyWire) error {
	if wire.RecordVersion != recordVersionV1 {
		return ErrUnsupportedVersion
	}
	if len(wire.EventBodyBytes) == 0 || len(wire.EventBodyBytes) > maxEventBodyBytes {
		return ErrSchemaViolation
	}
	if wire.EventDigestAlgorithm != digestAlgorithmSHA256 || len(wire.EventDigest) != digestBytes {
		return ErrSchemaViolation
	}
	if err := validateSignerKeyReference(wire.SignerKeyReference); err != nil {
		return err
	}
	if wire.SignatureAlgorithm != signatureAlgorithmP256 || wire.SignatureEncoding != signatureEncodingASN1DER {
		return ErrSchemaViolation
	}
	if !isValidSignatureLength(wire.SignatureBytes) {
		return ErrSchemaViolation
	}
	return nil
}

func validateSignerKeyReference(reference string) error {
	if len(reference) == 0 || len(reference) > maxSignerReferenceBytes {
		return ErrSchemaViolation
	}
	for index := range len(reference) {
		if reference[index] < 0x20 || reference[index] > 0x7e {
			return ErrSchemaViolation
		}
	}
	return nil
}

func isValidSignatureLength(signatureBytes []byte) bool {
	return len(signatureBytes) >= minSignatureBytes && len(signatureBytes) <= maxSignatureBytes
}

func digestRecordBody(recordBodyBytes []byte) Digest {
	return domainSeparatedDigest(domainRecordDigestV1, recordBodyBytes)
}
