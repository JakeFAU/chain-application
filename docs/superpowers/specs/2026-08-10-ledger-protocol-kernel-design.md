# Ledger Protocol Kernel Design

## Status

This document defines the approved design for the first durable Attribution
Chain ledger protocol slice. It is a protocol design, not an implementation
plan. Implementation begins only after this written specification is reviewed
and a separate test-driven implementation plan is approved.

The protocol is versioned independently from the HTTP API. OpenAPI remains the
authority for HTTP requests and responses; this document defines durable ledger
bytes and replay rules.

## Goal

Create a small, pure Go protocol kernel that can construct and validate the
first ledger record without choosing PostgreSQL storage, a KMS client, an HTTP
contract, or application-domain events.

The kernel establishes the durable foundations that later work must be able to
trust:

- a closed, versioned schema expressed in CDDL and normative prose;
- a strict deterministic-CBOR profile;
- stable event and admitted-record digest constructions;
- an explicit genesis record;
- exact logical ledger ordering and predecessor-link rules;
- bounded parsing of untrusted bytes;
- structural chain consistency validation; and
- stable, language-neutral conformance vectors.

The result proves attributable bytes, integrity, ordering, and structural
validity. It does not prove that claim content is true.

## Scope

### In scope

- The version 1 deterministic-CBOR profile.
- CDDL for the version 1 event body, admitted record, and genesis payload.
- Typed, immutable protocol values with explicit byte ownership.
- Construction of a genesis event from explicit deterministic inputs.
- Event-digest and record-digest calculation.
- Strict parsing and validation at every encoded boundary.
- Structural validation of ledger identity, sequence, and predecessor links.
- Semantic replay of the only supported event, `ledger_initialized/v1`.
- Exact golden vectors, negative conformance tests, and fuzz targets.
- One bounded third-party CBOR implementation behind a package-local boundary.

### Out of scope

- PostgreSQL schema, migrations, transactions, locking, or physical
  partitioning.
- KMS clients, network calls, key creation, key discovery, or key rotation.
- Producing signatures or holding private keys.
- Cryptographically verifying ECDSA signatures.
- Strict ASN.1 DER parsing, scalar validation, or a low-S admission rule.
- Subject, identity, endorsement, revocation, claim, artifact, or scoring
  events.
- HTTP endpoints, OpenAPI changes, generated clients, or JSON as an
  authoritative representation.
- Live admission, deployment, Cloud Run, Cloud SQL, or any GCP mutation.
- Projections, graph indexes, search, or other derived state.
- Runtime-selected hash, signature, encoding, or protocol algorithms.

Later cryptographic work must complete signature parsing, key discovery, and
verification before any record is admitted live. Later persistence work must
store and retrieve these exact protocol bytes without redefining them.

## Authority and Package Boundaries

Language-neutral protocol artifacts live under `protocol/ledger/v1`. The CDDL
schema and this normative prose are the protocol authority. CDDL describes the
data model, while this document supplies constraints that CDDL alone cannot
express, including deterministic encoding, digest preimages, cross-field
rules, size limits, and replay behavior.

CDDL and normative prose are jointly normative. A contradiction between them is
a specification defect and must be resolved by protocol review; an
implementation must not treat either as taking precedence over the other, and
must not resolve the contradiction by choosing the reading its layer finds
convenient. The structural domains the CDDL declares — notably
`positive-uint64` for `sequence`, `event_kind`, and `payload_version` — are
protocol structure, not implementation detail, and no downstream layer may
widen or narrow them.

The Go implementation lives under `internal/ledger/v1` with package name
`ledgerv1`. Go types implement the protocol but do not define it. No generated
Go code is produced from CDDL in this slice.

The package is pure and deterministic:

- no clock reads;
- no randomness;
- no environment reads;
- no network, filesystem, database, KMS, logging, or telemetry calls;
- no package-level mutable state; and
- no behavior selected through ambient or runtime configuration.

Callers provide every value that affects protocol output. Input byte slices
are treated as untrusted, and returned values do not expose mutable internal
buffers. The package copies bytes at trust and ownership boundaries where a
caller could otherwise mutate validated state.

