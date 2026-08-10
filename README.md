# Attribution Chain Application

The Go application repository for Attribution Chain. This foundation establishes
the reproducible local toolchain only; it does not implement domain or ledger
behavior and does not authorize a remote, cloud resource, deployment, or other
live GCP operation.

## Prerequisites

The local baseline is:

```text
Go:              1.26.5 (darwin/arm64)
dbmate:          2.35.0
Docker:          29.4.0
Staticcheck:      2026.1 (v0.7.0)
govulncheck:      1.6.0
```

Go 1.26 or newer, dbmate, and Docker are required. The repository pins
Staticcheck and govulncheck, so use the local binaries installed by:

```bash
make tools
```

The command writes only ignored repository-local artifacts under `./bin`.

## Commands

The Make targets are `setup`, `tools`, `fmt`, `fmt-check`, `vet`,
`staticcheck`, `test`, `test-race`, `build`, `vuln`, `generate`,
`generate-check`, `check`, `db-up`, `db-down`, `db-logs`, `migrate`, and
`migrate-status`.

All listed targets are operational. The database targets require Docker 29.4.0
and dbmate 2.35.0; container build and smoke commands remain unavailable until
their defining task commits their contracts.

The direct commands behind the wrappers are part of the repository contract:

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

## Run locally

Build and start the API with local-safe defaults:

```bash
make build
./bin/chain-api
```

The process listens on all interfaces at port `8080` by default. Override the
port only at the startup boundary when needed:

```bash
PORT=18080 ./bin/chain-api
curl --fail --silent --show-error http://127.0.0.1:18080/healthz
```

The health response is `{"status":"ok"}`. Send `SIGINT` or `SIGTERM` for a
bounded graceful shutdown. HTTP closes before telemetry flush, and logger
synchronization is attempted last. Telemetry is disabled by default, so local
startup does not require an OpenTelemetry Collector.

## Local PostgreSQL and migrations

The database workflow is local-only. Copy the committed template to an ignored
file and replace its password placeholder with a local-only value:

```bash
cp .env.example .env.local
```

`compose.yaml` runs PostgreSQL 18.4 and binds its configurable port only to
`127.0.0.1`; it is not a live database or a Cloud SQL resource. It persists
data in the named `postgres-data` Docker volume, mounted at the PostgreSQL 18
parent data path `/var/lib/postgresql`. `make db-down` stops and removes the
container and network but deliberately preserves that named volume. There is no
routine destructive reset target.

Use these commands after creating `.env.local`:

```bash
make db-up
make migrate-status
make migrate
make db-logs
make db-down
```

dbmate 2.35.0 reads `DATABASE_URL` from `.env.local` and uses
`db/migrations`. With no dbmate `.sql` files, `make migrate` succeeds with a
clear no-op message; after an approved schema task adds a migration, it invokes
dbmate and preserves any migration failure. No schema, domain table, ledger
table, or protocol migration exists yet. Relational migrations and deterministic
ledger replay are separate concerns.

## Local configuration and acceptance boundaries

Keep local secrets in ignored `.env` or `.env.*` files. `.env.example` is a
sanitized, trackable template; never commit `.env.local`, credentials, or local
state.

Startup configuration is read once at the process boundary. Values are trimmed;
an absent or empty value uses its default where one exists. Invalid values stop
startup before the application accepts traffic.

| Variable | Default | Validation |
| --- | --- | --- |
| `PORT` | `8080` | Base-10 integer from `1` through `65535`. |
| `CHAIN_LOG_LEVEL` | `info` | One of `debug`, `info`, `warn`, or `error`. |
| `CHAIN_SHUTDOWN_TIMEOUT` | `8s` | Go duration greater than `1s` and at most `9s`. |
| `CHAIN_OTEL_ENABLED` | `false` | Go boolean accepted by `strconv.ParseBool`. When true, `OTEL_EXPORTER_OTLP_ENDPOINT` is required. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | none | Required when `CHAIN_OTEL_ENABLED` is true. |
| `CHAIN_GCP_PROJECT_ID` | none | Required when `CHAIN_DEPLOYMENT_ENVIRONMENT` is `production`. |
| `CHAIN_DEPLOYMENT_ENVIRONMENT` | `local` | One of `local`, `development`, or `production`. |

Local command results establish only local tool or code behavior. PostgreSQL,
container, hosted-CI, and GCP acceptance are separate boundaries and require
their respective commands or authorization. This repository has no remote or
cloud authorization from this setup work.
