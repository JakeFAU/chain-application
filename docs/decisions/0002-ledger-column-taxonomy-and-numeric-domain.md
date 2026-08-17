# 0002 — Ledger column taxonomy, numeric domain, and conflict determinism

## Status

`accepted` — 2026-08-17.

Supersedes `0001-ledger-schema-authoritative-derived-boundaries.md` in full and
carries forward the parts of it that remain correct. Resolves open protocol
decision 7 in `AGENTS.md`.

Open decisions 1 through 3 remain outstanding: the ledger protocol kernel merged
without the decision records its own policy requires, and that obligation is
unchanged by this record.

## Context

Record 0001 established that `record_bytes` is the sole authoritative column and
that PostgreSQL enforces only structure universal to every record. Review of the
shipped schema against the normative protocol found three defects in how that
principle was carried out, and one place where the prose claimed more than the
mechanism delivers.

**The numeric domain was wrong.** 0001 chose `CHECK (column BETWEEN 0 AND
18446744073709551615)` for `sequence_number`, `event_kind`, and
`payload_version`, reasoning that rejecting zero was a version-1 Go validation
rule rather than a property of the wire type. That is false. `ledger.cddl`
defines `positive-uint64 = 1..18446744073709551615` and declares all three
fields with it, and `validateEventBodyWire` rejects zero for all three inside
`ValidateEventStructure`, which runs for every event regardless of kind.
Positivity is universal structural validity. The schema was therefore looser
than the protocol's own structural domain — the storage layer silently widening
the protocol, which is the exact failure 0001 existed to prevent.

**Duplicate and head-conflict were not deterministic.** An exact replay of a
stored record violates both the digest primary key and the
`(ledger_id, sequence_number)` uniqueness constraint. 0001 required
classification by SQLSTATE plus constraint name, which is necessary but not
sufficient: with two constraints violated simultaneously, the typed error
depends on which one PostgreSQL reports first. That is an implementation detail
of index checking order, not a contract.

**The column taxonomy had two categories and needed three.** 0001 said every
column other than `record_bytes` is derived and reproducible from it by
decoding, then correctly described `inserted_at` as server wall-clock
operational metadata carrying no protocol authority. Both statements cannot be
true of the same column.

**The read-corruption claim was broader than the mechanism.** 0001 said reads
re-validate stored bytes so a corrupt row fails closed. Re-validating
`record_bytes` detects corruption of the authoritative bytes. It does not detect
every disagreement between valid bytes and a drifted derived column: `Head`
locates and orders rows by derived columns, and valid bytes cannot reveal that a
row was excluded from consideration because its derived sequence drifted.

## Decision

### Column taxonomy

Three categories, not two.

1. **Authoritative protocol data** — `record_bytes`. The canonical record CBOR,
   byte for byte, never re-encoded.
2. **Derived protocol columns** — `record_digest`, `ledger_id`,
   `sequence_number`, `previous_record_digest`, `event_kind`,
   `payload_version`. Each must be reproducible from `record_bytes` by decoding
   alone. None is a source of truth. Where a derived column and the bytes
   disagree, the bytes are correct and the row is corrupt.
3. **Operational metadata** — `inserted_at`. Server wall-clock time, not
   reproducible from `record_bytes`, never protocol input, never replay input,
   and never an ordering authority.

### Numeric domain

Derived protocol columns representing protocol `positive-uint64` values —
`sequence_number`, `event_kind`, `payload_version` — are `numeric(20,0)` with

```sql
CHECK (column BETWEEN 1 AND 18446744073709551615)
```

The floor is one, matching `positive-uint64` in the normative CDDL. `numeric`
is used rather than `bigint` because the protocol range exceeds signed 64-bit;
the explicit bounds are required because `numeric(20,0)` also admits negatives
and values above the maximum.

A future protocol version that widens this domain is a protocol change,
resolved by protocol review and a reviewed migration at that time. The storage
layer does not pre-authorize values the current protocol structurally forbids.

