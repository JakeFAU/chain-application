# Application Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap a reproducible, Cloud Run-ready Go application foundation
with one OpenAPI-defined health endpoint, typed configuration, Zap and
OpenTelemetry lifecycle, local PostgreSQL/dbmate tooling, container packaging,
and credential-free CI—without implementing domain or ledger behavior.

**Architecture:** `cmd/chain-api` is a thin composition root. Typed config,
HTTP transport, observability, and process lifecycle live in focused
`internal/` packages. OpenAPI owns transport types; Zap writes JSON to
stdout/stderr; OpenTelemetry traces and metrics use an explicitly enabled
OTLP/gRPC exporter; PostgreSQL remains local tooling with no domain schema.

**Tech Stack:** Go 1.26.5, chi v5.3.1, oapi-codegen v2.8.0, Zap v1.28.0,
OpenTelemetry Go v1.45.0, otelhttp v0.70.0, Google Cloud trace propagator
v0.59.0, PostgreSQL 18.4, dbmate 2.35.0, Docker 29.4.0, GitHub Actions.

## Global Constraints

- Module path: `github.com/JakeFAU/chain-application`.
- Default branch: local `main`; do not create a GitHub remote.
- Before Task 1, commit only the reviewed ignore files, instructions, design,
  and this plan on `main`, then implement on an isolated
  `agent/application-foundation` worktree.
- Read root `../AGENTS.md`, repository `AGENTS.md`, and
  `docs/superpowers/specs/2026-08-10-application-foundation-design.md` first.
- `.gitignore` and `.dockerignore` already exist and must be reviewed before
  the first commit; do not weaken secret, credential, local-state, or artifact
  exclusions.
- Production behavior uses RED-GREEN-REFACTOR with the RED output recorded in
  the task report before implementation.
- Generated OpenAPI code is committed and never edited manually.
- Do not implement domain, endorsement, ledger, crypto, replay, OIDC, KMS,
  embedding, deployment, remote, or live GCP behavior.
- Do not add a Cloud Logging client, OTel log exporter, Grafana stack, ORM, DI
  framework, or extra service.
- No commit may contain `.env`, credentials, keys, local tools, test output,
  agent scratch, database state, or infrastructure state/plan artifacts.

---

## Pre-Execution Safety Baseline

Before any implementation task, run the ignore audit from Task 1 Step 1 plus:

```bash
git check-ignore -v .worktrees/application-foundation
git diff --check
git add .dockerignore .gitignore AGENTS.md docs
git diff --cached --check
git commit -m "chore: establish application repository"
```

Inspect `git diff --cached --name-only` before committing. The first commit on
`main` must contain only the two defensive ignore files, repository
instructions, design, and implementation plan. Then use the worktree workflow
to create `.worktrees/application-foundation` on branch
`agent/application-foundation`; all numbered tasks execute there.

---

### Task 1: Reproducible Go Toolchain and Command Contract

**Files:**

- Create: `go.mod`
- Create: `go.sum`
- Create: `.staticcheck-version`
- Create: `.govulncheck-version`
- Create: `Makefile`
- Create: `README.md`
- Modify: `.gitignore`
- Modify: `.dockerignore`
- Modify: `AGENTS.md`

**Interfaces:**

- Produces module `github.com/JakeFAU/chain-application` at Go 1.26.5.
- Produces pinned repository-local commands under `./bin`.
- Produces Make targets consumed by every later task: `tools`, `fmt`,
  `fmt-check`, `vet`, `staticcheck`, `test`, `test-race`, `build`, `vuln`,
  `generate`, `generate-check`, and `check`.

- [ ] **Step 1: Re-audit ignore behavior in the isolated worktree**

Run:

```bash
git check-ignore -v .env .env.local bin/staticcheck coverage.out \
  .superpowers/sdd/progress.md service-account-dev.json local.tfstate
```

Expected: every path is ignored by a specific defensive rule.

Run:

