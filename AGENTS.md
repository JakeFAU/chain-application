# AGENTS.md

This file specializes the project-wide [`../AGENTS.md`](../AGENTS.md) for
`chain-application`. Read both files before changing this repository. The root
file remains authoritative for project purpose, ledger semantics, privacy,
cloud change gates, identity and key-management decisions, and workspace
safety. Do not restate, reinterpret, or weaken those rules here.

This file is the application-engineering contract. It defines how work is
planned, decomposed, implemented, tested, reviewed, and operated for the Go
service.

## Current Status and Scope

As of 2026-08-09, this directory contains only this file. It is not yet a Git
repository and has no Go module, source, tests, generated code, database schema,
local services, build, CI, deployment, or application artifact. Creating this
file does not authorize the broader scaffold.

After its separately approved bootstrap, this repository will own the Go 1.26+
application: authoritative domain policy, the HTTP API implementation, ledger
admission and replay behavior, and application-owned projections. It does not
own thin generated clients, frontend behavior, permanent GCP infrastructure,
or infrastructure deployment logic.

Cloud Run is the eventual default runtime and local PostgreSQL is the initial
database. No Cloud Run service, Cloud SQL instance, application service
account, deployer binding, GitHub WIF/IAM binding, system-signing KMS resource,
or deployment pipeline currently exists. The live Artifact Registry repository
and enabled project APIs are infrastructure-owned facts, not authorization to
deploy or mutate infrastructure from this repository.

## Engineering Priorities

Optimize for, in order:

1. correctness of durable protocol, cryptographic, and ledger behavior;
2. simple, explicit composition that can be reasoned about locally;
3. deterministic and independently testable domain behavior;
4. small interfaces owned by their consumers;
5. operational correctness on Cloud Run;
6. readable Go that follows standard-library conventions;
7. evidence-backed delivery through tests, static analysis, and review; and
8. performance only after measurement identifies a real constraint.

Do not trade a clear boundary for a framework abstraction, speculative
flexibility, or fewer lines of code. Do not build extension points for imagined
future requirements.

## Agent Workflow: Design, Plan, TDD, Verify

For any non-trivial feature, bug fix, behavior change, protocol change,
refactor spanning multiple responsibilities, or task likely to touch several
files, use the Superpowers workflow before implementation. When the skills are
available, invoke them explicitly; otherwise follow the same discipline
manually:

1. inspect the relevant repository state and existing conventions;
2. use `superpowers:brainstorming` to make boundaries and trade-offs explicit;
3. after the design is approved, use `superpowers:writing-plans` and write the
   plan under `docs/superpowers/plans/YYYY-MM-DD-<feature-name>.md`;
4. decompose the plan into independently reviewable slices with exact files,
   interfaces, tests, commands, and expected outcomes;
5. implement behavioral slices with `superpowers:test-driven-development` and
   RED-GREEN-REFACTOR; use an isolated worktree when concurrent or risky work
   makes branch isolation valuable; and
6. use `superpowers:verification-before-completion` and fresh command output
   before claiming the slice or task is complete.

A plan is not a narrative wishlist. Each task must identify what it consumes,
what it produces, the files it changes, the failing test that establishes the
behavior, the minimum implementation required to make that test pass, and the
verification commands that prove the result.

Prefer small coherent commits. A reviewer should be able to reject one planned
slice without invalidating unrelated work. Do not mix opportunistic refactors,
format churn, dependency upgrades, or unrelated cleanup into a behavioral
change.

Documentation-only edits, generated outputs, and mechanical configuration
changes do not require artificial test-first ceremony. They still require the
smallest meaningful validation and a final diff review.

### TDD Is the Default

For production behavior, the required cycle is RED-GREEN-REFACTOR:

- **RED:** write one focused test for the desired behavior and run it. Confirm
  it fails for the expected reason rather than because the test is broken.
- **GREEN:** implement the minimum coherent production change that makes the
  test pass. Do not add adjacent features because they seem convenient.
- **REFACTOR:** improve names, structure, and duplication only while the tests
  remain green.

If production behavior was written before its regression test, do not merely
backfill a test that passes against the existing implementation. Re-establish
that the test can detect the missing or broken behavior.

