# Application Foundation Design

## Goal

Bootstrap `chain-application` as an independent, reproducible Go repository
that proves its development, HTTP-contract, runtime-lifecycle, observability,
local-PostgreSQL, container, and CI boundaries without implementing
Attribution Chain domain behavior or freezing durable ledger protocol choices.

The Go module path is `github.com/JakeFAU/chain-application`. The local default
branch is `main`. This work does not create a GitHub repository or remote.

## Scope

The foundation provides:

- defensive `.gitignore` and `.dockerignore` policies before the first commit;
- Go 1.26.5 module and repository-pinned development tools;
- documented setup, format, vet, static-analysis, test, race, build,
  vulnerability, generation, database, and container commands;
- one authoritative OpenAPI source contract with reproducible generated Go
  transport code;
- a minimal `GET /healthz` endpoint that proves HTTP wiring but no dependency
  health or domain behavior;
- typed startup configuration, explicit process composition, signal handling,
  and bounded graceful shutdown;
- Zap as the sole application logger, emitting Cloud Logging-compatible JSON
  to stdout/stderr;
- OpenTelemetry traces and metrics with an OTLP exporter boundary that is
  disabled locally unless explicitly configured;
- a local PostgreSQL Docker Compose service and dbmate workflow with no ledger
  tables or protocol migrations; and
- credential-free CI and a locally buildable Cloud Run-compatible container.

The foundation does not implement endorsements, proposals, admission, ledger
events, hashing, canonical serialization, signatures, replay, projections,
OIDC, KMS, embeddings, a Cloud Run service, an OpenTelemetry sidecar, IAM,
deployment, or any live GCP mutation.

## Repository Safety

The ignore policy is created before Git initialization and before any commit.
It excludes secrets and local environment files while allowing a sanitized
`.env.example`; local tool binaries and build/test/fuzz/profile outputs;
database data and Compose runtime state; IDE and OS files; cloud credentials,
keys, certificates, and plan/state artifacts; and Superpowers scratch state.

Generated OpenAPI Go code, migrations, source contracts, design/plan documents,
workflow files, and lock/checksum files remain committable. The Docker build
context excludes Git metadata, documentation and planning scratch, local
configuration, credentials, test output, local tools, and database state while
retaining the source and generated code needed to build the service.

## Toolchain and Commands

`go.mod` records Go 1.26.5. Staticcheck 2026.1 (`v0.7.0`) and govulncheck
`v1.6.0` are pinned in committed version files and installed into ignored
`./bin` from their official Go modules. `oapi-codegen v2.8.0` is pinned through
the Go module's tool directive and invoked with `go tool oapi-codegen`.

A small `Makefile` exposes discoverable wrappers while preserving the direct
commands documented in `AGENTS.md`. `make check` is the broad local gate and
runs formatting verification, vet, Staticcheck, unit/integration-independent
tests, race tests, build, vulnerability scanning, OpenAPI regeneration checks,
and Compose validation. Live PostgreSQL and container smoke checks remain
explicit targets so their stronger environmental requirements are visible.

## HTTP Contract and Generated Code

`api/openapi.yaml` is the sole HTTP request/response contract. The initial
contract contains only `GET /healthz`, with a stable operation ID and a JSON
response whose status is `ok`. The endpoint is deliberately shallow: it proves
that the process can serve requests, not that PostgreSQL, Google Cloud, or any
future provider is healthy.

The generator and configuration are pinned. `oapi-codegen v2.8.0` generates
strict chi v5.3.1 server bindings from OpenAPI 3.1.1. Generated Go models and
server interfaces live under `internal/httpapi` with a generated-file marker
and are committed. A clean-generation check regenerates them and fails on a
diff.
Handlers convert transport values at the boundary and contain no domain policy.

## Runtime Composition

`cmd/chain-api` is a boring composition root. It loads and validates typed
configuration, constructs one Zap logger, initializes OpenTelemetry, builds the
HTTP handler, creates an explicit `http.Server`, opens the listener, and runs
the server under a signal-aware root context.

