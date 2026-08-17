# 0001 — Ledger schema authoritative-versus-derived boundaries

## Status

`superseded` — 2026-08-17 — by
`0002-ledger-column-taxonomy-and-numeric-domain.md`.

This record was `accepted` on 2026-08-16. It is superseded because its numeric
domain is wrong: it permits zero for `sequence_number`, `event_kind`, and
`payload_version`, while the normative CDDL defines all three as
`positive-uint64` (`1..18446744073709551615`). The reasoning below, that
rejecting zero is a version-1 validation rule rather than a property of the
wire type, is incorrect on a checkable fact.

The body is preserved unchanged, including that error. Records are immutable
once accepted; the correction lives in 0002, not in edits here.

Open protocol decision 7 is resolved by 0002. Open decisions 1 through 3 remain
outstanding: the ledger protocol kernel merged without the decision records its
own policy requires, and that obligation is unchanged.

## Context

Admitted ledger records must be stored in PostgreSQL and retrieved as the exact
protocol bytes the kernel produced. A relational table can hold those bytes, but
it cannot enforce ordering or linkage without also holding fields decoded out of
them. Every such field is a second copy of information the bytes already carry,
and every second copy can drift.

The ledger's value is that signed history is immutable and verifiable. A schema
that lets a decoded column become the effective source of truth would undermine
that guarantee quietly, because queries would return the column rather than the
bytes.

`db/migrations/README.md` already states the governing distinction: migrations
evolve a disposable relational representation, while deterministic replay
reconstructs ledger-derived state from accepted, ordered ledger events.

## Decision

`record_bytes` is the only authoritative column in an authoritative ledger
table. It stores the canonical record CBOR byte for byte, with no re-encoding.

All other columns are derived. A derived column must be reproducible from
`record_bytes` by decoding alone, exists only to enforce structural invariants
or to locate records without decoding every blob, and is never a source of
truth. Where a derived column and the bytes disagree, the bytes are correct and
the row is corrupt.

The derived set is the minimum the constraints require, plus `event_kind` and
`payload_version` so that records this protocol version cannot semantically
validate remain findable, plus an operational `inserted_at` that carries no
ordering authority and is never replay input.

PostgreSQL enforces only invariants that hold for every record regardless of
event kind: digest uniqueness, sequence uniqueness within a ledger, the chain
link as a foreign key, fixed digest lengths, a bounded record size, unsigned
64-bit range bounds on the numeric protocol fields, and append-only behavior.
Version-specific semantic rules stay in Go.

The predecessor foreign key proves only that the referenced digest exists.
Same-ledger membership, sequence adjacency, and semantic validity of the link
are Go-level admission rules. The database enforces referential existence, not
chain validity.

Derived columns representing protocol `uint64` values use `numeric(20,0)`, not
`bigint`, so the stored derivation covers the protocol's full range. Because
`numeric(20,0)` also admits negatives and values above the unsigned 64-bit
maximum, each such column carries an explicit
`CHECK (column BETWEEN 0 AND 18446744073709551615)`. The floor is zero rather
than one: version 1 rejects sequence zero, but that is a validation rule rather
than a property of the wire type, and this layer holds only universal structure.

Constraint violations are identified by SQLSTATE together with the violated
constraint's name, never by SQLSTATE alone, because digest uniqueness and
sequence uniqueness both raise `23505`. All constraints are explicitly named in
migrations so the mapping is stable.

Authoritative ledger migrations do not provide automated destructive down
migrations. Their down block is present and raises an exception carrying this
policy. Rebuildable projections retain conventional reversible migrations.

## Alternatives

**Uniqueness constraints only, chain linkage in Go.** Simpler schema and simpler
migrations, but a defect in the writer can persist a forked or gapped chain that
the database accepts and that only replay later rejects. Rejected because the
database is the last line of defense for the one property the system exists to
provide.

**Bytes only, every invariant in Go.** Maximum freedom to change storage layout
later, at the cost of a store that can hold history replay will reject.
Rejected for the same reason.

**Fuller denormalization**, adding `admitted_at_unix_ms`, `signer_key_reference`,
and signature bytes as columns. Convenient for query paths that do not exist
yet, but it widens the derived surface, and every derived column is a drift risk
and a verification obligation. Rejected as premature.

**`bigint` for `uint64` protocol values.** Ergonomic in Go and cheaper to index,
but signed and therefore unable to represent the protocol's upper range.
Rejected because narrowing a protocol range through a storage choice resolves a
protocol question by implementation shortcut.

**Conventional `DROP TABLE` on down.** Standard practice and harmless while the
table is empty. Rejected because a migration file is durable policy rather than
a description of the moment it was written; the row count changes the cost of
destruction, not its semantic category.

**A silent no-op down block.** Rejected because dbmate would delete the
`schema_migrations` row while the table survived, so the next `dbmate up` would
fail against an existing table.

## Consequences

Storage cannot silently disagree with signed history. A corrupted derived column
is detectable by re-deriving from `record_bytes`, and reads re-validate stored
bytes so a corrupt row fails closed.

Future protocol versions can be stored and structurally inspected without this
version assigning them meaning, because the database enforces no version-specific
semantic rule.

Optimistic concurrency is settled for admission: the sequence uniqueness
constraint arbitrates concurrent appends, and a losing writer must rebuild and
re-sign, because the signature covers the sequence and the previous digest.

`numeric(20,0)` costs a wider index and gives up direct `int64` mapping in Go.

Rolling a ledger migration back requires an explicit operator procedure. This is
deliberate friction on an irreversible operation.

Replay is unaffected. Replay input remains the ordered record stream, and no
column introduced here is replay input.