The kernel does not generate the ledger ID. The later initialization use case
must generate it once with a cryptographically secure random source and pass
the resulting 32 bytes to the pure genesis constructor. The admitted timestamp
is likewise an explicit input.

## Terminology and Layering

The protocol has three nested layers:

1. `payload_bytes` is the exact deterministic-CBOR encoding selected by
   `(event_kind, payload_version)`.
2. `event_body_bytes` is the exact deterministic-CBOR event body that commits
   the payload to one logical ledger identity and position.
3. `record_body_bytes` is the exact deterministic-CBOR admitted envelope that
   commits the event digest and one exact signature representation. The outer
   `ledger_record` carries the record digest.

The ledger links record digests, not event digests. This distinction preserves
two different identities:

- `event_digest` identifies one exact ordered ledger event body, not the
  underlying claim or content independently of admission.
- `record_digest` identifies the exact admitted representation, including its
  signer reference and signature bytes.

The signature signs the event digest, but the event digest is not the
signature's identity. The same event body always has the same event digest and
may have multiple independently valid signature records. A particular linear
ledger position nevertheless commits to one exact record body; admitting
additional attestations to that event would require a separately designed
protocol event or collection.

Replacing a signer key reference, signature algorithm identifier, signature
encoding identifier, or signature bytes changes the record digest. Because the
next event links the previous record digest, changing any admitted signature
evidence invalidates every later link.

## Version 1 Deterministic-CBOR Profile

All encoded values in this protocol use CBOR as defined by RFC 8949 and satisfy
its Core Deterministic Encoding Requirements. The profile further restricts
the CBOR data model.

Every version 1 value must satisfy all of these rules:

- Every item has a definite length.
- Integers and length arguments use their shortest permitted encodings.
- Map keys are ordered by the bytewise lexicographic order of their
  deterministic encodings.
- Maps use only the unsigned-integer keys declared by their closed schema.
- Duplicate, unknown, and missing map keys are rejected.
- Unsigned integers are used only where the schema permits them. Negative
  integers are rejected.
- Text strings contain valid UTF-8 and are preserved byte-for-byte. No Unicode
  normalization, case folding, whitespace transformation, or locale-sensitive
  operation is performed.
- Floating-point values, CBOR tags, indefinite-length values, `undefined`, and
  unassigned or schema-unsupported simple values are rejected.
- Optional values are omitted unless a schema explicitly assigns meaning to
  `null`. A decoder never treats omitted and `null` as interchangeable.
- Arrays are permitted only when a payload schema declares an order with
  semantic meaning. Version 1 genesis uses no arrays.
- Each byte string documented as embedded CBOR contains exactly one complete
  deterministic-CBOR data item with no leading or trailing bytes.
- Every externally supplied encoded container is bounded before decoding.
  Embedded byte strings are length-checked immediately after their enclosing
  field is structurally decoded and before they are independently decoded,
  copied into durable validated state, or hashed.
- An accepted encoded value re-encodes to the exact input bytes. Parsing never
  normalizes non-conforming input and never rehashes a normalized substitute.

JSON is not an input to any digest and is never authoritative. A future JSON or
debug adapter may render admitted Unix milliseconds as UTC RFC 3339. A value
outside that adapter's representable range produces a derived-view error; it
does not change or reinterpret the durable event.

## Normative CDDL

The committed schema will express the following model. The implementation plan
may split these definitions across files without changing their meaning.

```cddl
ledger-record-v1 = {
  0: 1,                              ; record_version
  1: bstr .size (1..131072),         ; record_body_bytes
  2: "sha-256",                      ; record_digest_algorithm
  3: bstr .size 32                   ; record_digest
}

record-body-v1 = {
  0: 1,                              ; record_version
  1: bstr .size (1..98304),          ; event_body_bytes
  2: "sha-256",                      ; event_digest_algorithm
  3: bstr .size 32,                  ; event_digest
  4: tstr .size (1..512),            ; signer_key_reference
  5: "ecdsa-p256-sha256",            ; signature_algorithm
  6: "asn1-der",                     ; signature_encoding
  7: bstr .size (8..72)              ; signature_bytes
}

event-body-v1 = {
  0: 1,                              ; protocol_version
  1: bstr .size 32,                  ; ledger_id
  2: positive-uint64,                ; sequence
  3: null / bstr .size 32,           ; previous_record_digest
  4: uint64,                         ; admitted_at_unix_ms
  5: positive-uint64,                ; event_kind
  6: positive-uint64,                ; payload_version
  7: bstr .size (1..65536)           ; payload_bytes
}

ledger-initialized-payload-v1 = {}

uint64 = 0..18446744073709551615
positive-uint64 = 1..18446744073709551615
```

