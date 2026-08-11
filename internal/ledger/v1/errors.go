package ledgerv1

import (
	"errors"
	"fmt"
)

var (
	ErrOversizedInput     = errors.New("ledger v1 input exceeds protocol limit")
	ErrMalformedCBOR      = errors.New("ledger v1 malformed CBOR")
	ErrNonConformingCBOR  = errors.New("ledger v1 non-conforming CBOR")
	ErrUnsupportedVersion = errors.New("ledger v1 unsupported version")
	ErrSchemaViolation    = errors.New("ledger v1 schema violation")
	ErrDigestMismatch     = errors.New("ledger v1 digest mismatch")
	ErrInvalidSequence    = errors.New("ledger v1 invalid sequence")
	ErrLedgerMismatch     = errors.New("ledger v1 ledger identity mismatch")
	ErrChainLinkMismatch  = errors.New("ledger v1 chain link mismatch")
	ErrUnsupportedEvent   = errors.New("ledger v1 unsupported event")
)

type validationStage string

const (
	stageEncoding     validationStage = "encoding"
	stageDecoding     validationStage = "decoding"
	stageEventBody    validationStage = "event body"
	stageRecordBody   validationStage = "record body"
	stageLedgerRecord validationStage = "ledger record"
	stagePayload      validationStage = "payload"
)

func wrapValidationError(stage validationStage, err error) error {
	return fmt.Errorf("ledger v1 %s: %w", safeStageLabel(stage), err)
}

func safeStageLabel(stage validationStage) string {
	switch stage {
	case stageEncoding, stageDecoding, stageEventBody, stageRecordBody, stageLedgerRecord, stagePayload:
		return string(stage)
	default:
		return "validation"
	}
}