```bash
for tracked_path in \
  .env.example \
  api/openapi.yaml \
  internal/httpapi/openapi.gen.go \
  db/migrations/README.md \
  docs/superpowers/plans/example.md \
  testdata/fuzz/example; do
  if git check-ignore -q "$tracked_path"; then
    echo "unexpected ignore: $tracked_path" >&2
    exit 1
  fi
done
```

Expected: exit 0 with no output.

- [ ] **Step 2: Initialize and pin the module and generator**

Run:

```bash
go mod init github.com/JakeFAU/chain-application
go mod edit -go=1.26.5
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
```

Expected: `go.mod` records the exact module, Go version, and `tool` directive.

- [ ] **Step 3: Add exact local tool pins**

Create `.staticcheck-version`:

```text
v0.7.0
```

Create `.govulncheck-version`:

```text
v1.6.0
```

- [ ] **Step 4: Add the initial Make command contract**

Create `Makefile` with named tool modules and no `latest` references:

```makefile
SHELL := /bin/sh

GO := go
BIN_DIR := $(CURDIR)/bin
APP := $(BIN_DIR)/chain-api
STATICCHECK_VERSION := $(shell tr -d '[:space:]' < .staticcheck-version)
GOVULNCHECK_VERSION := $(shell tr -d '[:space:]' < .govulncheck-version)

.PHONY: setup tools fmt fmt-check vet staticcheck test test-race build vuln \
	generate generate-check check

setup: tools generate

tools: $(BIN_DIR)/staticcheck $(BIN_DIR)/govulncheck

$(BIN_DIR):
	mkdir -p $@

$(BIN_DIR)/staticcheck: .staticcheck-version | $(BIN_DIR)
	GOBIN=$(BIN_DIR) $(GO) install \
		honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

$(BIN_DIR)/govulncheck: .govulncheck-version | $(BIN_DIR)
	GOBIN=$(BIN_DIR) $(GO) install \
		golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || { \
		echo "gofmt required for:" >&2; \
		gofmt -l . >&2; \
		exit 1; \
	}

vet:
	$(GO) vet ./...

staticcheck: $(BIN_DIR)/staticcheck
	$< ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

build: | $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -o $(APP) ./cmd/chain-api

vuln: $(BIN_DIR)/govulncheck
	$< ./...

generate:
	$(GO) generate ./...

generate-check: generate
	git diff --exit-code

check: fmt-check vet staticcheck test test-race build vuln generate-check
```

Task 2 creates packages referenced by `build` and `generate`; before then,
verify only `make tools`.

- [ ] **Step 5: Document current and future commands honestly**

Create `README.md` with: prerequisites and exact versions; `make tools`;
the full target list; the fact that application targets become operational in
Tasks 2–7; local `.env` handling; no remote/cloud authorization; and separate
local, PostgreSQL, container, hosted-CI, and GCP acceptance boundaries.

Update `AGENTS.md` command status to name the now-operational tool installation
command while leaving application, database, generation, and container commands
marked unavailable until their creating tasks land.

- [ ] **Step 6: Verify pins and review the initial tracked set**

Run:

```bash
make tools
./bin/staticcheck -version
./bin/govulncheck --version
go tool oapi-codegen -version
git status --short
git diff --check --cached
```

Expected: exact pinned versions; no ignored artifacts in `git status`.

- [ ] **Step 7: Commit the reproducible toolchain**

Run:

```bash
git add AGENTS.md README.md Makefile go.mod go.sum .staticcheck-version \
  .govulncheck-version
git diff --cached --check
git commit -m "chore: add reproducible Go toolchain"
```

Expected: the commit exists on `agent/application-foundation` and contains no
ignored or sensitive artifact.

---

### Task 2: OpenAPI-Defined Health Boundary

**Files:**

- Create: `api/openapi.yaml`
- Create: `internal/httpapi/generate.go`
- Create: `internal/httpapi/oapi-codegen.yaml`
- Generate: `internal/httpapi/openapi.gen.go`
- Test: `internal/httpapi/server_test.go`
- Create: `internal/httpapi/server.go`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Produces `func NewHandler(server *Server) http.Handler`.
- Produces `type Server struct{}` implementing generated
  `StrictServerInterface`.