Prefer tests of observable behavior over tests of mocks. Use fakes or test
implementations at explicit interfaces when isolation is necessary; do not
mock internal call choreography merely to preserve an implementation detail.

### Verification Before Completion

Never report a command, test, build, migration, lint result, replay result, or
cloud acceptance result as passing unless it was run in the current work and
its output was checked.

Before declaring implementation complete, run and report, as applicable:

1. the targeted test that drove the changed behavior;
2. relevant package tests;
3. `go fmt ./...` and `go vet ./...`;
4. `./bin/staticcheck ./...`;
5. `go test ./...`;
6. `go test -race ./...` for concurrency, shared state, caches, cryptographic
   key state, or other race-sensitive changes;
7. `go build ./...`;
8. relevant migration, replay, generation, integration, fuzz, and protocol
   checks; and
9. a final diff and generated-file review.

Separate local unit/integration evidence from live PostgreSQL, hosted CI, GCP,
or deployment acceptance. Evidence from one layer is not evidence for another.

## Prerequisites and Command Status

The local baseline verified for this bootstrap request is:

```text
Go:              1.26.5 (darwin/arm64)
dbmate:          2.35.0
Docker:          29.4.0
Staticcheck:      2026.1 (v0.7.0)
govulncheck:      1.6.0
```

Go 1.26 or newer, dbmate, and Docker are required. The bootstrap must record Go
1.26.5 in `go.mod` and CI; upgrades are intentional compatibility changes.

Pin Staticcheck in a committed `.staticcheck-version` and govulncheck in a
committed `.govulncheck-version`. CI and repository commands must use those
versions rather than ambient installations or `latest`. Upgrade either tool
intentionally after reviewing its release notes and running the full repository
checks.

No setup, formatting, linting, test, build, migration, OpenAPI-generation, or
local-service command exists yet because the files those commands require do
not exist. Do not report any such command as passing before bootstrap.

The only currently meaningful prerequisite checks are:

```bash
go version
dbmate --version
docker --version
```

The next approved foundation slice must make these application commands work
and document any intentional replacement in the README and this file:

```bash
go mod download
go fmt ./...
go vet ./...
./bin/staticcheck ./...
go test ./...
go test -race ./...
go build ./...
./bin/govulncheck ./...
```

Install the pinned tools from their official Go modules; the bootstrap may wrap
these commands without changing them:

```bash
STATICCHECK_VERSION="$(tr -d '[:space:]' < .staticcheck-version)"
GOBIN="$PWD/bin" go install \
  honnef.co/go/tools/cmd/staticcheck@"$STATICCHECK_VERSION"

GOVULNCHECK_VERSION="$(tr -d '[:space:]' < .govulncheck-version)"
GOBIN="$PWD/bin" go install \
  golang.org/x/vuln/cmd/govulncheck@"$GOVULNCHECK_VERSION"
```

Pin repository-executed development tools where their supported distribution
mechanism permits it. Record and check minimum host-tool versions otherwise. A
`Makefile` or scripts may provide discoverable wrappers, but direct commands
remain the documented contract and wrappers must not change their meaning.
Docker Compose, dbmate, and OpenAPI-generation commands remain unavailable
until the bootstrap defines and commits their service names, paths, environment
contract, database image pin, dbmate strategy, and pinned generator; update
this section in the same change that creates them.

## Go Design Rules

Write ordinary Go. Prefer the standard library and small dependencies with a
clear boundary over framework-specific abstractions.

### Composition and Dependency Injection

Use composition through plain structs and constructors. The composition root
belongs in `cmd/<service>`; it creates concrete adapters, injects them into
application services, constructs the HTTP server, and owns process lifetime.

Do not introduce a dependency-injection framework, service locator, global
container, package-level mutable singleton, or implicit init-time wiring.
Dependencies must be visible in constructors or function parameters.

Constructors should normally return concrete types. Consumers may accept small
interfaces when they need substitution, isolation, or multiple implementations.
Do not return interfaces merely to hide a concrete type.

### Interfaces

Define an interface in the package that consumes the behavior, not in the
package that implements it. Keep it as small as the consumer requires.
One-method interfaces are healthy when they describe a real capability.

