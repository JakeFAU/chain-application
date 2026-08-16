package ledgerv1

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

var protocolErrorIdentities = []struct {
	name string
	err  error
}{
	{name: "oversized", err: ErrOversizedInput},
	{name: "malformed", err: ErrMalformedCBOR},
	{name: "non-conforming", err: ErrNonConformingCBOR},
	{name: "version", err: ErrUnsupportedVersion},
	{name: "schema", err: ErrSchemaViolation},
	{name: "digest", err: ErrDigestMismatch},
	{name: "sequence", err: ErrInvalidSequence},
	{name: "ledger", err: ErrLedgerMismatch},
	{name: "link", err: ErrChainLinkMismatch},
	{name: "event", err: ErrUnsupportedEvent},
}

// TestConformanceRejects exercises invalid protocol bytes at the public
// structural and semantic boundaries.  It deliberately reconstructs enclosing
// containers after an inner mutation, so a digest mismatch cannot hide a
// missing inner-layer validation.
func TestConformanceRejects(t *testing.T) {
	genesisEvent := readProtocolFixture(t, "genesis-event-body.cbor")
	genesisRecord := readProtocolFixture(t, "genesis-ledger-record.cbor")

	t.Run("declared input limits", func(t *testing.T) {
		assertConformanceRejection(t, ErrOversizedInput, nil, func() error {
			_, err := ValidateEventStructure(make([]byte, maxEventBodyBytes+1))
			return err
		})
		assertConformanceRejection(t, ErrOversizedInput, nil, func() error {
			_, err := ValidateRecordStructure(make([]byte, maxLedgerRecordBytes+1))
			return err
		})

		oversizedPayload := testGenesisEventBodyWire()
		oversizedPayload.PayloadBytes = make([]byte, maxPayloadBytes+1)
		assertConformanceRejection(t, ErrSchemaViolation, nil, func() error {
			_, err := ValidateEventStructure(encodeEventBodyForTest(t, oversizedPayload))
			return err
		})

		oversizedEvent := testGenesisRecordBodyWire(t)
		oversizedEvent.EventBodyBytes = make([]byte, maxEventBodyBytes+1)
		assertConformanceRejection(t, ErrSchemaViolation, nil, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, oversizedEvent))
			return err
		})

		oversizedBody := ledgerRecordWire{
			RecordVersion:         recordVersionV1,
			RecordBodyBytes:       make([]byte, maxRecordBodyBytes+1),
			RecordDigestAlgorithm: digestAlgorithmSHA256,
			RecordDigest:          make([]byte, digestBytes),
		}
		assertConformanceRejection(t, ErrSchemaViolation, nil, func() error {
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, oversizedBody))
			return err
		})
	})

	t.Run("canonical CBOR at every map layer", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			input []byte
			want  error
		}{
			{"event indefinite map", conformanceIndefiniteMap(genesisEvent), ErrNonConformingCBOR},
			{"event non-shortest integer", conformanceReplaceFirst(genesisEvent, []byte{0x00, 0x01}, []byte{0x00, 0x18, 0x01}), ErrNonConformingCBOR},
			{"event indefinite payload byte string", conformanceEventWithRawField(t, eventBodyKeyPayloadBytes, []byte{0x5f, 0x40, 0xff}), ErrNonConformingCBOR},
			{"event non-shortest byte string length", conformanceEventWithRawField(t, eventBodyKeyPayloadBytes, []byte{0x58, 0x01, 0xa0}), ErrNonConformingCBOR},
			{"event out of order keys", conformanceReorderedMap(t, genesisEvent), ErrNonConformingCBOR},
			{"event duplicate key", conformanceDuplicateFirstMapKey(t, genesisEvent), ErrNonConformingCBOR},
			{"event unknown key", conformanceUnknownMapKey(t, genesisEvent), ErrSchemaViolation},
			{"event missing key", conformanceMissingLastMapKey(t, genesisEvent), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateEventStructure(test.input)
					return err
				})
			})
		}

		for _, test := range []struct {
			name string
			body []byte
			want error
		}{
			{"record body indefinite map", conformanceIndefiniteMap(readProtocolFixture(t, "genesis-record-body.cbor")), ErrNonConformingCBOR},
			{"record body indefinite byte string", conformanceRecordBodyWithRawField(t, testGenesisRecordBodyWire(t), recordBodyKeyEventBodyBytes, []byte{0x5f, 0x40, 0xff}), ErrNonConformingCBOR},
			{"record body out of order keys", conformanceReorderedMap(t, readProtocolFixture(t, "genesis-record-body.cbor")), ErrNonConformingCBOR},
			{"record body duplicate key", conformanceDuplicateFirstMapKey(t, readProtocolFixture(t, "genesis-record-body.cbor")), ErrNonConformingCBOR},
			{"record body unknown key", conformanceUnknownMapKey(t, readProtocolFixture(t, "genesis-record-body.cbor")), ErrSchemaViolation},
			{"record body missing key", conformanceMissingLastMapKey(t, readProtocolFixture(t, "genesis-record-body.cbor")), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				input := conformanceOuterWithBody(t, test.body)
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(input)
					return err
				})
			})
		}

		for _, test := range []struct {
			name  string
			input []byte
			want  error
		}{
			{"outer indefinite map", conformanceIndefiniteMap(genesisRecord), ErrNonConformingCBOR},
			{"outer indefinite byte string", conformanceOuterWithRawField(t, ledgerRecordKeyRecordBodyBytes, []byte{0x5f, 0x40, 0xff}), ErrNonConformingCBOR},
			{"outer out of order keys", conformanceReorderedMap(t, genesisRecord), ErrNonConformingCBOR},
			{"outer duplicate key", conformanceDuplicateFirstMapKey(t, genesisRecord), ErrNonConformingCBOR},
			{"outer unknown key", conformanceUnknownMapKey(t, genesisRecord), ErrSchemaViolation},
			{"outer missing key", conformanceMissingLastMapKey(t, genesisRecord), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(test.input)
					return err
				})
			})
		}
	})

	t.Run("forbidden CBOR forms", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			value []byte
			want  error
		}{
			{"negative integer", []byte{0x20}, ErrSchemaViolation},
			{"float", []byte{0xf9, 0x00, 0x00}, ErrSchemaViolation},
			{"tag", []byte{0xc0, 0x00}, ErrNonConformingCBOR},
			{"undefined", []byte{0xf7}, ErrNonConformingCBOR},
			{"unassigned simple value", []byte{0xf0}, ErrNonConformingCBOR},
		} {
			t.Run(test.name, func(t *testing.T) {
				input := conformanceEventWithRawField(t, eventBodyKeyProtocolVersion, test.value)
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateEventStructure(input)
					return err
				})
			})
		}
	})

	t.Run("no extraneous bytes at every boundary", func(t *testing.T) {
		for _, test := range []struct {
			name string
			run  func([]byte) error
			base []byte
		}{
			{"event", func(input []byte) error { _, err := ValidateEventStructure(input); return err }, genesisEvent},
			{"record", func(input []byte) error { _, err := ValidateRecordStructure(input); return err }, genesisRecord},
		} {
			for _, placement := range []struct {
				name string
				data []byte
			}{
				{"leading", append([]byte{0}, test.base...)},
				{"trailing", append(bytes.Clone(test.base), 0)},
			} {
				t.Run(test.name+" "+placement.name, func(t *testing.T) {
					assertConformanceRejection(t, ErrMalformedCBOR, nil, func() error { return test.run(placement.data) })
				})
			}
		}

		for _, test := range []struct {
			name string
			body func([]byte) []byte
			want error
		}{
			{"event body leading", func(value []byte) []byte { return conformanceRecordWithEventBody(t, append([]byte{0}, value...)) }, ErrMalformedCBOR},
			{"event body trailing", func(value []byte) []byte { return conformanceRecordWithEventBody(t, append(bytes.Clone(value), 0)) }, ErrMalformedCBOR},
			{"record body leading", func(value []byte) []byte { return conformanceOuterWithBody(t, append([]byte{0}, value...)) }, ErrMalformedCBOR},
			{"record body trailing", func(value []byte) []byte { return conformanceOuterWithBody(t, append(bytes.Clone(value), 0)) }, ErrMalformedCBOR},
		} {
			t.Run(test.name, func(t *testing.T) {
				base := genesisEvent
				if strings.HasPrefix(test.name, "record body") {
					base = readProtocolFixture(t, "genesis-record-body.cbor")
				}
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(test.body(base))
					return err
				})
			})
		}

		for _, payload := range [][]byte{
			append([]byte{0}, []byte(ledgerInitializedPayloadCBOR)...),
			append([]byte(ledgerInitializedPayloadCBOR), 0),
		} {
			event := testGenesisEventBodyWire()
			event.PayloadBytes = payload
			record, err := ValidateRecordStructure(conformanceRecord(t, testRecordBodyForEvent(t, event)))
			if err != nil {
				t.Fatalf("ValidateRecordStructure payload boundary: %v", err)
			}
			assertConformanceRejection(t, ErrMalformedCBOR, nil, func() error { return ValidateEventSemantics(record.Event()) })
		}
	})

	t.Run("fixed fields and private values", func(t *testing.T) {
		const privateSignature = "private-invalid-utf8-signature-marker"
		body := testGenesisRecordBodyWire(t)
		body.SignatureBytes = []byte(privateSignature)
		invalidUTF8 := conformanceRecordBodyWithRawField(t, body, recordBodyKeySignerKeyReference, []byte{0x61, 0xff})
		for _, test := range []struct {
			name      string
			input     []byte
			want      error
			forbidden []string
		}{
			{"invalid UTF-8 signer", conformanceOuterWithBody(t, invalidUTF8), ErrNonConformingCBOR, []string{privateSignature, "\xff"}},
			{"wrong ledger identifier type", conformanceEventWithRawField(t, eventBodyKeyLedgerID, []byte{0x01}), ErrSchemaViolation, nil},
			{"short ledger identifier", conformanceEventWithRawField(t, eventBodyKeyLedgerID, []byte{0x41, 0x00}), ErrSchemaViolation, nil},
			{"long predecessor", conformanceEventWithRawField(t, eventBodyKeyPreviousRecordDigest, append([]byte{0x58, digestBytes + 1}, make([]byte, digestBytes+1)...)), ErrSchemaViolation, nil},
			{"wrong digest identifier", conformanceOuterWithRawField(t, ledgerRecordKeyDigestAlgorithm, conformanceText("sha-512")), ErrSchemaViolation, nil},
			{"wrong scalar signature", conformanceRecordWithRawField(t, recordBodyKeySignatureBytes, conformanceText("not-bytes")), ErrSchemaViolation, nil},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertConformanceRejection(t, test.want, test.forbidden, func() error {
					_, err := ValidateRecordStructure(test.input)
					return err
				})
			})
		}

		for _, test := range []struct {
			name  string
			input []byte
			want  error
		}{
			{"event version", conformanceRecordWithEventBody(t, conformanceEventWithRawField(t, eventBodyKeyProtocolVersion, []byte{0x00})), ErrUnsupportedVersion},
			{"record body version", conformanceOuterWithBody(t, conformanceRecordBodyWithRawField(t, testGenesisRecordBodyWire(t), recordBodyKeyVersion, []byte{0x00})), ErrUnsupportedVersion},
			{"outer version", conformanceOuterWithRawField(t, ledgerRecordKeyVersion, []byte{0x00}), ErrUnsupportedVersion},
			{"record body digest identifier", conformanceRecordWithRawField(t, recordBodyKeyEventDigestAlgorithm, conformanceText("sha-512")), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(test.input)
					return err
				})
			})
		}

		const nestedPrivateMarker = "private-nested-marker"
		privateBody := testGenesisRecordBodyWire(t)
		privateBody.SignerKeyReference = nestedPrivateMarker
		privateBody.EventDigest[0] ^= 0xff
		assertConformanceRejection(t, ErrDigestMismatch, []string{nestedPrivateMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, privateBody))
			return err
		})
	})

	t.Run("one-property field boundary matrix", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			key   uint64
			value []byte
			want  error
		}{
			{"event protocol version scalar", eventBodyKeyProtocolVersion, conformanceText("private-event-version"), ErrSchemaViolation},
			{"event ledger ID scalar", eventBodyKeyLedgerID, conformanceText("private-event-ledger"), ErrSchemaViolation},
			{"event sequence scalar", eventBodyKeySequence, conformanceText("private-event-sequence"), ErrSchemaViolation},
			{"event predecessor scalar", eventBodyKeyPreviousRecordDigest, conformanceText("private-event-previous"), ErrSchemaViolation},
			{"event timestamp scalar", eventBodyKeyAdmittedAtUnixMS, conformanceText("private-event-time"), ErrSchemaViolation},
			{"event kind scalar", eventBodyKeyEventKind, conformanceText("private-event-kind"), ErrSchemaViolation},
			{"event payload version scalar", eventBodyKeyPayloadVersion, conformanceText("private-event-payload-version"), ErrSchemaViolation},
			{"event payload scalar", eventBodyKeyPayloadBytes, conformanceText("private-event-payload"), ErrSchemaViolation},
			{"event short ledger ID", eventBodyKeyLedgerID, conformanceBytes([]byte("private-short-ledger")), ErrSchemaViolation},
			{"event long ledger ID", eventBodyKeyLedgerID, conformanceBytes(bytes.Repeat([]byte("p"), ledgerIDBytes+1)), ErrSchemaViolation},
			{"event short predecessor", eventBodyKeyPreviousRecordDigest, conformanceBytes([]byte("private-short-predecessor")), ErrSchemaViolation},
			{"event long predecessor", eventBodyKeyPreviousRecordDigest, conformanceBytes(bytes.Repeat([]byte("p"), digestBytes+1)), ErrSchemaViolation},
			{"event version two", eventBodyKeyProtocolVersion, []byte{0x02}, ErrUnsupportedVersion},
		} {
			t.Run(test.name, func(t *testing.T) {
				input := conformanceRecordWithEventBody(t, conformanceEventWithRawField(t, test.key, test.value))
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(input)
					return err
				})
			})
		}

		for _, test := range []struct {
			name  string
			key   uint64
			value []byte
			want  error
		}{
			{"record body version two", recordBodyKeyVersion, []byte{0x02}, ErrUnsupportedVersion},
			{"record body event scalar", recordBodyKeyEventBodyBytes, conformanceText("private-record-event"), ErrSchemaViolation},
			{"record body digest algorithm scalar", recordBodyKeyEventDigestAlgorithm, conformanceBytes([]byte("private-record-algorithm")), ErrSchemaViolation},
			{"record body digest scalar", recordBodyKeyEventDigest, conformanceText("private-record-digest"), ErrSchemaViolation},
			{"record body signer scalar", recordBodyKeySignerKeyReference, conformanceBytes([]byte("private-record-signer")), ErrSchemaViolation},
			{"record body signature algorithm scalar", recordBodyKeySignatureAlgorithm, conformanceBytes([]byte("private-signature-algorithm")), ErrSchemaViolation},
			{"record body signature encoding scalar", recordBodyKeySignatureEncoding, conformanceBytes([]byte("private-signature-encoding")), ErrSchemaViolation},
			{"record body signature scalar", recordBodyKeySignatureBytes, conformanceText("private-signature-bytes"), ErrSchemaViolation},
			{"record body short event digest", recordBodyKeyEventDigest, conformanceBytes([]byte("private-short-event-digest")), ErrSchemaViolation},
			{"record body long event digest", recordBodyKeyEventDigest, conformanceBytes(bytes.Repeat([]byte("p"), digestBytes+1)), ErrSchemaViolation},
			{"record body empty signer", recordBodyKeySignerKeyReference, conformanceText(""), ErrSchemaViolation},
			{"record body long signer", recordBodyKeySignerKeyReference, conformanceText(strings.Repeat("p", maxSignerReferenceBytes+1)), ErrSchemaViolation},
			{"record body signature algorithm identifier", recordBodyKeySignatureAlgorithm, conformanceText("ecdsa-p256-sha512"), ErrSchemaViolation},
			{"record body signature encoding identifier", recordBodyKeySignatureEncoding, conformanceText("raw"), ErrSchemaViolation},
			{"record body short signature", recordBodyKeySignatureBytes, conformanceBytes([]byte("private")), ErrSchemaViolation},
			{"record body long signature", recordBodyKeySignatureBytes, conformanceBytes(bytes.Repeat([]byte("p"), maxSignatureBytes+1)), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				body := conformanceRecordBodyWithRawField(t, testGenesisRecordBodyWire(t), test.key, test.value)
				input := conformanceOuterWithBody(t, body)
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(input)
					return err
				})
			})
		}

		for _, test := range []struct {
			name  string
			key   uint64
			value []byte
			want  error
		}{
			{"outer version two", ledgerRecordKeyVersion, []byte{0x02}, ErrUnsupportedVersion},
			{"outer version scalar", ledgerRecordKeyVersion, conformanceText("private-outer-version"), ErrSchemaViolation},
			{"outer body scalar", ledgerRecordKeyRecordBodyBytes, conformanceText("private-outer-body"), ErrSchemaViolation},
			{"outer digest algorithm scalar", ledgerRecordKeyDigestAlgorithm, conformanceBytes([]byte("private-outer-algorithm")), ErrSchemaViolation},
			{"outer digest scalar", ledgerRecordKeyDigest, conformanceText("private-outer-digest"), ErrSchemaViolation},
			{"outer short digest", ledgerRecordKeyDigest, conformanceBytes([]byte("private-short-record-digest")), ErrSchemaViolation},
			{"outer long digest", ledgerRecordKeyDigest, conformanceBytes(bytes.Repeat([]byte("p"), digestBytes+1)), ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				input := conformanceOuterWithRawField(t, test.key, test.value)
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateRecordStructure(input)
					return err
				})
			})
		}
	})

	t.Run("nested rejection privacy", func(t *testing.T) {
		const (
			payloadMarker      = "private-payload-marker"
			signerMarker       = "private-signer-marker"
			signatureMarker    = "private-signature-marker"
			eventDigestMarker  = "private-event-digest-marker"
			recordDigestMarker = "private-record-digest-marker"
			eventBytesMarker   = "private-event-bytes-marker"
			recordBytesMarker  = "private-record-bytes-marker"
		)

		payloadEvent := testGenesisEventBodyWire()
		payloadEvent.PayloadBytes = conformanceText(payloadMarker)
		payloadRecord, err := ValidateRecordStructure(conformanceRecord(t, testRecordBodyForEvent(t, payloadEvent)))
		if err != nil {
			t.Fatalf("ValidateRecordStructure payload marker: %v", err)
		}
		assertConformanceRejection(t, ErrSchemaViolation, []string{payloadMarker}, func() error {
			return ValidateEventSemantics(payloadRecord.Event())
		})

		signerBody := testGenesisRecordBodyWire(t)
		signerBody.SignerKeyReference = signerMarker
		signerBody.SignatureAlgorithm = "invalid"
		assertConformanceRejection(t, ErrSchemaViolation, []string{signerMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, signerBody))
			return err
		})

		signatureBody := testGenesisRecordBodyWire(t)
		signatureBody.SignatureBytes = []byte(signatureMarker)
		signatureBody.SignatureEncoding = "raw"
		assertConformanceRejection(t, ErrSchemaViolation, []string{signatureMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, signatureBody))
			return err
		})

		eventDigestBody := testGenesisRecordBodyWire(t)
		eventDigestBody.EventDigest = conformanceDigestMarker(eventDigestMarker)
		assertConformanceRejection(t, ErrDigestMismatch, []string{eventDigestMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, eventDigestBody))
			return err
		})

		recordDigest := conformanceDigestMarker(recordDigestMarker)
		outer := testLedgerRecordWire(t, encodeRecordBodyForTest(t, testGenesisRecordBodyWire(t)))
		outer.RecordDigest = recordDigest
		assertConformanceRejection(t, ErrDigestMismatch, []string{recordDigestMarker}, func() error {
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, outer))
			return err
		})

		eventBytesBody := testGenesisRecordBodyWire(t)
		eventBytesBody.EventBodyBytes = []byte(eventBytesMarker)
		eventDigest := conformanceEventDigest(eventBytesBody.EventBodyBytes)
		eventBytesBody.EventDigest = eventDigest[:]
		assertConformanceRejection(t, ErrMalformedCBOR, []string{eventBytesMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, eventBytesBody))
			return err
		})

		assertConformanceRejection(t, ErrMalformedCBOR, []string{recordBytesMarker}, func() error {
			_, err := ValidateRecordStructure(conformanceOuterWithBody(t, []byte(recordBytesMarker)))
			return err
		})
	})

	t.Run("noncanonical known genesis remains layer-reachable", func(t *testing.T) {
		noncanonicalPayload := []byte{0xa1, 0x18, 0x00, 0xf6}
		event := testGenesisEventBodyWire()
		event.PayloadBytes = noncanonicalPayload
		record, err := ValidateRecordStructure(conformanceRecord(t, testRecordBodyForEvent(t, event)))
		if err != nil {
			t.Fatalf("ValidateRecordStructure noncanonical payload: %v", err)
		}
		assertConformanceRejection(t, ErrNonConformingCBOR, nil, func() error {
			return ValidateEventSemantics(record.Event())
		})

		noncanonicalEvent := conformanceReplaceFirst(genesisEvent, []byte{0x00, 0x01}, []byte{0x00, 0x18, 0x01})
		assertConformanceRejection(t, ErrNonConformingCBOR, nil, func() error {
			_, err := ValidateRecordStructure(conformanceRecordWithEventBody(t, noncanonicalEvent))
			return err
		})

		noncanonicalBody := conformanceReplaceFirst(readProtocolFixture(t, "genesis-record-body.cbor"), []byte{0x00, 0x01}, []byte{0x00, 0x18, 0x01})
		assertConformanceRejection(t, ErrNonConformingCBOR, nil, func() error {
			_, err := ValidateRecordStructure(conformanceOuterWithBody(t, noncanonicalBody))
			return err
		})
	})

	t.Run("digest commitments", func(t *testing.T) {
		body := testGenesisRecordBodyWire(t)
		body.EventDigest[0] ^= 0xff
		assertConformanceRejection(t, ErrDigestMismatch, nil, func() error {
			_, err := ValidateRecordStructure(conformanceRecord(t, body))
			return err
		})
		outer := testLedgerRecordWire(t, encodeRecordBodyForTest(t, testGenesisRecordBodyWire(t)))
		outer.RecordDigest[0] ^= 0xff
		assertConformanceRejection(t, ErrDigestMismatch, nil, func() error {
			_, err := ValidateRecordStructure(encodeLedgerRecordForTest(t, outer))
			return err
		})
	})

	t.Run("every invalid genesis shape", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			mutate func(*eventBodyWire)
			want   error
		}{
			{"sequence two", func(w *eventBodyWire) { w.Sequence = 2 }, ErrInvalidSequence},
			{"predecessor present", func(w *eventBodyWire) { w.PreviousRecordDigest = make([]byte, digestBytes) }, ErrInvalidSequence},
			{"unknown kind", func(w *eventBodyWire) { w.EventKind = 2 }, ErrUnsupportedEvent},
			{"unknown payload version", func(w *eventBodyWire) { w.PayloadVersion = 2 }, ErrUnsupportedEvent},
			{"nonempty payload", func(w *eventBodyWire) { w.PayloadBytes = []byte{0xa1, 0x00, 0x00} }, ErrSchemaViolation},
			{"zero sequence", func(w *eventBodyWire) { w.Sequence = 0 }, ErrInvalidSequence},
			{"zero event kind", func(w *eventBodyWire) { w.EventKind = 0 }, ErrSchemaViolation},
			{"zero payload version", func(w *eventBodyWire) { w.PayloadVersion = 0 }, ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				wire := testGenesisEventBodyWire()
				test.mutate(&wire)
				event, err := ValidateEventStructure(encodeEventBodyForTest(t, wire))
				if test.name == "zero sequence" || test.name == "zero event kind" || test.name == "zero payload version" {
					assertConformanceRejection(t, test.want, nil, func() error {
						_, err := ValidateRecordStructure(conformanceRecordWithEventBody(t, encodeEventBodyForTest(t, wire)))
						return err
					})
					return
				}
				if err != nil {
					t.Fatalf("ValidateEventStructure: %v", err)
				}
				assertConformanceRejection(t, test.want, nil, func() error { return ValidateEventSemantics(event) })
			})
		}
	})

	t.Run("chain identity sequence overflow and predecessor", func(t *testing.T) {
		genesis, err := ValidateRecordStructure(genesisRecord)
		if err != nil {
			t.Fatalf("ValidateRecordStructure genesis: %v", err)
		}
		state, err := ValidateChainConsistency(ChainState{}, genesis)
		if err != nil {
			t.Fatalf("ValidateChainConsistency genesis: %v", err)
		}
		otherLedger := genesis.Event().LedgerID()
		otherLedger[0] ^= 0xff
		genesisLedgerID := genesis.Event().LedgerID()
		continuation := newUnknownContinuationRecord(t, genesis, 2, 0)
		continuationState, err := ValidateChainConsistency(state, continuation)
		if err != nil {
			t.Fatalf("ValidateChainConsistency continuation: %v", err)
		}
		for _, test := range []struct {
			name   string
			state  ChainState
			record StructuralRecord
			want   error
		}{
			{"second genesis", state, newRecordFromWire(t, eventBodyWire{ProtocolVersion: protocolVersionV1, LedgerID: genesisLedgerID[:], Sequence: 2, PreviousRecordDigest: digestBytesFrom(genesis.RecordDigest()), EventKind: uint64(EventKindLedgerInitialized), PayloadVersion: ledgerInitializedPayloadVersionV1, PayloadBytes: []byte{0xa0}}), ErrInvalidSequence},
			{"wrong ledger", state, newUnknownRecord(t, otherLedger, 2, digestBytesFrom(genesis.RecordDigest()), 0), ErrLedgerMismatch},
			{"duplicate sequence", state, newUnknownContinuationRecord(t, genesis, 1, 0), ErrInvalidSequence},
			{"reversed sequence", continuationState, newUnknownContinuationRecord(t, continuation, 1, 0), ErrInvalidSequence},
			{"skipped sequence", state, newUnknownContinuationRecord(t, genesis, 3, 0), ErrInvalidSequence},
			{"missing predecessor", state, newUnknownRecord(t, genesis.Event().LedgerID(), 2, nil, 0), ErrChainLinkMismatch},
			{"wrong predecessor", state, newUnknownRecord(t, genesis.Event().LedgerID(), 2, make([]byte, digestBytes), 0), ErrChainLinkMismatch},
			{"overflow", ChainState{initialized: true, ledgerID: genesis.Event().LedgerID(), lastSequence: ^uint64(0), lastRecordDigest: genesis.RecordDigest()}, newUnknownContinuationRecord(t, genesis, 2, 0), ErrInvalidSequence},
			{"zero structural record", ChainState{}, StructuralRecord{}, ErrSchemaViolation},
		} {
			t.Run(test.name, func(t *testing.T) {
				before := test.state
				assertConformanceRejection(t, test.want, nil, func() error {
					_, err := ValidateChainConsistency(test.state, test.record)
					return err
				})
				if test.state != before {
					t.Fatal("ValidateChainConsistency mutated input state")
				}
			})
		}
		assertConformanceRejection(t, ErrUnsupportedEvent, nil, func() error {
			_, err := ValidateChainConsistency(ChainState{}, newUnknownRecord(t, genesisLedgerID, 1, nil, 0))
			return err
		})
	})
}