- Produces `GET /healthz -> 200 application/json {"status":"ok"}`.

- [ ] **Step 1: Add the authoritative OpenAPI contract and generator config**

Create `api/openapi.yaml`:

```yaml
openapi: 3.1.1
info:
  title: Attribution Chain API
  version: 0.1.0
paths:
  /healthz:
    get:
      operationId: getHealthz
      responses:
        "200":
          description: Service is healthy.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthzResponse"
components:
  schemas:
    HealthzResponse:
      type: object
      additionalProperties: false
      required: [status]
      properties:
        status:
          type: string
          enum: [ok]
```

Create `internal/httpapi/oapi-codegen.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/oapi-codegen/oapi-codegen/v2.8.0/configuration-schema.json
package: httpapi
output: openapi.gen.go
generate:
  models: true
  chi-server: true
  strict-server: true
```

Create `internal/httpapi/generate.go`:

```go
package httpapi

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.yaml
```

Run `go generate ./internal/httpapi` and commit generated output only after
reviewing its generated-file marker and imports.

- [ ] **Step 2: Write the failing real-router health test**

Create `internal/httpapi/server_test.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzReturnsContractResponse(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	NewHandler(&Server{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type");
		!strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var response map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 1 {
		t.Fatalf("response members = %d, want 1", len(response))
	}

	status, ok := response["status"]
	if !ok {
		t.Fatal("response is missing status")
	}

	var value string
	if err := json.Unmarshal(status, &value); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if value != "ok" {
		t.Fatalf("status body = %q, want %q", value, "ok")
	}
}
```

- [ ] **Step 3: Run RED**

Run: `go test ./internal/httpapi -run TestHealthzReturnsContractResponse -v`

Expected: compile failure because `NewHandler` and `Server` do not exist.

- [ ] **Step 4: Implement the strict handler and chi wiring**

Create `internal/httpapi/server.go`:

```go
package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct{}

const healthStatus = "ok"

var _ StrictServerInterface = (*Server)(nil)

func (*Server) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: healthStatus}, nil
}

func NewHandler(server *Server) http.Handler {
	router := chi.NewRouter()
	return HandlerFromMux(NewStrictHandler(server, nil), router)
}
```

Run `go mod tidy` after the generated and handwritten imports exist.

- [ ] **Step 5: Run GREEN and generation checks**

Run:

```bash
go test ./internal/httpapi -run TestHealthzReturnsContractResponse -v
go test ./internal/httpapi
go generate ./...
git diff --exit-code -- internal/httpapi/openapi.gen.go
```

Expected: all pass and generation is clean.

- [ ] **Step 6: Update command truth and commit**

Update README and AGENTS so format, vet, test, generation, and generation-check
commands are current; build remains unavailable until Task 5.

Run:

```bash
git add api internal/httpapi go.mod go.sum README.md AGENTS.md
git diff --cached --check
git commit -m "feat: add OpenAPI health boundary"
```

---

### Task 3: Typed Startup Configuration

**Files:**

- Test: `internal/config/config_test.go`
- Create: `internal/config/config.go`
- Modify: `README.md`

**Interfaces:**

- Produces `type LookupEnv func(string) (string, bool)`.
- Produces `func Load(lookup LookupEnv, buildVersion string) (Config, error)`.
- Produces `func (Config) Address() string`.
- Produces bounded `LogLevel` and `DeploymentEnvironment` types consumed by
  observability and `cmd/chain-api`.

- [ ] **Step 1: Write failing configuration boundary tests**

Create table-driven tests that call `Load` with a map-backed `LookupEnv` and
prove these exact claims:

