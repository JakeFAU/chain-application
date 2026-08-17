package ledgerv1

import (
	"errors"
	"math"
	"testing"
)

func TestValidateChainConsistencyAdvancesGenesis(t *testing.T) {
	t.Parallel()

	record := newGoldenGenesisRecord(t)
	state, err := ValidateChainConsistency(ChainState{}, record)
	if err != nil {
		t.Fatalf("ValidateChainConsistency: %v", err)
	}
	if !state.Initialized() || state.LastSequence() != 1 {
		t.Fatalf("state not initialized at sequence 1")
	}
	if ledgerID, ok := state.LedgerID(); !ok || ledgerID != record.Event().LedgerID() {
		t.Fatalf("LedgerID = (%x, %t), want (%x, true)", ledgerID, ok, record.Event().LedgerID())
	}
	if digest, ok := state.LastRecordDigest(); !ok || digest != record.RecordDigest() {
		t.Fatalf("LastRecordDigest = (%x, %t), want (%x, true)", digest, ok, record.RecordDigest())
	}
}

func TestUnknownContinuationAdvancesConsistencyButStopsReplay(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	chainState, err := ValidateChainConsistency(ChainState{}, genesis)
	if err != nil {
		t.Fatalf("advance genesis: %v", err)
	}
	unknown := newUnknownContinuationRecord(t, genesis, 2, 0)
	next, err := ValidateChainConsistency(chainState, unknown)
	if err != nil || next.LastSequence() != 2 {
		t.Fatalf("advance unknown = (%v, %v), want sequence 2", next, err)
	}
	replay, err := Apply(ReplayState{}, genesis)
	if err != nil {
		t.Fatalf("apply genesis: %v", err)
	}
	if nextReplay, err := Apply(replay, unknown); !errors.Is(err, ErrUnsupportedEvent) || nextReplay != replay {
		t.Fatalf("Apply unknown = (%v, %v), want unchanged replay and ErrUnsupportedEvent", nextReplay, err)
	}
}

func TestValidateChainConsistencyRejectsInvalidTransitionsWithoutChangingState(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	genesisState, err := ValidateChainConsistency(ChainState{}, genesis)
	if err != nil {
		t.Fatalf("advance genesis: %v", err)
	}
	continuation := newUnknownContinuationRecord(t, genesis, 2, 0)
	continuationState, err := ValidateChainConsistency(genesisState, continuation)
	if err != nil {
		t.Fatalf("advance continuation: %v", err)
	}

	genesisLedgerID := genesis.Event().LedgerID()
	otherLedgerID := testLedgerID()
	otherLedgerID[0] ^= 0xff
	wrongLedger := newUnknownRecord(t, otherLedgerID, 2, digestBytesFrom(genesis.RecordDigest()), 0)
	secondGenesis := newRecordFromWire(t, eventBodyWire{
		ProtocolVersion:      protocolVersionV1,
		LedgerID:             genesisLedgerID[:],
		Sequence:             2,
		PreviousRecordDigest: digestBytesFrom(genesis.RecordDigest()),
		AdmittedAtUnixMS:     0,
		EventKind:            uint64(EventKindLedgerInitialized),
		PayloadVersion:       ledgerInitializedPayloadVersionV1,
		PayloadBytes:         []byte(ledgerInitializedPayloadCBOR),
	})

	tests := []struct {
		name   string
		state  ChainState
		record StructuralRecord
		want   error
	}{
		{
			name:   "empty state receives unknown event",
			record: newUnknownRecord(t, genesisLedgerID, 1, nil, 0),
			want:   ErrUnsupportedEvent,
		},
		{name: "second genesis", state: genesisState, record: secondGenesis, want: ErrInvalidSequence},
		{name: "wrong ledger ID", state: genesisState, record: wrongLedger, want: ErrLedgerMismatch},
		{
			name:   "duplicate sequence",
			state:  continuationState,
			record: newUnknownContinuationRecord(t, genesis, 2, 0),
			want:   ErrInvalidSequence,
		},
		{
			name:   "reversed sequence",
			state:  continuationState,
			record: newUnknownContinuationRecord(t, continuation, 1, 0),
			want:   ErrInvalidSequence,
		},
		{
			name:   "skipped sequence",
			state:  continuationState,
			record: newUnknownContinuationRecord(t, continuation, 4, 0),
			want:   ErrInvalidSequence,
		},
		{
			name:   "null predecessor",
			state:  genesisState,
			record: newUnknownRecord(t, genesisLedgerID, 2, nil, 0),
			want:   ErrChainLinkMismatch,
		},
		{
			name:   "wrong predecessor",
			state:  genesisState,
			record: newUnknownRecord(t, genesisLedgerID, 2, make([]byte, digestBytes), 0),
			want:   ErrChainLinkMismatch,
		},
		{
			name: "sequence overflow",
			state: ChainState{
				initialized:      true,
				ledgerID:         genesisLedgerID,
				lastSequence:     math.MaxUint64,
				lastRecordDigest: genesis.RecordDigest(),
			},
			record: newUnknownContinuationRecord(t, genesis, 1, 0),
			want:   ErrInvalidSequence,
		},
		{name: "zero structural record", record: StructuralRecord{}, want: ErrSchemaViolation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.state
			next, err := ValidateChainConsistency(test.state, test.record)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateChainConsistency error = %v, want %v", err, test.want)
			}
			if test.state != before {
				t.Fatal("input state changed after failure")
			}
			if next != before {
				t.Fatalf("returned state = %v, want unchanged %v", next, before)
			}
		})
	}
}

