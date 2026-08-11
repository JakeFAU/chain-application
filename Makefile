SHELL := /bin/sh

GO := go
BIN_DIR := $(CURDIR)/bin
APP := $(BIN_DIR)/chain-api
STATICCHECK_VERSION := $(shell tr -d '[:space:]' < .staticcheck-version)
GOVULNCHECK_VERSION := $(shell tr -d '[:space:]' < .govulncheck-version)
GENERATED_OPENAPI := internal/httpapi/openapi.gen.go
OPENAPI_CONFIG := internal/httpapi/oapi-codegen.yaml
OPENAPI_SPEC := api/openapi.yaml
ENV_FILE ?= .env.local
MIGRATIONS_DIR ?= db/migrations
DBMATE := dbmate --env-file $(ENV_FILE) --migrations-dir $(MIGRATIONS_DIR)
COMPOSE := docker compose --env-file $(ENV_FILE)
IMAGE ?= chain-application:local
SMOKE_CONTAINER_PREFIX := chain-application-smoke
SMOKE_OWNERSHIP_LABEL := org.attribution-chain.container-smoke.owner
SMOKE_OWNERSHIP_TOKEN ?=
SMOKE_HOST_PORT := 18080
SMOKE_HEALTH_URL := http://127.0.0.1:$(SMOKE_HOST_PORT)/healthz
SMOKE_ATTEMPTS := 30
SMOKE_REQUEST_TIMEOUT_SECONDS := 2
SMOKE_RETRY_INTERVAL_SECONDS := 1
SMOKE_RESPONSE := {"status":"ok"}
FUZZ_TIME ?= 10s

.PHONY: setup tools fmt fmt-check vet staticcheck test test-race build vuln \
	generate generate-check check db-config db-up db-down db-logs migrate \
	migrate-status container-build container-smoke fuzz-protocol

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
	@unformatted="$$(git ls-files -z -- '*.go' | xargs -0 gofmt -l --)"; \
	test -z "$$unformatted" || { \
		echo "gofmt required for:" >&2; \
		printf '%s\n' "$$unformatted" >&2; \
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

generate-check:
	@temporary_directory="$$(mktemp -d)"; \
	temporary_output="$$temporary_directory/openapi.gen.go"; \
	cleanup() { \
		rm -f "$$temporary_output"; \
		rmdir "$$temporary_directory"; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 1' HUP INT TERM; \
	generator="$$( $(GO) tool -n oapi-codegen )"; \
	(cd "$$temporary_directory" && "$$generator" \
		-config "$(CURDIR)/$(OPENAPI_CONFIG)" "$(CURDIR)/$(OPENAPI_SPEC)"); \
	if ! cmp -s $(GENERATED_OPENAPI) "$$temporary_output"; then \
		echo "$(GENERATED_OPENAPI) is out of date; run make generate" >&2; \
		diff -u $(GENERATED_OPENAPI) "$$temporary_output" >&2 || true; \
		exit 1; \
	fi

check: fmt-check vet staticcheck test test-race build vuln generate-check

fuzz-protocol:
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateEventStructure$$' -fuzztime $(FUZZ_TIME)
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateRecordStructure$$' -fuzztime $(FUZZ_TIME)
	$(GO) test ./internal/ledger/v1 -run '^$$' \
		-fuzz '^FuzzValidateChainConsistency$$' -fuzztime $(FUZZ_TIME)

db-config:
	@test -f $(ENV_FILE) || { echo "copy .env.example to $(ENV_FILE) and replace local placeholders" >&2; exit 1; }

db-up: db-config
	$(COMPOSE) up -d --wait postgres

db-down: db-config
	$(COMPOSE) down

db-logs: db-config
	$(COMPOSE) logs postgres

migrate: db-config
	@if [ ! -d "$(MIGRATIONS_DIR)" ]; then \
		echo "migration directory does not exist: $(MIGRATIONS_DIR)" >&2; \
		exit 1; \
	elif [ -z "$$(find "$(MIGRATIONS_DIR)" -type f -name '*.sql' -print -quit)" ]; then \
		echo "no dbmate migration files in $(MIGRATIONS_DIR); nothing to apply"; \
	else \
		$(DBMATE) up; \
	fi

migrate-status: db-config
	$(DBMATE) status

container-build:
	docker build --build-arg BUILD_VERSION=$$(git rev-parse --short HEAD 2>/dev/null || echo devel) -t $(IMAGE) .

container-smoke: container-build
	@set -eu; \
	temporary_directory=""; \
	ownership_token="$(SMOKE_OWNERSHIP_TOKEN)"; \
	if [ -z "$$ownership_token" ]; then \
		temporary_directory="$$(mktemp -d "$${TMPDIR:-/tmp}/chain-application-smoke.XXXXXX")"; \
		ownership_token="$${temporary_directory##*.}"; \
	fi; \
	container_name="$(SMOKE_CONTAINER_PREFIX)-$$ownership_token"; \
	cleanup() { \
		observed_token="$$(docker inspect --format \
			'{{ index .Config.Labels "$(SMOKE_OWNERSHIP_LABEL)" }}' \
			"$$container_name" 2>/dev/null || true)"; \
		if [ "$$observed_token" = "$$ownership_token" ]; then \
			docker rm -f "$$container_name" >/dev/null 2>&1 || true; \
		fi; \
		if [ -n "$$temporary_directory" ]; then \
			rmdir "$$temporary_directory"; \
		fi; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 1' HUP INT TERM; \
	docker run --rm -d --name "$$container_name" \
		--label "$(SMOKE_OWNERSHIP_LABEL)=$$ownership_token" \
		-p 127.0.0.1:$(SMOKE_HOST_PORT):8080 $(IMAGE) >/dev/null; \
	attempt=1; \
	while [ "$$attempt" -le "$(SMOKE_ATTEMPTS)" ]; do \
		response="$$(curl --fail --silent --show-error \
			--max-time "$(SMOKE_REQUEST_TIMEOUT_SECONDS)" \
			"$(SMOKE_HEALTH_URL)" 2>/dev/null || true)"; \
		if [ "$$response" = '$(SMOKE_RESPONSE)' ]; then \
			echo "container health response: $$response"; \
			exit 0; \
		fi; \
		attempt=$$((attempt + 1)); \
		sleep "$(SMOKE_RETRY_INTERVAL_SECONDS)"; \
	done; \
	echo "container health check failed after $(SMOKE_ATTEMPTS) attempts" >&2; \
	docker logs "$$container_name" >&2 || true; \
	exit 1
