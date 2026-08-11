# Ledger Protocol Kernel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the pure version 1 ledger protocol kernel: strict
deterministic CBOR, explicit genesis, event and record digests, bounded
structural validation, chain consistency, semantic replay, and stable
language-neutral conformance vectors.

**Architecture:** Language-neutral CDDL and fixtures under
`protocol/ledger/v1` define durable bytes. The pure `internal/ledger/v1`
package owns immutable codec modes, versioned values, digest construction,
structural validation, and replay without I/O, clocks, randomness, logging, or
telemetry. Distinct structural event and record types prevent hash consistency
from being mislabeled as signature verification.

**Tech Stack:** Go 1.26.5, standard-library `crypto/sha256`, and
`github.com/fxamacker/cbor/v2` v2.9.2 using `CoreDetEncOptions()` plus an
explicit strict decoder mode.

## Global Constraints

- Read `../AGENTS.md`, `AGENTS.md`, and
  `docs/superpowers/specs/2026-08-10-ledger-protocol-kernel-design.md` before
  changing code.
- Work only on `agent/ledger-protocol-kernel`; do not push, open a pull
  request, merge, deploy, or mutate GCP without a separate user gate.
- Use RED-GREEN-REFACTOR for every production behavior. Record the focused RED
  command and expected failure in
  `.superpowers/sdd/2026-08-10-ledger-protocol-kernel/task-1-report.md` through
  `task-6-report.md`, using the file that matches the task number, before
  writing that task's production code.
- Use a fresh implementer for each task. After each task, run a fresh
  spec-compliance review and then a fresh code-quality review; resolve both
  before beginning the next task.
- Run an independent whole-branch protocol review after all numbered tasks.
- Protocol version is exactly `1`; event kind `ledger_initialized` is exactly
  `1`; its payload version is exactly `1` and its payload bytes are exactly
  `a0`.
- Event digest domain is exactly ASCII
  `attribution-chain:event-digest:v1`, followed by exactly one `0x00` byte and
  the exact validated event-body bytes.
- Record digest domain is exactly ASCII
  `attribution-chain:record-digest:v1`, followed by exactly one `0x00` byte and
  the exact validated record-body bytes.
- Both digest algorithm identifiers are exactly `sha-256`; signature algorithm
  is exactly `ecdsa-p256-sha256`; signature encoding is exactly `asn1-der`.
- Payload, event-body, record-body, complete-record, signer-reference, and
  signature limits are respectively 65,536, 98,304, 131,072, 131,200, 512,
  and 72 bytes. Signature bytes have a minimum of 8 bytes.
- All maps are closed unsigned-integer-keyed maps. Unknown, duplicate, and
  missing keys are rejected. Optional values are omitted; only genesis uses
  `null`, for `previous_record_digest`.
- Deterministic acceptance requires strict decode and schema validation,
  deterministic re-encoding, and byte-for-byte equality with the input.
- Sequence alone orders records. Timestamps are unsigned Unix milliseconds,
  are not required to be monotonic, and never affect ordering.
- `event_digest` identifies one ordered event body; `record_digest` identifies
  the exact admitted signature representation. The chain links record digests.
- Signatures remain opaque and unverified. Do not add DER parsing, scalar
  checks, low-S policy, KMS, key discovery, or cryptographic verification.
- Unknown events may produce structurally validated records and participate in
  chain consistency inspection, but semantic replay stops with
  `ErrUnsupportedEvent`.
- No error includes raw CBOR, payload bytes, signature bytes, signer key
  references, or full digests. The package emits no logs or telemetry.
- No PostgreSQL, migration, OpenAPI, HTTP, configuration, container, cloud, or
  application composition changes belong in this slice.
- No dependency type escapes `internal/ledger/v1`. No second CBOR dependency,
  CDDL runtime, code generator, framework, or runtime algorithm agility is
  added.

---

## File and Interface Map

Create or modify only these tracked files:

- `protocol/ledger/v1/ledger.cddl`: language-neutral closed version 1 schema.
- `protocol/ledger/v1/testdata/genesis-v1.json`: human-readable fixture
  metadata and exact hexadecimal expectations.
- `protocol/ledger/v1/testdata/genesis-*.cbor`: exact binary conformance
  inputs.
- `protocol/ledger/v1/testdata/vectors_generate.go`: build-ignored deterministic
  materializer for the reviewed manifest hex; imports no production package.
- `internal/ledger/v1/constants.go`: versioned constants, bounded value types,
  digest domains, and signature status.
- `internal/ledger/v1/errors.go`: bounded sentinel errors and safe wrapping.
- `internal/ledger/v1/codec.go`: immutable CBOR modes, exact-key checking,
  bounded encoding, and deterministic re-encode equality.
- `internal/ledger/v1/event.go`: event wire form, genesis construction, event
  digest, structural event validation, and event semantic validation.
- `internal/ledger/v1/record.go`: record wire forms, record construction,
  record digest, and outside-in structural record validation.
- `internal/ledger/v1/chain.go`: chain consistency state and semantic replay
  fold.
- `internal/ledger/v1/*_test.go`: focused behavior, conformance, ownership,
  privacy, mutation, and fuzz proofs.
- `go.mod`, `go.sum`: exact CBOR dependency pin.
- `Makefile`: explicit bounded protocol-fuzz command only; normal `make check`
  remains deterministic.
- `README.md`, `AGENTS.md`: truthful protocol and command status.

The tasks produce this public package surface. Do not add a generic codec API,
algorithm parameter, signer interface, or exported wire struct.

