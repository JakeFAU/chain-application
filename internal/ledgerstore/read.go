package ledgerstore

import (
	"context"
	"errors"
	"fmt"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/jackc/pgx/v5"
)

const selectHeadStatement = `
SELECT record_bytes
FROM ledger_record
WHERE ledger_id = $1
ORDER BY sequence_number DESC
LIMIT 1
`

const selectRecordStatement = `
SELECT record_bytes
FROM ledger_record
WHERE record_digest = $1
`

const selectRecordsFromSequenceStatement = `
SELECT record_bytes
FROM ledger_record
WHERE ledger_id = $1 AND sequence_number >= $2
ORDER BY sequence_number ASC
LIMIT $3
`

// Head returns the chain state established by the highest-sequence record in
// the ledger, or uninitialized state when the ledger holds no records.
func (store *Store) Head(
	ctx context.Context,
	ledgerID ledgerv1.LedgerID,
) (ledgerv1.ChainState, error) {
	var recordBytes []byte
	err := store.pool.QueryRow(ctx, selectHeadStatement, ledgerID[:]).Scan(&recordBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerv1.ChainState{}, nil
	}
	if err != nil {
		return ledgerv1.ChainState{}, fmt.Errorf("read chain head: %w", err)
	}

	record, err := ledgerv1.ValidateRecordStructure(recordBytes)
	if err != nil {
		return ledgerv1.ChainState{}, fmt.Errorf("validate stored head record: %w", err)
	}
	state := ledgerv1.ChainStateFromRecord(record)
	if stateLedgerID, ok := state.LedgerID(); !ok || stateLedgerID != ledgerID {
		return ledgerv1.ChainState{}, fmt.Errorf(
			"stored head record for ledger %x does not re-derive to that ledger_id column", ledgerID,
		)
	}
	return state, nil
}

// Record returns one stored record by digest. Stored bytes are re-validated so
// a corrupted row fails closed rather than returning a record that was never
// valid.
func (store *Store) Record(
	ctx context.Context,
	digest ledgerv1.Digest,
) (ledgerv1.StructuralRecord, error) {
	var recordBytes []byte
	err := store.pool.QueryRow(ctx, selectRecordStatement, digest[:]).Scan(&recordBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerv1.StructuralRecord{}, ErrRecordNotFound
	}
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("read ledger record: %w", err)
	}

	record, err := ledgerv1.ValidateRecordStructure(recordBytes)
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("validate stored record: %w", err)
	}
	if record.RecordDigest() != digest {
		return ledgerv1.StructuralRecord{}, fmt.Errorf(
			"stored record %x does not re-derive to its record_digest column", digest,
		)
	}
	return record, nil
}

// ScanRecords reads up to limit records for a ledger starting from sequenceNumber in ascending order.
func (store *Store) ScanRecords(
	ctx context.Context,
	ledgerID ledgerv1.LedgerID,
	fromSequence uint64,
	limit int,
) ([]ledgerv1.StructuralRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := store.pool.Query(ctx, selectRecordsFromSequenceStatement, ledgerID[:], unsignedNumeric(fromSequence), limit)
	if err != nil {
		return nil, fmt.Errorf("scan ledger records: %w", err)
	}
	defer rows.Close()

	var records []ledgerv1.StructuralRecord
	for rows.Next() {
		var recordBytes []byte
		if err := rows.Scan(&recordBytes); err != nil {
			return nil, fmt.Errorf("scan record row: %w", err)
		}
		record, err := ledgerv1.ValidateRecordStructure(recordBytes)
		if err != nil {
			return nil, fmt.Errorf("validate scanned record: %w", err)
		}
		if recordLedgerID := record.Event().LedgerID(); recordLedgerID != ledgerID {
			return nil, fmt.Errorf("scanned record for ledger %x does not re-derive to that ledger_id", ledgerID)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scanned records: %w", err)
	}
	return records, nil
}