The Go validator enforces the CDDL numeric bounds independently.

The algorithm strings are exact lowercase ASCII byte sequences. The signer key
reference is 1 through 512 bytes of printable ASCII, U+0020 through U+007E. A
later KMS boundary will require the full immutable key-version identifier; an
alias that can move between versions is insufficient.

The closed empty genesis payload has exactly one valid encoding:

```text
a0
```

## Field Semantics

### Event body

| Key | Field | Version 1 rule |
| --- | --- | --- |
| `0` | `protocol_version` | Unsigned integer exactly `1`. |
| `1` | `ledger_id` | Exactly 32 bytes. Constant for the lifetime and all replicas or forks of one logical ledger. |
| `2` | `sequence` | Unsigned 64-bit integer starting at `1`. The only authoritative event ordering. |
| `3` | `previous_record_digest` | `null` only for genesis; otherwise exactly 32 bytes. |
| `4` | `admitted_at_unix_ms` | Unsigned 64-bit Unix milliseconds supplied at admission. It is provenance, not ordering. |
| `5` | `event_kind` | Unsigned 64-bit identifier. `1` means `ledger_initialized`. `0` is invalid. |
| `6` | `payload_version` | Unsigned 64-bit payload-schema version. Genesis requires `1`; `0` is invalid. |
| `7` | `payload_bytes` | Exact separately validated deterministic-CBOR item, at most 65,536 bytes. |

Timestamps need not be monotonic and may move backward. Replay never compares
timestamps to establish order and never reads the wall clock.

### Record body

| Key | Field | Version 1 rule |
| --- | --- | --- |
| `0` | `record_version` | Unsigned integer exactly `1`. |
| `1` | `event_body_bytes` | Exact validated event-body bytes, at most 98,304 bytes. |
| `2` | `event_digest_algorithm` | Text exactly `sha-256`. |
| `3` | `event_digest` | Exactly 32 bytes and equal to the recomputed digest. |
| `4` | `signer_key_reference` | 1 through 512 printable ASCII bytes; later verification requires a full immutable key-version identifier. |
| `5` | `signature_algorithm` | Text exactly `ecdsa-p256-sha256`. |
| `6` | `signature_encoding` | Text exactly `asn1-der`. |
| `7` | `signature_bytes` | 8 through 72 bytes, structurally opaque in this slice. |

The 8-through-72-byte limit is only a defensive representation bound for bytes
declared to be an ASN.1 DER-encoded P-256 signature. Passing it does not
establish that the bytes contain a DER object, valid P-256 scalars, a value that
satisfies a future low-S rule, or a cryptographically valid signature.

### Complete ledger record

| Key | Field | Version 1 rule |
| --- | --- | --- |
| `0` | `record_version` | Unsigned integer exactly `1` and equal to the inner record version. |
| `1` | `record_body_bytes` | Exact validated record-body bytes, at most 131,072 bytes. |
| `2` | `record_digest_algorithm` | Text exactly `sha-256`. |
| `3` | `record_digest` | Exactly 32 bytes and equal to the recomputed digest. |

The complete encoded ledger record is limited to 131,200 bytes. All limits are
version 1 protocol rules, not runtime configuration.

The nested container limits are defensive upper bounds and need not be
reachable by a schema-valid version 1 value. Schema-specific validation can
and does impose tighter effective limits.

Large documents, media, or data are not embedded in the ledger. A future event
may attest to a content digest and bounded retrieval URL, such as a bucket
object URL. The digest supplies integrity; the URL is only a retrieval hint and
its availability or ownership is not implied by the ledger.

## Event Digest