```go
package ledgerv1

type LedgerID [32]byte
type Digest [32]byte
type EventKind uint64

const EventKindLedgerInitialized EventKind = 1

type SignatureStatus uint8

const SignatureStatusUnverified SignatureStatus = iota

type StructuralEvent struct {
	bytes                   []byte
	digest                  Digest
	ledgerID                LedgerID
	sequence                uint64
	previousRecordDigest    Digest
	hasPreviousRecordDigest bool
	admittedAtUnixMS        uint64
	kind                    EventKind
	payloadVersion          uint64
	payloadBytes            []byte
}

func NewGenesisEvent(ledgerID LedgerID, admittedAtUnixMS uint64) (StructuralEvent, error)
func ValidateEventStructure(encoded []byte) (StructuralEvent, error)
func ValidateEventSemantics(event StructuralEvent) error
func (event StructuralEvent) Bytes() []byte
func (event StructuralEvent) Digest() Digest
func (event StructuralEvent) LedgerID() LedgerID
func (event StructuralEvent) Sequence() uint64
func (event StructuralEvent) PreviousRecordDigest() (Digest, bool)
func (event StructuralEvent) AdmittedAtUnixMS() uint64
func (event StructuralEvent) Kind() EventKind
func (event StructuralEvent) PayloadVersion() uint64
func (event StructuralEvent) PayloadBytes() []byte

type StructuralRecord struct {
	bytes              []byte
	recordBodyBytes    []byte
	recordDigest       Digest
	event              StructuralEvent
	signerKeyReference string
	signatureBytes     []byte
}

func NewRecord(event StructuralEvent, signerKeyReference string, signatureBytes []byte) (StructuralRecord, error)
func ValidateRecordStructure(encoded []byte) (StructuralRecord, error)
func (record StructuralRecord) Bytes() []byte
func (record StructuralRecord) RecordBodyBytes() []byte
func (record StructuralRecord) RecordDigest() Digest
func (record StructuralRecord) Event() StructuralEvent
func (record StructuralRecord) SignerKeyReference() string
func (record StructuralRecord) SignatureBytes() []byte
func (record StructuralRecord) SignatureStatus() SignatureStatus

type ChainState struct {
	initialized      bool
	ledgerID         LedgerID
	lastSequence     uint64
	lastRecordDigest Digest
}

func ValidateChainConsistency(state ChainState, record StructuralRecord) (ChainState, error)
func (state ChainState) Initialized() bool
func (state ChainState) LedgerID() (LedgerID, bool)
func (state ChainState) LastSequence() uint64
func (state ChainState) LastRecordDigest() (Digest, bool)

type ReplayState struct {
	chain ChainState
}

func Apply(state ReplayState, record StructuralRecord) (ReplayState, error)
func (state ReplayState) ChainState() ChainState
```

Required sentinel identities:

```go
var (
	ErrOversizedInput     = errors.New("ledger v1 input exceeds protocol limit")
	ErrMalformedCBOR      = errors.New("ledger v1 malformed CBOR")
	ErrNonConformingCBOR  = errors.New("ledger v1 non-conforming CBOR")
	ErrUnsupportedVersion = errors.New("ledger v1 unsupported version")
	ErrSchemaViolation    = errors.New("ledger v1 schema violation")
	ErrDigestMismatch     = errors.New("ledger v1 digest mismatch")
	ErrInvalidSequence    = errors.New("ledger v1 invalid sequence")
	ErrLedgerMismatch     = errors.New("ledger v1 ledger identity mismatch")
	ErrChainLinkMismatch  = errors.New("ledger v1 chain link mismatch")
	ErrUnsupportedEvent   = errors.New("ledger v1 unsupported event")
)
```

---

### Task 1: CDDL and Deterministic-CBOR Foundation

**Files:**

- Create: `protocol/ledger/v1/ledger.cddl`
- Create: `internal/ledger/v1/constants.go`
- Create: `internal/ledger/v1/errors.go`
- Test: `internal/ledger/v1/codec_test.go`
- Create: `internal/ledger/v1/codec.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

- Produces the constants, bounded types, `SignatureStatus`, and sentinels from
  the File and Interface Map.
- Produces package-private immutable `protocolEncMode` and `protocolDecMode`.
- Produces package-private helpers:

```go
type validationStage string

func encodeCanonical(value any, maxBytes int, stage validationStage) ([]byte, error)
func decodeCanonicalMap(
	encoded []byte,
	maxBytes int,
	expectedKeys []uint64,
	destination any,
	stage validationStage,
) error
```

- Later tasks consume these helpers; they must never return dependency types or
  raw dependency errors.

- [ ] **Step 1: Add the exact CDDL contract before Go implementation**

Create `protocol/ledger/v1/ledger.cddl`:

```cddl
ledger-record-v1 = {
  0: 1,
  1: bstr .size (1..131072),
  2: "sha-256",
  3: bstr .size 32
}

record-body-v1 = {
  0: 1,
  1: bstr .size (1..98304),
  2: "sha-256",
  3: bstr .size 32,
  4: tstr .size (1..512),
  5: "ecdsa-p256-sha256",
  6: "asn1-der",
  7: bstr .size (8..72)
}

event-body-v1 = {
  0: 1,
  1: bstr .size 32,
  2: positive-uint64,
  3: null / bstr .size 32,
  4: uint64,
  5: positive-uint64,
  6: positive-uint64,
  7: bstr .size (1..65536)
}

ledger-initialized-payload-v1 = {}