func assertConformanceRejection(t *testing.T, want error, forbidden []string, reject func() error) {
	t.Helper()
	first := reject()
	second := reject()
	if !errors.Is(first, want) || !errors.Is(second, want) {
		t.Fatalf("errors = (%v, %v), want errors.Is(_, %v)", first, second, want)
	}
	if protocolErrorIdentity(first) != protocolErrorIdentity(second) {
		t.Fatalf("error identities differ: %q and %q", protocolErrorIdentity(first), protocolErrorIdentity(second))
	}
	assertErrorIsPrivate(t, first, forbidden...)
	assertErrorIsPrivate(t, second, forbidden...)
}

func assertErrorIsPrivate(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want rejection")
	}
	for _, value := range forbidden {
		if strings.Contains(err.Error(), value) {
			t.Fatalf("error disclosed private value %q: %v", value, err)
		}
	}
}

func protocolErrorIdentity(err error) string {
	if err == nil {
		return "nil"
	}
	for _, identity := range protocolErrorIdentities {
		if errors.Is(err, identity.err) {
			return identity.name
		}
	}
	return "other"
}

// conformanceRecord rebuilds the record body and outer record and recomputes
// the test-only SHA-256 commitment.  Golden vector bytes remain the independent
// digest oracle; this helper only routes adversarial input to its intended layer.
func conformanceRecord(t *testing.T, body recordBodyWire) []byte {
	t.Helper()
	return conformanceOuterWithBody(t, encodeRecordBodyForTest(t, body))
}