```go
func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	config, err := Load(mapLookup(nil), "test-version")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", config.Port)
	}
	if config.Address() != ":8080" {
		t.Fatalf("Address = %q, want :8080", config.Address())
	}
	if config.LogLevel != LogLevelInfo {
		t.Fatalf("LogLevel = %q, want %q", config.LogLevel, LogLevelInfo)
	}
	if config.ShutdownTimeout != 8*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 8s", config.ShutdownTimeout)
	}
	if config.Telemetry.Enabled {
		t.Fatal("Telemetry.Enabled = true, want false")
	}
	if config.Telemetry.ServiceName != "attribution-chain-api" {
		t.Fatalf("ServiceName = %q", config.Telemetry.ServiceName)
	}
	if config.Telemetry.Version != "test-version" {
		t.Fatalf("Version = %q", config.Telemetry.Version)
	}
}
```

Add cases expecting an error for: port `0`, port `65536`, non-numeric port,
unsupported log level, invalid boolean, shutdown timeout `1s`, shutdown timeout
`10s`, unsupported environment, telemetry enabled without
`OTEL_EXPORTER_OTLP_ENDPOINT`, and production without
`CHAIN_GCP_PROJECT_ID`. Add a valid enabled case using endpoint
`http://localhost:4317` and project `attribution-chain-505000`.

`mapLookup` is test-only:

```go
func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
```

- [ ] **Step 2: Run RED**

Run: `go test ./internal/config -v`

Expected: compile failure because `Load`, `Config`, and typed constants do not
exist.

- [ ] **Step 3: Implement typed parsing and validation**

Create these types and constants in `internal/config/config.go`:

```go
type LookupEnv func(string) (string, bool)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type DeploymentEnvironment string

const (
	EnvironmentLocal       DeploymentEnvironment = "local"
	EnvironmentDevelopment DeploymentEnvironment = "development"
	EnvironmentProduction  DeploymentEnvironment = "production"
)

type Telemetry struct {
	Enabled     bool
	Endpoint    string
	ProjectID   string
	Environment DeploymentEnvironment
	ServiceName string
	Version     string
}

type Config struct {
	Port            uint16
	LogLevel        LogLevel
	ShutdownTimeout time.Duration
	Telemetry       Telemetry
}

func (config Config) Address() string {
	return net.JoinHostPort("", strconv.FormatUint(uint64(config.Port), 10))
}
```

Use named constants for every environment name and default. `Load` trims input,
uses defaults only when a variable is absent or empty, wraps parse errors with
the variable name, validates the allowed enums, enforces a shutdown duration
greater than one second and at most nine seconds, requires an endpoint when
telemetry is enabled, and requires a project ID in production. Do not call
`os.Getenv` inside this package.

- [ ] **Step 4: Run GREEN and broad package checks**

Run:

```bash
go test ./internal/config -v
go test ./...
go vet ./...
```

Expected: all pass.

- [ ] **Step 5: Document environment variables and commit**

Add an environment-variable table to README with exact defaults and validation
rules for `PORT`, `CHAIN_LOG_LEVEL`, `CHAIN_SHUTDOWN_TIMEOUT`,
`CHAIN_OTEL_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT`,
`CHAIN_GCP_PROJECT_ID`, and `CHAIN_DEPLOYMENT_ENVIRONMENT`.

Run:

```bash
git add internal/config README.md
git diff --cached --check
git commit -m "feat: add typed runtime configuration"
```

---

### Task 4: Zap and OpenTelemetry Runtime

**Files:**

