package ledgerv1

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

const (
	goldenEventBodyHex   = "a80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a0"
	goldenEventDigestHex = "26bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df72"
)

func TestNewGenesisEvent(t *testing.T) {
	t.Parallel()

	ledgerID := testLedgerID()
	event, err := NewGenesisEvent(ledgerID, 1_735_689_600_123)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}

	if got := hex.EncodeToString(event.Bytes()); got != goldenEventBodyHex {
		t.Fatalf("event bytes = %s, want %s", got, goldenEventBodyHex)
	}
	digest := event.Digest()
	if got := hex.EncodeToString(digest[:]); got != goldenEventDigestHex {
		t.Fatalf("event digest = %s, want %s", got, goldenEventDigestHex)
	}
	if got := event.LedgerID(); got != ledgerID {
		t.Fatalf("ledger ID = %x, want %x", got, ledgerID)
	}
	if got := event.Sequence(); got != 1 {
		t.Fatalf("sequence = %d, want 1", got)
	}
	if digest, ok := event.PreviousRecordDigest(); ok || digest != (Digest{}) {
		t.Fatalf("previous record digest = (%x, %t), want (zero, false)", digest, ok)
	}
	if got := event.AdmittedAtUnixMS(); got != 1_735_689_600_123 {
		t.Fatalf("admitted at = %d, want 1735689600123", got)
	}
	if got := event.Kind(); got != EventKindLedgerInitialized {
		t.Fatalf("kind = %d, want %d", got, EventKindLedgerInitialized)
	}
	if got := event.PayloadVersion(); got != ledgerInitializedPayloadVersionV1 {
		t.Fatalf("payload version = %d, want %d", got, ledgerInitializedPayloadVersionV1)
	}
	if got := event.PayloadBytes(); !bytes.Equal(got, []byte{0xa0}) {
		t.Fatalf("payload bytes = %x, want a0", got)
	}
	if err := ValidateEventSemantics(event); err != nil {
		t.Fatalf("ValidateEventSemantics: %v", err)
	}

	mutatedBody := event.Bytes()
	mutatedBody[0] ^= 0xff
	if got := hex.EncodeToString(event.Bytes()); got != goldenEventBodyHex {
		t.Fatalf("event body mutated through accessor: %s", got)
	}
	mutatedPayload := event.PayloadBytes()
	mutatedPayload[0] ^= 0xff
	if got := event.PayloadBytes(); !bytes.Equal(got, []byte{0xa0}) {
		t.Fatalf("payload mutated through accessor: %x", got)
	}
}

func TestNewGenesisEventAcceptsAnyUnsignedTimestamp(t *testing.T) {
	t.Parallel()

	event, err := NewGenesisEvent(testLedgerID(), 0)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	if got := event.AdmittedAtUnixMS(); got != 0 {
		t.Fatalf("admitted at = %d, want 0", got)
	}
}

func TestNewEventContinuation(t *testing.T) {
	t.Parallel()

	ledgerID := testLedgerID()
	prevDigest := Digest{0x01, 0x02, 0x03, 0x04}
	payload := []byte{0xa1, 0x00, 0x01} // valid canonical CBOR map {0: 1}

	event, err := NewEvent(
		ledgerID,
		2,
		&prevDigest,
		1_735_689_600_500,
		EventKind(100),
		1,
		payload,
	)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	if event.Sequence() != 2 {
		t.Fatalf("sequence = %d, want 2", event.Sequence())
	}
	if digest, ok := event.PreviousRecordDigest(); !ok || digest != prevDigest {
		t.Fatalf("previous digest = (%x, %t), want (%x, true)", digest, ok, prevDigest)
	}
	if event.Kind() != EventKind(100) {
		t.Fatalf("kind = %d, want 100", event.Kind())
	}
	if event.PayloadVersion() != 1 {
		t.Fatalf("payload version = %d, want 1", event.PayloadVersion())
	}
	if !bytes.Equal(event.PayloadBytes(), payload) {
		t.Fatalf("payload bytes = %x, want %x", event.PayloadBytes(), payload)
	}
}

