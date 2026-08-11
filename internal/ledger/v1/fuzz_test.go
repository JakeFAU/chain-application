package ledgerv1

import (
	"bytes"
	"errors"
	"testing"
)

func FuzzValidateEventStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-event-body.cbor"))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		event, err := ValidateEventStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateEventStructure mutated input")
		}
		if err != nil {
			return
		}
		if !bytes.Equal(event.Bytes(), input) {
			t.Fatal("accepted event bytes differ from input")
		}
		copyBytes := event.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, event.Bytes()) {
			t.Fatal("event Bytes returned aliased storage")
		}
		again, err := ValidateEventStructure(input)
		if err != nil || again.Digest() != event.Digest() || again.Sequence() != event.Sequence() {
			t.Fatalf("second validation differs: %v", err)
		}
	})
}

func FuzzValidateRecordStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-ledger-record.cbor"))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		record, err := ValidateRecordStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateRecordStructure mutated input")
		}
		if err != nil {
			return
		}
		if !bytes.Equal(record.Bytes(), input) {
			t.Fatal("accepted record bytes differ from input")
		}
		copyBytes := record.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, record.Bytes()) {
			t.Fatal("record Bytes returned aliased storage")
		}
		again, err := ValidateRecordStructure(input)
		if err != nil || again.RecordDigest() != record.RecordDigest() {
			t.Fatalf("second validation differs: %v", err)
		}
	})
}

func FuzzValidateChainConsistency(f *testing.F) {
	golden := readProtocolFixture(f, "genesis-ledger-record.cbor")
	unknown := conformanceUnknownContinuationSeed(f, golden)
	assertChainFuzzSeeds(f, golden, unknown)
	f.Add(golden, golden)
	f.Add(golden, unknown)
	f.Fuzz(func(t *testing.T, firstBytes, secondBytes []byte) {
		firstOriginal := bytes.Clone(firstBytes)
		secondOriginal := bytes.Clone(secondBytes)
		first, err := ValidateRecordStructure(firstBytes)
		if !bytes.Equal(firstBytes, firstOriginal) || !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("record validation mutated fuzz input")
		}
		if err != nil {
			return
		}
		state, err := ValidateChainConsistency(ChainState{}, first)
		if err != nil {
			return
		}
		second, err := ValidateRecordStructure(secondBytes)
		if !bytes.Equal(secondBytes, secondOriginal) {
			t.Fatal("second record validation mutated fuzz input")
		}
		if err != nil {
			return
		}
		nextOne, errOne := ValidateChainConsistency(state, second)
		nextTwo, errTwo := ValidateChainConsistency(state, second)
		if protocolErrorIdentity(errOne) != protocolErrorIdentity(errTwo) {
			t.Fatalf("error identities differ: %v and %v", errOne, errTwo)
		}
		if errOne == nil && nextOne != nextTwo {
			t.Fatal("successful chain results differ")
		}
	})
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
