.DEFAULT_GOAL := help

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: install
install: ## pnpm install
	pnpm install --frozen-lockfile

.PHONY: build
build: ## next build
	pnpm build

.PHONY: dev
dev: ## next dev
	pnpm dev

.PHONY: test
test: ## vitest run
	pnpm test

.PHONY: lint
lint: ## next lint + tsc
	pnpm lint
	pnpm type-check

.PHONY: fmt
fmt:
	pnpm exec prettier --write "src/**/*.{ts,tsx,css}" || true

.PHONY: e2e
e2e:
	pnpm test:e2e

.PHONY: clean
clean:
	rm -rf .next node_modules