The event-body schema and deterministic profile are validated before the event
is accepted. Non-conforming CBOR is rejected rather than normalized.

```text
DOMAIN_EVENT_DIGEST_V1 =
    ASCII("attribution-chain:event-digest:v1")

event_digest =
    SHA-256(
        DOMAIN_EVENT_DIGEST_V1 ||
        0x00 ||
        event_body_bytes
    )
```

`DOMAIN_EVENT_DIGEST_V1` is exactly the displayed ASCII byte sequence. The
separator is exactly one zero byte. `event_body_bytes` is the exact already
validated deterministic-CBOR encoding, not a Go value or a re-encoding of
untrusted bytes. The digest is exactly 32 bytes.

The event digest algorithm identifier is exactly `sha-256`. A version 1
validator rejects every other identifier. Algorithm selection is a protocol
version property and never runtime input. A future digest construction requires
a new protocol and domain definition, such as version 2; version 1 is never
extended by negotiating another algorithm.

## Signature Commitment

The version 1 signature operation consumes the event digest directly:

```text
signature =
    ECDSA_P256_SignDigest(
        private_key,
        event_digest
    )
```

`event_digest` is the exact 32-byte ECDSA message representative supplied to
the signing operation. The protocol must not hash it again before ECDSA
signing. For Google Cloud KMS `EC_SIGN_P256_SHA256`, the later signer supplies
these 32 bytes through the SHA-256 digest field. The signature returned by KMS
is preserved exactly in its ASN.1 DER encoding.

The version 1 metadata identifiers are fixed:

```text
signature_algorithm = "ecdsa-p256-sha256"
signature_encoding  = "asn1-der"
```

This kernel records and commits the declared metadata and opaque signature
bytes. It does not claim the signature is well formed or valid. The next crypto
slice must define and test strict DER consumption, integer/scalar bounds,
low-S policy, immutable key-version resolution, public-key parsing, and actual
ECDSA verification.

## Record Digest

The record digest commits one exact admitted signature representation without
making that representation part of the event's identity.

```text
record_body = {
    record_version,
    event_body_bytes,
    event_digest_algorithm,
    event_digest,
    signer_key_reference,
    signature_algorithm,
    signature_encoding,
    signature_bytes
}

record_body_bytes = deterministic_cbor(record_body)

DOMAIN_RECORD_DIGEST_V1 =
    ASCII("attribution-chain:record-digest:v1")

record_digest =
    SHA-256(
        DOMAIN_RECORD_DIGEST_V1 ||
        0x00 ||
        record_body_bytes
    )
```

The `record_body` pseudocode names fields for readability; the actual bytes are
the integer-keyed closed map defined above. The record digest is not inside the
record body and therefore does not recursively commit itself. The outer ledger
record carries the exact record-body bytes, digest algorithm identifier, and
record digest.

The record digest algorithm identifier is exactly `sha-256` and follows the
same versioning rule as the event digest algorithm.

## Genesis

Every logical ledger begins with exactly one explicit genesis event:

```text
protocol_version       = 1
sequence               = 1
previous_record_digest = null
event_kind             = 1  ; ledger_initialized
payload_version        = 1
payload_bytes          = h'a0'
```

`ledger_id` is a newly generated 32-byte value supplied explicitly to the
kernel. Replicas, backups, replays, and forks of that ledger retain it. An
independent ledger receives a different value. The ledger ID is independent of
database identifiers and deployment environments.

There is no zero digest sentinel and no synthetic predecessor record. `null`
has this meaning only at sequence 1. Every sequence greater than one requires
an exact 32-byte previous record digest.

The genesis payload is deliberately empty. The event establishes the ledger's
identity and initial position without pretending that a subject, endorsement,
operator, deployment, or cryptographic key is part of genesis semantics.

Genesis construction is staged:

1. Validate the explicit ledger ID and admitted time.
2. Encode and validate the exact empty genesis payload.
3. Construct and encode the event body.
4. Compute the event digest.
5. Obtain a signature in a later signing boundary.
6. Accept the supplied fixed metadata, key-version reference, and signature
   bytes.
7. Construct the record body, compute the record digest, and construct the
   outer ledger record.

