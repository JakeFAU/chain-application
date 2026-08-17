# Ledger Record Store Design

## Status

This document defines the approved design for the first durable PostgreSQL
storage slice for admitted ledger records. It is a storage design, not an
implementation plan. Implementation begins only after this specification is
reviewed and a separate test-driven implementation plan is approved.

This design resolves open protocol decision 7, the ledger database schema's
authoritative-versus-derived boundaries, recorded in
`docs/decisions/0001-ledger-schema-authoritative-derived-boundaries.md`. Open
decisions 1 through 3 remain outstanding and are not addressed here.

## Goal

Store and retrieve the exact protocol bytes produced by `internal/ledger/v1`
without redefining them, and enforce in PostgreSQL the structural invariants
that hold for every record regardless of event kind.

The store is the authoritative home of admitted ledger history. It is not a
projection, and nothing in this slice derives application state from it.

## Out of scope

- Cryptographic signature verification. `SignatureStatus` continues to report
  `unverified`.
- Projections, derived tables, graph indexes, or search. The store exposes only
  the primitives admission and verification need: append, read chain state, and
  read one record by digest.
- HTTP endpoints, OpenAPI changes, or generated clients.
- A replay or rebuild command.
- Physical partitioning, sharding, or index tuning.
- Cloud SQL, deployment, or any GCP mutation.

## Authoritative and Derived Columns

`record_bytes` is the sole authoritative column. It holds the canonical
`ledger_record` CBOR exactly as `ValidateRecordStructure` accepted it, byte for
byte, with no re-encoding.

Every other column is derived and must be reproducible from `record_bytes` by
decoding alone. Derived columns exist to let PostgreSQL enforce structural
invariants and to let tooling locate records without decoding every blob. They
are never a source of truth. Where a derived column and the bytes disagree, the
bytes are correct and the row is corrupt.

The derived set is deliberately minimal:

| Column | Purpose |
| --- | --- |
| `record_digest` | Primary key; the identity every link targets. |
| `ledger_id` | Scopes sequence uniqueness to one ledger. |
| `sequence_number` | Logical order within a ledger. |
| `previous_record_digest` | The chain link, enforced as a foreign key. |
| `event_kind` | Locates records this version cannot semantically validate. |
| `payload_version` | Same, paired with `event_kind`. |
| `inserted_at` | Operational only; never protocol input and never replay input. |

`event_kind` and `payload_version` are included because the protocol kernel
requires that tooling preserve and structurally inspect future records without
assigning them version 1 meaning. Finding those records must not require
decoding every stored blob.

`inserted_at` records server wall-clock time for operations. It is not the
protocol's `admitted_at_unix_ms`, carries no ordering authority, and is never an
input to replay.

## Structural Invariants in PostgreSQL

PostgreSQL enforces only what is true of every record regardless of event kind.
Semantic rules, including which event kinds are admissible in this version,
remain in Go.

- `record_digest` is the primary key, so a digest appears at most once.
- `(ledger_id, sequence_number)` is unique, so one ledger cannot hold two
  records at the same logical position.
- `previous_record_digest` references `record_digest`, so a record cannot be
  stored before its predecessor. Genesis stores `NULL`.
- Length checks pin `record_digest`, `ledger_id`, and `previous_record_digest`
  to 32 bytes and bound `record_bytes` by the protocol's maximum record size.
- Range checks pin `sequence_number`, `event_kind`, and `payload_version` to
  `BETWEEN 0 AND 18446744073709551615`.
- Triggers reject `UPDATE`, `DELETE`, and `TRUNCATE`.

The predecessor foreign key proves only that the referenced digest exists.
Same-ledger membership, sequence adjacency, and semantic validity of the link
remain Go-level admission rules. The database enforces referential existence;
the kernel decides what constitutes a valid chain edge. Encoding adjacency
relationally would smuggle protocol semantics into the persistence layer.

The range checks use zero as the floor rather than one. Version 1 rejects
sequence zero in `validateEventBodyWire`, but that is a Go validation rule
rather than a property of the wire type, which is a plain `uint64`. A floor of
one would therefore encode a version rule in a layer that is meant to hold only
universal structure, so Go keeps owning that rejection and the stored bound is
deliberately looser than version 1 admits.

The rule that sequence 1 implies no previous digest is deliberately not
enforced. That is a version 1 semantic rule rather than a universal structural
one, and encoding it would constrain future protocol versions that this storage
layer is required to preserve.

Trigger-based append-only enforcement is chosen over role grants so the
guarantee lives in the migration rather than in separately provisioned roles. It
defends against application defects, not against a database superuser, who can
drop the trigger.

