package ledgerv1

import (
	"bytes"
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

func conformanceUnknownContinuationSeed(f *testing.F, genesisBytes []byte) []byte {
	f.Helper()
	genesis, err := ValidateRecordStructure(genesisBytes)
	if err != nil {
		f.Fatalf("ValidateRecordStructure genesis seed: %v", err)
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
		f.Fatalf("encode unknown continuation seed: %v", err)
	}
	event, err := ValidateEventStructure(encoded)
	if err != nil {
		f.Fatalf("ValidateEventStructure unknown continuation seed: %v", err)
	}
	record, err := NewRecord(event, goldenSignerKeyReference, goldenOpaqueSignature)
	if err != nil {
		f.Fatalf("NewRecord unknown continuation seed: %v", err)
	}
	return record.Bytes()
}