The runtime binds all interfaces on the configured `PORT`, defaults to port
8080 only for local development, and uses named bounded HTTP and shutdown
timeouts. The total graceful-shutdown budget defaults to eight seconds, with a
named one-second reserve for telemetry flush so the process remains below Cloud
Run's ten-second termination window. It propagates cancellation, treats normal
server closure as success, shuts down the server before owned exporters, and
reports startup/runtime failures once at the owning boundary. No goroutine
lacks an explicit owner and termination path.

## Configuration

Configuration is loaded once through an injected environment lookup and passed
as typed values. It includes the HTTP port, shutdown timeout, Zap level,
OpenTelemetry enabled flag, OTLP endpoint, and service name. Defaults are
explicit and local-safe. Invalid booleans, durations, ports, log levels, and an
enabled exporter without an endpoint fail before the listener opens.

No credentials or private values have defaults. Future provider settings remain
out of scope rather than being represented by unused configuration fields.

## Observability

Zap is the sole application logging API. The production logger emits one JSON
object per line to stdout/stderr using Cloud Logging-recognized `severity` and
`message` fields. Request logging is bounded and excludes bodies, credentials,
claims, signatures, identifiers with unbounded cardinality, and private
proposal/denial content. A current valid span context may add Cloud Logging
trace correlation.

OpenTelemetry v1.45.0 owns traces and metrics, with `otelhttp v0.70.0` wrapping
HTTP traffic. When telemetry is disabled, initialization performs no network
I/O and returns safe no-op providers. When enabled, the application exports
OTLP/gRPC to an explicitly configured endpoint; the future Cloud Run default is
`http://localhost:4317` for the Google-built Collector sidecar. The localhost
connection may be insecure because it never leaves the instance. Exporter
shutdown is bounded and joined with other owned cleanup errors.

Logs rely on Cloud Run's automatic stdout/stderr ingestion rather than an OTel
log exporter, logging agent, or Cloud Logging client. Grafana, Prometheus,
Tempo, and Loki are deferred; vendor-neutral OpenTelemetry and OTLP preserve
that later option.

## Local PostgreSQL and Migrations

Docker Compose defines one local-only PostgreSQL service using
`postgres:18.4-bookworm@sha256:882236b897e39051d2368c5ccc6cda944904723506b2dfc97f2a8f5bc9afa382`.
It requires an ignored local password, binds the configurable host port to
loopback, and uses a health check plus a named volume mounted at PostgreSQL
18's `/var/lib/postgresql` parent data path. The credentials are not valid
outside the disposable local service.

dbmate reads `DATABASE_URL` from ignored `.env.local`; `.env.example` provides
only the safe local value. Migrations live under `db/migrations`. Bootstrap adds
no domain or ledger schema. Commands validate Compose, start and stop the
database, inspect migration status, and prove that the empty migration history
can initialize a fresh local database.

Database readiness is not part of `/healthz`. Integration tests that require
PostgreSQL are kept distinct from unit tests and use real PostgreSQL rather than
SQLite or SQL mocks.

## Container and CI

The container build uses
`golang:1.26.5-bookworm@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599`
as its builder and
`gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35`
as its non-root runtime. It builds one static `chain-api` binary, contains no
shell or development tools in the runtime stage, listens through `PORT`, and
runs as a non-root user. The Docker build context is governed by
`.dockerignore`; no credentials or local state enter it.

Credential-free GitHub Actions run the same repository commands as local
development, verify generated output, and build the container without pushing
it. `actions/checkout v7.0.1` and `actions/setup-go v7.0.0` are pinned by full
commit SHA. The workflow requests only `contents: read` and no GCP identity
token.

## Testing and Acceptance

Production behavior is developed test-first. Required proofs include:

- configuration default and validation tests;
- exact health response and HTTP method/contract tests using `httptest` and
  the real chi router;
- Zap JSON field and log-level tests using an in-memory sink;
- telemetry-disabled tests that prove no exporter/network dependency;
- server cancellation and graceful-shutdown tests using real local listeners
  and condition-based synchronization rather than sleeps;
- OpenAPI regeneration producing no diff;
- Compose configuration validation and a fresh-database dbmate smoke test;
- container build and local health smoke; and
- the complete format, vet, Staticcheck, test, race, build, and govulncheck
  gate.

Local tests, live local-PostgreSQL checks, container smoke, hosted CI, and GCP
acceptance are reported separately. This slice has no GCP or deployment
acceptance because it performs no live cloud operation.
