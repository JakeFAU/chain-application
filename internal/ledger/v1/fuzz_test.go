package ledgerv1

import (
	"bytes"
	"errors"
	"testing"
)

func FuzzValidateEventStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-event-body.cbor"))
	f.Add(conformanceInvalidKnownEventPayloadSeed(f))
	f.Add(conformanceUnknownEventSeed(f))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		eventOne, errOne := ValidateEventStructure(input)
		eventTwo, errTwo := ValidateEventStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateEventStructure mutated input")
		}
		assertStableProtocolErrorIdentity(t, "event structure", errOne, errTwo)
		if errOne != nil {
			return
		}
		assertStructuralEventsEqual(t, eventOne, eventTwo)
		if !bytes.Equal(eventOne.Bytes(), input) {
			t.Fatal("accepted event bytes differ from input")
		}

		semanticErrOne := ValidateEventSemantics(eventOne)
		semanticErrTwo := ValidateEventSemantics(eventOne)
		assertStableProtocolErrorIdentity(t, "event semantics", semanticErrOne, semanticErrTwo)
		if !bytes.Equal(input, original) || !bytes.Equal(eventOne.Bytes(), input) {
			t.Fatal("ValidateEventSemantics mutated accepted event or fuzz input")
		}

		copyBytes := eventOne.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, eventOne.Bytes()) {
			t.Fatal("event Bytes returned aliased storage")
		}
	})
}

func FuzzValidateRecordStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-ledger-record.cbor"))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		recordOne, errOne := ValidateRecordStructure(input)
		recordTwo, errTwo := ValidateRecordStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateRecordStructure mutated input")
		}
		assertStableProtocolErrorIdentity(t, "record structure", errOne, errTwo)
		if errOne != nil {
			return
		}
		assertStructuralRecordsEqual(t, recordOne, recordTwo)
		if !bytes.Equal(recordOne.Bytes(), input) {
			t.Fatal("accepted record bytes differ from input")
		}
		copyBytes := recordOne.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, recordOne.Bytes()) {
			t.Fatal("record Bytes returned aliased storage")
		}
	})
}

func FuzzValidateChainConsistency(f *testing.F) {
	golden := readProtocolFixture(f, "genesis-ledger-record.cbor")
	unknown := conformanceUnknownContinuationSeed(f, golden)
	assertChainFuzzSeeds(f, golden, unknown)
	f.Add([]byte{0xff}, golden)
	f.Add(unknown, golden)
	f.Add(golden, []byte{0xff})
	f.Add(golden, golden)
	f.Add(golden, unknown)
	f.Fuzz(func(t *testing.T, firstBytes, secondBytes []byte) {
		firstOriginal := bytes.Clone(firstBytes)
		secondOriginal := bytes.Clone(secondBytes)
		firstOne, firstErrOne := ValidateRecordStructure(firstBytes)
		firstTwo, firstErrTwo := ValidateRecordStructure(firstBytes)
		if !bytes.Equal(firstBytes, firstOriginal) || !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("first record validation mutated fuzz input")
		}
		assertStableProtocolErrorIdentity(t, "first record structure", firstErrOne, firstErrTwo)
		if firstErrOne != nil {
			return
		}
		assertStructuralRecordsEqual(t, firstOne, firstTwo)
		if !bytes.Equal(firstOne.Bytes(), firstBytes) {
			t.Fatal("accepted first record bytes differ from input")
		}

		stateOne, stateErrOne := ValidateChainConsistency(ChainState{}, firstOne)
		stateTwo, stateErrTwo := ValidateChainConsistency(ChainState{}, firstTwo)
		if !bytes.Equal(firstBytes, firstOriginal) || !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("initial chain validation mutated fuzz input")
		}
		assertStableProtocolErrorIdentity(t, "initial chain consistency", stateErrOne, stateErrTwo)
		if stateErrOne != nil {
			return
		}
		if stateOne != stateTwo {
			t.Fatal("successful initial chain results differ")
		}

		secondOne, secondErrOne := ValidateRecordStructure(secondBytes)
		secondTwo, secondErrTwo := ValidateRecordStructure(secondBytes)
		if !bytes.Equal(firstBytes, firstOriginal) || !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("second record validation mutated fuzz input")
		}
		assertStableProtocolErrorIdentity(t, "second record structure", secondErrOne, secondErrTwo)
		if secondErrOne != nil {
			return
		}
		assertStructuralRecordsEqual(t, secondOne, secondTwo)
		if !bytes.Equal(secondOne.Bytes(), secondBytes) {
			t.Fatal("accepted second record bytes differ from input")
		}

		nextOne, nextErrOne := ValidateChainConsistency(stateOne, secondOne)
		nextTwo, nextErrTwo := ValidateChainConsistency(stateTwo, secondTwo)
		if !bytes.Equal(firstBytes, firstOriginal) || !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("next chain validation mutated fuzz input")
		}
		assertStableProtocolErrorIdentity(t, "next chain consistency", nextErrOne, nextErrTwo)
		if nextErrOne == nil && nextOne != nextTwo {
			t.Fatal("successful chain results differ")
		}
	})
}

