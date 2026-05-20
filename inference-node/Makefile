BINARY    := inferenced
BUILD_DIR := $(CURDIR)/bin

.DEFAULT_GOAL := help

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build inferenced binary
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

.PHONY: test
test: ## Run tests
	go test -race -count=1 ./...

.PHONY: lint
lint:
	golangci-lint run --timeout=5m ./...

.PHONY: fmt
fmt:
	gofmt -s -w .
	goimports -w -local github.com/aios .

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