- Test: `internal/observability/logging_test.go`
- Test: `internal/observability/runtime_test.go`
- Create: `internal/observability/logging.go`
- Create: `internal/observability/runtime.go`
- Create: `internal/observability/trace.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

- Produces `func New(ctx context.Context, cfg config.Config, options ...Option)
  (*Runtime, error)`.
- Produces `func (Runtime) Logger() *zap.Logger`.
- Produces `func (Runtime) WrapHTTP(http.Handler) http.Handler`.
- Produces `func (Runtime) Shutdown(context.Context) error`.
- Produces `func TraceFields(context.Context, string) []zap.Field`.

- [ ] **Step 1: Add exact observability dependencies**

Run:

```bash
go get github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator@v0.59.0
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.70.0
go get go.opentelemetry.io/otel@v1.45.0
go get go.opentelemetry.io/otel/sdk@v1.45.0
go get go.opentelemetry.io/otel/sdk/metric@v1.45.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.45.0
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.45.0
go get go.uber.org/zap@v1.28.0
```

- [ ] **Step 2: Write failing Zap JSON and trace-correlation tests**

Use a `bytes.Buffer` sink and assert one JSON object with:

```go
map[string]any{
	"severity": "WARNING",
	"message":  "bounded message",
}
```

Build a valid remote sampled `trace.SpanContext`, attach it to context, call
`TraceFields`, log once, and assert:

```text
logging.googleapis.com/trace=projects/attribution-chain-505000/traces/<trace-id>
logging.googleapis.com/spanId=<span-id>
logging.googleapis.com/trace_sampled=true
```

Also assert that invalid span context or empty project ID returns no correlation
fields.

- [ ] **Step 3: Write the failing disabled-runtime test**

Construct default config from Task 3, call `New`, wrap a handler, serve one
`httptest` request, call `Shutdown`, and assert no error and no network
dependency. Inject the logger sink through:

```go
type Option func(*options)

func WithLogSink(sink zapcore.WriteSyncer) Option
```

The test must pass an in-memory sink and must not install or mutate global OTel
providers.

- [ ] **Step 4: Run RED**

Run:

```bash
go test ./internal/observability -run 'Test(NewLogger|TraceFields|DisabledRuntime)' -v
```

Expected: compile failure because the observability runtime does not exist.

- [ ] **Step 5: Implement Zap production JSON**

Start from `zap.NewProductionEncoderConfig`, set `TimeKey` to `timestamp`,
`MessageKey` to `message`, `LevelKey` to `severity`, use RFC3339-nano time, and
encode levels exactly as `DEBUG`, `INFO`, `WARNING`, `ERROR`, and `CRITICAL`.
Write application logs to the injected sink or locked stdout; keep Zap internal
errors on locked stderr. Parse only Task 3's bounded log levels.

`TraceFields` reads the current span context and emits the three documented
Cloud Logging keys only when both span context and project ID are valid.

- [ ] **Step 6: Implement explicit, non-global OTel providers**

Store provider interfaces and a shutdown function on `Runtime`. For disabled
telemetry use `trace/noop` and `metric/noop` providers and create no exporter.
For enabled telemetry:

- build one resource by merging `resource.Default()` with namespace
  `attribution-chain`, service name from config, build version, and bounded
  deployment environment;
- create OTLP/gRPC trace and metric exporters from the explicit endpoint with
  insecure transport only when its URL host is loopback;
- use a batch trace processor and periodic metric reader;
- combine Google `CloudTraceOneWayPropagator`, W3C TraceContext, and Baggage so
  W3C extraction wins when both formats exist; and
- return joined shutdown errors without installing global providers.

`WrapHTTP` uses one bounded span name, `http.server.request`, and passes the
runtime's tracer provider, meter provider, and propagator explicitly to
`otelhttp.NewHandler`.

- [ ] **Step 7: Put OTel outside the real chi handler and run GREEN**

Change the HTTP construction interface to:

```go
func NewHandler(server *Server, wrap func(http.Handler) http.Handler) http.Handler
```

Build the strict chi handler first, then apply `wrap` once at the outside. Tests
pass an identity wrapper; the composition root later passes `Runtime.WrapHTTP`.

Run:

```bash
go test ./internal/observability -v
go test ./internal/httpapi -v
go test ./...
go vet ./...
```

Expected: all pass with no collector running.

- [ ] **Step 8: Commit**

Run:

```bash
git add internal/observability internal/httpapi go.mod go.sum
git diff --cached --check
git commit -m "feat: add application observability runtime"
```

---

### Task 5: Process Lifecycle and Composition Root

**Files:**

- Test: `internal/app/runtime_test.go`
- Create: `internal/app/runtime.go`
- Test: `internal/app/server_test.go`
- Create: `internal/app/server.go`
- Create: `cmd/chain-api/main.go`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Produces `func Run(context.Context, time.Duration, Dependencies) error`.
- Produces `func NewHTTPServer(string, http.Handler) *http.Server`.
- Produces a `chain-api` process that loads config, owns all dependencies, uses
  `signal.NotifyContext`, and shuts down HTTP before telemetry within one
  bounded deadline.

- [ ] **Step 1: Write failing lifecycle tests**

Define the dependency contract in the test first:

```go
type Dependencies struct {
	Server            *http.Server
	Listener          net.Listener
	ShutdownTelemetry func(context.Context) error
	SyncLogger        func() error
}
```

Use a real loopback listener and an HTTP handler that records when a request
has completed. Start `Run` in a goroutine, make one successful request, cancel
the parent context, and assert in order that:

1. `Run` returns;
2. the server no longer accepts requests;
3. telemetry shutdown was called with a live context; and
4. logger sync was attempted last.

Add cases proving `http.ErrServerClosed` is normal, a non-sentinel serve error
is returned, nil dependencies fail fast, and cleanup errors are joined rather
than discarded. Use channels and bounded contexts for coordination; do not use
fixed sleeps.

Write `internal/app/server_test.go` to assert the configured address and every
named timeout on the returned `http.Server`.

- [ ] **Step 2: Run RED**

Run:

```bash
go test ./internal/app -run 'Test(Run|NewHTTPServer)' -v
```

Expected: compile failure because the lifecycle package does not exist.

- [ ] **Step 3: Implement bounded shutdown ownership**

In `internal/app/runtime.go`, define one named
`telemetryShutdownReserve = time.Second`. Validate all dependencies and require
the configured total timeout to exceed the reserve. Run `Server.Serve` once.
On cancellation:

- create one total shutdown context;
- give HTTP shutdown `total - telemetryShutdownReserve`;
- then give telemetry the remaining bounded context;
- always attempt logger sync after telemetry;
- treat `http.ErrServerClosed` as success; and
- return `errors.Join` of independent serve/shutdown failures.

Logger sync errors caused by unsupported stdout/stderr sync are non-fatal, but
must not prevent prior cleanup. Keep this policy in the composition root rather
than teaching domain or transport code about terminal file descriptors.

In `internal/app/server.go`, use named constants for `ReadHeaderTimeout`,
`ReadTimeout`, `WriteTimeout`, and `IdleTimeout`; do not rely on the standard
library's zero-value timeouts.

- [ ] **Step 4: Run lifecycle GREEN and race checks**

Run:

```bash
go test ./internal/app -v
go test -race ./internal/app
```

Expected: all pass without arbitrary sleeps or leaked goroutines.

- [ ] **Step 5: Add the thin executable composition root**

Create `cmd/chain-api/main.go` with:

```go
var buildVersion = "devel"