This kernel implements stages 1 through 4 and 6 through 7. It never performs
stage 5 and never owns a signer.

## Parsing and Validation Flow

Validation proceeds outside-in. Passing an early phase does not imply later
phases have passed.

1. Reject a complete input larger than 131,200 bytes before decoding it.
2. Strictly validate the outer map, its exact keys and types, record version,
   digest identifier, and digest length, and immediately reject an embedded
   record body larger than 131,072 bytes.
3. Deterministically re-encode the validated outer value and require exact
   byte equality with the supplied complete record.
4. Strictly validate the exact record-body bytes, their closed map, fixed
   identifiers, field bounds, and the 98,304-byte event-body limit.
5. Deterministically re-encode the validated record body and require exact byte
   equality, then recompute its record digest and require an exact match.
6. Strictly validate the exact event-body bytes, their closed map, field types,
   versions, field bounds, and the 65,536-byte payload limit.
7. Deterministically re-encode the validated event body and require exact byte
   equality, then recompute its event digest and require an exact match.
8. Select the payload schema using `(event_kind, payload_version)`, strictly
   validate the exact embedded payload bytes, and require byte equality after
   deterministic re-encoding.
9. Apply supported event-specific semantic rules, including the exact genesis
   payload and genesis cross-field requirements.

Steps 1 through 7 yield a structurally validated record independently of event
semantics. Chain consistency validation may consume that result. Steps 8 and 9
establish supported semantics in a separate operation. For an unknown event,
that operation returns the typed unsupported-event error while the prior
structural result remains usable.

No phase rewrites bytes for a later phase. Digest checks always use the exact
validated bytes carried by the containing structure.

An unknown event kind or payload version can still be parsed far enough to
check the outer encoding, both digests, and physical chain link. Semantic
validation and replay then stop with a typed unsupported-event error. This
allows tooling to preserve and structurally inspect future records without
silently assigning them version 1 meaning.

## Logical Ordering and Chain Consistency Validation

Version 1 defines one global logical ledger sequence and one record-digest
chain. Physical PostgreSQL partitioning, indexing, or sharding may later change
storage layout but never changes the logical order or replay input.

Chain consistency state contains only:

- whether genesis has been observed;
- the 32-byte ledger ID;
- the last sequence; and
- the last record digest.

For genesis, chain consistency validation requires:

- no prior state;
- sequence exactly 1;
- `previous_record_digest` exactly `null`;
- event kind exactly `ledger_initialized`;
- payload version exactly 1; and
- the exact empty-map payload.

For every continuation, chain consistency validation requires:

- initialized state;
- the exact same ledger ID;
- sequence exactly `last_sequence + 1`, with no gap and no unsigned overflow;
- a non-null predecessor digest exactly equal to `last_record_digest`; and
- no second `ledger_initialized` event.

Sequence is the only ordering mechanism. Timestamps cannot repair, break ties,
or override sequence. A structurally valid future event can advance structural
chain inspection even when this version 1 kernel cannot apply its semantics.

## Semantic Replay

Semantic replay is a deterministic fold:

```text
next_state = apply(previous_state, validated_record)
```

The initial kernel semantically applies only `ledger_initialized/v1`. Any other
`(event_kind, payload_version)` returns `unsupported event` and stops semantic
replay. It is never skipped, guessed, coerced to a known version, or treated as
a no-op.

Future protocol work will extend semantic state and event handling through an
explicitly reviewed versioned design. Replay remains independent of clocks,
randomness, environment, network state, databases other than the supplied
ordered record stream, and unstable map iteration.

## Truthful Validation Results

The package exposes specific evidence rather than one misleading aggregate
`Valid` or `Verified` boolean. Protocol API and type names reserve `verify` and
`verified` for the later cryptographic signature-verification boundary;
structure, digest, and chain operations use `validate`, `validated`, or
`consistency`. Callers must be able to distinguish:

- canonical structure validated;
- record digest validated;
- event digest validated;
- chain link validated;
- supported payload semantics validated; and
- signature not verified.

