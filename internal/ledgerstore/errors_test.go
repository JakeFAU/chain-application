package ledgerstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

func TestAppendRejectsDuplicateRecord(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	err := store.Append(context.Background(), record)
	if !errors.Is(err, ErrDuplicateRecord) {
		t.Fatalf("second Append error = %v, want ErrDuplicateRecord", err)
	}
	// A replayed identical record must not be reported as a head conflict.
	if errors.Is(err, ErrChainHeadMoved) {
		t.Fatal("duplicate record reported as ErrChainHeadMoved")
	}
}

func TestAppendRejectsUnknownPredecessor(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)

	orphan := newTestContinuationRecord(t, ledgerID, 2, ledgerv1.Digest{0x01, 0x02, 0x03})
	err := store.Append(context.Background(), orphan)
	if !errors.Is(err, ErrUnknownPredecessor) {
		t.Fatalf("Append error = %v, want ErrUnknownPredecessor", err)
	}
}

func TestAppendRejectsSequenceConflictFromDistinctRecord(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)

	first := newTestGenesisRecord(t, ledgerID)
	if err := store.Append(context.Background(), first); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	// Same ledger and sequence, different signer reference, so the record
	// digest differs and the sequence constraint is what fires.
	event, err := ledgerv1.NewGenesisEvent(ledgerID, 1_755_000_000_000)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	rival, err := ledgerv1.NewRecord(event, "rival-signer-key-reference", make([]byte, 70))
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	if rival.RecordDigest() == first.RecordDigest() {
		t.Fatal("rival record digest matches the first; the test cannot isolate the sequence constraint")
	}

	err = store.Append(context.Background(), rival)
	if !errors.Is(err, ErrChainHeadMoved) {
		t.Fatalf("Append error = %v, want ErrChainHeadMoved", err)
	}
}

func TestSchemaRejectsOversizedRecordBytes(t *testing.T) {
	store := openTestStore(t)

	oversized := make([]byte, ledgerv1.MaxRecordBytes+1)
	digest := make([]byte, 32)
	digest[0] = 0x7f
	ledgerID := make([]byte, 32)

	_, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO ledger_record (
			record_digest, ledger_id, sequence_number,
			event_kind, payload_version, record_bytes
		) VALUES ($1, $2, 1, 1, 1, $3)`,
		digest, ledgerID, oversized,
	)
	if err == nil {
		t.Fatal("oversized insert succeeded, want size-bound rejection")
	}
	if !strings.Contains(err.Error(), "ledger_record_bytes_bound") {
		t.Fatalf("error = %v, want ledger_record_bytes_bound violation", err)
	}
}
