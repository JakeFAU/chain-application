# 0005 — Event ordering, chain linkage, and concurrency

## Status

`accepted` — 2026-08-17.

Resolves open protocol decision 3 in `AGENTS.md`.

## Context

A decentralized attestation ledger requires a linear, unambiguous total ordering of events within each ledger instance. Replay determinism (Root Invariant 2) demands that replaying the ledger from genesis through event `N` always reconstructs the identical logical state regardless of when or where replay is executed.

When multiple writers concurrently attempt to append events to the same ledger:
1. The system must decide which event is admitted and reject or fail competing appends without corrupting chain continuity.
2. The server must never alter the contents, sequence number, or previous link of an event, because user and system cryptographic signatures cover those exact fields.
3. Replay of identical records must be idempotent.

## Decision

### 1. Chain Linkage and Ordering Invariants

1. **Strict Sequence Contiguity**: Sequence numbers are strictly monotonically increasing positive integers (`positive-uint64`, `1..18446744073709551615`).
2. **Genesis Event (`Sequence = 1`)**:
   - Must be the first event in a ledger.
   - `event_kind` must be `1` (`ledger_initialized`).
   - `previous_record_digest` must be `null` (`hasPreviousRecordDigest = false`).
   - `payload_bytes` must be the empty canonical CBOR map (`0xa0`).
3. **Successor Events (`Sequence = N + 1`)**:
   - For every record at sequence `N + 1` (where `N >= 1`), its `previous_record_digest` must exactly match the `record_digest` of the admitted record at sequence `N`.
   - `event_kind` cannot be `ledger_initialized` (genesis is unique per ledger).
   - Advancing beyond `18446744073709551615` (`math.MaxUint64`) is structurally rejected with `ErrInvalidSequence`.
4. **Single Ledger Scope**: All events in a chain must have the identical 32-byte `ledger_id`. Mixing ledger IDs in a chain fails with `ErrLedgerMismatch`.

### 2. Optimistic Concurrency and Admission Arbitration

1. **Storage Arbitration**: Admission ordering is arbitrated at the storage layer via the `(ledger_id, sequence_number)` unique constraint on `ledger_record`.
2. **Deterministic Conflict Classification**:
   As established in Decision 0002, records are inserted using:
   ```sql
   INSERT INTO ledger_record (...) VALUES (...)
   ON CONFLICT ON CONSTRAINT ledger_record_digest_pk DO NOTHING
   ```
   - **Exact Duplicate**: If 0 rows are affected, the exact record (matching `record_digest`) already exists in the store. This is handled as an idempotent duplicate append.
   - **Head Moved / Sequence Collision**: If an insertion raises SQLSTATE `23505` on `ledger_record_ledger_sequence_unique`, another record has already been admitted at that sequence position. The store returns `ErrChainHeadMoved`.
3. **No Server-Side Renumbering**: The server never mutates an incoming event or adjusts its sequence number and predecessor link. Because the event's signature commits to `sequence` and `previous_record_digest`, modifying them would invalidate cryptographic signatures.
4. **Client Concurrency Contract**: When an append fails with `ErrChainHeadMoved`, the client or writer must:
   - Read the current chain head from the ledger.
   - Re-evaluate its domain proposal against the new state.
   - Construct a new event with the updated `sequence` (`last_sequence + 1`) and `previous_record_digest` (`last_record_digest`).
   - Re-sign and re-submit the proposal.

### 3. Pure Deterministic Replay

1. **Replay State Machine**: The pure kernel in `internal/ledger/v1` implements chain state advancement via `Apply`:
   ```go
   func Apply(state ReplayState, record StructuralRecord) (ReplayState, error)
   ```
2. **Isolation from Ambient State**: Replay evaluation depends solely on the ordered record stream and explicit protocol rules. It does not access wall-clock time, environment variables, network resources, or external databases.

## Alternatives

**Pessimistic Table/Row Locking**. Locking the ledger table or head row during admission prevents concurrent conflicts but introduces severe throughput bottlenecks, long lock wait times, and deadlock risks across horizontally replicated Cloud Run instances. Rejected in favor of optimistic concurrency via unique constraints.

**Server-Side Auto-Sequencing**. Having the server assign the sequence number and hash link at insertion time. Rejected because attributable claims must be signed by the submitter or author committing to a specific parent event or point in history.

**DAG / Multi-Branch Forks**. Allowing concurrent branches to merge later via CRDTs or consensus. Rejected as unnecessary complexity for the initial authoritative single-sequencer admission ledger model.

## Consequences

- The ledger forms a cryptographically unbroken, tamper-evident hash chain.
- Concurrent appends are safely and deterministically arbitrated by PostgreSQL with zero risk of chain forks or sequence gaps.
- Replay from genesis deterministically reproduces the exact chain head and state.
