package ledgerstore

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Typed admission failures. Callers distinguish these; SQLSTATE alone cannot,
// because digest uniqueness and sequence uniqueness both raise 23505.
var (
	// ErrChainHeadMoved means a concurrent writer took this sequence. The
	// signature covers the sequence and previous digest, so the record cannot
	// be renumbered server-side; the client must rebuild and re-sign.
	ErrChainHeadMoved = errors.New("ledger store chain head moved")

	// ErrDuplicateRecord means this exact record digest is already stored.
	ErrDuplicateRecord = errors.New("ledger store duplicate record")

	// ErrUnknownPredecessor means the referenced previous record is absent.
	ErrUnknownPredecessor = errors.New("ledger store unknown predecessor")
)

// Typed lookup failure. Unlike the admission sentinels above, this does not
// come from a PostgreSQL constraint violation; it means a read found no
// matching row at all.
//
// ErrRecordNotFound means no record with the requested digest is stored.
var ErrRecordNotFound = errors.New("ledger store record not found")

const (
	uniqueViolationCode     = "23505"
	foreignKeyViolationCode = "23503"

	digestPrimaryKeyConstraint = "ledger_record_digest_pk"
	ledgerSequenceConstraint   = "ledger_record_ledger_sequence_unique"
	previousRecordConstraint   = "ledger_record_previous_fk"
)

// classifyAppendError maps a PostgreSQL error to a typed admission failure
// using SQLSTATE together with the violated constraint name. Matching on
// SQLSTATE alone would report a replayed identical record as a head conflict.
func classifyAppendError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == ledgerSequenceConstraint:
		return ErrChainHeadMoved
	case pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == digestPrimaryKeyConstraint:
		return ErrDuplicateRecord
	case pgErr.Code == foreignKeyViolationCode && pgErr.ConstraintName == previousRecordConstraint:
		return ErrUnknownPredecessor
	default:
		return err
	}
}
