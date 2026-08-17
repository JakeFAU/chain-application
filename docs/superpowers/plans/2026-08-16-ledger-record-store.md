# Ledger Record Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist admitted ledger records in PostgreSQL as exact protocol bytes, with structural invariants enforced by the database.

**Architecture:** One append-only table, `ledger_record`, whose only authoritative column is `record_bytes`. Every other column is derived from those bytes and exists to let PostgreSQL enforce invariants true of all records regardless of event kind. A new `internal/ledgerstore` package owns all SQL, derives columns from validated record accessors, and maps constraint violations to typed errors by SQLSTATE plus constraint name.

**Tech Stack:** Go 1.26.6, PostgreSQL 18.4, dbmate 2.35.0, pgx v5, `internal/ledger/v1`.

## Global Constraints

- Source spec: `docs/superpowers/specs/2026-08-16-ledger-record-store-design.md`. Decision record: `docs/decisions/0001-ledger-schema-authoritative-derived-boundaries.md`.
- TDD is mandatory: every production change is preceded by a test that was run and observed to fail for the expected reason.
- `record_bytes` is the sole authoritative column. Derived columns are always computed from the validated record's own accessors, never from caller-supplied values.
- PostgreSQL enforces only invariants true of every record regardless of event kind. Version-specific semantic rules stay in Go.
- Every constraint is explicitly named in the migration. Error mapping keys on SQLSTATE **and** constraint name, never SQLSTATE alone.
- Numeric protocol columns are `numeric(20,0)` with `CHECK (col BETWEEN 0 AND 18446744073709551615)`. The floor is `0`, not `1`.
- Authoritative ledger migrations have no automated destructive down migration. The down block raises an exception carrying the policy.
- No ORM. Plain SQL only.
- Integration tests require real PostgreSQL and are skipped when `CHAIN_TEST_DATABASE_URL` is unset, so `make test` stays offline-clean.
- Run `make check` before every commit.

---

### Task 1: Export the protocol surface storage needs

The storage layer needs two things `internal/ledger/v1` does not currently expose: the maximum record size (for the schema pinning test) and a way to build a `ChainState` from an admitted record (because `ChainState` fields are unexported and only `ValidateChainConsistency` can produce one today).

`ChainStateFromRecord` is deliberately narrower than a general constructor: state can only be derived from a validated record, so it cannot describe a chain that was never accepted.

**Files:**
- Modify: `internal/ledger/v1/constants.go`
- Modify: `internal/ledger/v1/chain.go`
- Test: `internal/ledger/v1/chain_test.go`, `internal/ledger/v1/record_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ledgerv1.MaxRecordBytes` (untyped int constant, value 131200); `ledgerv1.ChainStateFromRecord(record StructuralRecord) ChainState`.

- [ ] **Step 1: Write the failing test for the exported size bound**

Append to `internal/ledger/v1/record_test.go`:

```go
func TestMaxRecordBytesBoundsValidation(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxRecordBytes+1)
	if _, err := ValidateRecordStructure(oversized); !errors.Is(err, ErrOversizedInput) {
		t.Fatalf("ValidateRecordStructure error = %v, want ErrOversizedInput", err)
	}
}
```

- [ ] **Step 2: Run it and verify it fails**

Run: `go test ./internal/ledger/v1/ -run TestMaxRecordBytesBoundsValidation`
Expected: FAIL to build with `undefined: MaxRecordBytes`.

- [ ] **Step 3: Export the constant**

In `internal/ledger/v1/constants.go`, immediately after the `maxLedgerRecordBytes` line inside the same `const` block, add:

```go
)

// MaxRecordBytes is the maximum size in bytes of a canonical ledger record.
// Storage layers need this bound to pin their own size constraints to the
// protocol rather than to a copied literal.
const MaxRecordBytes = maxLedgerRecordBytes
```

Note: close the existing `const (` block first, then declare `MaxRecordBytes` as a separate exported constant so the unexported block stays unexported.

- [ ] **Step 4: Run it and verify it passes**

Run: `go test ./internal/ledger/v1/ -run TestMaxRecordBytesBoundsValidation`
Expected: PASS

- [ ] **Step 5: Write the failing test for ChainStateFromRecord**

Append to `internal/ledger/v1/chain_test.go`:

