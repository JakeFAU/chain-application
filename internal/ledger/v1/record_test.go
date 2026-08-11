package ledgerv1

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goldenSignerKeyReference = "projects/attribution-chain-505000/locations/global/keyRings/system-signing/cryptoKeys/ledger-admission/cryptoKeyVersions/1"
	goldenRecordDigestHex    = "3c9758df59d7a938da29f4cb5d82b7c99a446cfad07e4d890dd3edfc8ab4f035"
	goldenLedgerRecordHex    = "a4000101590111a8000101583ba80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a002677368612d32353603582026bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df7204787a70726f6a656374732f6174747269627574696f6e2d636861696e2d3530353030302f6c6f636174696f6e732f676c6f62616c2f6b657952696e67732f73797374656d2d7369676e696e672f63727970746f4b6579732f6c65646765722d61646d697373696f6e2f63727970746f4b657956657273696f6e732f31057165636473612d703235362d736861323536066861736e312d6465720748300602010102010102677368612d3235360358203c9758df59d7a938da29f4cb5d82b7c99a446cfad07e4d890dd3edfc8ab4f035"
)

var goldenOpaqueSignature = []byte{0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01}

type genesisVectorManifest struct {
	AdmittedAtUnixMS   uint64            `json:"admitted_at_unix_ms"`
	EventDigestHex     string            `json:"event_digest_hex"`
	Files              map[string]string `json:"files"`
	LedgerIDHex        string            `json:"ledger_id_hex"`
	RecordDigestHex    string            `json:"record_digest_hex"`
	SignatureHex       string            `json:"signature_hex"`
	SignerKeyReference string            `json:"signer_key_reference"`
}

func TestNewRecord(t *testing.T) {
	t.Parallel()

	event, err := NewGenesisEvent(testLedgerID(), 1_735_689_600_123)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	record, err := NewRecord(event, goldenSignerKeyReference, goldenOpaqueSignature)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}

	digest := record.RecordDigest()
	if got := hex.EncodeToString(digest[:]); got != goldenRecordDigestHex {
		t.Fatalf("record digest = %s, want %s", got, goldenRecordDigestHex)
	}
	if got := hex.EncodeToString(record.Bytes()); got != goldenLedgerRecordHex {
		t.Fatalf("record bytes = %s, want %s", got, goldenLedgerRecordHex)
	}
	if got := record.SignerKeyReference(); got != goldenSignerKeyReference {
		t.Fatalf("signer key reference = %q, want %q", got, goldenSignerKeyReference)
	}
	if got := record.SignatureBytes(); !bytes.Equal(got, goldenOpaqueSignature) {
		t.Fatalf("signature bytes = %x, want %x", got, goldenOpaqueSignature)
	}
	if got := record.SignatureStatus(); got != SignatureStatusUnverified {
		t.Fatalf("signature status = %d, want %d", got, SignatureStatusUnverified)
	}
	validated, err := ValidateRecordStructure(record.Bytes())
	if err != nil {
		t.Fatalf("ValidateRecordStructure: %v", err)
	}
	if validated.RecordDigest() != record.RecordDigest() {
		t.Fatal("validated record digest differs")
	}
}

func TestGenesisConformanceVector(t *testing.T) {
	t.Parallel()

	manifestBytes := readProtocolFixture(t, "genesis-v1.json")
	var manifest genesisVectorManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal vector manifest: %v", err)
	}
	for name, encodedHex := range manifest.Files {
		want, err := hex.DecodeString(encodedHex)
		if err != nil {
			t.Fatalf("decode manifest fixture %s: %v", name, err)
		}
		if got := readProtocolFixture(t, name); !bytes.Equal(got, want) {
			t.Fatalf("fixture %s differs from manifest hex", name)
		}
	}

	decodedLedgerID, err := hex.DecodeString(manifest.LedgerIDHex)
	if err != nil {
		t.Fatalf("decode ledger ID: %v", err)
	}
	if len(decodedLedgerID) != ledgerIDBytes {
		t.Fatalf("ledger ID bytes = %d, want %d", len(decodedLedgerID), ledgerIDBytes)
	}
	var ledgerID LedgerID
	copy(ledgerID[:], decodedLedgerID)
	signature, err := hex.DecodeString(manifest.SignatureHex)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	event, err := NewGenesisEvent(ledgerID, manifest.AdmittedAtUnixMS)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	if got := event.Bytes(); !bytes.Equal(got, readProtocolFixture(t, "genesis-event-body.cbor")) {
		t.Fatal("event bytes differ from conformance fixture")
	}
	eventDigest := event.Digest()
	if got := hex.EncodeToString(eventDigest[:]); got != manifest.EventDigestHex {
		t.Fatalf("event digest = %s, want manifest %s", got, manifest.EventDigestHex)
	}
	record, err := NewRecord(event, manifest.SignerKeyReference, signature)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	if got := record.RecordBodyBytes(); !bytes.Equal(got, readProtocolFixture(t, "genesis-record-body.cbor")) {
		t.Fatal("record body bytes differ from conformance fixture")
	}
	if got := record.Bytes(); !bytes.Equal(got, readProtocolFixture(t, "genesis-ledger-record.cbor")) {
		t.Fatal("record bytes differ from conformance fixture")
	}
	recordDigest := record.RecordDigest()
	if got := hex.EncodeToString(recordDigest[:]); got != manifest.RecordDigestHex {
		t.Fatalf("record digest = %s, want manifest %s", got, manifest.RecordDigestHex)
	}
	if _, err := ValidateRecordStructure(readProtocolFixture(t, "genesis-ledger-record.cbor")); err != nil {
		t.Fatalf("ValidateRecordStructure fixture: %v", err)
	}
}

func TestNewRecordRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	event := testGenesisStructuralEvent(t)
	tests := []struct {
		name      string
		event     StructuralEvent
		signer    string
		signature []byte
		want      error
	}{
		{name: "zero event", signer: goldenSignerKeyReference, signature: goldenOpaqueSignature, want: ErrSchemaViolation},
		{name: "empty signer", event: event, signature: goldenOpaqueSignature, want: ErrSchemaViolation},
		{name: "oversized signer", event: event, signer: strings.Repeat("a", maxSignerReferenceBytes+1), signature: goldenOpaqueSignature, want: ErrSchemaViolation},
		{name: "non ASCII signer", event: event, signer: "key-\u00e9", signature: goldenOpaqueSignature, want: ErrSchemaViolation},
		{name: "control signer", event: event, signer: "key\nreference", signature: goldenOpaqueSignature, want: ErrSchemaViolation},
		{name: "short signature", event: event, signer: goldenSignerKeyReference, signature: make([]byte, minSignatureBytes-1), want: ErrSchemaViolation},
		{name: "long signature", event: event, signer: goldenSignerKeyReference, signature: make([]byte, maxSignatureBytes+1), want: ErrSchemaViolation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRecord(test.event, test.signer, test.signature)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewRecord error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStructuralRecordOwnsReturnedBytes(t *testing.T) {
	t.Parallel()

	event := testGenesisStructuralEvent(t)
	signature := bytes.Clone(goldenOpaqueSignature)
	record, err := NewRecord(event, goldenSignerKeyReference, signature)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}

	signature[0] ^= 0xff
	if bytes.Equal(record.SignatureBytes(), signature) {
		t.Fatal("record retained caller-owned signature bytes")
	}
	for _, mutation := range []struct {
		name string
		get  func() []byte
		want func() []byte
	}{
		{name: "record bytes", get: record.Bytes, want: record.Bytes},
		{name: "record body bytes", get: record.RecordBodyBytes, want: record.RecordBodyBytes},
		{name: "signature bytes", get: record.SignatureBytes, want: record.SignatureBytes},
		{name: "event bytes", get: record.Event().Bytes, want: record.Event().Bytes},
		{name: "event payload", get: record.Event().PayloadBytes, want: record.Event().PayloadBytes},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			got := mutation.get()
			got[0] ^= 0xff
			if bytes.Equal(got, mutation.want()) {
				t.Fatal("returned bytes alias record storage")
			}
		})
	}
}