uint64 = 0..18446744073709551615
positive-uint64 = 1..18446744073709551615
```

Then run:

```bash
git add --intent-to-add protocol/ledger/v1/ledger.cddl
git diff --check -- protocol/ledger/v1/ledger.cddl
rg -n 'ledger-record-v1|record-body-v1|event-body-v1|positive-uint64' \
  protocol/ledger/v1/ledger.cddl
```

Expected: no whitespace errors; all schema roots and integer bounds are
present.

- [ ] **Step 2: Pin the sole CBOR dependency**

Run:

```bash
go get github.com/fxamacker/cbor/v2@v2.9.2
go list -m github.com/fxamacker/cbor/v2
```

Expected: the final command prints
`github.com/fxamacker/cbor/v2 v2.9.2`; `go.mod` contains no second CBOR module.

- [ ] **Step 3: Write the failing codec behavior tests**

Create `internal/ledger/v1/codec_test.go` with same-package tests. Use this
wire type so zero remains a valid value while key presence is checked
separately:

```go
type codecTestWire struct {
	Value uint64 `cbor:"0,keyasint"`
}

func TestDecodeCanonicalMapAcceptsExactEncoding(t *testing.T) {
	t.Parallel()
	encoded := []byte{0xa1, 0x00, 0x01}
	var decoded codecTestWire
	if err := decodeCanonicalMap(encoded, len(encoded), []uint64{0}, &decoded, "test"); err != nil {
		t.Fatalf("decodeCanonicalMap: %v", err)
	}
	if decoded.Value != 1 {
		t.Fatalf("value = %d, want 1", decoded.Value)
	}
}

func TestDecodeCanonicalMapRejectsNonConformingEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		encoded []byte
	}{
		{name: "non-shortest integer", encoded: []byte{0xa1, 0x00, 0x18, 0x01}},
		{name: "indefinite map", encoded: []byte{0xbf, 0x00, 0x01, 0xff}},
		{name: "duplicate key", encoded: []byte{0xa2, 0x00, 0x01, 0x00, 0x02}},
		{name: "tag", encoded: []byte{0xd9, 0xd9, 0xf7, 0xa1, 0x00, 0x01}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoded codecTestWire
			err := decodeCanonicalMap(test.encoded, 32, []uint64{0}, &decoded, "test")
			if !errors.Is(err, ErrNonConformingCBOR) {
				t.Fatalf("error = %v, want ErrNonConformingCBOR", err)
			}
		})
	}
}
```

Also add cases for missing and unknown keys, an over-limit outer slice,
truncated CBOR, an out-of-order map, a text key, invalid UTF-8, `undefined`, a
float where an unsigned integer is required, and trailing bytes. Each case
asserts a declared sentinel and that its private marker is absent from the
error text.

- [ ] **Step 4: Run the codec RED**

Run:

```bash
go test ./internal/ledger/v1 -run '^TestDecodeCanonicalMap' -count=1
```

Expected: FAIL because `decodeCanonicalMap` and sentinels are undefined. Record
the exact failure before adding production Go files.

- [ ] **Step 5: Implement constants, errors, and immutable codec modes**

Create named constants for all versions, keys, domains, algorithms, and limits.
Create the exact sentinels above and a safe wrapper that includes only a
package-owned stage label. Build the encoder from `cbor.CoreDetEncOptions()`.
Build the decoder from explicit options that enforce duplicate-key rejection,
forbid indefinite lengths and tags, reject invalid UTF-8, return unknown-field
errors, and set nesting, array, and map limits to the smallest supported values
that cover the v1 schema.

`decodeCanonicalMap` performs: outer length check; strict generic decode; Core
Deterministic re-encode byte equality; exact
`map[uint64]cbor.RawMessage` key-set validation; then typed decode into
`destination`. Never use `RawMessage` for the deterministic re-encode
comparison because it preserves supplied bytes.
Classify dependency failures only through exported error types and the phase
that failed; never inspect or return dependency error strings.

- [ ] **Step 6: Run GREEN and dependency gates**

Run:

```bash
go fmt ./internal/ledger/v1
go mod tidy
go list -m github.com/fxamacker/cbor/v2
go test ./internal/ledger/v1 -run '^TestDecodeCanonicalMap' -count=1
go test ./internal/ledger/v1 -count=1
go vet ./internal/ledger/v1
./bin/staticcheck ./internal/ledger/v1
git diff --check
```

Expected: all pass; the only direct new production dependency is
`github.com/fxamacker/cbor/v2 v2.9.2`.

- [ ] **Step 7: Review and commit the protocol foundation**

After task-scoped spec and quality reviews:

```bash
git add go.mod go.sum protocol/ledger/v1/ledger.cddl internal/ledger/v1
git diff --cached --check
git commit -m "feat: add deterministic CBOR protocol foundation"
```

---

### Task 2: Genesis Event and Event Digest

**Files:**

- Test: `internal/ledger/v1/event_test.go`
- Create: `internal/ledger/v1/event.go`
- Modify: `internal/ledger/v1/constants.go`
- Modify: `internal/ledger/v1/errors.go`

**Interfaces:**

- Consumes Task 1 codec helpers and versioned types.
- Produces the complete `StructuralEvent` interface from the File and Interface
  Map. Byte-returning accessors always return copies.

- [ ] **Step 1: Write the failing exact genesis test**

Use these independently calculated expectations:

```go
const goldenEventBodyHex = "a80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a0"
const goldenEventDigestHex = "26bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df72"
```

Construct a `LedgerID` containing bytes `00` through `1f`, call
`NewGenesisEvent(ledgerID, 1735689600123)`, and assert exact body hex, exact
digest hex, every field accessor, and successful `ValidateEventSemantics`.
Bind `event.Digest()` to a local variable before slicing the array for hex.
Also prove mutating returned `Bytes()` and `PayloadBytes()` cannot mutate the
event.

- [ ] **Step 2: Write failing structural and semantic rejection tests**

Use same-package `eventBodyWire` values encoded by `encodeCanonical` to cover:
protocol version 0 and 2; sequence 0; event kind or payload version 0;
predecessor lengths 0 and 31; payload lengths 0 and 65,537; sequence 1 with a
predecessor; `ledger_initialized` at sequence 2; genesis payload other than
exact `a0`; the zero `StructuralEvent` value; and unknown event kind 2.
Structure must accept the canonical unknown event while semantics returns
`ErrUnsupportedEvent`. The zero value returns `ErrSchemaViolation`.

- [ ] **Step 3: Run event RED**

```bash
go test ./internal/ledger/v1 -run '^(TestNewGenesisEvent|TestValidateEvent)' -count=1
```

Expected: FAIL because event APIs do not exist.

- [ ] **Step 4: Implement event construction and validation**

Use this private wire struct:

```go
type eventBodyWire struct {
	ProtocolVersion      uint64 `cbor:"0,keyasint"`
	LedgerID             []byte `cbor:"1,keyasint"`
	Sequence             uint64 `cbor:"2,keyasint"`
	PreviousRecordDigest []byte `cbor:"3,keyasint"`
	AdmittedAtUnixMS     uint64 `cbor:"4,keyasint"`
	EventKind            uint64 `cbor:"5,keyasint"`
	PayloadVersion       uint64 `cbor:"6,keyasint"`
	PayloadBytes         []byte `cbor:"7,keyasint"`
}
```

Nil `PreviousRecordDigest` means explicit CBOR `null`, while a missing key is
already rejected by exact key checking. Validate fixed lengths before copying
to `LedgerID` or `Digest` arrays.

Compute only:

```text
SHA256(ASCII("attribution-chain:event-digest:v1") || 0x00 || eventBodyBytes)
```

`ValidateEventStructure` checks canonical event-body bytes, bounds, positive
fields, and protocol version while preserving `payload_bytes` as opaque bytes.
The known genesis semantic validator decodes the payload as a closed empty map,
requires deterministic re-encode equality, and requires exact `a0`.
`ValidateEventSemantics` accepts only `(1, 1)`, sequence 1, and null predecessor;
unknown pairs return `ErrUnsupportedEvent` without decoding their payload.

- [ ] **Step 5: Run event GREEN**

```bash
go fmt ./internal/ledger/v1
go test ./internal/ledger/v1 -run '^(TestNewGenesisEvent|TestValidateEvent|TestStructuralEvent)' -count=1
go test ./internal/ledger/v1 -count=1
go test -race ./internal/ledger/v1 -count=1
git diff --check
```

Expected: all pass, including ownership and backward-timestamp independence.

- [ ] **Step 6: Review and commit event identity**

```bash
git add internal/ledger/v1/constants.go internal/ledger/v1/errors.go \
  internal/ledger/v1/event.go internal/ledger/v1/event_test.go