func TestValidateEventStructureRejectsInvalidBodies(t *testing.T) {
	t.Parallel()

	valid := testGenesisEventBodyWire()
	tests := []struct {
		name   string
		mutate func(*eventBodyWire)
		want   error
	}{
		{
			name: "protocol version zero",
			mutate: func(wire *eventBodyWire) {
				wire.ProtocolVersion = 0
			},
			want: ErrUnsupportedVersion,
		},
		{
			name: "protocol version two",
			mutate: func(wire *eventBodyWire) {
				wire.ProtocolVersion = 2
			},
			want: ErrUnsupportedVersion,
		},
		{
			name: "sequence zero",
			mutate: func(wire *eventBodyWire) {
				wire.Sequence = 0
			},
			want: ErrInvalidSequence,
		},
		{
			name: "event kind zero",
			mutate: func(wire *eventBodyWire) {
				wire.EventKind = 0
			},
			want: ErrSchemaViolation,
		},
		{
			name: "payload version zero",
			mutate: func(wire *eventBodyWire) {
				wire.PayloadVersion = 0
			},
			want: ErrSchemaViolation,
		},
		{
			name: "empty predecessor digest",
			mutate: func(wire *eventBodyWire) {
				wire.PreviousRecordDigest = []byte{}
			},
			want: ErrSchemaViolation,
		},
		{
			name: "short predecessor digest",
			mutate: func(wire *eventBodyWire) {
				wire.PreviousRecordDigest = make([]byte, digestBytes-1)
			},
			want: ErrSchemaViolation,
		},
		{
			name: "empty payload",
			mutate: func(wire *eventBodyWire) {
				wire.PayloadBytes = []byte{}
			},
			want: ErrSchemaViolation,
		},
		{
			name: "oversized payload",
			mutate: func(wire *eventBodyWire) {
				wire.PayloadBytes = make([]byte, maxPayloadBytes+1)
			},
			want: ErrSchemaViolation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := valid
			test.mutate(&wire)
			encoded := encodeEventBodyForTest(t, wire)
			_, err := ValidateEventStructure(encoded)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateEventStructure error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateEventSemanticsRejectsInvalidGenesis(t *testing.T) {
	t.Parallel()

	valid := testGenesisEventBodyWire()
	tests := []struct {
		name   string
		mutate func(*eventBodyWire)
		want   error
	}{
		{
			name: "sequence one has predecessor",
			mutate: func(wire *eventBodyWire) {
				wire.PreviousRecordDigest = make([]byte, digestBytes)
			},
			want: ErrInvalidSequence,
		},
		{
			name: "ledger initialized has sequence two",
			mutate: func(wire *eventBodyWire) {
				wire.Sequence = 2
			},
			want: ErrInvalidSequence,
		},
		{
			name: "genesis payload is not empty map",
			mutate: func(wire *eventBodyWire) {
				wire.PayloadBytes = []byte{0xa1, 0x00, 0x00}
			},
			want: ErrSchemaViolation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := valid
			test.mutate(&wire)
			event, err := ValidateEventStructure(encodeEventBodyForTest(t, wire))
			if err != nil {
				t.Fatalf("ValidateEventStructure: %v", err)
			}
			if err := ValidateEventSemantics(event); !errors.Is(err, test.want) {
				t.Fatalf("ValidateEventSemantics error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateEventStructureAcceptsUnknownEvent(t *testing.T) {
	t.Parallel()

	wire := testGenesisEventBodyWire()
	wire.EventKind = 2
	wire.PayloadBytes = []byte{0xff}
	event, err := ValidateEventStructure(encodeEventBodyForTest(t, wire))
	if err != nil {
		t.Fatalf("ValidateEventStructure: %v", err)
	}
	if got := event.Kind(); got != 2 {
		t.Fatalf("kind = %d, want 2", got)
	}
	if got := event.PayloadBytes(); !bytes.Equal(got, wire.PayloadBytes) {
		t.Fatalf("unknown payload bytes = %x, want %x", got, wire.PayloadBytes)
	}
	if err := ValidateEventSemantics(event); !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("ValidateEventSemantics error = %v, want ErrUnsupportedEvent", err)
	}
}

func TestValidateEventSemanticsRejectsZeroStructuralEvent(t *testing.T) {
	t.Parallel()

	if err := ValidateEventSemantics(StructuralEvent{}); !errors.Is(err, ErrSchemaViolation) {
		t.Fatalf("ValidateEventSemantics error = %v, want ErrSchemaViolation", err)
	}
}

func testLedgerID() LedgerID {
	var ledgerID LedgerID
	for index := range ledgerID {
		ledgerID[index] = byte(index)
	}
	return ledgerID
}

func testGenesisEventBodyWire() eventBodyWire {
	ledgerID := testLedgerID()
	return eventBodyWire{
		ProtocolVersion:  protocolVersionV1,
		LedgerID:         ledgerID[:],
		Sequence:         1,
		AdmittedAtUnixMS: 1_735_689_600_123,
		EventKind:        uint64(EventKindLedgerInitialized),
		PayloadVersion:   ledgerInitializedPayloadVersionV1,
		PayloadBytes:     []byte{0xa0},
	}
}

func encodeEventBodyForTest(t *testing.T, wire eventBodyWire) []byte {
	t.Helper()

	encoded, err := encodeCanonical(wire, maxEventBodyBytes, stageEventBody)
	if err != nil {
		t.Fatalf("encodeCanonical: %v", err)
	}
	return encoded
}