```go
func TestChainStateFromRecordDerivesAdmittedState(t *testing.T) {
	t.Parallel()

	genesis := newGoldenGenesisRecord(t)
	state := ChainStateFromRecord(genesis)

	if !state.Initialized() {
		t.Fatal("Initialized() = false, want true")
	}
	ledgerID, ok := state.LedgerID()
	if !ok || ledgerID != genesis.Event().LedgerID() {
		t.Fatalf("LedgerID() = %x, %v, want %x, true", ledgerID, ok, genesis.Event().LedgerID())
	}
	if state.LastSequence() != genesis.Event().Sequence() {
		t.Fatalf("LastSequence() = %d, want %d", state.LastSequence(), genesis.Event().Sequence())
	}
	digest, ok := state.LastRecordDigest()
	if !ok || digest != genesis.RecordDigest() {
		t.Fatalf("LastRecordDigest() = %x, %v, want %x, true", digest, ok, genesis.RecordDigest())
	}
}

func TestChainStateFromRecordRejectsZeroRecord(t *testing.T) {
	t.Parallel()

	if state := ChainStateFromRecord(StructuralRecord{}); state.Initialized() {
		t.Fatal("Initialized() = true for a zero record, want false")
	}
}
```

- [ ] **Step 6: Run it and verify it fails**

Run: `go test ./internal/ledger/v1/ -run TestChainStateFromRecord`
Expected: FAIL to build with `undefined: ChainStateFromRecord`.

- [ ] **Step 7: Implement ChainStateFromRecord**

Append to `internal/ledger/v1/chain.go`:

```go
// ChainStateFromRecord derives the chain state produced by admitting record.
// A zero record yields uninitialized state, so callers cannot manufacture a
// chain position that no validated record established.
func ChainStateFromRecord(record StructuralRecord) ChainState {
	if len(record.bytes) == 0 {
		return ChainState{}
	}
	event := record.Event()
	return ChainState{
		initialized:      true,
		ledgerID:         event.LedgerID(),
		lastSequence:     event.Sequence(),
		lastRecordDigest: record.RecordDigest(),
	}
}
```

- [ ] **Step 8: Run the full package and verify it passes**

Run: `go test ./internal/ledger/v1/`
Expected: `ok`

- [ ] **Step 9: Run the gate and commit**

```bash
make check
git add internal/ledger/v1/
git commit -m "feat: expose record size bound and admitted chain state"
```

---

### Task 2: Create the migration and pin its size bound

**Files:**
- Create: `db/migrations/<timestamp>_create_ledger_record.sql` (generated by dbmate)
- Create: `internal/ledgerstore/schema_test.go`
- Modify: `db/migrations/README.md`

**Interfaces:**
- Consumes: `ledgerv1.MaxRecordBytes` from Task 1.
- Produces: table `ledger_record`; named constraints `ledger_record_digest_pk`, `ledger_record_ledger_sequence_unique`, `ledger_record_previous_fk`.

- [ ] **Step 1: Generate the migration file**

```bash
cd /Users/jacob/dev/personal/attribution_chain/chain-application
dbmate --migrations-dir db/migrations new create_ledger_record
```

Expected: prints the created path, e.g. `db/migrations/20260816120000_create_ledger_record.sql`.

- [ ] **Step 2: Write the migration**

Replace the generated file's contents entirely:

```sql
-- migrate:up
CREATE TABLE ledger_record (
    record_digest          bytea         NOT NULL,
    ledger_id              bytea         NOT NULL,
    sequence_number        numeric(20,0) NOT NULL,
    previous_record_digest bytea         NULL,
    event_kind             numeric(20,0) NOT NULL,
    payload_version        numeric(20,0) NOT NULL,
    record_bytes           bytea         NOT NULL,
    inserted_at            timestamptz   NOT NULL DEFAULT now(),

    CONSTRAINT ledger_record_digest_pk
        PRIMARY KEY (record_digest),
    CONSTRAINT ledger_record_ledger_sequence_unique
        UNIQUE (ledger_id, sequence_number),
    CONSTRAINT ledger_record_previous_fk
        FOREIGN KEY (previous_record_digest) REFERENCES ledger_record (record_digest),
    CONSTRAINT ledger_record_digest_len
        CHECK (octet_length(record_digest) = 32),
    CONSTRAINT ledger_record_ledger_id_len
        CHECK (octet_length(ledger_id) = 32),
    CONSTRAINT ledger_record_previous_len
        CHECK (previous_record_digest IS NULL OR octet_length(previous_record_digest) = 32),
    CONSTRAINT ledger_record_bytes_bound
        CHECK (octet_length(record_bytes) BETWEEN 1 AND 131200),
    CONSTRAINT ledger_record_sequence_range
        CHECK (sequence_number BETWEEN 0 AND 18446744073709551615),
    CONSTRAINT ledger_record_event_kind_range
        CHECK (event_kind BETWEEN 0 AND 18446744073709551615),
    CONSTRAINT ledger_record_payload_version_range
        CHECK (payload_version BETWEEN 0 AND 18446744073709551615)
);

CREATE FUNCTION ledger_record_append_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_record is append-only';
END;
$$;

CREATE TRIGGER ledger_record_no_mutate
    BEFORE UPDATE OR DELETE ON ledger_record
    FOR EACH ROW EXECUTE FUNCTION ledger_record_append_only();

CREATE TRIGGER ledger_record_no_truncate
    BEFORE TRUNCATE ON ledger_record
    FOR EACH STATEMENT EXECUTE FUNCTION ledger_record_append_only();

-- migrate:down
-- Policy: authoritative ledger tables have no automated destructive rollback.
-- ledger_record holds signed history that cannot be rebuilt from anywhere else.
-- Removing it is a Stop-and-Escalate operator decision with an explicit
-- preservation procedure, not a `dbmate rollback`. This block raises rather
-- than no-opping: a silent no-op would delete the schema_migrations row while
-- the table survived, so the next `dbmate up` would fail against an existing
-- table.
DO $$
BEGIN
    RAISE EXCEPTION 'ledger_record holds authoritative ledger history; rollback is an explicit operator decision';
END;
$$;
```

- [ ] **Step 3: Write the failing pinning test**

Create `internal/ledgerstore/schema_test.go`:

```go
package ledgerstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

var recordBytesBoundPattern = regexp.MustCompile(
	`octet_length\(record_bytes\) BETWEEN 1 AND (\d+)`,
)

// The migration must carry a SQL literal because SQL cannot read the Go
// constant. This test is the only thing keeping the two from drifting.
func TestMigrationRecordSizeBoundMatchesProtocol(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*_create_ledger_record.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d create_ledger_record migrations, want exactly 1", len(matches))
	}

	migration, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	found := recordBytesBoundPattern.FindSubmatch(migration)
	if found == nil {
		t.Fatalf("migration %s has no record_bytes size bound", matches[0])
	}
	bound, err := strconv.Atoi(string(found[1]))
	if err != nil {
		t.Fatalf("parse bound: %v", err)
	}
	if bound != ledgerv1.MaxRecordBytes {
		t.Fatalf("migration bound = %d, want ledgerv1.MaxRecordBytes (%d)", bound, ledgerv1.MaxRecordBytes)
	}
}
```

- [ ] **Step 4: Run it and verify it passes**

Run: `go test ./internal/ledgerstore/ -run TestMigrationRecordSizeBoundMatchesProtocol`
Expected: PASS.

This test is written after the migration, so it passes immediately. It is a pinning test, not a RED-driven one. Verify it can fail: temporarily change `131200` to `131201` in the migration, re-run, confirm FAIL, then change it back and confirm PASS.

- [ ] **Step 5: Apply the migration against real PostgreSQL**

```bash
cp .env.example .env.local
```

Edit `.env.local` and replace both occurrences of `replace-with-local-password` with the same local-only value.

```bash
make db-up
make migrate
make migrate-status
```

Expected: `migrate-status` lists the migration as applied.

- [ ] **Step 6: Verify the down block refuses**

```bash
dbmate --env-file .env.local --migrations-dir db/migrations rollback
```

Expected: fails with `ledger_record holds authoritative ledger history`. Then confirm the bookkeeping survived:

```bash
make migrate-status
```

Expected: still lists the migration as applied.

- [ ] **Step 7: Document the policy**

Replace the final paragraph of `db/migrations/README.md` with:

```markdown
Relational migration history is not ledger replay: migrations evolve a
disposable relational representation, while deterministic replay reconstructs
ledger-derived state from accepted, ordered ledger events.

Authoritative ledger migrations do not provide automated destructive down
migrations. Their down block is present and raises an exception carrying that
policy, so an operator sees deliberate policy rather than a missing half of the
file. Schema rollback that would remove or mutate ledger history is an explicit
Stop-and-Escalate operator decision. Rebuildable projections retain conventional
reversible migrations, including `DROP TABLE` on down, because the ledger can
reconstruct them.
```

- [ ] **Step 8: Run the gate and commit**

```bash
make check
git add db/migrations/ internal/ledgerstore/
git commit -m "feat: add append-only ledger_record schema"
```

---

### Task 3: Open the store and append a record

**Files:**
- Create: `internal/ledgerstore/store.go`
- Create: `internal/ledgerstore/testing_test.go`
- Create: `internal/ledgerstore/append_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `ledgerv1.StructuralRecord`, `ledgerv1.NewGenesisEvent`, `ledgerv1.NewRecord`.
- Produces: `ledgerstore.Store`; `Open(ctx context.Context, databaseURL string) (*Store, error)`; `(*Store).Close()`; `(*Store).Append(ctx context.Context, record ledgerv1.StructuralRecord) error`.

- [ ] **Step 1: Add pgx**

```bash
go get github.com/jackc/pgx/v5@latest
```

- [ ] **Step 2: Write the integration test harness**

Create `internal/ledgerstore/testing_test.go`:

```go
package ledgerstore