git diff --cached --check
git commit -m "feat: add genesis event digest"
```

---

### Task 3: Admitted Record, Record Digest, and Golden Vectors

**Files:**

- Test: `internal/ledger/v1/record_test.go`
- Create: `internal/ledger/v1/record.go`
- Create: `protocol/ledger/v1/testdata/genesis-v1.json`
- Create: `protocol/ledger/v1/testdata/vectors_generate.go`
- Generate: `protocol/ledger/v1/testdata/genesis-payload.cbor`
- Generate: `protocol/ledger/v1/testdata/genesis-event-body.cbor`
- Generate: `protocol/ledger/v1/testdata/genesis-record-body.cbor`
- Generate: `protocol/ledger/v1/testdata/genesis-ledger-record.cbor`

**Interfaces:**

- Consumes `StructuralEvent` from Task 2.
- Produces the complete `StructuralRecord` interface from the File and Interface
  Map.
- Construction writes fixed v1 identifiers. Callers supply only a structural
  event, a signer key reference, and opaque signature bytes.

- [ ] **Step 1: Write the failing exact record test**

Use:

```go
const goldenSignerKeyReference = "projects/attribution-chain-505000/locations/global/keyRings/system-signing/cryptoKeys/ledger-admission/cryptoKeyVersions/1"

var goldenOpaqueSignature = []byte{0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01}

const goldenRecordDigestHex = "3c9758df59d7a938da29f4cb5d82b7c99a446cfad07e4d890dd3edfc8ab4f035"
```

Construct the golden event, call `NewRecord`, and assert exact record digest,
signer reference, signature bytes, `SignatureStatusUnverified`, and successful
`ValidateRecordStructure(record.Bytes())`. Use the complete record hex from the
manifest in Step 5 as the exact byte expectation.

- [ ] **Step 2: Write failing record-boundary tests**

Cover signer reference empty, 513 bytes, non-ASCII, and ASCII control bytes;
signature lengths 7 and 73; all nested size bounds; every non-exact digest and
signature identifier; outer and inner record versions other than 1; event and
record digest mutations; signer/signature mutation without record-digest
update; wrong embedded event digest; `NewRecord(StructuralEvent{}, ...)`;
ownership of every returned byte slice; and privacy of errors containing fixed
signer, signature, payload, and digest markers. The zero event is rejected with
`ErrSchemaViolation` before encoding a record body.

- [ ] **Step 3: Run record RED**

```bash
go test ./internal/ledger/v1 -run '^(TestNewRecord|TestValidateRecord|TestStructuralRecord)' -count=1
```

Expected: FAIL because record APIs do not exist.

- [ ] **Step 4: Implement construction and outside-in record validation**

Use these private wire structs:

```go
type recordBodyWire struct {
	RecordVersion        uint64 `cbor:"0,keyasint"`
	EventBodyBytes       []byte `cbor:"1,keyasint"`
	EventDigestAlgorithm string `cbor:"2,keyasint"`
	EventDigest          []byte `cbor:"3,keyasint"`
	SignerKeyReference   string `cbor:"4,keyasint"`
	SignatureAlgorithm   string `cbor:"5,keyasint"`
	SignatureEncoding    string `cbor:"6,keyasint"`
	SignatureBytes       []byte `cbor:"7,keyasint"`
}

