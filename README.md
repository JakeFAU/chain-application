# Attribution Chain Application

The Go application repository for Attribution Chain. This foundation proves a
reproducible toolchain, OpenAPI health boundary, typed runtime configuration,
observability and process lifecycle, local PostgreSQL workflow, static
container, and credential-free CI. It also contains a pure version 1 ledger
protocol kernel. It does not implement ledger persistence or admission,
application domain behavior, or authorize a remote, cloud resource, deployment,
or live GCP operation.

## Ledger protocol kernel (v1)

The CDDL schema in [`protocol/ledger/v1/ledger.cddl`](protocol/ledger/v1/ledger.cddl),
together with the approved normative design, is the language-neutral protocol
authority. The pure Go implementation is `internal/ledger/v1` (package
`ledgerv1`). It accepts only deterministic CBOR and requires decoded values to
re-encode exactly to their input bytes.

The kernel distinguishes the event digest (the exact ordered event body) from
the record digest (the exact admitted record body, including declared signer
metadata and signature bytes). Version 1 supports semantic replay only for the
genesis `ledger_initialized` event. It can structurally inspect unknown events
and advance a structurally consistent chain, but semantic replay stops for an
unsupported event.

Version 1 fixes its SHA-256 digest and P-256/ASN.1-DER metadata identifiers;
they are not runtime configuration. The committed golden vectors under
`protocol/ledger/v1/testdata` provide exact binary and hex conformance evidence.
Run bounded parser fuzzing with:

```bash
make fuzz-protocol
```

Signature bytes are structurally opaque in this kernel. It does not establish
DER validity, low-S policy, key resolution, or cryptographic signature
authenticity. PostgreSQL ledger schema and persistence, live admission, KMS
signing, non-genesis domain events, HTTP/API behavior, and all hosted, cloud,
deployment, and live acceptance remain outside this slice.

## Prerequisites

The locally validated baseline is:

```text
Go:              1.26.6 (darwin/arm64)
dbmate:          2.35.0
Docker:          29.4.0
Staticcheck:      2026.1 (v0.7.0)
govulncheck:      1.6.0
```

Go 1.26 or newer, Git, Make, and a POSIX shell are required for the core
repository commands. Docker 29.4.0 and Docker Compose are required for the
database and container workflows; container smoke also requires `curl`.
dbmate 2.35.0 is required for migration commands.

Install the repository-pinned Staticcheck and govulncheck binaries under the
ignored `./bin` directory:

```bash
make tools
```

Initial tool installation needs network access to the Go module proxy.

## Commands

Every listed target is operational:

| Target | Contract and external requirements |
| --- | --- |
| `make setup` | Installs pinned tools and regenerates OpenAPI code; generation mutates the committed output when the source contract changed. |
| `make tools` | Installs pinned Staticcheck and govulncheck under ignored `./bin`; initial installation needs network access. |
| `make fmt` | Formats Go source in place. |
| `make fmt-check` | Checks formatting without rewriting source. |
| `make vet` | Runs `go vet ./...`. |
| `make staticcheck` | Runs the pinned Staticcheck. |
| `make test` | Runs the Go test suite. |
| `make test-race` | Runs the Go test suite with the race detector. |
| `make build` | Builds the CGO-disabled `./bin/chain-api` with trim paths. |
| `make vuln` | Runs pinned govulncheck; vulnerability database access may require network access. |
| `make generate` | Regenerates the OpenAPI Go binding in place. |
| `make generate-check` | Generates to a temporary file and checks only the committed OpenAPI output for drift. |
| `make check` | Runs `fmt-check`, vet, Staticcheck, tests, race tests, build, govulncheck, and `generate-check` without rewriting tracked source. |
| `make fuzz-protocol` | Runs bounded Go fuzzing of the version 1 structural event, record, and chain validators; it does not cryptographically verify signatures. |
| `make db-config` | Checks that the selected ignored `ENV_FILE` exists; it is the shared prerequisite for database targets. |
| `make db-up` | Starts the loopback-only PostgreSQL Compose service and waits for health; requires Docker. |
| `make db-down` | Stops the Compose service without deleting its named volume; requires Docker. |
| `make db-logs` | Reads PostgreSQL Compose logs; requires Docker. |
| `make migrate` | Applies dbmate migrations, or reports a successful no-op while no migration SQL exists; requires dbmate and the local database. |
| `make migrate-status` | Reads dbmate migration status; requires dbmate and the local database. |
| `make container-build` | Builds the pinned static, non-root image; requires Docker and may need network access for uncached bases or modules. |
| `make container-smoke` | Builds and runs the image on `127.0.0.1:18080`, polls the exact health contract with bounded retries and a per-request deadline, and always removes its container. |

The direct core commands behind the wrappers are also part of the repository
contract:

```bash
go mod download
go fmt ./...
go vet ./...
./bin/staticcheck ./...
go test ./...
go test -race ./...
go build ./...
./bin/govulncheck ./...
go generate ./...
```

## Run locally

Build and start the API with local-safe defaults in one terminal:

```bash
make build
./bin/chain-api
```

The process listens on all interfaces at port `8080` by default. To use another
port, start the process in one terminal:

```bash
PORT=18080 ./bin/chain-api
```

Then verify it from a second terminal:

```bash
curl --fail --silent --show-error http://127.0.0.1:18080/healthz
```