func conformanceRecordWithEventBody(t *testing.T, eventBody []byte) []byte {
	t.Helper()
	body := testGenesisRecordBodyWire(t)
	body.EventBodyBytes = bytes.Clone(eventBody)
	digest := conformanceEventDigest(eventBody)
	body.EventDigest = digest[:]
	return conformanceRecord(t, body)
}

func testRecordBodyForEvent(t *testing.T, eventWire eventBodyWire) recordBodyWire {
	t.Helper()
	eventBody := encodeEventBodyForTest(t, eventWire)
	body := testGenesisRecordBodyWire(t)
	body.EventBodyBytes = eventBody
	digest := conformanceEventDigest(eventBody)
	body.EventDigest = digest[:]
	return body
}

func conformanceRecordWithRawField(t *testing.T, key uint64, value []byte) []byte {
	t.Helper()
	body := conformanceRecordBodyWithRawField(t, testGenesisRecordBodyWire(t), key, value)
	return conformanceOuterWithBody(t, body)
}

func conformanceRecordBodyWithRawField(t *testing.T, body recordBodyWire, key uint64, value []byte) []byte {
	t.Helper()
	encoded := encodeRecordBodyForTest(t, body)
	return conformanceMapWithRawField(t, encoded, key, value)
}

func conformanceOuterWithBody(t *testing.T, body []byte) []byte {
	t.Helper()
	digest := conformanceRecordDigest(body)
	outer := ledgerRecordWire{
		RecordVersion:         recordVersionV1,
		RecordBodyBytes:       bytes.Clone(body),
		RecordDigestAlgorithm: digestAlgorithmSHA256,
		RecordDigest:          digest[:],
	}
	return encodeLedgerRecordForTest(t, outer)
}