func assertStableProtocolErrorIdentity(t testing.TB, stage string, errOne, errTwo error) {
	t.Helper()
	identityOne := protocolErrorIdentity(errOne)
	identityTwo := protocolErrorIdentity(errTwo)
	if identityOne != identityTwo {
		t.Fatalf("%s error identities differ: %q and %q", stage, identityOne, identityTwo)
	}
}

func assertStructuralEventsEqual(t testing.TB, eventOne, eventTwo StructuralEvent) {
	t.Helper()
	previousOne, hasPreviousOne := eventOne.PreviousRecordDigest()
	previousTwo, hasPreviousTwo := eventTwo.PreviousRecordDigest()
	if !bytes.Equal(eventOne.Bytes(), eventTwo.Bytes()) ||
		eventOne.Digest() != eventTwo.Digest() ||
		eventOne.LedgerID() != eventTwo.LedgerID() ||
		eventOne.Sequence() != eventTwo.Sequence() ||
		previousOne != previousTwo ||
		hasPreviousOne != hasPreviousTwo ||
		eventOne.AdmittedAtUnixMS() != eventTwo.AdmittedAtUnixMS() ||
		eventOne.Kind() != eventTwo.Kind() ||
		eventOne.PayloadVersion() != eventTwo.PayloadVersion() ||
		!bytes.Equal(eventOne.PayloadBytes(), eventTwo.PayloadBytes()) {
		t.Fatal("successful event structure results differ")
	}
}

func assertStructuralRecordsEqual(t testing.TB, recordOne, recordTwo StructuralRecord) {
	t.Helper()
	if !bytes.Equal(recordOne.Bytes(), recordTwo.Bytes()) ||
		!bytes.Equal(recordOne.RecordBodyBytes(), recordTwo.RecordBodyBytes()) ||
		recordOne.RecordDigest() != recordTwo.RecordDigest() ||
		recordOne.SignerKeyReference() != recordTwo.SignerKeyReference() ||
		!bytes.Equal(recordOne.SignatureBytes(), recordTwo.SignatureBytes()) ||
		recordOne.SignatureStatus() != recordTwo.SignatureStatus() {
		t.Fatal("successful record structure results differ")
	}
	assertStructuralEventsEqual(t, recordOne.Event(), recordTwo.Event())
}

func TestFuzzChainSeedPreflight(t *testing.T) {
	golden := readProtocolFixture(t, "genesis-ledger-record.cbor")
	unknown := conformanceUnknownContinuationSeed(t, golden)
	assertChainFuzzSeeds(t, golden, unknown)
}