### Conflict determinism

Insertion handles digest conflict explicitly rather than inferring it from which
constraint PostgreSQL reported:

```sql
INSERT INTO ledger_record (...) VALUES (...)
ON CONFLICT ON CONSTRAINT ledger_record_digest_pk DO NOTHING
```

- Zero rows affected means the exact record is already stored: duplicate.
- A surviving `23505` on `ledger_record_ledger_sequence_unique` means a
  different record already holds that position: chain head moved.

Classification remains keyed on SQLSTATE together with constraint name. The
`ON CONFLICT` clause makes the duplicate case deterministic instead of
dependent on constraint-checking order.

### Integrity guarantee, stated to match the mechanism

Writes derive protocol columns from validated bytes, so a derived column cannot
disagree with the authority at insertion. Append-only enforcement prevents
application-level drift afterwards. Reads independently re-validate
`record_bytes`, so corruption of the authoritative bytes fails closed, and
`Record` additionally confirms the re-derived digest equals the digest
requested, making the lookup self-checking. Arbitrary privileged mutation of
derived columns — by a superuser bypassing the triggers — is outside the
integrity guarantee.

### Admission boundary

`Store.Append` persists a record that has already passed the caller's applicable
admission checks. It does not itself establish semantic or cryptographic
admissibility. The store is the authoritative home of admitted history; it is
not the thing that decides what may be admitted.

### Carried forward unchanged from 0001

PostgreSQL enforces only invariants true of every record regardless of event
kind: digest uniqueness, sequence uniqueness within a ledger, the chain link as
a foreign key, fixed digest lengths, a bounded record size, the numeric bounds
above, and append-only behavior. Version-specific semantic rules stay in Go.

The predecessor foreign key proves only that the referenced digest exists.
Same-ledger membership, sequence adjacency, and link validity are Go-level
admission rules.

Optimistic concurrency: the sequence uniqueness constraint arbitrates concurrent
appends, and a losing writer must rebuild and re-sign, because the signature
covers the sequence and the previous digest. The server never renumbers.

Authoritative ledger migrations provide no automated destructive down
migration. Their down block raises an exception carrying that policy.
Rebuildable projections retain conventional reversible migrations.

## Alternatives

**Amend 0001 in place.** Rejected. `docs/decisions/README.md` states records are
immutable once accepted and that changing a decision means writing a superseding
record. 0001 was one day old and wrong on a checkable fact, which made amendment
tempting — which is exactly when the rule matters. An immutability rule that
yields whenever honoring it is inconvenient is not a rule. 0001 keeps its
incorrect reasoning verbatim so the error stays visible.

**Keep the zero floor and change the CDDL to `uint64`.** Rejected. That would
resolve a protocol question to suit a storage convenience, in the direction of
weakening a structural guarantee, and would invalidate existing validation.

**Keep classification on constraint name alone.** Rejected. It is correct for
singly-violated constraints and undefined for the exact-replay case, which is
the one a client is most likely to produce by retrying.

**Widen the guarantee prose instead of narrowing it.** Rejected. Claiming
detection of arbitrary derived-column corruption would require re-deriving every
column on every read, and would still not cover rows excluded from a query by a
drifted column.

## Consequences

Storage no longer admits values the protocol structurally forbids. A migration
tightens the three range constraints; all existing rows already satisfy them,
because the protocol validator rejected zero before any row was written.

An exact replay deterministically reports duplicate, and a genuine position
conflict deterministically reports chain head moved, independent of PostgreSQL's
constraint-checking order. `ON CONFLICT ... DO NOTHING` makes replay idempotent
at the storage layer.

The integrity guarantee is now stated at the strength it actually holds, which
means privileged mutation is explicitly named as out of scope rather than
implied to be covered.

Every schema constraint named as an enforced invariant is covered by a test that
attempts the violation, including the length and numeric bounds that previously
had none.

Replay is unaffected. No column introduced or changed here is replay input.
