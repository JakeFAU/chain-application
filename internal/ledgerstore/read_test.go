package ledgerstore

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

func TestHeadReportsUninitializedForUnknownLedger(t *testing.T) {
	store := openTestStore(t)

	state, err := store.Head(context.Background(), newTestLedgerID(t))
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if state.Initialized() {
		t.Fatal("Initialized() = true for an unknown ledger, want false")
	}
}

func TestHeadReportsAdmittedGenesis(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	state, err := store.Head(context.Background(), ledgerID)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !state.Initialized() {
		t.Fatal("Initialized() = false, want true")
	}
	if state.LastSequence() != record.Event().Sequence() {
		t.Fatalf("LastSequence() = %d, want %d", state.LastSequence(), record.Event().Sequence())
	}
	digest, ok := state.LastRecordDigest()
	if !ok || digest != record.RecordDigest() {
		t.Fatalf("LastRecordDigest() = %x, want %x", digest, record.RecordDigest())
	}
}

func TestRecordRoundTripsExactBytes(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	stored, err := store.Record(context.Background(), record.RecordDigest())
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !bytes.Equal(stored.Bytes(), record.Bytes()) {
		t.Fatal("retrieved bytes differ from stored bytes")
	}
	if stored.RecordDigest() != record.RecordDigest() {
		t.Fatalf("digest = %x, want %x", stored.RecordDigest(), record.RecordDigest())
	}
}

func TestRecordReportsMissingRecord(t *testing.T) {
	store := openTestStore(t)

	_, err := store.Record(context.Background(), ledgerv1.Digest{0xaa, 0xbb})
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Record error = %v, want ErrRecordNotFound", err)
	}
}

// Every derived column must be reproducible from the stored bytes alone. This
// is the check that would catch a derived column drifting from the authority.
func TestStoredDerivedColumnsMatchRecordBytes(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		storedRecordDigest   []byte
		storedLedgerID       []byte
		storedSequence       string
		storedPreviousDigest []byte
		storedKind           string
		storedVersion        string
		storedBytes          []byte
	)
	digest := record.RecordDigest()
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT record_digest, ledger_id, sequence_number::text,
		        previous_record_digest, event_kind::text,
		        payload_version::text, record_bytes
		 FROM ledger_record WHERE record_digest = $1`,
		digest[:],
	).Scan(
		&storedRecordDigest, &storedLedgerID, &storedSequence,
		&storedPreviousDigest, &storedKind, &storedVersion, &storedBytes,
	)
	if err != nil {
		t.Fatalf("query stored row: %v", err)
	}

	rederived, err := ledgerv1.ValidateRecordStructure(storedBytes)
	if err != nil {
		t.Fatalf("re-validate stored bytes: %v", err)
	}
	event := rederived.Event()
	rederivedLedgerID := event.LedgerID()
	rederivedDigest := rederived.RecordDigest()

	if !bytes.Equal(storedRecordDigest, rederivedDigest[:]) {
		t.Fatalf("record_digest = %x, re-derives to %x", storedRecordDigest, rederivedDigest)
	}
	if !bytes.Equal(storedLedgerID, rederivedLedgerID[:]) {
		t.Fatalf("ledger_id = %x, re-derives to %x", storedLedgerID, rederivedLedgerID)
	}
	if storedSequence != strconv.FormatUint(event.Sequence(), 10) {
		t.Fatalf("sequence_number = %s, re-derives to %d", storedSequence, event.Sequence())
	}
	rederivedPreviousDigest, hasPreviousDigest := event.PreviousRecordDigest()
	if hasPreviousDigest {
		if !bytes.Equal(storedPreviousDigest, rederivedPreviousDigest[:]) {
			t.Fatalf(
				"previous_record_digest = %x, re-derives to %x",
				storedPreviousDigest, rederivedPreviousDigest,
			)
		}
	} else if storedPreviousDigest != nil {
		t.Fatalf(
			"previous_record_digest = %x, want NULL because the record has no previous digest",
			storedPreviousDigest,
		)
	}
	if storedKind != strconv.FormatUint(uint64(event.Kind()), 10) {
		t.Fatalf("event_kind = %s, re-derives to %d", storedKind, event.Kind())
	}
	if storedVersion != strconv.FormatUint(event.PayloadVersion(), 10) {
		t.Fatalf("payload_version = %s, re-derives to %d", storedVersion, event.PayloadVersion())
	}
}

// A corrupted row must fail closed rather than yield a record that was never
// valid. The row is inserted with raw SQL because ledger_record rejects
// UPDATE: every column satisfies its CHECK/FK constraints, but record_bytes
// is not valid CBOR, since the constraints validate lengths and ranges, not
// CBOR well-formedness. This exercises Store.Record's own re-validation path
// directly, not just the structural validator it calls.
func TestRecordFailsClosedOnCorruptedBytes(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	digest := ledgerv1.Digest(ledgerID)
	corruptedBytes := []byte{0xff, 0xff, 0xff, 0xff}

	_, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO ledger_record (
		     record_digest, ledger_id, sequence_number, previous_record_digest,
		     event_kind, payload_version, record_bytes
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		digest[:], ledgerID[:], "1", nil, "1", "1", corruptedBytes,
	)
	if err != nil {
		t.Fatalf("insert row with corrupted record_bytes: %v", err)
	}

	_, err = store.Record(context.Background(), digest)
	if err == nil {
		t.Fatal("Record succeeded on a row with corrupted record_bytes, want a fail-closed error")
	}
	if errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Record error = ErrRecordNotFound, want a validation error; the row exists: %v", err)
	}
}

// Head must return the record with the highest sequence number, not merely
// the first or last row returned without an explicit order. A single-record
// ledger cannot distinguish ORDER BY sequence_number DESC from no ordering at
// all, so this appends a second record before calling Head.
func TestHeadReturnsHighestSequenceRecord(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	genesis := newTestGenesisRecord(t, ledgerID)
	if err := store.Append(context.Background(), genesis); err != nil {
		t.Fatalf("Append genesis: %v", err)
	}

	// Append does not enforce Go-level chain semantics, so this continuation
	// need only satisfy the table's own foreign key, which the genesis row
	// already does.
	continuation := newTestContinuationRecord(t, ledgerID, 2, genesis.RecordDigest())
	if err := store.Append(context.Background(), continuation); err != nil {
		t.Fatalf("Append continuation: %v", err)
	}

	state, err := store.Head(context.Background(), ledgerID)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !state.Initialized() {
		t.Fatal("Initialized() = false, want true")
	}
	if state.LastSequence() != continuation.Event().Sequence() {
		t.Fatalf(
			"LastSequence() = %d, want %d (the continuation's sequence, not the genesis's)",
			state.LastSequence(), continuation.Event().Sequence(),
		)
	}
	digest, ok := state.LastRecordDigest()
	if !ok || digest != continuation.RecordDigest() {
		t.Fatalf(
			"LastRecordDigest() = %x, want %x (the continuation's digest, not the genesis's)",
			digest, continuation.RecordDigest(),
		)
	}
}