import (
	"context"
	"os"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

const testDatabaseURLEnvironment = "CHAIN_TEST_DATABASE_URL"

// openTestStore skips when no test database is configured, so the default
// offline `make test` run stays clean.
func openTestStore(t *testing.T) *Store {
	t.Helper()

	databaseURL, ok := os.LookupEnv(testDatabaseURLEnvironment)
	if !ok || databaseURL == "" {
		t.Skipf("%s is not set; skipping database integration test", testDatabaseURLEnvironment)
	}

	store, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// newTestLedgerID returns a distinct ledger ID per test so tests sharing one
// database never collide on the sequence uniqueness constraint.
func newTestLedgerID(t *testing.T) ledgerv1.LedgerID {
	t.Helper()

	var ledgerID ledgerv1.LedgerID
	copy(ledgerID[:], t.Name())
	ledgerID[len(ledgerID)-1] = byte(len(t.Name()))
	return ledgerID
}

func newTestGenesisRecord(t *testing.T, ledgerID ledgerv1.LedgerID) ledgerv1.StructuralRecord {
	t.Helper()

	event, err := ledgerv1.NewGenesisEvent(ledgerID, 1_755_000_000_000)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	record, err := ledgerv1.NewRecord(event, "test-signer-key-reference", make([]byte, 70))
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	return record
}
```

- [ ] **Step 3: Write the failing append test**

Create `internal/ledgerstore/append_test.go`:

```go
package ledgerstore

import (
	"context"
	"testing"
)

func TestAppendStoresGenesisRecord(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}
}
```

- [ ] **Step 4: Run it and verify it fails**

```bash
export CHAIN_TEST_DATABASE_URL="$(grep '^DATABASE_URL=' .env.local | cut -d= -f2-)"
go test ./internal/ledgerstore/ -run TestAppendStoresGenesisRecord
```

Expected: FAIL to build with `undefined: Store`.

- [ ] **Step 5: Implement the store**

Create `internal/ledgerstore/store.go`:

```go
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
```

- [ ] **Step 6: Run it and verify it passes**

```bash
go test ./internal/ledgerstore/ -run TestAppendStoresGenesisRecord
```

Expected: PASS.

- [ ] **Step 7: Verify the skip guard works offline**

```bash
env -u CHAIN_TEST_DATABASE_URL go test ./internal/ledgerstore/ -v -run TestAppendStoresGenesisRecord
```

Expected: `SKIP` with the message about `CHAIN_TEST_DATABASE_URL`.

- [ ] **Step 8: Run the gate and commit**

```bash
make check
git add go.mod go.sum internal/ledgerstore/
git commit -m "feat: append ledger records to PostgreSQL"
```

---

### Task 4: Map constraint violations to typed errors

**Files:**
- Create: `internal/ledgerstore/errors.go`
- Create: `internal/ledgerstore/errors_test.go`
- Modify: `internal/ledgerstore/store.go`

**Interfaces:**
- Consumes: `Store.Append` from Task 3.
- Produces: `ErrChainHeadMoved`, `ErrDuplicateRecord`, `ErrUnknownPredecessor`.

- [ ] **Step 1: Write the failing violation tests**

Create `internal/ledgerstore/errors_test.go`:

```go
package ledgerstore

import (
	"context"
	"errors"
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
```

Add this helper to `internal/ledgerstore/testing_test.go`. It builds a record whose event references an arbitrary predecessor, which version 1 semantics reject but the structural layer stores:

```go
func newTestContinuationRecord(
	t *testing.T,
	ledgerID ledgerv1.LedgerID,
	sequence uint64,
	previousRecordDigest ledgerv1.Digest,
) ledgerv1.StructuralRecord {
	t.Helper()

	encoded := encodeContinuationEventForTest(t, ledgerID, sequence, previousRecordDigest)
	event, err := ledgerv1.ValidateEventStructure(encoded)
	if err != nil {
		t.Fatalf("ValidateEventStructure: %v", err)
	}
	record, err := ledgerv1.NewRecord(event, "test-signer-key-reference", make([]byte, 70))
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	return record
}
```

`encodeContinuationEventForTest` must produce canonical CBOR for an event body with keys 0 through 7. Implement it in `testing_test.go` using the same `cbor.CoreDetEncOptions()` mode the protocol uses:

```go
func encodeContinuationEventForTest(
	t *testing.T,
	ledgerID ledgerv1.LedgerID,
	sequence uint64,
	previousRecordDigest ledgerv1.Digest,
) []byte {
	t.Helper()

	mode, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		t.Fatalf("EncMode: %v", err)
	}
	encoded, err := mode.Marshal(map[uint64]any{
		0: uint64(1),
		1: ledgerID[:],
		2: sequence,
		3: previousRecordDigest[:],
		4: uint64(1_755_000_000_000),
		5: uint64(1),
		6: uint64(1),
		7: []byte{0xa0},
	})
	if err != nil {
		t.Fatalf("marshal continuation event: %v", err)
	}
	return encoded
}
```

Add `"github.com/fxamacker/cbor/v2"` to the imports of `testing_test.go`.

- [ ] **Step 2: Run and verify they fail**

```bash
go test ./internal/ledgerstore/ -run 'TestAppendRejects'
```

Expected: FAIL to build with `undefined: ErrDuplicateRecord`.

- [ ] **Step 3: Implement the error mapping**

Create `internal/ledgerstore/errors.go`:

```go
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
```

- [ ] **Step 4: Route Append through the mapping**

In `internal/ledgerstore/store.go`, replace the error return at the end of `Append`:

```go
	if err != nil {
		if classified := classifyAppendError(err); !errors.Is(classified, err) {
			return classified
		}
		return fmt.Errorf("append ledger record: %w", err)
	}
	return nil
```

`store.go` already imports `errors` from Task 3.

- [ ] **Step 5: Write the failing distinct-record sequence-conflict test**

The duplicate test above trips the digest primary key. `ErrChainHeadMoved` requires two *different* records at the same `(ledger_id, sequence_number)`. Append to `internal/ledgerstore/errors_test.go`:

```go
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
```

- [ ] **Step 6: Write the failing oversized-record test**

The size bound cannot be reached through `ledgerv1`, which rejects oversized input first, so it is exercised with raw SQL. Append to `internal/ledgerstore/errors_test.go`:

```go
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
```

Add `"strings"` to the imports of `errors_test.go`.

- [ ] **Step 7: Run and verify they pass**

```bash
go test ./internal/ledgerstore/
```

Expected: `ok`. Steps 5 and 6 were written before their behavior existed only in the sense that the mapping did; re-run both and confirm `TestAppendRejectsSequenceConflictFromDistinctRecord` reports `ErrChainHeadMoved` and not `ErrDuplicateRecord`, which is the exact mistake the constraint-name mapping exists to prevent.

- [ ] **Step 8: Run the gate and commit**

```bash
make check
git add internal/ledgerstore/
git commit -m "feat: map constraint violations to typed admission errors"
```

---

### Task 5: Read chain state and records

**Files:**
- Create: `internal/ledgerstore/read.go`
- Create: `internal/ledgerstore/read_test.go`

**Interfaces:**
- Consumes: `Store` from Task 3; `ledgerv1.ChainStateFromRecord` from Task 1.
- Produces: `(*Store).Head(ctx context.Context, ledgerID ledgerv1.LedgerID) (ledgerv1.ChainState, error)`; `(*Store).Record(ctx context.Context, digest ledgerv1.Digest) (ledgerv1.StructuralRecord, error)`; `ErrRecordNotFound`.

- [ ] **Step 1: Write the failing read tests**

Create `internal/ledgerstore/read_test.go`:

```go
package ledgerstore

import (
	"bytes"
	"context"
	"errors"
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
```

Add `"strconv"` to the imports of `read_test.go`.

Note on the corruption test: it proves the validation gate rejects corrupted bytes, which is the property `Record` depends on. It deliberately does not mutate the stored row, because the append-only trigger forbids that — and that trigger is itself the stronger guarantee, verified in Task 6.

- [ ] **Step 2: Run and verify they fail**

```bash
go test ./internal/ledgerstore/ -run 'TestHead|TestRecord'
```

Expected: FAIL to build with `store.Head undefined`.

- [ ] **Step 3: Implement the reads**

Create `internal/ledgerstore/read.go`:

```go
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
```

- [ ] **Step 4: Run and verify they pass**

```bash
go test ./internal/ledgerstore/
```

Expected: `ok`.

- [ ] **Step 5: Run the gate and commit**

```bash
make check
git add internal/ledgerstore/
git commit -m "feat: read chain head and stored records"
```

---

### Task 6: Prove the append-only and concurrency guarantees

**Files:**
- Create: `internal/ledgerstore/guarantees_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3 through 5.
- Produces: nothing.

- [ ] **Step 1: Write the guarantee tests**

Create `internal/ledgerstore/guarantees_test.go`:

```go
package ledgerstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// The unique sequence constraint is the sole arbiter of concurrent appends.
// Exactly one writer wins; the loser must rebuild and re-sign.
func TestConcurrentAppendsYieldOneWinner(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)

	const writers = 2
	results := make(chan error, writers)
	var start sync.WaitGroup
	start.Add(1)

	for range writers {
		go func() {
			record := newTestGenesisRecord(t, ledgerID)
			start.Wait()
			results <- store.Append(context.Background(), record)
		}()
	}
	start.Done()

	var succeeded, rejected int
	for range writers {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDuplicateRecord), errors.Is(err, ErrChainHeadMoved):
			rejected++
		default:
			t.Fatalf("unexpected Append error: %v", err)
		}
	}
	if succeeded != 1 || rejected != writers-1 {
		t.Fatalf("succeeded = %d, rejected = %d, want 1 and %d", succeeded, rejected, writers-1)
	}
}

func TestLedgerRecordRejectsMutation(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	digest := record.RecordDigest()
	statements := map[string]string{
		"update":   `UPDATE ledger_record SET ledger_id = ledger_id WHERE record_digest = $1`,
		"delete":   `DELETE FROM ledger_record WHERE record_digest = $1`,
		"truncate": `TRUNCATE ledger_record`,
	}

	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			var err error
			if name == "truncate" {
				_, err = store.pool.Exec(context.Background(), statement)
			} else {
				_, err = store.pool.Exec(context.Background(), statement, digest[:])
			}
			if err == nil {
				t.Fatalf("%s succeeded, want append-only rejection", name)
			}
			if !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("%s error = %v, want append-only rejection", name, err)
			}
		})
	}
}
```

Note: both writers in the concurrency test build the same genesis record, so the digest primary key and the sequence constraint can each be the arbiter depending on timing. Accepting either rejection is correct; accepting a success from both is not.

- [ ] **Step 2: Run and verify they pass**

```bash
go test ./internal/ledgerstore/ -run 'TestConcurrent|TestLedgerRecordRejects' -v
```

Expected: PASS, with the `truncate` subtest confirming the statement-level trigger fires.

- [ ] **Step 3: Run the gate and commit**

```bash
make check
git add internal/ledgerstore/
git commit -m "test: prove append-only and concurrent admission guarantees"
```

---

### Task 7: Run integration tests in CI

**Files:**
- Modify: `.github/workflows/ci.yaml`
- Modify: `README.md`

**Interfaces:**
- Consumes: `CHAIN_TEST_DATABASE_URL` from Task 3.
- Produces: nothing.

- [ ] **Step 1: Add the PostgreSQL service**

In `.github/workflows/ci.yaml`, add a `services` block to the `go` job, immediately after `runs-on: ubuntu-latest`:

```yaml
    services:
      postgres:
        image: postgres:18.4-bookworm@sha256:882236b897e39051d2368c5ccc6cda944904723506b2dfc97f2a8f5bc9afa382
        env:
          POSTGRES_DB: attribution_chain
          POSTGRES_USER: attribution_chain
          POSTGRES_PASSWORD: ci-local-password
        ports:
          - 5432:5432
        options: >-
          --health-cmd "pg_isready -U attribution_chain -d attribution_chain"
          --health-interval 5s
          --health-timeout 5s
          --health-retries 5
```

The image digest must match `compose.yaml` so CI and local development run the same PostgreSQL build.

- [ ] **Step 2: Pin dbmate the way the repo pins its other tools**

dbmate is currently an ambient developer tool, but `AGENTS.md` requires CI to use committed pins rather than ambient installations. Follow the existing `.staticcheck-version` pattern.

Create `.dbmate-version` containing exactly:

```
v2.35.0
```

In `Makefile`, add after the `GOVULNCHECK_VERSION` line:

```make
DBMATE_VERSION := $(shell tr -d '[:space:]' < .dbmate-version)
```

Change the `DBMATE` variable to use the pinned binary:

```make
DBMATE := $(BIN_DIR)/dbmate --env-file $(ENV_FILE) --migrations-dir $(MIGRATIONS_DIR)
```

Add an install rule beside the other two:

```make
$(BIN_DIR)/dbmate: .dbmate-version | $(BIN_DIR)
	GOBIN=$(BIN_DIR) $(GO) install \
		github.com/amacneil/dbmate/v2@$(DBMATE_VERSION)
```

Add it to the `tools` target so `make setup` installs it:

```make
tools: $(BIN_DIR)/staticcheck $(BIN_DIR)/govulncheck $(BIN_DIR)/dbmate
```

Make the migration targets depend on it by adding `$(BIN_DIR)/dbmate` to the prerequisites of `migrate` and `migrate-status`:

```make
migrate: db-config $(BIN_DIR)/dbmate
migrate-status: db-config $(BIN_DIR)/dbmate
```

Verify locally:

```bash
make tools && ./bin/dbmate --version
```

Expected: `v2.35.0`.

- [ ] **Step 3: Apply migrations and run integration tests in CI**

In the same job, after the `Run repository checks` step, add:

```yaml
      - name: Apply migrations
        env:
          DATABASE_URL: postgres://attribution_chain:ci-local-password@127.0.0.1:5432/attribution_chain?sslmode=disable
        run: ./bin/dbmate --migrations-dir db/migrations up

      - name: Run database integration tests
        env:
          CHAIN_TEST_DATABASE_URL: postgres://attribution_chain:ci-local-password@127.0.0.1:5432/attribution_chain?sslmode=disable
        run: go test ./internal/ledgerstore/ -count=1
```

`./bin/dbmate` exists because the `Install pinned tools` step already runs `make tools`.

- [ ] **Step 4: Document local integration testing**

Append to the `## Local PostgreSQL and migrations` section of `README.md`:

```markdown
Database integration tests are skipped unless `CHAIN_TEST_DATABASE_URL` is set,
so the default `make test` run needs no database. To run them against the local
container:

```bash
make db-up && make migrate
CHAIN_TEST_DATABASE_URL="$(grep '^DATABASE_URL=' .env.local | cut -d= -f2-)" \
    go test ./internal/ledgerstore/ -count=1
```
```

- [ ] **Step 5: Verify the workflow parses**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yaml')); print('workflow parses')"
```

Expected: `workflow parses`.

- [ ] **Step 6: Run the gate and commit**

```bash
make check
git add .github/workflows/ci.yaml README.md .dbmate-version Makefile
git commit -m "ci: run ledger store integration tests against PostgreSQL"
```

- [ ] **Step 7: Push and confirm CI is green**

```bash
git push -u origin agent/ledger-record-store
gh pr create --base main --title "feat: add the ledger record store" --body "Implements docs/superpowers/specs/2026-08-16-ledger-record-store-design.md and decision record 0001."
gh pr checks --watch
```

Expected: all checks pass, including the new integration test step.