func conformanceOuterWithRawField(t *testing.T, key uint64, value []byte) []byte {
	t.Helper()
	return conformanceMapWithRawField(t, readProtocolFixture(t, "genesis-ledger-record.cbor"), key, value)
}

func conformanceEventWithRawField(t *testing.T, key uint64, value []byte) []byte {
	t.Helper()
	return conformanceMapWithRawField(t, readProtocolFixture(t, "genesis-event-body.cbor"), key, value)
}

func conformanceMapWithRawField(t *testing.T, encoded []byte, key uint64, value []byte) []byte {
	t.Helper()
	values := conformanceRawMap(t, encoded)
	if _, ok := values[key]; !ok {
		t.Fatalf("map missing key %d", key)
	}
	values[key] = bytes.Clone(value)
	return conformanceEncodeRawMap(t, values, nil)
}

func conformanceReorderedMap(t *testing.T, encoded []byte) []byte {
	t.Helper()
	values := conformanceRawMap(t, encoded)
	keys := conformanceSortedKeys(values)
	keys[0], keys[1] = keys[1], keys[0]
	return conformanceEncodeRawMap(t, values, keys)
}

func conformanceDuplicateFirstMapKey(t *testing.T, encoded []byte) []byte {
	t.Helper()
	values := conformanceRawMap(t, encoded)
	keys := conformanceSortedKeys(values)
	duplicate := keys[0]
	return conformanceEncodeRawMapPairs(t, values, append([]uint64{duplicate}, keys...))
}

