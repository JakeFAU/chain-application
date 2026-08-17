# 0004 — Durable event and record envelope versioning

## Status

`accepted` — 2026-08-17.

Resolves open protocol decision 2 in `AGENTS.md`.

## Context

The Attribution Chain ledger stores events intended to remain verifiable over decades. Protocol schemas will inevitably evolve as new attestation types, cryptographic primitives, and admission metadata are introduced.

To ensure long-term replayability and forward compatibility:
1. Historical events must remain interpretable by future software without ambient or mutable schema repositories.
2. Nodes must be able to verify chain continuity and cryptographic integrity (structural validation) even for event types whose domain semantics they do not implement.
3. System admission metadata, cryptographic signatures, and domain claim payloads must be cleanly layered with clear envelope boundaries.

## Decision

### 1. Three-Tier Envelope Architecture

The protocol structures data into three concentric envelopes defined in `protocol/ledger/v1/ledger.cddl`:

```
+-----------------------------------------------------------------------+
| ledger-record-v1 (Storage / Outer Envelope)                           |
|   - 0: record_version (1)                                             |
|   - 1: record_body_bytes                                              |
|   - 2: record_digest_algorithm ("sha-256")                            |
|   - 3: record_digest (SHA-256 over record_body_bytes)                 |
|                                                                       |
|   +---------------------------------------------------------------+   |
|   | record-body-v1 (Admission & System Signature)                 |   |
|   |   - 0: record_version (1)                                     |   |
|   |   - 1: event_body_bytes                                       |   |
|   |   - 2: event_digest_algorithm ("sha-256")                     |   |
|   |   - 3: event_digest (SHA-256 over event_body_bytes)           |   |
|   |   - 4: signer_key_reference (1..512 printable ASCII)          |   |
|   |   - 5: signature_algorithm ("ecdsa-p256-sha256")              |   |
|   |   - 6: signature_encoding ("asn1-der")                        |   |
|   |   - 7: signature_bytes (8..72 bytes)                          |   |
|   |                                                               |   |
|   |   +-------------------------------------------------------+   |   |
|   |   | event-body-v1 (Domain Fact / Claim Commitment)        |   |   |
|   |   |   - 0: protocol_version (1)                           |   |   |
|   |   |   - 1: ledger_id (32-byte identifier)                 |   |   |
|   |   |   - 2: sequence (positive-uint64, 1..)                |   |   |
|   |   |   - 3: previous_record_digest (null or 32 bytes)      |   |   |
|   |   |   - 4: admitted_at_unix_ms (server timestamp)         |   |   |
|   |   |   - 5: event_kind (positive-uint64, 1..)              |   |   |
|   |   |   - 6: payload_version (positive-uint64, 1..)         |   |   |
|   |   |   - 7: payload_bytes (1..65536 canonical CBOR map)   |   |   |
|   |   +-------------------------------------------------------+   |   |
|   +---------------------------------------------------------------+   |
+-----------------------------------------------------------------------+
```

### 2. Envelope Roles and Layering

- **`event-body-v1`**: Contains the attributable claim or domain event, ordered within a specific ledger instance. It links to the preceding record via `previous_record_digest`.
- **`record-body-v1`**: Attaches system admission proof to the event body. The system signer signs over the canonical `record-body-v1` or commits to `event_digest` with recorded key identifier, algorithm, and DER signature bytes.
- **`ledger-record-v1`**: The outermost envelope stored verbatim in the database (`record_bytes`). It binds the `record_body_bytes` to its computed `record_digest`.

### 3. Explicit Size Bounds

Each layer enforces explicit, strict size ceilings to prevent resource exhaustion and buffer overflows:
- `payload_bytes`: maximum 65,536 bytes (64 KiB).
- `event_body_bytes`: maximum 98,304 bytes (96 KiB).
- `record_body_bytes`: maximum 131,072 bytes (128 KiB).
- `ledger-record-v1` total size (`MaxRecordBytes`): maximum 131,200 bytes.
- `signer_key_reference`: 1 to 512 printable ASCII characters (`0x20` to `0x7e`).
- `signature_bytes`: 8 to 72 bytes (accommodating ASN.1 DER-encoded P-256 ECDSA signatures).

### 4. Structural vs. Semantic Validation

The protocol enforces a strict separation between structural validation and semantic validation:

1. **Structural Validation** (`ValidateEventStructure`, `ValidateRecordStructure`):
   - Validates CBOR canonical encoding, exact key sets, field ranges, and internal digest integrity (`event_digest == digest(event_body_bytes)`, `record_digest == digest(record_body_bytes)`).
   - Operates purely on envelope structure without evaluating payload business logic.
   - Accepts unknown `event_kind` and `payload_version` values as long as they satisfy the wire schema and bounds. This allows nodes and tools to verify hash linkage, store records, and advance chain state without knowing future domain semantics.

2. **Semantic Validation** (`ValidateEventSemantics`):
   - Validates domain-level invariants for specific event kinds.
   - Version 1 supports semantic validation only for `event_kind = 1` (`ledger_initialized`), which requires `sequence = 1`, `previous_record_digest = null`, `payload_version = 1`, and `payload_bytes = 0xa0` (`{}`).
   - Unknown event kinds fail semantic validation with `ErrUnsupportedEvent`, halting semantic replay safely.

### 5. Protocol Versioning Rules

- **Envelope Schema Changes**: Changes to the outer envelope, key definitions, or required fields require incrementing `protocol_version` or `record_version`.
- **Domain Event Additions**: New event types are introduced by allocating a new `event_kind` and corresponding `payload_version`. Existing nodes continue to structurally parse and order these records even before receiving software updates to semantically evaluate them.

## Alternatives

**Single Flat Envelope**. Storing event fields, admission metadata, and signature in a single flat map. Rejected because admission metadata and signatures must commit to immutable event bytes; flattening them creates ambiguity over what was signed and prevents envelope re-wrapping or structural delegation.

**Loose JSON Schemas / External Schemas**. Storing arbitrary payloads validated against remote schema URLs. Rejected because external schema dependencies break deterministic offline replay, introduce network attack vectors, and permit unversioned schema drift.

## Consequences

- The immutable ledger format is strictly versioned and self-contained.
- Structural verification is decoupled from semantic interpretation, enabling robust indexing, replication, and storage without breaking when new event kinds are added.
- All envelope and payload sizes have hard protocol bounds, simplifying memory management in Go and storage constraints in PostgreSQL.