func TestValidateRecordStructureRejectsInvalidBodies(t *testing.T) {
	t.Parallel()

	valid := testGenesisRecordBodyWire(t)
	tests := []struct {
		name   string
		mutate func(*recordBodyWire)
		want   error
	}{
		{name: "inner version zero", mutate: func(w *recordBodyWire) { w.RecordVersion = 0 }, want: ErrUnsupportedVersion},
		{name: "inner version two", mutate: func(w *recordBodyWire) { w.RecordVersion = 2 }, want: ErrUnsupportedVersion},
		{name: "empty event body", mutate: func(w *recordBodyWire) { w.EventBodyBytes = nil }, want: ErrSchemaViolation},
		{name: "oversized event body", mutate: func(w *recordBodyWire) { w.EventBodyBytes = make([]byte, maxEventBodyBytes+1) }, want: ErrSchemaViolation},
		{name: "wrong event digest algorithm", mutate: func(w *recordBodyWire) { w.EventDigestAlgorithm = "sha-512" }, want: ErrSchemaViolation},
		{name: "short event digest", mutate: func(w *recordBodyWire) { w.EventDigest = make([]byte, digestBytes-1) }, want: ErrSchemaViolation},
		{name: "long event digest", mutate: func(w *recordBodyWire) { w.EventDigest = make([]byte, digestBytes+1) }, want: ErrSchemaViolation},
		{name: "empty signer", mutate: func(w *recordBodyWire) { w.SignerKeyReference = "" }, want: ErrSchemaViolation},
		{name: "oversized signer", mutate: func(w *recordBodyWire) { w.SignerKeyReference = strings.Repeat("a", maxSignerReferenceBytes+1) }, want: ErrSchemaViolation},
		{name: "non ASCII signer", mutate: func(w *recordBodyWire) { w.SignerKeyReference = "key-\u00e9" }, want: ErrSchemaViolation},
		{name: "control signer", mutate: func(w *recordBodyWire) { w.SignerKeyReference = "key\tref" }, want: ErrSchemaViolation},
		{name: "wrong signature algorithm", mutate: func(w *recordBodyWire) { w.SignatureAlgorithm = "rsa-pss" }, want: ErrSchemaViolation},
		{name: "wrong signature encoding", mutate: func(w *recordBodyWire) { w.SignatureEncoding = "raw" }, want: ErrSchemaViolation},
		{name: "short signature", mutate: func(w *recordBodyWire) { w.SignatureBytes = make([]byte, minSignatureBytes-1) }, want: ErrSchemaViolation},
		{name: "long signature", mutate: func(w *recordBodyWire) { w.SignatureBytes = make([]byte, maxSignatureBytes+1) }, want: ErrSchemaViolation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := valid
			test.mutate(&wire)
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, testLedgerRecordWire(t, encodeRecordBodyForTest(t, wire))))
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateRecordStructure error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateRecordStructureRejectsInvalidOuterRecord(t *testing.T) {
	t.Parallel()

	validBody := encodeRecordBodyForTest(t, testGenesisRecordBodyWire(t))
	valid := testLedgerRecordWire(t, validBody)
	tests := []struct {
		name   string
		mutate func(*ledgerRecordWire)
		want   error
	}{
		{name: "outer version zero", mutate: func(w *ledgerRecordWire) { w.RecordVersion = 0 }, want: ErrUnsupportedVersion},
		{name: "outer version two", mutate: func(w *ledgerRecordWire) { w.RecordVersion = 2 }, want: ErrUnsupportedVersion},
		{name: "empty record body", mutate: func(w *ledgerRecordWire) { w.RecordBodyBytes = nil }, want: ErrSchemaViolation},
		{name: "oversized record body", mutate: func(w *ledgerRecordWire) { w.RecordBodyBytes = make([]byte, maxRecordBodyBytes+1) }, want: ErrSchemaViolation},
		{name: "wrong record digest algorithm", mutate: func(w *ledgerRecordWire) { w.RecordDigestAlgorithm = "sha-512" }, want: ErrSchemaViolation},
		{name: "short record digest", mutate: func(w *ledgerRecordWire) { w.RecordDigest = make([]byte, digestBytes-1) }, want: ErrSchemaViolation},
		{name: "long record digest", mutate: func(w *ledgerRecordWire) { w.RecordDigest = make([]byte, digestBytes+1) }, want: ErrSchemaViolation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := valid
			test.mutate(&wire)
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, wire))
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateRecordStructure error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateRecordStructureRejectsMutatedCommitments(t *testing.T) {
	t.Parallel()

	validBody := testGenesisRecordBodyWire(t)
	validOuter := testLedgerRecordWire(t, encodeRecordBodyForTest(t, validBody))
	tests := []struct {
		name   string
		mutate func(*recordBodyWire, *ledgerRecordWire)
		want   error
	}{
		{name: "event digest", mutate: func(b *recordBodyWire, _ *ledgerRecordWire) { b.EventDigest[0] ^= 0xff }, want: ErrDigestMismatch},
		{name: "record digest", mutate: func(_ *recordBodyWire, r *ledgerRecordWire) { r.RecordDigest[0] ^= 0xff }, want: ErrDigestMismatch},
		{name: "signer", mutate: func(b *recordBodyWire, _ *ledgerRecordWire) { b.SignerKeyReference = "different-signer" }, want: ErrDigestMismatch},
		{name: "signature", mutate: func(b *recordBodyWire, _ *ledgerRecordWire) { b.SignatureBytes[0] ^= 0xff }, want: ErrDigestMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := cloneRecordBodyWire(validBody)
			outer := cloneLedgerRecordWire(validOuter)
			test.mutate(&body, &outer)
			if test.name != "record digest" {
				outer.RecordBodyBytes = encodeRecordBodyForTest(t, body)
			}
			if test.name == "event digest" {
				recordDigest := digestRecordBody(outer.RecordBodyBytes)
				outer.RecordDigest = recordDigest[:]
			}
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, outer))
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateRecordStructure error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateRecordStructureRejectsOversizedInput(t *testing.T) {
	t.Parallel()

	_, err := ValidateRecordStructure(make([]byte, maxLedgerRecordBytes+1))
	if !errors.Is(err, ErrOversizedInput) {
		t.Fatalf("ValidateRecordStructure error = %v, want %v", err, ErrOversizedInput)
	}
}

func TestValidateRecordStructureDoesNotLeakPrivateValues(t *testing.T) {
	t.Parallel()

	const (
		privateSigner    = "private-signer-marker"
		privatePayload   = "private-payload-marker"
		privateSignature = "private-signature-marker"
		privateDigest    = "private-digest-marker"
	)
	body := testGenesisRecordBodyWire(t)
	body.SignerKeyReference = privateSigner
	body.SignatureBytes = []byte(privateSignature)
	body.EventBodyBytes = []byte(privatePayload)
	body.EventDigest = []byte(privateDigest)
	outer := testLedgerRecordWire(t, encodeRecordBodyForTest(t, body))
	outer.RecordDigest = []byte(privateDigest)
	_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, outer))
	if err == nil {
		t.Fatal("ValidateRecordStructure error = nil, want rejection")
	}
	for _, marker := range []string{privateSigner, privatePayload, privateSignature, privateDigest} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("error text leaked private marker %q: %q", marker, err)
		}
	}
}