Conceptually, those results correspond to `StructureValid`,
`RecordDigestValid`, `EventDigestValid`, `ChainLinkValid`, `SemanticsValid`, and
`SignatureStatus = Unverified`. The implementation plan may refine the Go
shape without weakening these distinctions.

No API, type, test, log, or documentation may describe a record as
cryptographically verified merely because its bytes, hashes, and chain links
are internally consistent.

## Errors and Privacy

Errors are typed or have stable sentinel identities only where callers need to
branch. Required error classes include:

- oversized input;
- malformed CBOR;
- non-deterministic or otherwise non-conforming CBOR;
- unsupported protocol, record, event, or payload version;
- schema violation;
- digest mismatch;
- invalid sequence;
- ledger identity mismatch;
- predecessor-link mismatch; and
- unsupported event semantics.

Error text contains bounded protocol context such as the field category or
expected version. It never includes raw CBOR, payload bytes, signature bytes,
the signer key reference, full digests, or other unbounded attacker-controlled
values. Errors preserve causal identity without converting internal failures to
strings prematurely.

The pure package performs no logging or telemetry. A future caller may record a
bounded error class at the boundary that owns the response or replay decision.

## CBOR Dependency

The implementation pins `github.com/fxamacker/cbor/v2` at version `v2.9.2`.
The dependency supplies maintained RFC 8949 encoding and decoding, Core
Deterministic Encoding, duplicate-key handling, definite-length enforcement,
UTF-8 validation, tag controls, and decoder resource controls. Hand-writing a
general CBOR codec would add protocol and security risk without adding a
required capability.

The package constructs one immutable explicit encoding mode from
`CoreDetEncOptions()` and one immutable strict decoding mode assembled from
explicit decoder options. It does not use library defaults. The implementation
must set every relevant option deliberately and add protocol validation for any
rule the library does not express directly, including closed integer-keyed maps
and unsupported CBOR primitive types.

The decoder's structural restrictions are necessary but not sufficient to
establish deterministic encoding. After schema validation, each accepted item
must be encoded using the protocol's immutable Core Deterministic encoding mode
and compared byte-for-byte with the supplied encoding. A mismatch is
non-conforming CBOR. In compact form, subject to prior schema and type
validation:

```text
accept(x) implies deterministic_encode(decode(x)) == x
```

This byte-equality check is a protocol requirement, independent of whether a
future dependency version adds more decode-time deterministic checks.

No dependency type escapes the package. The committed CDDL, normative prose,
and golden bytes remain authoritative, so the implementation can replace the
library later only if every conformance vector and negative invariant remains
unchanged.

No separate CDDL generator, schema runtime, or second CBOR library is added in
this slice. Independent cross-language validation uses the committed binary
vectors rather than two Go implementations sharing assumptions.

## Conformance Fixtures

Language-neutral fixtures live under `protocol/ledger/v1/testdata`. They do not
depend on Go struct names or library output.

The first golden fixture uses fixed public test values for:

- ledger ID;
- admitted Unix-millisecond timestamp;
- full signer key-version reference;
- an explicitly non-verified opaque DER-shaped signature byte string; and
- every expected encoded layer and digest.

The fixture records:

- exact genesis payload bytes;
- exact event-body bytes;
- exact event digest;
- exact record-body bytes;
- exact record digest; and
- exact complete-record bytes.

A small human-readable manifest explains the fields and contains hexadecimal
expectations. The binary `.cbor` files are the byte authority. The manifest
must say that its signature fixture is not cryptographic acceptance evidence.

Golden-vector changes are durable protocol changes. Updating an expected
digest merely because implementation output changed is forbidden; the schema
and protocol decision must be reviewed first.

## Testing Strategy

Production behavior is implemented test-first. The protocol suite proves claims
at the public package boundary and supplements them with same-package tests only
where unexported parser invariants require direct evidence.

### Positive conformance

- Exact match with every golden payload, event body, event digest, record body,
  record digest, and complete record.
- Successful strict parse of every golden encoded value.
- Byte-identical re-encoding of every accepted input.
- Stable construction from copied inputs after the caller mutates its original
  slices.
- Genesis structural and semantic replay from empty state.
- Structural continuation using a clearly reserved test-only unknown event
  fixture, followed by semantic replay rejecting it as unsupported.