type ledgerRecordWire struct {
	RecordVersion        uint64 `cbor:"0,keyasint"`
	RecordBodyBytes      []byte `cbor:"1,keyasint"`
	RecordDigestAlgorithm string `cbor:"2,keyasint"`
	RecordDigest         []byte `cbor:"3,keyasint"`
}
```

Align the field names with `gofmt`; the names and tags are normative for the
plan even when spacing changes. `NewRecord` validates printable ASCII U+0020
through U+007E and signature length 8 through 72, copies inputs, writes fixed
identifiers, computes the record digest, and always reports
`SignatureStatusUnverified`.

`ValidateRecordStructure` performs:

1. complete-record bound, strict outer decode, record-body bound, and exact
   outer deterministic re-encode equality;
2. strict record-body decode, event-body and field bounds, and exact record-body
   deterministic re-encode equality;
3. exact record-digest recomputation and comparison;
4. exact structural event validation; and
5. exact declared event-digest comparison.

It does not call `ValidateEventSemantics` or perform cryptography. Compute only:

```text
SHA256(ASCII("attribution-chain:record-digest:v1") || 0x00 || recordBodyBytes)
```

- [ ] **Step 5: Add reviewed language-neutral vectors**

Create `protocol/ledger/v1/testdata/genesis-v1.json` with this complete content:

```json
{
  "name": "attribution-chain-ledger-v1-genesis",
  "signature_status": "unverified",
  "ledger_id_hex": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
  "admitted_at_unix_ms": 1735689600123,
  "signer_key_reference": "projects/attribution-chain-505000/locations/global/keyRings/system-signing/cryptoKeys/ledger-admission/cryptoKeyVersions/1",
  "files": {
    "genesis-payload.cbor": "a0",
    "genesis-event-body.cbor": "a80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a0",
    "genesis-record-body.cbor": "a8000101583ba80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a002677368612d32353603582026bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df7204787a70726f6a656374732f6174747269627574696f6e2d636861696e2d3530353030302f6c6f636174696f6e732f676c6f62616c2f6b657952696e67732f73797374656d2d7369676e696e672f63727970746f4b6579732f6c65646765722d61646d697373696f6e2f63727970746f4b657956657273696f6e732f31057165636473612d703235362d736861323536066861736e312d64657207483006020101020101",
    "genesis-ledger-record.cbor": "a4000101590111a8000101583ba80001015820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f020103f6041b000001941f297c7b050106010741a002677368612d32353603582026bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df7204787a70726f6a656374732f6174747269627574696f6e2d636861696e2d3530353030302f6c6f63616c2f6b657952696e67732f73797374656d2d7369676e696e672f63727970746f4b6579732f6c65646765722d61646d697373696f6e2f63727970746f4b657956657273696f6e732f31057165636473612d703235362d736861323536066861736e312d6465720748300602010102010102677368612d3235360358203c9758df59d7a938da29f4cb5d82b7c99a446cfad07e4d890dd3edfc8ab4f035"
  },
  "event_digest_hex": "26bab9e3db670d0bb9bb1bbaff5fe975f19359c28409401abe6c316c82e9df72",
  "record_digest_hex": "3c9758df59d7a938da29f4cb5d82b7c99a446cfad07e4d890dd3edfc8ab4f035",
  "signature_hex": "3006020101020101",
  "signature_note": "DER-shaped opaque fixture only; not a cryptographically verified signature."
}
```

Create build-ignored `vectors_generate.go` with this structure; error checks
must call `log.Fatal` or `log.Fatalf` rather than being discarded:

```go
//go:build ignore

package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const fixtureDirectory = "protocol/ledger/v1/testdata"

var fixtureNames = []string{
	"genesis-payload.cbor",
	"genesis-event-body.cbor",
	"genesis-record-body.cbor",
	"genesis-ledger-record.cbor",
}

type vectorManifest struct {
	Files map[string]string `json:"files"`
}

