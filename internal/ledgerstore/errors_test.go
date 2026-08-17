package ledgerstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAppendRejectsDuplicateRecord(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	// Exact replay violates both the digest primary key and the sequence
	// uniqueness constraint. ON CONFLICT ... DO NOTHING on the digest key
	// suppresses that violation deterministically, leaving zero rows
	// affected rather than a race against which constraint PostgreSQL
	// reports first.
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
	// Human-readable error strings are not a machine contract; assert on the
	// constraint name and SQLSTATE, the same way errors.go itself classifies
	// admission failures.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v, want a *pgconn.PgError", err)
	}
	if pgErr.ConstraintName != "ledger_record_bytes_bound" {
		t.Fatalf("ConstraintName = %q, want %q", pgErr.ConstraintName, "ledger_record_bytes_bound")
	}
	if pgErr.Code != "23514" {
		t.Fatalf("Code = %q, want %q (check_violation)", pgErr.Code, "23514")
	}
}

// TestSchemaEnforcesConstraintMatrix proves, with raw SQL rather than through
// Store.Append, that every length and numeric-domain constraint the design
// claims PostgreSQL enforces actually rejects the violation, keyed on the
// named constraint rather than error text.
func TestSchemaEnforcesConstraintMatrix(t *testing.T) {
	store := openTestStore(t)

	digest := func(seed byte, length int) []byte {
		buf := make([]byte, length)
		buf[0] = seed
		return buf
	}

	cases := []struct {
		name           string
		recordDigest   []byte
		ledgerID       []byte
		previousDigest []byte
		sequenceNumber string
		eventKind      string
		payloadVersion string
		wantConstraint string
	}{
		{
			name:           "record digest 31 bytes",
			recordDigest:   digest(0x01, 31),
			ledgerID:       digest(0x02, 32),
			sequenceNumber: "1", eventKind: "1", payloadVersion: "1",
			wantConstraint: "ledger_record_digest_len",
		},
		{
			name:           "record digest 33 bytes",
			recordDigest:   digest(0x03, 33),
			ledgerID:       digest(0x04, 32),
			sequenceNumber: "1", eventKind: "1", payloadVersion: "1",
			wantConstraint: "ledger_record_digest_len",
		},
		{
			name:           "ledger id 31 bytes",
			recordDigest:   digest(0x05, 32),
			ledgerID:       digest(0x06, 31),
			sequenceNumber: "1", eventKind: "1", payloadVersion: "1",
			wantConstraint: "ledger_record_ledger_id_len",
		},
		{
			name:           "previous record digest 31 bytes",
			recordDigest:   digest(0x07, 32),
			ledgerID:       digest(0x08, 32),
			previousDigest: digest(0x09, 31),
			sequenceNumber: "1", eventKind: "1", payloadVersion: "1",
			wantConstraint: "ledger_record_previous_len",
		},
		{
			name:           "sequence number zero",
			recordDigest:   digest(0x0a, 32),
			ledgerID:       digest(0x0b, 32),
			sequenceNumber: "0", eventKind: "1", payloadVersion: "1",
			wantConstraint: "ledger_record_sequence_range",
		},
		{
			name:           "event kind zero",
			recordDigest:   digest(0x0c, 32),
			ledgerID:       digest(0x0d, 32),
			sequenceNumber: "1", eventKind: "0", payloadVersion: "1",
			wantConstraint: "ledger_record_event_kind_range",
		},
		{
			name:           "payload version zero",
			recordDigest:   digest(0x0e, 32),
			ledgerID:       digest(0x0f, 32),
			sequenceNumber: "1", eventKind: "1", payloadVersion: "0",
			wantConstraint: "ledger_record_payload_version_range",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// sequence_number, event_kind, and payload_version are embedded
			// as literals, matching TestSchemaRejectsOversizedRecordBytes
			// above, because the test needs to place out-of-domain values
			// like 0 without pgx's parameter type inference getting in the
			// way.
			statement := fmt.Sprintf(
				`INSERT INTO ledger_record (
					record_digest, ledger_id, sequence_number,
					previous_record_digest, event_kind, payload_version, record_bytes
				) VALUES ($1, $2, %s, $3, %s, %s, $4)`,
				testCase.sequenceNumber, testCase.eventKind, testCase.payloadVersion,
			)
			_, err := store.pool.Exec(
				context.Background(),
				statement,
				testCase.recordDigest, testCase.ledgerID, testCase.previousDigest, []byte{0xa0},
			)
			if err == nil {
				t.Fatal("insert succeeded, want constraint violation")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("error = %v, want a *pgconn.PgError", err)
			}
			if pgErr.ConstraintName != testCase.wantConstraint {
				t.Fatalf("ConstraintName = %q, want %q", pgErr.ConstraintName, testCase.wantConstraint)
			}
		})
	}
}