The structural continuation fixture is not a production event kind and is not
exposed through a production builder.

### Negative conformance

Mutation tests cover at least:

- declared and actual sizes over every protocol limit;
- indefinite-length items;
- non-shortest integer and length encodings;
- out-of-order, duplicate, unknown, and missing map keys;
- invalid UTF-8;
- negative integers, floats, tags, `undefined`, unassigned simple values, and
  all other schema-incompatible major types;
- leading or trailing bytes around an embedded item;
- wrong scalar types and byte-string lengths;
- zero or unsupported versions and event kinds;
- every non-exact algorithm identifier;
- noncanonical embedded payload, event-body, and record-body bytes;
- event- and record-digest mutations;
- invalid genesis sequence, predecessor, kind, version, or payload;
- second genesis;
- wrong ledger ID;
- duplicate, skipped, reversed, and overflowing sequence values;
- missing or incorrect predecessor links; and
- mutation of signer reference, signature metadata, and signature bytes without
  a corresponding record-digest update.

Every rejection test checks its stable error class and confirms the error does
not disclose the rejected bytes or bounded private metadata.

### Fuzzing

Fuzz targets cover payload, event-body, record-body, complete-record, and
structural-chain entry points. For arbitrary bytes they prove:

- no panic;
- bounded work and allocations under the declared input limits;
- deterministic results;
- no mutation or aliasing of caller-owned buffers; and
- every accepted value re-encodes exactly to its input.

Fuzzing complements explicit edge cases. Every discovered failure becomes a
committed deterministic regression test or corpus seed. Long-running fuzzing is
an explicit task; the normal repository gate may run only deterministic corpus
tests unless a separately bounded fuzz-smoke command is added.

## Acceptance Gates

Implementation is not complete until fresh output establishes:

1. The focused RED-GREEN-REFACTOR tests for each behavior.
2. Exact conformance-vector tests.
3. Relevant package tests.
4. `make fmt` and `make generate` followed by `git diff --check`.
5. `make check`, including format verification, vet, Staticcheck, tests, race,
   build, govulncheck, and generation consistency.
6. Focused fuzz corpus execution and any approved bounded fuzz smoke.
7. Final review of CDDL, Go field keys, constants, limits, algorithm strings,
   domains, fixtures, and documentation as one compatibility surface.
8. Dependency and secret/artifact audit.
9. An independent protocol-focused whole-branch review before merge.

Database, container, hosted CI, KMS, network, Cloud Run, and live GCP acceptance
are not evidence for this pure kernel and are not claimed by this slice.

## Deferred Decisions

The following decisions are deliberately preserved for later reviewed slices:

- strict DER parsing and exact ECDSA scalar requirements;
- whether admission requires low-S normalization or rejection;
- KMS key discovery, public-key history, and retirement;
- the live signing and admission transaction boundary;
- storage schema, concurrency control, idempotency, and PostgreSQL physical
  partitioning;
- domain event kinds and payload schemas;
- artifact-digest and retrieval-URL schemas;
- retraction, revocation, contradiction, redaction, and tombstone semantics;
- protocol upgrade and historical multi-version replay mechanics; and
- HTTP representation and client compatibility.

None may be inferred from convenience choices made while implementing this
kernel.

## References

- [RFC 8949: Concise Binary Object Representation (CBOR)](https://datatracker.ietf.org/doc/html/rfc8949), especially Section 4.2.1.
- [RFC 8610: Concise Data Definition Language (CDDL)](https://datatracker.ietf.org/doc/html/rfc8610).
- [NIST FIPS 180-4: Secure Hash Standard](https://csrc.nist.gov/pubs/fips/180-4/upd1/final).
- [fxamacker/cbor](https://github.com/fxamacker/cbor) and its [v2.9.2 release](https://github.com/fxamacker/cbor/releases/tag/v2.9.2).
- [Google Cloud KMS key purposes and algorithms](https://docs.cloud.google.com/kms/docs/algorithms).
- [Google Cloud KMS: Creating and validating digital signatures](https://docs.cloud.google.com/kms/docs/create-validate-signatures).