func main() {
	os.Exit(run())
}
```

`run` must load configuration through `config.Load(os.LookupEnv,
buildVersion)`, construct observability, construct the strict API handler,
listen on the configured address, create the bounded HTTP server, derive a
signal context for `os.Interrupt` and `syscall.SIGTERM`, then call `app.Run`.
Startup failures are logged through Zap once it exists; configuration or logger
construction failures may use a bounded standard-library fallback on stderr.
No package-level mutable dependency or global OTel provider is allowed.

- [ ] **Step 6: Verify the binary and live health endpoint**

Run:

```bash
go mod tidy
make fmt
make test
make test-race
make build
PORT=18080 ./bin/chain-api &
app_pid=$!
trap 'kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true' EXIT
curl --fail --silent --show-error http://127.0.0.1:18080/healthz
kill -TERM "$app_pid"
wait "$app_pid"
trap - EXIT
```

Expected: curl returns `{"status":"ok"}`, SIGTERM exits cleanly, and no OTel
collector is needed with telemetry disabled.

- [ ] **Step 7: Update command truth and commit**

Update README startup instructions and AGENTS command status. Run:

```bash
git add cmd internal/app README.md AGENTS.md go.mod go.sum
git diff --cached --check
git commit -m "feat: add bounded application lifecycle"
```

---

### Task 6: Local PostgreSQL and dbmate Workflow

**Files:**

- Create: `compose.yaml`
- Create: `.env.example`
- Create: `db/migrations/README.md`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Produces loopback-only PostgreSQL 18.4 local development.
- Produces `db-up`, `db-down`, `db-logs`, `migrate`, and `migrate-status`
  targets using an ignored `.env.local` file.
- Produces no application schema and no Cloud SQL resource.

- [ ] **Step 1: Prove the local environment remains untracked**

Run:

```bash
git check-ignore -v .env.local
git check-ignore -q .env.example && exit 1 || true
```

Expected: `.env.local` is ignored and `.env.example` is trackable.

- [ ] **Step 2: Add the explicit Compose contract**

Create `.env.example` with non-secret local placeholders for
`POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_PORT`, and a
matching `DATABASE_URL` using `sslmode=disable`. Document that developers must
replace the placeholder in their ignored `.env.local`.

Create `compose.yaml` with one `postgres` service using exactly:

```yaml
image: postgres:18.4-bookworm@sha256:882236b897e39051d2368c5ccc6cda944904723506b2dfc97f2a8f5bc9afa382
```

Interpolate every database value from the environment, bind the host port to
`127.0.0.1`, use `pg_isready` for the health check, and mount a named volume at
`/var/lib/postgresql`, the PostgreSQL 18 image's version-aware data parent.
Do not expose the database on all interfaces and do not bake a password into
the Compose file.

- [ ] **Step 3: Add migration and database command contracts**

Create `db/migrations/README.md` explaining that no schema exists yet, dbmate
files use timestamp prefixes, schema design requires its own approved task, and
ledger replay is distinct from relational migration history.

Add to `Makefile`:

```makefile
ENV_FILE ?= .env.local
DBMATE := dbmate --env-file $(ENV_FILE) --migrations-dir db/migrations
COMPOSE := docker compose --env-file $(ENV_FILE)

