SHELL := /bin/sh

GO := go
BIN_DIR := $(CURDIR)/bin
APP := $(BIN_DIR)/chain-api
STATICCHECK_VERSION := $(shell tr -d '[:space:]' < .staticcheck-version)
GOVULNCHECK_VERSION := $(shell tr -d '[:space:]' < .govulncheck-version)
ENV_FILE ?= .env.local
MIGRATIONS_DIR ?= db/migrations
DBMATE := dbmate --env-file $(ENV_FILE) --migrations-dir $(MIGRATIONS_DIR)
COMPOSE := docker compose --env-file $(ENV_FILE)

.PHONY: setup tools fmt fmt-check vet staticcheck test test-race build vuln \
	generate generate-check check db-config db-up db-down db-logs migrate \
	migrate-status

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
