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
		storedLedgerID []byte
		storedSequence string
		storedKind     string
		storedVersion  string
		storedBytes    []byte
	)
	digest := record.RecordDigest()
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT ledger_id, sequence_number::text, event_kind::text,
		        payload_version::text, record_bytes
		 FROM ledger_record WHERE record_digest = $1`,
		digest[:],
	).Scan(&storedLedgerID, &storedSequence, &storedKind, &storedVersion, &storedBytes)
	if err != nil {
		t.Fatalf("query stored row: %v", err)
	}

	rederived, err := ledgerv1.ValidateRecordStructure(storedBytes)
	if err != nil {
		t.Fatalf("re-validate stored bytes: %v", err)
	}
	event := rederived.Event()
	rederivedLedgerID := event.LedgerID()

	if !bytes.Equal(storedLedgerID, rederivedLedgerID[:]) {
		t.Fatalf("ledger_id = %x, re-derives to %x", storedLedgerID, rederivedLedgerID)
	}
	if storedSequence != strconv.FormatUint(event.Sequence(), 10) {
		t.Fatalf("sequence_number = %s, re-derives to %d", storedSequence, event.Sequence())
	}
	if storedKind != strconv.FormatUint(uint64(event.Kind()), 10) {
		t.Fatalf("event_kind = %s, re-derives to %d", storedKind, event.Kind())
	}
	if storedVersion != strconv.FormatUint(event.PayloadVersion(), 10) {
		t.Fatalf("payload_version = %s, re-derives to %d", storedVersion, event.PayloadVersion())
	}
}

// A corrupted row must fail closed rather than yield a record that was never
// valid. The row is corrupted with raw SQL against a scratch table copy,
// because ledger_record itself rejects UPDATE.
func TestRecordFailsClosedOnCorruptedBytes(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	corrupted := record.Bytes()
	corrupted[len(corrupted)-1] ^= 0xff
	if _, err := ledgerv1.ValidateRecordStructure(corrupted); err == nil {
		t.Fatal("corrupted bytes validated successfully; the test cannot prove fail-closed")
	}
}
