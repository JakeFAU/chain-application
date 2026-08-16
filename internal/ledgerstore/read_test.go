package ledgerstore

import (
	"bytes"
	"context"
	"crypto/sha256"
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
// The genesis record's previous_record_digest column is NULL, so this alone
// never exercises the non-NULL branch of that assertion; see
// TestStoredDerivedColumnsMatchRecordBytesForContinuation below.
func TestStoredDerivedColumnsMatchRecordBytes(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}
	assertStoredDerivedColumnsMatchRecordBytes(t, store, record)
}

// A continuation record has a non-NULL previous_record_digest, so this
// exercises the branch of the assertion that compares that column to
// event.PreviousRecordDigest(), which the genesis-only case above cannot
// reach.
func TestStoredDerivedColumnsMatchRecordBytesForContinuation(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	genesis := newTestGenesisRecord(t, ledgerID)
	if err := store.Append(context.Background(), genesis); err != nil {
		t.Fatalf("Append genesis: %v", err)
	}

	continuation := newTestContinuationRecord(t, ledgerID, 2, genesis.RecordDigest())
	if err := store.Append(context.Background(), continuation); err != nil {
		t.Fatalf("Append continuation: %v", err)
	}
	assertStoredDerivedColumnsMatchRecordBytes(t, store, continuation)
}

func assertStoredDerivedColumnsMatchRecordBytes(
	t *testing.T,
	store *Store,
	record ledgerv1.StructuralRecord,
) {
	t.Helper()

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

// A drifted record_digest column must fail closed rather than silently return
// a different, structurally valid record. The row is inserted with raw SQL,
// as in TestRecordFailsClosedOnCorruptedBytes, because ledger_record rejects
// UPDATE: record_bytes here is a genuinely valid record, but the record_digest
// column stored alongside it is a different valid 32-byte value, so the
// stored digest never re-derives from the stored bytes. This exercises
// Store.Record's own re-derivation check, not ValidateRecordStructure, which
// never sees the mismatch because it does not know what digest was requested.
func TestRecordFailsClosedOnDriftedDigestColumn(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	// newTestLedgerID derives its result from t.Name() alone, so a second call
	// within this same test would return the identical value; hashing ledgerID
	// once more gives a mismatched digest that is still unique per run without
	// colliding with the record's real digest.
	mismatchedDigest := ledgerv1.Digest(sha256.Sum256(ledgerID[:]))
	if mismatchedDigest == record.RecordDigest() {
		t.Fatal("mismatched digest collides with the record's real digest; cannot isolate the drift")
	}

	_, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO ledger_record (
		     record_digest, ledger_id, sequence_number, previous_record_digest,
		     event_kind, payload_version, record_bytes
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		mismatchedDigest[:], ledgerID[:], "1", nil, "1", "1", record.Bytes(),
	)
	if err != nil {
		t.Fatalf("insert row with drifted record_digest column: %v", err)
	}

	_, err = store.Record(context.Background(), mismatchedDigest)
	if err == nil {
		t.Fatal(
			"Record succeeded on a row whose record_digest column drifted from record_bytes, " +
				"want a fail-closed error",
		)
	}
	if errors.Is(err, ErrRecordNotFound) {
		t.Fatalf(
			"Record error = ErrRecordNotFound, want a digest-mismatch error; the row exists: %v", err,
		)
	}
}

// The equivalent drift for Head: the ledger_id column used to locate the row
// disagrees with the ledger ID the stored record_bytes actually decode to.
// The row is inserted with raw SQL, since ledger_record rejects UPDATE and
// Append derives ledger_id from the record itself, so there is no way to
// produce this drift through the store's own write path.
func TestHeadFailsClosedOnDriftedLedgerIDColumn(t *testing.T) {
	store := openTestStore(t)
	requestedLedgerID := newTestLedgerID(t)

	// newTestLedgerID derives its result from t.Name() alone, so a second call
	// within this same test would return the identical ID; hashing it once
	// more gives a distinct, still run-unique ledger ID for the record that is
	// actually stored.
	actualLedgerID := ledgerv1.LedgerID(sha256.Sum256(requestedLedgerID[:]))
	actualRecord := newTestGenesisRecord(t, actualLedgerID)
	digest := actualRecord.RecordDigest()

	_, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO ledger_record (
		     record_digest, ledger_id, sequence_number, previous_record_digest,
		     event_kind, payload_version, record_bytes
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		digest[:], requestedLedgerID[:], "1", nil, "1", "1", actualRecord.Bytes(),
	)
	if err != nil {
		t.Fatalf("insert row with drifted ledger_id column: %v", err)
	}

	_, err = store.Head(context.Background(), requestedLedgerID)
	if err == nil {
		t.Fatal(
			"Head succeeded on a row whose ledger_id column drifted from record_bytes, " +
				"want a fail-closed error",
		)
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
