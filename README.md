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
`generate-check`, and `check`.

`make tools`, `make fmt`, `make fmt-check`, `make vet`, `make staticcheck`,
`make test`, `make test-race`, `make generate`, and `make generate-check` are
operational. `make build` remains unavailable until Task 5 adds the application
composition root, so `make check` is also unavailable. Database, Compose, and
container commands remain unavailable until their defining tasks commit their
service and image contracts.

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

## Local configuration and acceptance boundaries

Keep local secrets in ignored `.env` or `.env.*` files. A future sanitized
`.env.example` may be committed; never commit credentials or local state.

Local command results establish only local tool or code behavior. PostgreSQL,
container, hosted-CI, and GCP acceptance are separate boundaries and require
their respective commands or authorization. This repository has no remote or
cloud authorization from this setup work.