Do not create an interface for every struct. Introduce one because there is a
boundary that matters: persistence, time, randomness, signing, identity,
network I/O, model/provider access, telemetry emission, or another external or
nondeterministic dependency.

Prefer capability-shaped names and methods over generic repositories or
managers. If an interface grows because unrelated callers need unrelated
methods, split the consumers rather than growing a universal abstraction.

Accept interfaces where substitution is useful; return concrete values from
constructors unless callers genuinely need polymorphism.

### Packages

Package names should describe a cohesive capability and remain short. Avoid
`util`, `utils`, `common`, `shared`, `helpers`, `base`, and catch-all `types`
packages. A package that becomes a junk drawer is an architectural bug.

Keep packages as flat as practical. Do not create a directory merely to obtain
an architectural diagram that looks impressive. Split a package when it has
multiple responsibilities, unstable dependency direction, or files that no
longer fit comfortably in working context.

Files that change together should usually live together. Do not mechanically
separate every type into its own file or create Java-style layers whose only
job is forwarding calls.

### Context, Concurrency, and Lifecycle

Pass `context.Context` as the first parameter for request-scoped cancellation,
deadlines, and tracing. Do not store a context on long-lived structs and do not
use context as an untyped parameter bag for application data.

The caller owns goroutine lifetime unless an API explicitly documents another
owner. Every goroutine must have a termination path. Prefer synchronous code
until concurrency provides a demonstrated benefit. Bound worker counts,
queues, retries, and fan-out; never create unbounded goroutines from
user-controlled input.

Shared mutable state requires an explicit synchronization strategy and race
tests. Prefer immutable values, request-local state, and database-backed truth
over process-local coordination.

Channels are synchronization tools, not a default abstraction layer. Do not
return a channel unless cancellation, closure, ownership, and backpressure are
obvious to the caller.

### Errors and Panics

Return errors rather than panicking across package boundaries. Panics are for
programmer invariants that make continued execution invalid; the HTTP recovery
boundary exists for containment, not as normal error handling.

Wrap errors with `%w` when preserving identity matters and use `errors.Is` or
`errors.As` at decision boundaries. Create sentinel or typed errors only when
callers are expected to branch on them. Human-readable error strings are not a
machine contract.

Map domain/application errors to HTTP responses in one transport boundary.
Do not let PostgreSQL, KMS, OIDC, or provider-specific errors leak into API
contracts.

### Data Ownership and Determinism

Make mutation and ownership obvious. Do not return internal mutable slices,
maps, or buffers when a caller could corrupt package state. Copy at trust
boundaries when required.

Do not rely on map iteration order for protocol behavior, hashing,
serialization, replay, tests, or user-visible deterministic output. Inject
clock and randomness dependencies when their values affect behavior that must
be reproducible.

## Suggested Repository Layout

The following is a target shape, not authorization to scaffold empty packages:

```text
chain-application/
├── AGENTS.md
├── README.md
├── go.mod
├── go.sum
├── api/
│   └── openapi.yaml
├── cmd/
│   └── chain-api/
│       └── main.go
├── internal/
│   ├── application/       # use cases; transaction/idempotency boundaries; ports
│   ├── claim/             # pure claim/domain types and invariants
│   ├── endorsement/       # pure endorsement/admission domain policy
│   ├── ledger/            # versioned events, admission rules, deterministic replay
│   ├── crypto/            # canonical crypto/signature helpers; extra scrutiny/tests
│   ├── httpapi/           # chi router, handlers, middleware, transport mapping
│   ├── postgres/          # PostgreSQL adapters implementing consumer-owned ports
│   ├── oidc/              # identity/authentication adapter
│   ├── kms/               # system-signing adapter; no protocol policy
│   ├── embedding/         # application-owned embedding provider adapter
│   ├── config/            # typed startup configuration and validation
│   └── observability/     # OpenTelemetry and Zap process/adaptor setup
├── db/
│   └── migrations/
├── docs/
│   └── superpowers/
│       └── plans/
└── testdata/              # repository-wide immutable fixtures only when justified
```

Start flatter if the implementation is smaller. Create a package only when it
has real code and a coherent responsibility. If a capability becomes large,
split by capability before inventing generic technical layers.

