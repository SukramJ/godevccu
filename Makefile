SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

GO            ?= go
GOLANGCI_LINT ?= golangci-lint
GOFUMPT       ?= gofumpt

export CGO_ENABLED := 0

BIN_DIR  := bin
BIN_NAME := godevccu
BIN      := $(BIN_DIR)/$(BIN_NAME)
MODULE   := github.com/SukramJ/godevccu

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w
GO_BUILD_FLAGS := -trimpath -ldflags="$(LDFLAGS)"

PYDEVCCU ?= ../pydevccu

.PHONY: help
help: ## show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: setup
setup: ## install developer tooling
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

.PHONY: data
data: ## copy device/paramset descriptions from pydevccu (PYDEVCCU=..)
	./script/copy_data.sh $(PYDEVCCU)/pydevccu

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: fmt
fmt: ## format the source tree
	$(GO) fmt ./...

.PHONY: vet
vet: ## run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## run golangci-lint
	$(GOLANGCI_LINT) run --timeout=5m

.PHONY: test
test: ## run all tests
	$(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: cover
cover: test ## generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

.PHONY: build
build: ## build the godevccu CLI into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GO_BUILD_FLAGS) -o $(BIN) ./cmd/godevccu

.PHONY: run
run: build ## start godevccu with sensible defaults
	$(BIN) -mode openccu -xml-rpc-port 2001 -json-rpc-port 8080 -defaults

.PHONY: clean
clean: ## remove build artefacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
