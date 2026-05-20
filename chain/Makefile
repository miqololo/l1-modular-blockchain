# chain/ — Cosmos SDK L1 with x/aiservice module

BINARY      := aid
PKG_PATH    := github.com/aios/aios
BUILD_DIR   := $(CURDIR)/bin
LDFLAGS     := -s -w \
  -X $(PKG_PATH)/cmd/$(BINARY)/cmd.Version=$(shell git describe --tags --always 2>/dev/null || echo dev) \
  -X $(PKG_PATH)/cmd/$(BINARY)/cmd.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.DEFAULT_GOAL := help

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the aid binary
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

.PHONY: install
install: ## go install the aid binary
	CGO_ENABLED=0 go install -ldflags="$(LDFLAGS)" ./cmd/$(BINARY)

.PHONY: test
test: ## Run unit + integration tests
	go test -race -count=1 -timeout 5m ./...

.PHONY: test-unit
test-unit:
	go test -race -count=1 -short ./...

.PHONY: test-integration
test-integration:
	go test -race -count=1 -timeout 10m -run Integration ./testutil/integration/...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run --timeout=5m ./...

.PHONY: fmt
fmt:
	gofmt -s -w .
	goimports -w -local github.com/aios .

.PHONY: proto-gen
proto-gen: ## Regenerate Go from proto files
	@echo "Generating proto bindings via buf"
	cd ../proto && buf generate

.PHONY: proto-lint
proto-lint:
	cd ../proto && buf lint

.PHONY: localnet
localnet: build ## Bootstrap a single-node devnet locally (outside docker)
	./scripts/init-devnet.sh

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