`cmd/chain-api` is a composition root, not an application package. It should be
boring: load validated configuration, construct dependencies, start the server,
handle signals, and shut down cleanly.

`internal/application` coordinates use cases and owns interfaces required from
adapters. It must not become a dumping ground for domain policy. If a use case
can be expressed as a pure domain operation, keep it in the domain package.

Adapters may import application/domain contracts. Domain packages must not
import HTTP, PostgreSQL, GCP, environment, telemetry, or adapter packages.

## Cryptography and `internal/crypto`

`internal/crypto` is high-risk code. Treat changes there as protocol/security
work even when the diff is small.

Do not invent cryptographic primitives, signature schemes, canonicalization
rules, key derivation, hashing constructions, randomness strategies, or key
formats. Use standard-library or explicitly approved, well-reviewed primitives.
Protocol-level choices remain subject to the root-file approval gates.

Keep cryptographic helpers narrow and deterministic where possible. Separate
pure operations from KMS/network access. `internal/crypto` may define pure
operations and validation; `internal/kms` implements cloud-backed key access
behind application-owned interfaces.

Every meaningful cryptographic change requires stronger evidence than ordinary
application code. As applicable, include:

- known-answer or published test vectors for approved primitives/formats;
- positive and negative signature/verification tests;
- malformed, truncated, oversized, wrong-key, wrong-algorithm, and
  wrong-version inputs;
- deterministic serialization/canonicalization fixtures;
- round-trip and cross-version compatibility tests where a durable format
  exists;
- fuzz tests for parsers, decoders, canonicalization, and verification entry
  points that accept untrusted bytes;
- race tests for caches, key rotation state, or shared verifier/signer state;
- explicit tests that secrets and private key material are not serialized,
  logged, or returned in errors; and
- regression corpus entries for every discovered parsing or verification bug.

Coverage percentage alone is not acceptance evidence. Crypto tests must target
failure modes and invariants.

Never log private keys, secret material, raw bearer tokens, KMS plaintext,
unbounded attacker-controlled bytes, or sensitive payloads. Use constant-time
comparison APIs when equality itself protects a secret; do not hand-roll them.

## HTTP API

The default HTTP router for this service is `github.com/go-chi/chi/v5` unless a
concrete requirement justifies another router. Prefer chi because it composes
with `net/http`, keeps handlers easy to exercise with `httptest`, and avoids
making a framework-specific context the application's dependency boundary.

Do not add Gin merely from familiarity. If Gin is proposed later, document the
requirement it satisfies that chi plus `net/http` does not.

### Router and Handler Boundaries

The HTTP layer translates protocol to application calls. It does not contain
business policy.

Handlers should:

1. read bounded request input;
2. decode and validate the OpenAPI transport contract;
3. obtain authenticated identity from approved middleware/context values;
4. call one application use case;
5. translate typed results/errors to the documented HTTP contract; and
6. return.

Handlers must not open database transactions directly, call KMS directly,
embed domain rules, or perform hidden background work.

Keep router construction explicit. Middleware should be visible in route setup
rather than hidden behind package initialization.

Use standard `net/http` types at the boundary where practical. Keep chi-specific
APIs in `internal/httpapi`; domain and application packages must not import chi.

### Middleware

Middleware must have one clear responsibility. Typical concerns include request
IDs, panic recovery, authentication, bounded access logging, tracing, request
size limits, and timeout/cancellation propagation.

Middleware order is part of behavior and requires tests when order affects
security, identity, recovery, tracing, or responses.

Do not log request/response bodies by default. Do not place credentials,
tokens, raw claims, signatures, private proposal/denial content, or unbounded
user-controlled values in logs or trace attributes.

### OpenAPI

OpenAPI is authoritative for HTTP requests and responses. Keep one reviewed
source contract under `api/`; generation must be reproducible with a pinned
generator and a repository command selected during the contract-workflow
design.

Generated files must carry a generated-file marker, are never edited by hand,
and must be regenerated from the source contract. CI should fail when
regeneration produces an uncommitted diff.

Generated Go server types/adapters may live here. Generated Go, Python, and
TypeScript consumer clients belong in their respective `chain-clients`
repositories and remain thin. Review HTTP compatibility independently from
durable ledger/replay compatibility.

