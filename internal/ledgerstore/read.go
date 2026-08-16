package ledgerstore

import (
	"context"
	"errors"
	"fmt"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/jackc/pgx/v5"
)

// ErrRecordNotFound means no record with the requested digest is stored.
var ErrRecordNotFound = errors.New("ledger store record not found")

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
	return ledgerv1.ChainStateFromRecord(record), nil
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
	return record, nil
}
