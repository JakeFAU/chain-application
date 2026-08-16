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

	// The generic decode above already carries the exact key set, so the key
	// check reuses it rather than decoding the same bytes a second time.
	if !hasExactKeys(generic, expectedKeys) {
		return wrapValidationError(stage, ErrSchemaViolation)
	}

	if err := protocolDecMode.Unmarshal(encoded, destination); err != nil {
		return wrapValidationError(stage, classifyDecodeError(err))
	}
	// Typed re-encoding rejects values such as null that decode as a no-op into
	// a required Go field, while preserving null for explicitly nullable fields.
	typedReencoded, err := protocolEncMode.Marshal(destination)
	if err != nil {
		return wrapValidationError(stage, ErrSchemaViolation)
	}
	if !bytes.Equal(encoded, typedReencoded) {
		return wrapValidationError(stage, ErrNonConformingCBOR)
	}
	return nil
}

func hasExactKeys(values map[uint64]any, expectedKeys []uint64) bool {
	if len(values) != len(expectedKeys) {
		return false
	}
	expected := make(map[uint64]struct{}, len(expectedKeys))
	for _, key := range expectedKeys {
		if _, exists := expected[key]; exists {
			return false
		}
		expected[key] = struct{}{}
	}
	for key := range values {
		if _, ok := expected[key]; !ok {
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