func TestValidateChainConsistencyIgnoresTimestampOrdering(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	state, err := ValidateChainConsistency(ChainState{}, genesis)
	if err != nil {
		t.Fatalf("advance genesis: %v", err)
	}
	backwardTimestamp := newUnknownContinuationRecord(t, genesis, 2, 0)
	next, err := ValidateChainConsistency(state, backwardTimestamp)
	if err != nil {
		t.Fatalf("advance record with backward timestamp: %v", err)
	}
	if got := next.LastSequence(); got != 2 {
		t.Fatalf("LastSequence = %d, want 2", got)
	}
}

func TestApplyAdvancesGenesis(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	replay, err := Apply(ReplayState{}, genesis)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !replay.ChainState().Initialized() || replay.ChainState().LastSequence() != 1 {
		t.Fatalf("replay did not advance through genesis")
	}
}

func newGoldenGenesisRecord(t *testing.T) StructuralRecord {
	t.Helper()

	event, err := NewGenesisEvent(testLedgerID(), 1_735_689_600_123)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	record, err := NewRecord(event, goldenSignerKeyReference, goldenOpaqueSignature)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	return record
}

func newUnknownContinuationRecord(t *testing.T, predecessor StructuralRecord, sequence, admittedAtUnixMS uint64) StructuralRecord {
	t.Helper()

	return newUnknownRecord(
		t,
		predecessor.Event().LedgerID(),
		sequence,
		digestBytesFrom(predecessor.RecordDigest()),
		admittedAtUnixMS,
	)
}

func newUnknownRecord(t *testing.T, ledgerID LedgerID, sequence uint64, previousRecordDigest []byte, admittedAtUnixMS uint64) StructuralRecord {
	t.Helper()

	return newRecordFromWire(t, eventBodyWire{
		ProtocolVersion:      protocolVersionV1,
		LedgerID:             ledgerID[:],
		Sequence:             sequence,
		PreviousRecordDigest: previousRecordDigest,
		AdmittedAtUnixMS:     admittedAtUnixMS,
		EventKind:            2,
		PayloadVersion:       1,
		PayloadBytes:         []byte{0xff},
	})
}

func newRecordFromWire(t *testing.T, wire eventBodyWire) StructuralRecord {
	t.Helper()

	event, err := ValidateEventStructure(encodeEventBodyForTest(t, wire))
	if err != nil {
		t.Fatalf("ValidateEventStructure: %v", err)
	}
	record, err := NewRecord(event, goldenSignerKeyReference, goldenOpaqueSignature)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	return record
}

func digestBytesFrom(digest Digest) []byte {
	return digest[:]
}

func TestChainStateFromRecordDerivesAdmittedState(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	state := ChainStateFromRecord(genesis)

	if !state.Initialized() {
		t.Fatal("Initialized() = false, want true")
	}
	ledgerID, ok := state.LedgerID()
	if !ok || ledgerID != genesis.Event().LedgerID() {
		t.Fatalf("LedgerID() = %x, %v, want %x, true", ledgerID, ok, genesis.Event().LedgerID())
	}
	if state.LastSequence() != genesis.Event().Sequence() {
		t.Fatalf("LastSequence() = %d, want %d", state.LastSequence(), genesis.Event().Sequence())
	}
	digest, ok := state.LastRecordDigest()
	if !ok || digest != genesis.RecordDigest() {
		t.Fatalf("LastRecordDigest() = %x, %v, want %x, true", digest, ok, genesis.RecordDigest())
	}
}

func TestChainStateFromRecordRejectsZeroRecord(t *testing.T) {
	t.Parallel()

	if state := ChainStateFromRecord(StructuralRecord{}); state.Initialized() {
		t.Fatal("Initialized() = true for a zero record, want false")
	}
}