func assertChainFuzzSeeds(t testing.TB, goldenBytes, unknownBytes []byte) {
	t.Helper()
	genesis, err := ValidateRecordStructure(goldenBytes)
	if err != nil {
		t.Fatalf("ValidateRecordStructure genesis seed: %v", err)
	}
	chain, err := ValidateChainConsistency(ChainState{}, genesis)
	if err != nil || !chain.Initialized() || chain.LastSequence() != 1 {
		t.Fatalf("ValidateChainConsistency genesis seed = (%v, %v)", chain, err)
	}
	replay, err := Apply(ReplayState{}, genesis)
	if err != nil || replay.ChainState() != chain {
		t.Fatalf("Apply genesis seed = (%v, %v)", replay, err)
	}

	unknown, err := ValidateRecordStructure(unknownBytes)
	if err != nil {
		t.Fatalf("ValidateRecordStructure unknown seed: %v", err)
	}
	next, err := ValidateChainConsistency(chain, unknown)
	if err != nil || next.LastSequence() != 2 {
		t.Fatalf("ValidateChainConsistency unknown seed = (%v, %v)", next, err)
	}
	if unchanged, err := Apply(replay, unknown); !errors.Is(err, ErrUnsupportedEvent) || unchanged != replay {
		t.Fatalf("Apply unknown seed = (%v, %v), want unchanged replay and ErrUnsupportedEvent", unchanged, err)
	}
}

func conformanceUnknownContinuationSeed(t testing.TB, genesisBytes []byte) []byte {
	t.Helper()
	genesis, err := ValidateRecordStructure(genesisBytes)
	if err != nil {
		t.Fatalf("ValidateRecordStructure genesis seed: %v", err)
	}
	ledgerID := genesis.Event().LedgerID()
	previousDigest := genesis.RecordDigest()
	encoded, err := encodeCanonical(eventBodyWire{
		ProtocolVersion:      protocolVersionV1,
		LedgerID:             ledgerID[:],
		Sequence:             2,
		PreviousRecordDigest: previousDigest[:],
		EventKind:            2,
		PayloadVersion:       1,
		PayloadBytes:         []byte{0xff},
	}, maxEventBodyBytes, stageEventBody)
	if err != nil {
		t.Fatalf("encode unknown continuation seed: %v", err)
	}
	event, err := ValidateEventStructure(encoded)
	if err != nil {
		t.Fatalf("ValidateEventStructure unknown continuation seed: %v", err)
	}
	record, err := NewRecord(event, goldenSignerKeyReference, goldenOpaqueSignature)
	if err != nil {
		t.Fatalf("NewRecord unknown continuation seed: %v", err)
	}
	return record.Bytes()
}

func conformanceUnknownEventSeed(t testing.TB) []byte {
	t.Helper()
	genesisBytes := readProtocolFixture(t, "genesis-ledger-record.cbor")
	unknownBytes := conformanceUnknownContinuationSeed(t, genesisBytes)
	unknown, err := ValidateRecordStructure(unknownBytes)
	if err != nil {
		t.Fatalf("ValidateRecordStructure unknown event seed: %v", err)
	}
	return unknown.Event().Bytes()
}

func conformanceInvalidKnownEventPayloadSeed(t testing.TB) []byte {
	t.Helper()
	genesisBytes := readProtocolFixture(t, "genesis-event-body.cbor")
	genesis, err := ValidateEventStructure(genesisBytes)
	if err != nil {
		t.Fatalf("ValidateEventStructure genesis event seed: %v", err)
	}
	ledgerID := genesis.LedgerID()
	encoded, err := encodeCanonical(eventBodyWire{
		ProtocolVersion:  protocolVersionV1,
		LedgerID:         ledgerID[:],
		Sequence:         1,
		AdmittedAtUnixMS: genesis.AdmittedAtUnixMS(),
		EventKind:        uint64(EventKindLedgerInitialized),
		PayloadVersion:   ledgerInitializedPayloadVersionV1,
		PayloadBytes:     []byte{0xff},
	}, maxEventBodyBytes, stageEventBody)
	if err != nil {
		t.Fatalf("encode invalid known event payload seed: %v", err)
	}
	event, err := ValidateEventStructure(encoded)
	if err != nil {
		t.Fatalf("ValidateEventStructure invalid known event payload seed: %v", err)
	}
	return event.Bytes()
}