func conformanceUnknownMapKey(t *testing.T, encoded []byte) []byte {
	t.Helper()
	values := conformanceRawMap(t, encoded)
	values[99] = []byte{0x01}
	return conformanceEncodeRawMap(t, values, nil)
}

func conformanceMissingLastMapKey(t *testing.T, encoded []byte) []byte {
	t.Helper()
	values := conformanceRawMap(t, encoded)
	keys := conformanceSortedKeys(values)
	delete(values, keys[len(keys)-1])
	return conformanceEncodeRawMap(t, values, nil)
}

func conformanceIndefiniteMap(encoded []byte) []byte {
	if len(encoded) == 0 || encoded[0] < 0xa0 || encoded[0] > 0xb7 {
		panic("conformance test requires a small definite map")
	}
	result := make([]byte, 0, len(encoded)+1)
	result = append(result, 0xbf)
	result = append(result, encoded[1:]...)
	return append(result, 0xff)
}

func conformanceReplaceFirst(encoded, old, new []byte) []byte {
	index := bytes.Index(encoded, old)
	if index < 0 {
		panic(fmt.Sprintf("conformance replacement %x not found", old))
	}
	result := make([]byte, 0, len(encoded)-len(old)+len(new))
	result = append(result, encoded[:index]...)
	result = append(result, new...)
	return append(result, encoded[index+len(old):]...)
}