Transport structs are not domain models. Convert explicitly at the boundary.
Do not expose database rows as HTTP contracts.

## Cloud Run Runtime Contract

Cloud Run is the production runtime assumption for application design, but
this repository does not own permanent Cloud Run or IAM infrastructure.

The service must be designed as a stateless, horizontally replicated process:

- listen on the port supplied through `PORT`; a local default may be provided
  only for development;
- bind the HTTP listener so it is reachable by the container runtime, not only
  loopback;
- do not assume instance affinity, singleton execution, or in-memory state
  survives between requests;
- do not use the container filesystem as durable storage;
- assume multiple requests may execute concurrently in one process and make
  all shared state safe for that model;
- bound database connection pools, provider concurrency, memory use, request
  bodies, queues, retries, and fan-out;
- reuse outbound clients and connection pools rather than constructing them per
  request;
- honor request cancellation and deadlines through all network/database calls;
- do not rely on goroutines continuing after an HTTP response for durable work;
  durable asynchronous work requires an explicitly designed external execution
  mechanism;
- validate configuration before accepting traffic;
- construct an explicit `http.Server` with intentional transport timeouts
  compatible with the Cloud Run request contract; do not hide production server
  behavior behind a bare package-level `http.ListenAndServe`;
- use signal-aware process cancellation and perform bounded graceful shutdown
  of the HTTP server, database pool, telemetry exporters, and owned workers; and
- construct one production Zap JSON logger, inject it explicitly, and write
  single-line structured logs to stdout/stderr so Cloud Run's built-in log
  collection sends them to Cloud Logging without an application Cloud Logging
  client or logging agent; and
- instrument process/transport/use-case/adapter boundaries with OpenTelemetry
  and export traces and metrics over OTLP through the approved Google Cloud
  ingestion path. On Cloud Run, the current target is a Google-built
  OpenTelemetry Collector sidecar; its deployment and IAM remain
  infrastructure-owned and separately approved.

Startup must not silently mutate permanent infrastructure or run irreversible
production migrations. Schema migration and infrastructure deployment are
separate controlled actions.

Health endpoints, if added, must be cheap, bounded, privacy-safe, and explicit
about what they prove. Do not turn a health check into an unbounded dependency
fan-out or leak configuration details.

Cloud Run configuration such as concurrency, CPU/memory, min/max instances,
timeouts, service accounts, VPC access, Cloud SQL attachment, IAM, and rollout
policy belongs to `chain-infra`. Application changes may document requirements
for those settings but may not mutate them without the root-file approval gate.

## Configuration and Secrets

Define typed configuration with defaults and boundary validation. Read the
environment once during startup and inject explicit values or interfaces.
Runtime-tunable model names, endpoints, timeouts, retry policies, dimensions,
sampling, and feature flags belong in configuration, not business logic.

Prefer one configuration struct assembled at the composition root over repeated
`os.Getenv` calls throughout the codebase. Production-required settings must
fail fast when absent or invalid; do not silently substitute a local default.

Use ADC or environment/managed secret injection for credentials. Never commit
`.env.local`, tokens, service-account keys, private signing material, database
credentials, or generated authentication files. A committed `.env.example` may
contain only safe local placeholders.

The initial embedding defaults are Vertex AI `gemini-embedding-2` with required
output dimensionality `768` and the three formats defined by the root file.
Keep them centralized and symmetric behind the provider boundary.

## PostgreSQL and Transactions

Use the Docker Compose `postgres` service for initial development. Do not add a
Cloud SQL dependency or permanent cloud resource merely to make local
development look production-like. Keep `DATABASE_URL` in ignored local
configuration.

When schema work begins, put ordered dbmate migrations under `db/migrations`.
Migrations must build a clean database from the complete migration history and
must not depend on manual state. Never edit an already shared migration to
rewrite history; add a forward migration. Destructive migrations, irreversible
data transformations, and ledger-affecting schema choices require review and
explicit approval.

Transaction boundaries belong to application use cases, not individual helper
methods. A repository/adapter must not secretly start independent transactions
when atomicity is required across operations.

