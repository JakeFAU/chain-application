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