func conformanceText(value string) []byte {
	encoded := conformanceStringHeader(0x60, len(value))
	return append(encoded, value...)
}

func conformanceBytes(value []byte) []byte {
	encoded := conformanceStringHeader(0x40, len(value))
	return append(encoded, value...)
}

func conformanceStringHeader(majorType byte, length int) []byte {
	switch {
	case length <= 23:
		return []byte{majorType | byte(length)}
	case length <= 0xff:
		return []byte{majorType | 24, byte(length)}
	case length <= 0xffff:
		return []byte{majorType | 25, byte(length >> 8), byte(length)}
	default:
		panic("conformance test string exceeds uint16 length")
	}
}

func conformanceRawMap(t *testing.T, encoded []byte) map[uint64]cbor.RawMessage {
	t.Helper()
	var values map[uint64]cbor.RawMessage
	if err := protocolDecMode.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("decode test map: %v", err)
	}
	return values
}

func conformanceSortedKeys(values map[uint64]cbor.RawMessage) []uint64 {
	keys := make([]uint64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for index := 1; index < len(keys); index++ {
		for previous := index; previous > 0 && keys[previous] < keys[previous-1]; previous-- {
			keys[previous], keys[previous-1] = keys[previous-1], keys[previous]
		}
	}
	return keys
}

func conformanceEncodeRawMap(t *testing.T, values map[uint64]cbor.RawMessage, order []uint64) []byte {
	t.Helper()
	if order == nil {
		order = conformanceSortedKeys(values)
	}
	return conformanceEncodeRawMapPairs(t, values, order)
}

func conformanceEncodeRawMapPairs(t *testing.T, values map[uint64]cbor.RawMessage, keys []uint64) []byte {
	t.Helper()
	if len(keys) > 23 {
		t.Fatal("too many test map pairs")
	}
	encoded := []byte{0xa0 | byte(len(keys))}
	for _, key := range keys {
		keyBytes, err := protocolEncMode.Marshal(key)
		if err != nil {
			t.Fatalf("encode test key: %v", err)
		}
		encoded = append(encoded, keyBytes...)
		encoded = append(encoded, values[key]...)
	}
	return encoded
}

func conformanceEventDigest(body []byte) Digest {
	preimage := append([]byte(domainEventDigestV1+string(digestDomainSeparator)), body...)
	return Digest(sha256.Sum256(preimage))
}

func conformanceRecordDigest(body []byte) Digest {
	preimage := append([]byte(domainRecordDigestV1+string(digestDomainSeparator)), body...)
	return Digest(sha256.Sum256(preimage))
}

func conformanceDigestMarker(marker string) []byte {
	digest := make([]byte, digestBytes)
	copy(digest, marker)
	return digest
}
