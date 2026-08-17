# 0003 — Canonical serialization and digest construction for ledger events

## Status

`accepted` — 2026-08-17.

Resolves open protocol decision 1 in `AGENTS.md`.

## Context

The Attribution Chain ledger is an immutable, append-only record of cryptographically attributable events. Replay determinism (Root Invariant 2) and cryptographic verification across replay (Root Invariant 4) require that binary representations and cryptographic digests are 100% reproducible across programming languages, CPU architectures, operating systems, and time.

Ambiguities in serialization encodings create malleability vulnerabilities: if multiple distinct byte sequences can represent the same logical value, an adversary or divergent encoder can alter a record's cryptographic digest without altering its decoded representation, or invalidate signatures across systems.

Furthermore, preimages hashed in different contexts must not be interchangeable. If an event body digest and a record body digest use undifferentiated hashing, a valid event preimage could be substituted where a record preimage is expected, creating cross-domain confusion.

## Decision

### 1. Language-Neutral Wire Format Authority

The normative authority for all ledger wire representations is the CDDL specification in `protocol/ledger/v1/ledger.cddl`, encoded strictly using Concise Binary Object Representation (CBOR, RFC 8949) under the Core Deterministic Encoding Profile (dCBOR).

### 2. Deterministic CBOR Encoding Profile

All protocol serialization and deserialization enforce the following deterministic rules:

1. **Shortest Integer Encoding**: Integers must be encoded using the minimum number of bytes (1, 2, 4, or 8 bytes) required to represent their value.
2. **Integer Map Keys in Ascending Order**: All map keys are unsigned integers and must appear in strictly ascending numerical order (`0, 1, 2, ...`).
3. **Exact Key Matching**: Every map in an envelope has an exact, fixed set of required integer keys. Missing keys, unexpected keys, or extra fields are rejected as schema violations.
4. **Duplicate Map Keys Forbidden**: Duplicate map keys are rejected immediately upon decoding (`DupMapKeyEnforcedAPF`).
5. **Indefinite Lengths Forbidden**: Indefinite-length byte strings, text strings, arrays, or maps are strictly forbidden. All lengths must be explicit and definite.
6. **CBOR Tags Forbidden**: CBOR semantic tags (Major type 6) are forbidden in core protocol structures.
7. **Strict UTF-8**: Text strings must be valid UTF-8 and use case-sensitive matching.
8. **Bounded Resource Limits**: Decoders enforce strict limits to prevent algorithmic complexity attacks:
   - Maximum nesting depth: 4 levels (`maxProtocolNestingLevels`).
   - Maximum array elements: 16 (`maxProtocolArrayElements`).
   - Maximum map pairs: 16 (`maxProtocolMapPairs`).
9. **Round-Trip Re-Encoding Verification**: Decoders must re-encode decoded structures using the canonical encoding mode and verify that the re-encoded bytes match the input bytes byte-for-byte (`bytes.Equal`). Any discrepancy fails closed with `ErrNonConformingCBOR`.

### 3. Digest Construction and Domain Separation

1. **Digest Algorithm**: The protocol digest algorithm is fixed to SHA-256 (32 bytes). Algorithm selection is pinned in protocol version 1 and is not runtime-configurable.
2. **Domain Separation**: All protocol digests use domain separation to prevent preimage reuse across different protocol stages:
   ```
   Digest = SHA-256( DomainString || 0x00 || BodyBytes )
   ```
   - The domain string and body bytes are separated by a single null byte (`0x00`).
   - Event body digest domain: `"attribution-chain:event-digest:v1"`
   - Record body digest domain: `"attribution-chain:record-digest:v1"`

## Alternatives

**JSON / Canonical JSON (RFC 8785 / JCS)**. Text-based formats require Base64 encoding for cryptographic keys, signatures, and binary digests, increasing wire overhead. Number representations in JSON are prone to IEEE 754 float conversion issues across platforms and cannot natively represent unsigned 64-bit integers up to `18446744073709551615`. Rejected.

**Protocol Buffers (protobuf)**. Protobuf does not guarantee deterministic binary serialization across compiler versions, language runtimes, or field ordering rules without custom, non-standard canonicalization layers. Furthermore, proto3 handles default/zero values by omitting them from the wire, creating ambiguity. Rejected.

**Raw Concatenation / Custom Binary Format**. Ad-hoc binary packing avoids external format dependencies but requires custom tooling, specification of padding/endianness, and extensive manual framing code that is error-prone to maintain across multiple client SDK languages (Go, Python, TypeScript). Rejected.

**Undifferentiated Hashing (`SHA-256(BodyBytes)`)**. Hashing raw serialized bodies without domain separation allows preimages from one layer (e.g. an event body) to be presented as another (e.g. a record body), opening cross-protocol collision attacks. Rejected.

## Consequences

- The wire format is fully deterministic, unambiguous, and mathematically verifiable across independent client and server implementations.
- Decoders fail closed against non-canonical CBOR, prohibiting malleable encodings from entering the ledger.
- Digest computation is isolated by domain strings, preventing cross-type preimage confusion.
- The `internal/ledger/v1` package serves as the reference Go implementation, validated against language-neutral CDDL and committed golden test vectors (`protocol/ledger/v1/testdata`).
