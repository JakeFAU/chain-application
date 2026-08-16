// Package ledgerstore persists admitted ledger records as exact protocol
// bytes. record_bytes is the only authoritative column; every other column is
// derived from it so PostgreSQL can enforce structural invariants.
package ledgerstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/jackc/pgx/v5/pgxpool"
)

const insertRecordStatement = `
INSERT INTO ledger_record (
    record_digest,
    ledger_id,
    sequence_number,
    previous_record_digest,
    event_kind,
    payload_version,
    record_bytes
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`

// Store owns all SQL for admitted ledger records.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL and verifies the connection.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (store *Store) Close() {
	store.pool.Close()
}

// Append stores one validated record. Every derived column is taken from the
// record's own accessors, so the derived set cannot disagree with the bytes.
func (store *Store) Append(ctx context.Context, record ledgerv1.StructuralRecord) error {
	event := record.Event()
	recordDigest := record.RecordDigest()
	ledgerID := event.LedgerID()

	var previousDigest []byte
	if previous, ok := event.PreviousRecordDigest(); ok {
		previousDigest = previous[:]
	}

	_, err := store.pool.Exec(
		ctx,
		insertRecordStatement,
		recordDigest[:],
		ledgerID[:],
		unsignedNumeric(event.Sequence()),
		previousDigest,
		unsignedNumeric(uint64(event.Kind())),
		unsignedNumeric(event.PayloadVersion()),
		record.Bytes(),
	)
	if err != nil {
		if classified := classifyAppendError(err); !errors.Is(classified, err) {
			return classified
		}
		return fmt.Errorf("append ledger record: %w", err)
	}
	return nil
}

// unsignedNumeric renders a uint64 for a numeric(20,0) column. The value is
// sent as its decimal text because the protocol range exceeds int64 and pgx has
// no native uint64 type; PostgreSQL parses the text into numeric exactly. These
// columns are never read back, so no decode path is needed.
func unsignedNumeric(value uint64) string {
	return strconv.FormatUint(value, 10)
}