The exact health response is `{"status":"ok"}`. Send `SIGINT` or `SIGTERM` to
begin bounded shutdown. The process attempts graceful HTTP shutdown first,
then telemetry shutdown, and logger synchronization last. An active handler
that exceeds the reserved HTTP drain window can still be running when the
bounded drain ends and later cleanup begins. Telemetry is disabled by default,
so local startup does not require an OpenTelemetry Collector.

When telemetry is enabled, `CHAIN_OTEL_TRACE_SAMPLE_RATIO` controls new root
traces and defaults to `1.0`. Valid sampled and unsampled upstream decisions
are honored through parent-based sampling. The application supplies this
policy explicitly. When telemetry is enabled, startup rejects the presence of
`OTEL_TRACES_SAMPLER` or `OTEL_TRACES_SAMPLER_ARG` before the OpenTelemetry SDK
is constructed; use `CHAIN_OTEL_TRACE_SAMPLE_RATIO` instead.

Each served request emits one bounded `httpRequest` log entry carrying only
`requestMethod`, `status`, and `latency`. Unregistered methods are recorded as
`OTHER`, and request URLs, query strings, headers, bodies, remote addresses,
and user agents are never logged. When `CHAIN_GCP_PROJECT_ID` is set and the
request carries a valid span context, the entry also carries
`logging.googleapis.com/trace`, `spanId`, and `trace_sampled` so Cloud Logging
links the entry to its trace.

## Local PostgreSQL and migrations

The database workflow is local-only. Copy the committed template to an ignored
file:

```bash
cp .env.example .env.local
```

Replace the password placeholder in both `POSTGRES_PASSWORD` and the password
component of `DATABASE_URL`; the two values must match. Percent-encode any
URI-reserved characters in the `DATABASE_URL` password.

`compose.yaml` runs PostgreSQL 18.4 and binds its configurable port only to
`127.0.0.1`; it is not a live database or a Cloud SQL resource. It persists
data in the named `postgres-data` Docker volume, mounted at PostgreSQL 18's
parent data path `/var/lib/postgresql`. `make db-down` removes the container and
network but deliberately preserves that volume. There is no routine
destructive reset target.

Use these commands after creating `.env.local`:

```bash
make db-config
make db-up
make migrate-status
make migrate
make db-logs
make db-down
```

dbmate reads `DATABASE_URL` from `.env.local` and uses `db/migrations`. With no
dbmate `.sql` files, `make migrate` succeeds with a clear no-op message; after
an approved schema task adds a migration, it invokes dbmate and preserves any
migration failure. No schema, domain table, ledger table, or protocol migration
exists yet. Relational migrations and deterministic ledger replay are separate
concerns.

Compose syntax can be validated without starting PostgreSQL:

```bash
docker compose --env-file .env.example config --quiet
```

## Container

Build the default local image or override its tag with `IMAGE`:

```bash
make container-build
IMAGE=example/chain-application:test make container-build
make container-smoke
```

The multi-stage build pins the Go 1.26.6 builder and distroless Debian 12
runtime by digest. It builds with `CGO_ENABLED=0`, trim paths, stripped symbols,
and the current short Git revision as the service version. The final image runs
as `nonroot:nonroot`, exposes port 8080, and receives only `chain-api` from the
builder.

## Local configuration

Keep local secrets in ignored `.env` or `.env.*` files. `.env.example` is a
sanitized, trackable database template; never commit `.env.local`, credentials,
or local state. The application does not load dotenv files automatically.

Startup configuration is read once at the process boundary. Values are trimmed;
an absent or empty value uses its default where one exists. Invalid values stop
startup before the application accepts traffic.

| Variable | Default | Validation |
| --- | --- | --- |
| `PORT` | `8080` | Base-10 integer from `1` through `65535`. |
| `CHAIN_LOG_LEVEL` | `info` | One of `debug`, `info`, `warn`, or `error`. |
| `CHAIN_SHUTDOWN_TIMEOUT` | `8s` | Go duration greater than `1s` and at most `9s`. |
| `CHAIN_OTEL_ENABLED` | `false` | Go boolean accepted by `strconv.ParseBool`. When true, `OTEL_EXPORTER_OTLP_ENDPOINT` is required. |
| `CHAIN_OTEL_TRACE_SAMPLE_RATIO` | `1.0` | Finite number from `0` through `1`; applies to new root traces while valid upstream sampled and unsampled decisions are honored. |
| `OTEL_TRACES_SAMPLER` | unsupported | Rejected when telemetry is enabled; use `CHAIN_OTEL_TRACE_SAMPLE_RATIO`. |
| `OTEL_TRACES_SAMPLER_ARG` | unsupported | Rejected when telemetry is enabled; use `CHAIN_OTEL_TRACE_SAMPLE_RATIO`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | none | Required when `CHAIN_OTEL_ENABLED` is true. |
| `CHAIN_GCP_PROJECT_ID` | none | Required when `CHAIN_DEPLOYMENT_ENVIRONMENT` is `production`. |
| `CHAIN_DEPLOYMENT_ENVIRONMENT` | `local` | One of `local`, `development`, or `production`. |

## CI and acceptance boundaries

GitHub Actions runs `make tools`, `make check`, Compose configuration
validation, and a container build on pushes and pull requests. The workflow has
only `contents: read`, persists no checkout credential, uses no repository
secrets or GCP authentication, and does not push, deploy, or provision.

Local core checks, local PostgreSQL, local container smoke, hosted GitHub
Actions, and Cloud Run/GCP acceptance are separate evidence boundaries. Hosted
CI has not run until the repository exists on GitHub and a workflow run
completes. No live Cloud Run or GCP acceptance is authorized or established by
this foundation.