The record-size bound is the one value this schema duplicates from Go: the
migration must carry a SQL literal while `internal/ledger/v1` holds the
constant, and SQL cannot read the Go value. Implementation pins the two together
with a test that asserts the constraint's bound equals the protocol constant, so
the pair cannot drift silently.

## Numeric Width

`sequence_number`, `event_kind`, and `payload_version` are `numeric(20,0)`
rather than `bigint`. The protocol defines all three as unsigned 64-bit values
and `bigint` is signed, so `bigint` cannot represent the upper half of the
range.

Because these columns are derived, a `bigint` column would be a lossy
derivation, and narrowing the protocol's value range through a storage choice
would resolve a protocol question by implementation rather than by decision
record. The cost is a wider index and no direct `int64` mapping in Go.

`numeric(20,0)` is wider than the domain it stores: it also admits negative
values and values above `18446744073709551615`. Unsignedness and the upper bound
are universal structural properties of these three protocol fields, so the
schema states them explicitly rather than relying on the column type. Each of
the three carries `CHECK (column BETWEEN 0 AND 18446744073709551615)`. Without
those checks the schema would guarantee less than this document claims.

## Migration Rollback Policy

Authoritative ledger migrations do not provide automated destructive down
migrations. Schema rollback that would remove or mutate ledger history is an
explicit Stop-and-Escalate operator decision requiring whatever preservation
procedure is appropriate at that time.

Rebuildable projections retain conventional reversible migrations, including
`DROP TABLE` on down, because the ledger can reconstruct them.

The down block for an authoritative ledger migration is present and explicit
rather than omitted. It raises an exception carrying the policy, so an operator
sees deliberate policy rather than a missing half of the file. A silent no-op is
rejected: dbmate would remove the `schema_migrations` row while the table
survived, leaving the next `dbmate up` to fail against an existing table.

The current row count does not change the semantic category of the operation. A
table that is empty today is still the source of truth for history that cannot
be rebuilt from anywhere else.

## Concurrency and Admission

Admission validates against the current chain head in Go and then inserts. The
`(ledger_id, sequence_number)` unique constraint is the sole arbiter of
concurrent appends. No lock is held across application validation.

A losing writer receives a typed chain-head-moved error. The server does not
retry with a bumped sequence, because the signature covers `sequence` and
`previous_record_digest`; any server-side renumbering would invalidate the
signature. Rebuilding and re-signing against the new head is the client's
responsibility.

This leaves a real window between reading the head and inserting. That window is
the accepted consequence of optimistic concurrency, and the unique constraint
makes its outcome safe rather than silent.

## Go Package Boundary

`internal/ledgerstore` owns all SQL for admitted records. It depends on
`internal/ledger/v1` for types and validation and on pgx v5 for the driver. It
uses plain SQL with no ORM.

The package exposes appending a validated record, reading a ledger's current
chain state, and reading one record by digest.

Appending derives every column from the validated record's own accessors and
never from caller-supplied values, so the derived set cannot disagree with
`record_bytes` by construction. Reading re-validates the stored bytes through
`ValidateRecordStructure`, so a corrupted row fails closed instead of returning
a record that was never valid.

Constraint violations map to typed errors by SQLSTATE **and constraint name**,
never by SQLSTATE alone. Both the primary key on `record_digest` and the
`(ledger_id, sequence_number)` uniqueness constraint raise `23505`, so the code
alone cannot distinguish them. Every constraint is therefore explicitly named in
the migration rather than left to PostgreSQL's generated names, and the mapping
matches on the name.

A `23505` from the `(ledger_id, sequence_number)` constraint becomes
chain-head-moved. A `23503` from the predecessor foreign key becomes
unknown-predecessor. Other uniqueness violations, including a duplicate
`record_digest`, remain distinct admission errors. Collapsing them would report
a replayed identical record as a chain-head conflict, which is both wrong and
operationally misleading, and the duplicate-digest integration test exists to
catch exactly that mistake.

pgx v5 is an approved material dependency for this slice. It handles `bytea` and
`numeric` natively and exposes the SQLSTATE codes the error mapping requires.

## Testing

The design places integrity in database constraints, so tests exercise a real
PostgreSQL instance. A mock or an in-memory substitute would verify none of the
behavior this design is built on.

Integration tests cover each enforced invariant by attempting the violation and
asserting the typed error: duplicate digest, duplicate sequence within a ledger,
a link to an absent predecessor, an oversized record, and attempted `UPDATE`,
`DELETE`, and `TRUNCATE`. Concurrency is covered by two writers racing the same
sequence, asserting exactly one success and one chain-head-moved error.

Round-trip tests assert that stored and retrieved bytes are identical and that
every derived column re-derives from the retrieved bytes.

Integration tests are guarded so the default offline test run stays clean.
Continuous integration adds a PostgreSQL service container, which is a workflow
change and not a Go dependency.
