BINARY    := harness
BUILD_DIR := $(CURDIR)/bin

.DEFAULT_GOAL := help

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: lint
lint:
	go vet ./...

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