func main() {
	manifestBytes, err := os.ReadFile(filepath.Join(fixtureDirectory, "genesis-v1.json"))
	if err != nil {
		log.Fatal(err)
	}
	var manifest vectorManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		log.Fatal(err)
	}
	if len(manifest.Files) != len(fixtureNames) {
		log.Fatalf("fixture entries = %d, want %d", len(manifest.Files), len(fixtureNames))
	}
	allowed := make(map[string]struct{}, len(fixtureNames))
	for _, name := range fixtureNames {
		allowed[name] = struct{}{}
	}
	for name := range manifest.Files {
		if _, ok := allowed[name]; !ok {
			log.Fatalf("unexpected fixture name %q", name)
		}
	}
	for _, name := range fixtureNames {
		encodedHex, ok := manifest.Files[name]
		if !ok {
			log.Fatalf("missing fixture name %q", name)
		}
		encoded, err := hex.DecodeString(encodedHex)
		if err != nil {
			log.Fatalf("decode %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDirectory, name), encoded, 0o644); err != nil {
			log.Fatalf("write %s: %v", name, err)
		}
	}
}
```

It imports no production package. Run:

```bash
go run ./protocol/ledger/v1/testdata/vectors_generate.go
```

Add a test that compares each binary to manifest hex and compares production
output and both digests to the committed expectations. Never derive expected
hex or digests from production output. Put this shared test helper in
`record_test.go` for Task 5:

```go
func readProtocolFixture(t testing.TB, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "protocol", "ledger", "v1", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read protocol fixture %s: %v", name, err)
	}
	return data
}
```

- [ ] **Step 6: Run record and vector GREEN**

```bash
go fmt ./internal/ledger/v1 ./protocol/ledger/v1/testdata/vectors_generate.go
go test ./internal/ledger/v1 -run '^(TestNewRecord|TestValidateRecord|TestStructuralRecord|TestGenesisConformanceVector)' -count=1
go test ./internal/ledger/v1 -count=1
go test -race ./internal/ledger/v1 -count=1
git diff --check
```

Expected: all pass and all four binary fixtures match the manifest.

- [ ] **Step 7: Review and commit admitted-record identity**

```bash
git add internal/ledger/v1 protocol/ledger/v1/testdata
git diff --cached --check
git commit -m "feat: add admitted ledger record"
```

---

### Task 4: Chain Consistency and Semantic Replay

**Files:**

- Test: `internal/ledger/v1/chain_test.go`
- Create: `internal/ledger/v1/chain.go`
- Modify: `internal/ledger/v1/errors.go`

**Interfaces:**

- Consumes `StructuralRecord`, `ValidateEventSemantics`, and digest accessors.
- Produces `ChainState`, `ValidateChainConsistency`, `ReplayState`, and `Apply`
  exactly as listed in the File and Interface Map.
- Chain consistency may advance through an unknown continuation; semantic
  replay must not.

- [ ] **Step 1: Write failing genesis and unknown-continuation tests**

Create the golden record through public constructors. Add a same-package helper
that builds event kind 2 through `eventBodyWire` and `encodeCanonical`, then
validates it structurally before `NewRecord`.

```go
func TestValidateChainConsistencyAdvancesGenesis(t *testing.T) {
	t.Parallel()
	record := newGoldenGenesisRecord(t)
	state, err := ValidateChainConsistency(ChainState{}, record)
	if err != nil {
		t.Fatalf("ValidateChainConsistency: %v", err)
	}
	if !state.Initialized() || state.LastSequence() != 1 {
		t.Fatalf("state not initialized at sequence 1")
	}
}

func TestUnknownContinuationAdvancesConsistencyButStopsReplay(t *testing.T) {
	t.Parallel()
	genesis := newGoldenGenesisRecord(t)
	chainState, err := ValidateChainConsistency(ChainState{}, genesis)
	if err != nil {
		t.Fatalf("advance genesis: %v", err)
	}
	unknown := newUnknownContinuationRecord(t, genesis, 2)
	next, err := ValidateChainConsistency(chainState, unknown)
	if err != nil || next.LastSequence() != 2 {
		t.Fatalf("advance unknown = (%v, %v), want sequence 2", next, err)
	}
	replay, err := Apply(ReplayState{}, genesis)
	if err != nil {
		t.Fatalf("apply genesis: %v", err)
	}
	if _, err := Apply(replay, unknown); !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("Apply error = %v, want ErrUnsupportedEvent", err)
	}
}
```

- [ ] **Step 2: Add the failing transition matrix**

Cover empty state receiving non-genesis; second genesis; wrong ledger ID;
duplicate, reversed, and skipped sequences; null or wrong predecessor;
`last_sequence == math.MaxUint64`; backward timestamp with correct sequence;
the zero `StructuralRecord`; and input-state immutability after each failure.
Assert the exact sentinel from the File and Interface Map; the zero record is
`ErrSchemaViolation`.

- [ ] **Step 3: Run chain RED**

```bash
go test ./internal/ledger/v1 -run '^(TestValidateChainConsistency|TestUnknownContinuation|TestApply)' -count=1
```

Expected: FAIL because chain and replay APIs do not exist.

- [ ] **Step 4: Implement immutable chain and replay folds**

```go
type ChainState struct {
	initialized      bool
	ledgerID         LedgerID
	lastSequence     uint64
	lastRecordDigest Digest
}

type ReplayState struct {
	chain ChainState
}
```

For empty state, require a semantically valid exact genesis. For initialized
state check overflow, exact ledger ID, exact next sequence, non-null exact
predecessor, and no second `ledger_initialized`. Do not inspect time. Unknown
events may advance consistency after those checks.

`Apply` validates event semantics before advancing. An unknown event returns
`ErrUnsupportedEvent` without mutating or returning an advanced replay state.

- [ ] **Step 5: Run chain GREEN**

```bash
go fmt ./internal/ledger/v1
go test ./internal/ledger/v1 -run '^(TestValidateChainConsistency|TestUnknownContinuation|TestApply)' -count=1
go test ./internal/ledger/v1 -count=1
go test -race ./internal/ledger/v1 -count=1
git diff --check
```

Expected: all pass, including the backward-timestamp case.

- [ ] **Step 6: Review and commit chain behavior**

```bash
git add internal/ledger/v1/chain.go internal/ledger/v1/chain_test.go \
  internal/ledger/v1/errors.go