db-config:
	@test -f $(ENV_FILE) || { echo "copy .env.example to $(ENV_FILE) and replace local placeholders" >&2; exit 1; }

db-up: db-config
	$(COMPOSE) up -d --wait postgres

db-down: db-config
	$(COMPOSE) down

db-logs: db-config
	$(COMPOSE) logs postgres

migrate: db-config
	$(DBMATE) up

migrate-status: db-config
	$(DBMATE) status
```

Add the new names to `.PHONY`. Do not add a routine destructive reset target.

- [ ] **Step 4: Validate Compose and exercise the real database**

Create `.env.local` from the example only as ignored local runtime state, then
replace its placeholder password. Run:

```bash
docker compose --env-file .env.local config --quiet
make db-up
make migrate-status
make migrate
make migrate-status
make db-down
git status --short
```

Expected: PostgreSQL becomes healthy, dbmate can connect and reports no domain
migrations, the container stops cleanly, and `.env.local` is absent from Git
status. If Docker is unavailable, record this as a separate unverified local-
service boundary rather than claiming success.

- [ ] **Step 5: Document and commit**

Document dbmate 2.35.0, Docker 29.4.0, the `.env.local` workflow, loopback-only
exposure, persistent named-volume behavior, and the fact that this is not a
live database or ledger schema.

Run:

```bash
git add .env.example compose.yaml db Makefile README.md AGENTS.md
git diff --cached --check
git commit -m "chore: add local PostgreSQL workflow"
```

---

### Task 7: Container, CI, Documentation, and Final Verification

**Files:**

- Create: `Dockerfile`
- Create: `.github/workflows/ci.yaml`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Produces a non-root, static, Cloud Run-compatible container.
- Produces credential-free pull-request and push CI.
- Makes repository prerequisites, setup, format, lint, test, race, build,
  generation, vulnerability, database, container, and local-run commands true.

- [ ] **Step 1: Add the pinned multi-stage container**

Create `Dockerfile` using these exact bases:

```dockerfile
FROM golang:1.26.5-bookworm@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599 AS build
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35
```

Copy `go.mod`/`go.sum` before source for cache locality. Build with
`CGO_ENABLED=0`, `-trimpath`, and:

```text
-ldflags=-s -w -X main.buildVersion=${BUILD_VERSION}
```

Copy only the binary into the final image, use `USER nonroot:nonroot`, expose
8080, and set an exec-form entrypoint. Do not include a shell, CA files copied
from the workspace, source, `.env`, docs, credentials, or local tools.

- [ ] **Step 2: Add reproducible container targets and smoke test**

Add named `IMAGE ?= chain-application:local` and targets:

```makefile
container-build:
	docker build --build-arg BUILD_VERSION=$$(git rev-parse --short HEAD 2>/dev/null || echo devel) -t $(IMAGE) .