func testGenesisStructuralEvent(t *testing.T) StructuralEvent {
	t.Helper()
	event, err := NewGenesisEvent(testLedgerID(), 1_735_689_600_123)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	return event
}

func testGenesisRecordBodyWire(t *testing.T) recordBodyWire {
	t.Helper()
	event := testGenesisStructuralEvent(t)
	digest := event.Digest()
	return recordBodyWire{
		RecordVersion:        recordVersionV1,
		EventBodyBytes:       event.Bytes(),
		EventDigestAlgorithm: digestAlgorithmSHA256,
		EventDigest:          digest[:],
		SignerKeyReference:   goldenSignerKeyReference,
		SignatureAlgorithm:   signatureAlgorithmP256,
		SignatureEncoding:    signatureEncodingASN1DER,
		SignatureBytes:       bytes.Clone(goldenOpaqueSignature),
	}
}

func testLedgerRecordWire(t *testing.T, recordBodyBytes []byte) ledgerRecordWire {
	t.Helper()
	digest := digestRecordBody(recordBodyBytes)
	return ledgerRecordWire{
		RecordVersion:         recordVersionV1,
		RecordBodyBytes:       bytes.Clone(recordBodyBytes),
		RecordDigestAlgorithm: digestAlgorithmSHA256,
		RecordDigest:          digest[:],
	}
}

func cloneRecordBodyWire(wire recordBodyWire) recordBodyWire {
	wire.EventBodyBytes = bytes.Clone(wire.EventBodyBytes)
	wire.EventDigest = bytes.Clone(wire.EventDigest)
	wire.SignatureBytes = bytes.Clone(wire.SignatureBytes)
	return wire
}

func cloneLedgerRecordWire(wire ledgerRecordWire) ledgerRecordWire {
	wire.RecordBodyBytes = bytes.Clone(wire.RecordBodyBytes)
	wire.RecordDigest = bytes.Clone(wire.RecordDigest)
	return wire
}

func encodeRecordBodyForTest(t *testing.T, wire recordBodyWire) []byte {
	t.Helper()
	encoded, err := encodeCanonical(wire, maxRecordBodyBytes, stageRecordBody)
	if err != nil {
		t.Fatalf("encode record body: %v", err)
	}
	return encoded
}

func encodeLedgerRecordForTest(t *testing.T, wire ledgerRecordWire) []byte {
	t.Helper()
	encoded, err := encodeCanonical(wire, maxLedgerRecordBytes, stageLedgerRecord)
	if err != nil {
		t.Fatalf("encode ledger record: %v", err)
	}
	return encoded
}

func readProtocolFixture(t testing.TB, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "protocol", "ledger", "v1", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read protocol fixture %s: %v", name, err)
	}
	return data
}
