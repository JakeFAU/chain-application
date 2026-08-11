package ledgerv1

import (
	"bytes"
	"errors"

	"github.com/fxamacker/cbor/v2"
)

var protocolEncMode = mustProtocolEncMode()

var protocolDecMode = mustProtocolDecMode()

func mustProtocolEncMode() cbor.EncMode {
	mode, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	return mode
}

func mustProtocolDecMode() cbor.DecMode {
	mode, err := (cbor.DecOptions{
		DupMapKey:         cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:   maxProtocolNestingLevels,
		MaxArrayElements:  maxProtocolArrayElements,
		MaxMapPairs:       maxProtocolMapPairs,
		IndefLength:       cbor.IndefLengthForbidden,
		TagsMd:            cbor.TagsForbidden,
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,
		UTF8:              cbor.UTF8RejectInvalid,
		FieldNameMatching: cbor.FieldNameMatchingCaseSensitive,
	}).DecMode()
	if err != nil {
		panic(err)
	}
	return mode
}

//lint:ignore U1000 Task one establishes this helper for later ledger protocol kernel tasks.
func encodeCanonical(value any, maxBytes int, stage validationStage) ([]byte, error) {
	encoded, err := protocolEncMode.Marshal(value)
	if err != nil {
		return nil, wrapValidationError(stage, ErrSchemaViolation)
	}
	if len(encoded) > maxBytes {
		return nil, wrapValidationError(stage, ErrOversizedInput)
	}
	return encoded, nil
}

func decodeCanonicalMap(
	encoded []byte,
	maxBytes int,
	expectedKeys []uint64,
	destination any,
	stage validationStage,
) error {
	if len(encoded) > maxBytes {
		return wrapValidationError(stage, ErrOversizedInput)
	}

	var generic map[uint64]any
	if err := protocolDecMode.Unmarshal(encoded, &generic); err != nil {
		return wrapValidationError(stage, classifyDecodeError(err))
	}

	reencoded, err := protocolEncMode.Marshal(generic)
	if err != nil {
		return wrapValidationError(stage, ErrSchemaViolation)
	}
	if !bytes.Equal(encoded, reencoded) {
		return wrapValidationError(stage, ErrNonConformingCBOR)
	}

	var rawMap map[uint64]cbor.RawMessage
	if err := protocolDecMode.Unmarshal(encoded, &rawMap); err != nil {
		return wrapValidationError(stage, classifyDecodeError(err))
	}
	if !hasExactKeys(rawMap, expectedKeys) {
		return wrapValidationError(stage, ErrSchemaViolation)
	}

	if err := protocolDecMode.Unmarshal(encoded, destination); err != nil {
		return wrapValidationError(stage, classifyDecodeError(err))
	}
	return nil
}

func hasExactKeys(values map[uint64]cbor.RawMessage, expectedKeys []uint64) bool {
	if len(values) != len(expectedKeys) {
		return false
	}
	for _, key := range expectedKeys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func classifyDecodeError(err error) error {
	var (
		syntaxErr     *cbor.SyntaxError
		semanticErr   *cbor.SemanticError
		duplicateErr  *cbor.DupMapKeyError
		indefiniteErr *cbor.IndefiniteLengthError
		tagsErr       *cbor.TagsMdError
		extraneousErr *cbor.ExtraneousDataError
		typeErr       *cbor.UnmarshalTypeError
		unknownErr    *cbor.UnknownFieldError
		depthErr      *cbor.MaxNestedLevelError
		arrayErr      *cbor.MaxArrayElementsError
		mapErr        *cbor.MaxMapPairsError
	)

	switch {
	case errors.As(err, &syntaxErr), errors.As(err, &extraneousErr):
		return ErrMalformedCBOR
	case errors.As(err, &semanticErr), errors.As(err, &duplicateErr),
		errors.As(err, &indefiniteErr), errors.As(err, &tagsErr):
		return ErrNonConformingCBOR
	case errors.As(err, &typeErr), errors.As(err, &unknownErr),
		errors.As(err, &depthErr), errors.As(err, &arrayErr), errors.As(err, &mapErr):
		return ErrSchemaViolation
	default:
		return ErrMalformedCBOR
	}
}