container-smoke: container-build
	docker run --rm -d --name chain-application-smoke -p 127.0.0.1:18080:8080 $(IMAGE)
```

Implement cleanup with a shell trap in the actual target, poll `/healthz` with
a bounded retry loop, and fail if the expected JSON is not returned. Avoid a
fixed startup sleep and always stop the named container.

- [ ] **Step 3: Add credential-free CI with immutable action pins**

Create `.github/workflows/ci.yaml` for pushes and pull requests with minimal
`contents: read` permission, cancellation per workflow/ref, and a single Go
job using:

```yaml
- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
- uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
  with:
    go-version: "1.26.5"
    cache: true
```

Run `make tools`, `make check`, `docker compose --env-file .env.example config
--quiet`, and `docker build --build-arg BUILD_VERSION=${{ github.sha }} .`.
Do not authenticate to GCP, push an image, deploy, create infrastructure, or
use repository secrets.

- [ ] **Step 4: Make the full local command contract operational**

Finalize `make check` as the non-mutating aggregate of `fmt-check`, `vet`,
`staticcheck`, `test`, `test-race`, `build`, `vuln`, and `generate-check`.
Keep `fmt` and `generate` as explicit mutating developer commands. Add
`container-build`, `container-smoke`, and database targets to `.PHONY`.

Rewrite command-status sections in README and AGENTS so every listed command
matches the repository. Keep clear labels for checks that require Docker,
dbmate, network access to the vulnerability database, hosted GitHub Actions,
or future Cloud Run/GCP acceptance.

- [ ] **Step 5: Run the complete verification matrix**

Run from the repository root:

```bash
make fmt
make generate
git diff --check
make check
docker compose --env-file .env.example config --quiet
make container-smoke
git status --short
git diff --stat
git diff
```

Also inspect tracked paths defensively:

```bash
git ls-files | rg '(^|/)(\.env($|\.)|bin/|dist/|tmp/|\.local/|\.superpowers/|\.worktrees/)|\.(pem|key|p12|pfx|tfstate|tfplan)$' && exit 1 || true
git diff --cached --check
```

Expected: all applicable local checks pass, generated output is clean, the
container returns the health contract, and no ignored/sensitive artifact is
tracked. Hosted CI and live GCP remain unverified and unauthorized.

- [ ] **Step 6: Commit the completed foundation**

Run:

```bash
git add Dockerfile .github Makefile README.md AGENTS.md
git diff --cached --check
git commit -m "ci: add container and verification pipeline"
git status --short --branch
git log --oneline --decorate --max-count=8
```

Expected: clean `agent/application-foundation` worktree with coherent commits;
`main` remains the safety/design baseline until the user authorizes integration.

---

## Independent Completion Review

After all task-scoped reviews pass, dispatch a fresh reviewer with no
implementation responsibility to inspect the entire branch against this plan,
the design spec, both AGENTS files, the final diff from `main`, and verification
evidence. Fix any justified findings with RED-GREEN evidence where behavior
changes, rerun the complete matrix, and ask the reviewer to re-review. Do not
merge to `main`, create a remote, push, deploy, or provision cloud resources
without a new explicit authorization gate.