git diff --cached --check
git commit -m "feat: add ledger chain consistency"
```

---

### Task 5: Adversarial Conformance and Fuzz Hardening

**Files:**

- Test: `internal/ledger/v1/conformance_test.go`
- Test: `internal/ledger/v1/fuzz_test.go`
- Modify: implementation files only when a new failing case proves a defect.
- Modify: `Makefile`

**Interfaces:**

- Consumes every public boundary from Tasks 1 through 4.
- Produces no new production API.
- Produces explicit `make fuzz-protocol` with `FUZZ_TIME ?= 10s`; it is not
  added to `make check`.

- [ ] **Step 1: Add the complete mutation matrix**

Create `conformance_test.go` with helpers that clone golden bytes and mutate one
property at a time. Cover:

- complete input 131,201 bytes; record body 131,073; event body 98,305; payload
  65,537;
- indefinite map and byte string; non-shortest integers and lengths;
- out-of-order, duplicate, unknown, and missing keys at all map layers;
- invalid UTF-8 signer reference;
- negative integers, floats, tags, `undefined`, and unassigned simple values;
- leading and trailing bytes at every independently decoded layer;
- wrong scalar types, fixed byte lengths, versions, and identifiers;
- noncanonical known-genesis payload, event body, and record body; the payload
  case must preserve structural record validation and fail semantic validation;
- event and record digest mismatches;
- every invalid genesis shape; and
- every chain identity, sequence, overflow, and predecessor failure.

Each case asserts a sentinel with `errors.Is`, runs twice to prove deterministic
classification, and invokes:

```go
func assertErrorIsPrivate(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want rejection")
	}
	for _, value := range forbidden {
		if strings.Contains(err.Error(), value) {
			t.Fatalf("error disclosed private value %q: %v", value, err)
		}
	}
}
```

When targeting an inner layer, rebuild the enclosing byte strings and recompute
the enclosing test digests so validation reaches the intended mutation instead
of stopping at an earlier digest mismatch. Use a test-only raw SHA-256 helper
with the exact committed domains; the independent golden vector remains the
digest correctness oracle.

- [ ] **Step 2: Run the mutation matrix and fix only demonstrated gaps**

```bash
go test ./internal/ledger/v1 -run '^TestConformanceRejects' -count=1
```

Expected: all cases pass if Tasks 1 through 4 already enforce them. If a case
fails because validation accepts it or returns the wrong stable class, record
that RED, make the smallest production correction, rerun the focused case, and
keep it as a regression. Do not manufacture a production change when the
matrix is already green.

- [ ] **Step 3: Add fuzz entry points with immutable seeds**

Create these entry points. `protocolErrorIdentity` checks all ten sentinels from
the File and Interface Map in a fixed order and returns the matching sentinel
name, `"nil"`, or `"other"`.

```go
func FuzzValidateEventStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-event-body.cbor"))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		event, err := ValidateEventStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateEventStructure mutated input")
		}
		if err != nil {
			return
		}
		if !bytes.Equal(event.Bytes(), input) {
			t.Fatal("accepted event bytes differ from input")
		}
		copyBytes := event.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, event.Bytes()) {
			t.Fatal("event Bytes returned aliased storage")
		}
		again, err := ValidateEventStructure(input)
		if err != nil || again.Digest() != event.Digest() || again.Sequence() != event.Sequence() {
			t.Fatalf("second validation differs: %v", err)
		}
	})
}

func FuzzValidateRecordStructure(f *testing.F) {
	f.Add(readProtocolFixture(f, "genesis-ledger-record.cbor"))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		record, err := ValidateRecordStructure(input)
		if !bytes.Equal(input, original) {
			t.Fatal("ValidateRecordStructure mutated input")
		}
		if err != nil {
			return
		}
		if !bytes.Equal(record.Bytes(), input) {
			t.Fatal("accepted record bytes differ from input")
		}
		copyBytes := record.Bytes()
		copyBytes[0] ^= 0xff
		if bytes.Equal(copyBytes, record.Bytes()) {
			t.Fatal("record Bytes returned aliased storage")
		}
		again, err := ValidateRecordStructure(input)
		if err != nil || again.RecordDigest() != record.RecordDigest() {
			t.Fatalf("second validation differs: %v", err)
		}
	})
}

func FuzzValidateChainConsistency(f *testing.F) {
	golden := readProtocolFixture(f, "genesis-ledger-record.cbor")
	f.Add(golden, golden)
	f.Fuzz(func(t *testing.T, firstBytes, secondBytes []byte) {
		first, err := ValidateRecordStructure(firstBytes)
		if err != nil {
			return
		}
		state, err := ValidateChainConsistency(ChainState{}, first)
		if err != nil {
			return
		}
		second, err := ValidateRecordStructure(secondBytes)
		if err != nil {
			return
		}
		nextOne, errOne := ValidateChainConsistency(state, second)
		nextTwo, errTwo := ValidateChainConsistency(state, second)
		if protocolErrorIdentity(errOne) != protocolErrorIdentity(errTwo) {
			t.Fatalf("error identities differ: %v and %v", errOne, errTwo)
		}
		if errOne == nil && nextOne != nextTwo {
			t.Fatal("successful chain results differ")
		}
	})
}