Keep SQL explicit and observable. Avoid an ORM unless a demonstrated
requirement outweighs the additional abstraction and migration/query
ambiguity. Database rows are persistence representations, not domain or API
types.

Test migration reproducibility and ledger replay separately. Database tables,
indexes, projections, graph views, search documents, and scores are derived
state unless an explicitly approved protocol design says otherwise.

## Testing Strategy

Tests are architecture feedback. If a package is painful to test without a web
of mocks, first question the package boundary.

### Unit Tests

Keep unit tests next to the package they exercise. Prefer table-driven tests
when several cases express the same behavior clearly; do not force every test
into a table when a named scenario reads better.

Test exported behavior through the package boundary when practical. Use
same-package tests when access to unexported invariants materially improves the
proof; do not expose implementation details solely for tests.

Use deterministic fakes for clocks, randomness, repositories, signers,
providers, and external services through consumer-owned interfaces. Avoid
sleep-based tests and wall-clock timing assertions.

### HTTP Tests

Exercise handlers and middleware with `net/http/httptest` and the actual chi
router. Assert observable status, headers, bodies, authentication behavior,
request limits, cancellation, and error mapping. Do not test router behavior by
mocking chi internals.

Contract tests must confirm that generated/OpenAPI behavior and implemented
routes stay aligned.

### PostgreSQL Integration Tests

Repository behavior and transaction semantics must be tested against real
PostgreSQL, not a SQLite substitute or SQL mock. Keep these tests isolated from
unit tests and make setup/cleanup deterministic.

Migrations must be tested from an empty database. Where relevant, also test
upgrade paths from supported prior schema states.

### Replay and Protocol Tests

Replay tests require stable fixtures and exact deterministic expectations.
Include tests for ordering, duplicate/idempotent inputs, supported historical
versions, rejected events, revocations/retractions, and any state transition
that affects durable meaning.

When a protocol bug is found, keep the smallest reproducing event stream as a
permanent regression fixture.

### Fuzz and Race Testing

Use Go fuzzing where untrusted structured bytes or combinatorial parser state
can expose bugs: crypto/protocol parsing, canonicalization, identifiers,
decoders, and other compact pure functions are prime candidates.

Fuzz failures become committed deterministic regression tests or corpus seeds.
Fuzzing complements, rather than replaces, explicit edge-case tests.

Run the race detector for concurrency-sensitive changes and periodically across
the full repository. A passing unit suite without race evidence is insufficient
for shared-state changes.

## Ledger, Replay, and Truth Boundaries

Only accepted endorsements are eligible for ledger admission; proposals and
denials stay private and off-ledger. Claims, observations, identity bindings,
system admission, and derived confidence retain distinct types and provenance.
A valid signature or admission event must never be presented as proof that the
claim content is true.

Replay through event `N` must be a deterministic function of ordered versioned
events, approved protocol rules, algorithm/policy versions, and explicit
configuration. Pure replay paths must not read wall-clock time, environment,
network state, databases other than the supplied event stream, uncontrolled
randomness, or unstable map iteration. Inject clocks and other nondeterminism,
record the inputs needed for reproduction, and define stable ordering.

Accepted history is append-only. Retraction, revocation, contradiction,
deactivation, redaction, and tombstone behavior is represented by later events,
not silent mutation. Preserve enough data to verify historical signatures and
to explain every score-bearing result using contributing events, graph
structure, identity mappings, snapshot, algorithm/version, parameters, and
evaluation policy.

Do not select canonical serialization, hash construction, event envelopes,
ordering/concurrency rules, durable schemas, or compatibility behavior merely
by choosing convenient Go or PostgreSQL defaults.

## Observability

OpenTelemetry is the instrumentation boundary for traces and metrics. Zap is
the sole application logging API. Construct the root `*zap.Logger` under
`internal/observability`, configure production JSON, and inject it explicitly;
do not maintain a parallel `log/slog` application path or add another logging
facade. Standard-library or third-party log output may be bridged into Zap at a
bounded adapter boundary.

Zap writes single-line JSON to stdout/stderr. Use Cloud Logging-recognized
fields such as `severity` and `message`, and add
`logging.googleapis.com/trace` from a valid current trace context when request
log correlation is available. Rely on Cloud Run's built-in container-log
collection by default; do not add a Cloud Logging client, logging agent, or
duplicate OpenTelemetry log export without a measured reliability or volume
requirement.