func protocolErrorIdentity(err error) string {
	if err == nil {
		return "nil"
	}
	identities := []struct {
		name string
		err  error
	}{
		{name: "oversized", err: ErrOversizedInput},
		{name: "malformed", err: ErrMalformedCBOR},
		{name: "non-conforming", err: ErrNonConformingCBOR},
		{name: "version", err: ErrUnsupportedVersion},
		{name: "schema", err: ErrSchemaViolation},
		{name: "digest", err: ErrDigestMismatch},
		{name: "sequence", err: ErrInvalidSequence},
		{name: "ledger", err: ErrLedgerMismatch},
		{name: "link", err: ErrChainLinkMismatch},
		{name: "event", err: ErrUnsupportedEvent},
	}
	for _, identity := range identities {
		if errors.Is(err, identity.err) {
			return identity.name
		}
	}
	return "other"
}
```

Add a second chain seed containing the Task 4 unknown continuation so
`ErrUnsupportedEvent` and successful structural advancement are both exercised
by the deterministic corpus before fuzzing begins.

- [ ] **Step 4: Run the fuzz-command RED**

```bash
FUZZ_TIME=1s make fuzz-protocol
```

Expected: FAIL with `No rule to make target 'fuzz-protocol'`. Record this as
the command-contract RED; the Go fuzz functions already compile as ordinary
corpus tests.

- [ ] **Step 5: Add the bounded explicit fuzz target**

Add `FUZZ_TIME ?= 10s`, add `fuzz-protocol` to `.PHONY`, and add:

```makefile
fuzz-protocol:
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateEventStructure$$' -fuzztime $(FUZZ_TIME)
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateRecordStructure$$' -fuzztime $(FUZZ_TIME)
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateChainConsistency$$' -fuzztime $(FUZZ_TIME)
```

Do not alter `make check` prerequisites.

- [ ] **Step 6: Run deterministic, race, and bounded fuzz GREEN**

```bash
go fmt ./internal/ledger/v1
go test ./internal/ledger/v1 -count=1
go test -race ./internal/ledger/v1 -count=1
FUZZ_TIME=5s make fuzz-protocol
git diff --check
```

Expected: all pass. Commit a Go fuzz corpus artifact only when it is a minimal
inspected deterministic regression input.

- [ ] **Step 7: Review and commit conformance hardening**

```bash
git add Makefile internal/ledger/v1
git diff --cached --check
git commit -m "test: harden ledger protocol conformance"
```

---

### Task 6: Truthful Documentation and Final Acceptance

**Files:**

- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Consumes the completed kernel and fresh command output.
- Produces no production API.
- Documents structural evidence separately from unverified signatures.

- [ ] **Step 1: Update repository truth**

Add a concise README section naming the CDDL authority, Go package, two-digest
distinction, genesis-only semantic support, fixed algorithms, deterministic
re-encode equality, golden vectors, `make fuzz-protocol`, and signature
verification deferral.

Update `AGENTS.md` Current Status and command status to say the v1 kernel exists
while PostgreSQL ledger schema, admission, KMS signing, cryptographic signature
verification, domain events, API behavior, and live acceptance remain absent.
Do not claim database, container, hosted-CI, or cloud acceptance.

- [ ] **Step 2: Run the complete local verification matrix**

```bash
make fmt
make generate
git diff --check
make check
FUZZ_TIME=10s make fuzz-protocol
go test ./internal/ledger/v1 -count=20
go test -race ./internal/ledger/v1 -count=10
```

Expected: all pass and generation changes no committed protocol fixture or
OpenAPI binding.

- [ ] **Step 3: Audit compatibility and repository safety**

```bash
git diff main -- protocol/ledger/v1 internal/ledger/v1 go.mod go.sum \
  Makefile README.md AGENTS.md
git diff main --check
git status --short
suspicious_paths="$(git ls-files | \
  rg '(\.env($|\.)|service-account|credential|private[-_]?key|\.pem$|\.key$|coverage)' | \
  rg -v '^\.env\.example$' || true)"
if [ -n "$suspicious_paths" ]; then
  printf '%s\n' "$suspicious_paths" >&2
  exit 1
fi
go list -m all | rg 'cbor'
```

Expected: only the approved CBOR dependency appears and no secret, private key,
coverage output, accidental fuzz cache, or unrelated artifact is tracked.
Manually compare CDDL keys, Go tags, constants, domains, limits, identifiers,
manifest hex, and binary fixtures as one compatibility surface.

- [ ] **Step 4: Run the independent whole-branch review**

Give a fresh reviewer the approved design, this plan, and `git diff main`, which
includes the uncommitted documentation from Step 1. Require exact-file
evidence. The reviewer checks:

- deterministic decode plus re-encode equality at every layer;
- no dependency-error or private-value disclosure;
- exact digest preimages and no second event-digest hash;
- structural unknown-event inspection without semantic skipping;
- copy ownership and absence of mutable global state;
- no aggregate `Verified` result or cryptographic claim;
- every size, version, identifier, genesis, sequence, and link rule; and
- no persistence, KMS, HTTP, cloud, or domain scope.

Resolve each important finding with a failing regression test before its fix,
commit each coherent correction separately, then rerun Steps 2 and 3. If a
finding contradicts the approved durable protocol rather than its
implementation, stop and return to the user review gate; do not edit the spec
or invent a resolution during execution.

- [ ] **Step 5: Commit documentation and review-backed corrections**

```bash
git add README.md AGENTS.md
git diff --cached --check
git diff --cached --name-only
git commit -m "docs: document ledger protocol kernel"
```

Expected: this commit contains only truthful repository documentation. Review
corrections, if any, were already committed with their regression tests and
recorded RED/GREEN evidence.

- [ ] **Step 6: Fresh post-commit verification and handoff**

```bash
git status --short --branch
make check
FUZZ_TIME=5s make fuzz-protocol
git log --oneline main..HEAD
git diff --stat main...HEAD
```

Expected: clean branch and passing checks. Report local deterministic tests and
bounded fuzz separately from database, KMS, hosted CI, deployment, and live GCP
acceptance, none of which is run for this pure kernel.

Do not push or open a pull request until the user explicitly selects that gate.