Instrument meaningful process, transport, use-case, database, provider, and
replay boundaries with OpenTelemetry. Export traces and metrics through a
configured OTLP path; Cloud Run does not ingest those signals merely because
the application emits them. The initial production direction is the
Google-built OpenTelemetry Collector as a Cloud Run sidecar, exporting to
Google Cloud Trace and Monitoring. Application code owns vendor-neutral
instrumentation and OTLP configuration; `chain-infra` owns the sidecar, IAM,
and backend wiring behind its separate authorization gates.

Do not add a Grafana-native, Prometheus, Tempo, or Loki pipeline during the
application bootstrap. OpenTelemetry and OTLP preserve the option to add a
Grafana-native backend later when an operating requirement justifies it.

Keep pure domain and replay calculations free of telemetry side effects;
callers record their outcomes. Flush owned OpenTelemetry exporters during
bounded shutdown. Attempt Zap synchronization on orderly shutdown where useful,
but do not fail shutdown solely because syncing stdout/stderr is unsupported by
the local runtime.

Telemetry must be bounded and privacy-safe. Never record credentials, tokens,
private keys, raw proposal or denial payloads, raw claim content, signatures,
unnecessary personal data, or unbounded user-controlled values. Record error
classes and approved bounded decision reasons rather than sensitive payloads.

Log an error at the boundary that owns the decision or response. Avoid every
layer logging and wrapping the same failure, producing three copies of one bad
day in Cloud Logging.

## Dependency Policy

Every new third-party dependency needs a concrete reason. Prefer the standard
library when it provides a clear, maintained solution.

Before adding a dependency, identify:

- the capability it provides;
- why existing code or the standard library is insufficient;
- the package boundary that contains it;
- its effect on testing, security, startup, and Cloud Run operation; and
- whether it becomes part of an API or durable compatibility surface.

Do not add a framework, queue, cache, ORM, DI container, logging facade,
configuration framework, retry framework, or generic helper library because it
might be useful later.

Router and logging choices are explicit exceptions already decided here: chi
v5 is the default HTTP router, and Zap is the sole application logger.

## Git and Generated Files

After this file is accepted, repository initialization may be proposed as part
of the approved bootstrap: initialize an independent repository with default
branch `main`. Do not create or guess a GitHub remote; ask for the repository
URL and stable numeric repository ID when a remote is actually required.

Keep commits small and coherent, preserve unrelated user work, and do not
rewrite history or force-push without explicit authorization. Commit source
contracts, migrations, reproducible tool/configuration files, and generated
outputs only when project policy requires those outputs. Never commit build
artifacts, local databases, coverage output, compose volumes, credentials,
secrets, or tool caches.

Generated files are reviewed as generated artifacts, not hand-edited source.
When generation changes, review both the source contract and the generated diff.

## Stop and Escalate

Present a recommendation and trade-offs before implementing decisions about:

- the Go module path or remote repository identity;
- ledger wire formats, canonical serialization, hashing, event schemas,
  ordering/concurrency, or replay compatibility;
- cryptographic primitives, system-signing lifecycle, historical key
  discovery, or user signing-key custody;
- identity/trust semantics, endorsement consent/admission, or the distinction
  between attributable claims and facts;
- deletion, redaction, tombstone, immutable-ledger privacy, or retention;
- breaking OpenAPI changes or generated-client compatibility;
- destructive migrations or state that cannot be rebuilt from the ledger;
- permanent GCP resources, IAM, WIF, KMS, Cloud SQL, Cloud Run, deployment, or
  any infrastructure change owned by `chain-infra`;
- a new service, queue, cache, framework, ORM, DI container, or material
  dependency without a demonstrated boundary or requirement; and
- a proposed exception to the TDD, crypto-testing, or deterministic-replay
  rules for production behavior.

For this bootstrap, acceptance of this file and approval of the next slice must
precede repository initialization or scaffolding. Remote creation,
infrastructure plan/apply, deployment, and live acceptance remain separate
authorization gates. After bootstrap, ordinary implementation already in an
approved request does not require an extra ceremonial gate.
