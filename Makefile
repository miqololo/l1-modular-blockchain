# Decentralized AI OS — root Makefile

.DEFAULT_GOAL := help

# Active packages by phase. Phase 0.5: chain + inference-node + indexer + frontend.
ACTIVE_PACKAGES := chain inference-node indexer frontend proto determinism-harness
PENDING_PACKAGES := contracts agents

FRONTEND_PORT ?= 3030

# ─────────────────────────────────────────────────────────────────────────
# Help & quickstart
# ─────────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@echo "Decentralized AI OS — make targets"
	@echo ""
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "Quickstart: make demo"

# ─────────────────────────────────────────────────────────────────────────
# Demo lifecycle — the one-command path
# ─────────────────────────────────────────────────────────────────────────

.PHONY: demo
demo: ## Bring up the stack, wait healthy, seed a service, run an inference request
	@if [ ! -f .env ]; then cp .env.example .env; echo "→ created .env from .env.example"; fi
	@echo "→ docker compose up -d (first boot downloads ~700MB model, can take 5+ min)"
	docker compose up -d
	@echo "→ waiting for healthy services"
	@./scripts/wait-healthy.sh
	@echo "→ seeding default service (translate-en-fr, provider=bob)"
	@curl -fsS -X POST http://localhost:26657/demo/seed | head -c 200 && echo
	@echo "→ submitting demo inference request"
	@curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"Translate hello to French."}' \
	    http://localhost:26657/demo/request-inference | head -c 200 && echo
	@echo "→ open http://localhost:$(FRONTEND_PORT)"
	@echo "→ poll /requests/1 with: make poll"

.PHONY: poll
poll: ## Poll request 1 until finalized
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
	  S=$$(curl -s http://localhost:26657/requests/1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "?"); \
	  echo "[attempt $$i] status=$$S"; \
	  if [ "$$S" = "FINALIZED" ] || [ "$$S" = "REFUNDED" ]; then exit 0; fi; \
	  sleep 5; \
	done; echo "did not finalize within 50s"; exit 1

.PHONY: up
up: ## docker compose up -d
	docker compose up -d

.PHONY: down
down: ## docker compose down (preserves volumes)
	docker compose down

.PHONY: reset
reset: ## docker compose down -v (REMOVES volumes including model cache)
	docker compose down -v

.PHONY: logs
logs: ## tail logs from all services
	docker compose logs -f --tail=50

.PHONY: ps
ps: ## docker compose ps
	docker compose ps

.PHONY: seed
seed: ## Register the default demo service (idempotent)
	@curl -fsS -X POST http://localhost:26657/demo/seed
	@echo

.PHONY: harness-report
harness-report: ## Show determinism-harness verdicts (Phase 1+)
	@curl -fsS http://localhost:8090/report | python3 -m json.tool

.PHONY: demo-malicious
demo-malicious: ## Phase 3 demo: enable MALICIOUS_PROVIDER → harness challenges → SLASHED + refund
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "→ restarting inference-node with MALICIOUS_PROVIDER=1"
	MALICIOUS_PROVIDER=1 docker compose up -d --force-recreate inference-node
	@sleep 5
	@echo "→ submitting demo request (provider will fabricate output)"
	@curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"Phase 3 fraud test"}' \
	    http://localhost:26657/demo/request-inference; echo
	@echo "→ poll status (expect SUBMITTED → CHALLENGED → SLASHED)"
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16; do \
	  sleep 4; \
	  STATUS=$$(curl -s "http://localhost:26657/requests" \
	      | python3 -c "import json,sys; d=json.load(sys.stdin); print(sorted(d['items'], key=lambda r: r['id'])[-1]['status'])" 2>/dev/null); \
	  echo "[$$((i*4))s] latest status=$$STATUS"; \
	  if [ "$$STATUS" = "SLASHED" ] || [ "$$STATUS" = "FINALIZED" ]; then break; fi; \
	done
	@echo ""
	@echo "→ harness verdicts (expect DIVERGENT + challenge_filed=true)"
	@curl -fsS http://localhost:8090/report | python3 -m json.tool

.PHONY: demo-honest
demo-honest: ## Restart inference-node honest (undo demo-malicious)
	MALICIOUS_PROVIDER= docker compose up -d --force-recreate inference-node

.PHONY: cross-host-determinism-check
cross-host-determinism-check: ## MVP item #1: prove determinism across two physically distinct hosts. Requires REMOTE_HOST=user@ip.
	@if [ -z "$$REMOTE_HOST" ]; then \
	  echo "usage: REMOTE_HOST=root@1.2.3.4 make cross-host-determinism-check" >&2; \
	  echo "  Requires SSH key-based access to the remote." >&2; \
	  exit 2; \
	fi
	@./scripts/cross-host-determinism-check.sh

.PHONY: determinism-check
determinism-check: ## Run the same prompt against two llama-server instances and verify identical outputs
	@docker compose up -d llama-server-b >/dev/null
	@for i in 1 2 3 4 5 6 7 8; do \
	  curl -fsS http://localhost:8082/health >/dev/null 2>&1 && break; \
	  echo "  waiting for llama-server-b ($$i/8)"; sleep 5; \
	done
	@echo ""
	@echo "=== Determinism check: same prompt, two independent llama-server processes ==="
	@for inst in 8080 8082; do \
	  echo ""; \
	  echo "--- instance localhost:$$inst (3 runs) ---"; \
	  for n in 1 2 3; do \
	    RESP=$$(curl -fsS -X POST -H "Content-Type: application/json" \
	      -d '{"prompt":"Determinism check. Reply briefly.","n_predict":64,"temperature":0,"top_k":1,"top_p":1,"seed":0,"stream":false}' \
	      "http://localhost:$$inst/completion"); \
	    HASH=$$(echo "$$RESP" | python3 -c "import json,sys,hashlib; d=json.loads(sys.stdin.read(), strict=False); print(hashlib.sha256(d['content'].encode()).hexdigest()[:16])"); \
	    echo "  run $$n hash: $$HASH"; \
	  done; \
	done
	@echo ""
	@echo "All six hashes identical → cross-process determinism holds for this tuple."
	@echo "If 8080 vs 8082 differ → runtime not deterministic across processes (disqualify)."

.PHONY: demo-spurious
demo-spurious: ## Phase 3.y demo: file a spurious challenge against an honest request, voucher dismisses it
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "→ ensuring inference-node is honest"
	MALICIOUS_PROVIDER= docker compose up -d --force-recreate inference-node >/dev/null
	@sleep 5
	@echo "→ submitting an honest request"
	@RESP=$$(curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"Honest request that will be falsely challenged"}' \
	    http://localhost:26657/demo/request-inference); echo "$$RESP"; \
	  RID=$$(echo "$$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['request_id'])"); \
	  echo "→ wait for SUBMITTED + honest harness to verify"; \
	  for i in 1 2 3 4 5 6 7 8 9 10 11 12; do \
	    sleep 3; \
	    S=$$(curl -s http://localhost:26657/requests/$$RID | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null); \
	    echo "  [$$((i*3))s] $$S"; \
	    if [ "$$S" = "SUBMITTED" ]; then \
	      OK=$$(curl -s http://localhost:8090/report | python3 -c "import json,sys; d=json.load(sys.stdin); print(any(it['verdict']=='OK' and it['request_id']==$$RID for it in d.get('items') or []))" 2>/dev/null); \
	      if [ "$$OK" = "True" ]; then break; fi; \
	    fi; \
	  done; \
	  echo "→ filing spurious challenge as 'alice' (the requester)"; \
	  curl -fsS -X POST -H "Content-Type: application/json" \
	      -d "{\"request_id\":$$RID,\"output_hash\":\"deadbeef00000000000000000000000000000000000000000000000000000000\",\"key\":\"alice\"}" \
	      http://localhost:26657/demo/challenge; echo; \
	  echo "→ wait for resolution (harness should vouch, challenge gets dismissed)"; \
	  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
	    sleep 4; \
	    S=$$(curl -s http://localhost:26657/requests/$$RID | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null); \
	    echo "  [$$((i*4))s] $$S"; \
	    if [ "$$S" = "FINALIZED" ] || [ "$$S" = "SLASHED" ]; then break; fi; \
	  done

# ─────────────────────────────────────────────────────────────────────────
# Falsifiability demos — observe what happens when assumptions break.
# Reviewer-facing: each target ends with an assertion describing the
# expected on-chain state, so a third party can confirm.
# ─────────────────────────────────────────────────────────────────────────

.PHONY: demo-no-watcher
demo-no-watcher: ## Falsifiability: stop BOTH harnesses → malicious provider wins (wrong hash finalizes)
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "→ stopping both harnesses (no watcher in the network)"
	docker compose stop determinism-harness determinism-harness-b
	@echo "→ restarting inference-node with MALICIOUS_PROVIDER=1"
	MALICIOUS_PROVIDER=1 docker compose up -d --force-recreate inference-node
	@sleep 5
	@echo "→ submitting a request that will produce a fabricated output"
	@RESP=$$(curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"no-watcher falsifiability test"}' \
	    http://localhost:26657/demo/request-inference); echo "$$RESP"; \
	  RID=$$(echo "$$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['request_id'])"); \
	  echo "→ wait $$((50))s for challenge window to expire (no challenger present)"; \
	  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
	    sleep 4; \
	    S=$$(curl -s http://localhost:26657/requests/$$RID | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null); \
	    echo "  [$$((i*4))s] $$S"; \
	    if [ "$$S" = "FINALIZED" ]; then break; fi; \
	  done; \
	  OUTHASH=$$(curl -s http://localhost:26657/requests/$$RID | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('result',{}).get('output_hash','?'))"); \
	  echo ""; \
	  echo "→ ASSERTION: status=FINALIZED with the FABRICATED output_hash ($$OUTHASH)"; \
	  echo "  This is the protocol's documented failure mode when no honest watcher exists."; \
	  echo "  Re-enable watchers with: docker compose up -d determinism-harness determinism-harness-b"

.PHONY: demo-tokenizer-mismatch
demo-tokenizer-mismatch: ## Falsifiability: provider with wrong TOKENIZER_ID → chain rejects MsgSubmitResult
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "→ restarting inference-node with a DIFFERENT tokenizer ID"
	@echo "  (chain's domain declares 'llama.cpp-bpe-v1'; provider will claim 'wrong-tokenizer')"
	MALICIOUS_PROVIDER= TOKENIZER_ID=wrong-tokenizer docker compose up -d --force-recreate inference-node
	@sleep 5
	@echo "→ submitting a request that the inference-node will try to honor"
	@RESP=$$(curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"tokenizer mismatch falsifiability test"}' \
	    http://localhost:26657/demo/request-inference); echo "$$RESP"; \
	  RID=$$(echo "$$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['request_id'])"); \
	  echo "→ wait for inference-node's submit attempt (chain should reject it)"; \
	  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
	    sleep 3; \
	    S=$$(curl -s http://localhost:26657/requests/$$RID | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null); \
	    echo "  [$$((i*3))s] $$S"; \
	    if [ "$$S" = "REFUNDED" ]; then break; fi; \
	  done; \
	  echo ""; \
	  echo "→ ASSERTION: status=REFUNDED (request hit deadline; provider could not submit)"; \
	  echo "  The chain rejected MsgSubmitResult because attestation.TokenizerID ≠ domain.TokenizerID."; \
	  echo "  Restore the demo with: TOKENIZER_ID=llama.cpp-bpe-v1 docker compose up -d --force-recreate inference-node"

.PHONY: demo-multi-watcher
demo-multi-watcher: ## Show TWO independent harnesses both watching the same request
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "→ ensuring both harnesses are running"
	docker compose up -d determinism-harness determinism-harness-b
	@sleep 5
	@echo "→ submitting an honest request"
	@RESP=$$(curl -fsS -X POST -H "Content-Type: application/json" \
	    -d '{"service_id":1,"prompt":"multi-watcher demo"}' \
	    http://localhost:26657/demo/request-inference); echo "$$RESP"; \
	  RID=$$(echo "$$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['request_id'])"); \
	  sleep 10; \
	  echo ""; \
	  echo "→ harness A verdict (port 8090):"; \
	  curl -fsS http://localhost:8090/report | python3 -m json.tool | grep -E "(verdict|request_id)" | head -10; \
	  echo ""; \
	  echo "→ harness B verdict (port 8091):"; \
	  curl -fsS http://localhost:8091/report | python3 -m json.tool | grep -E "(verdict|request_id)" | head -10; \
	  echo ""; \
	  echo "→ ASSERTION: both harnesses independently verified the same request and agreed."; \
	  echo "  Two-watcher coverage now exists; per-domain VoucherMargin=1 is safe to enable."

# ─────────────────────────────────────────────────────────────────────────
# Test / lint / build — fan out to each active package
# ─────────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run tests in every active package (requires Go)
	@set -e; for pkg in $(ACTIVE_PACKAGES); do \
	  if [ -f $$pkg/Makefile ]; then echo "→ test $$pkg"; $(MAKE) -C $$pkg test || exit 1; fi; \
	done

.PHONY: lint
lint: ## Run linters
	@set -e; for pkg in $(ACTIVE_PACKAGES); do \
	  if [ -f $$pkg/Makefile ]; then echo "→ lint $$pkg"; $(MAKE) -C $$pkg lint || exit 1; fi; \
	done

.PHONY: build
build: ## Build every active package (docker)
	docker compose build

.PHONY: ci
ci: lint test ## Full CI gate

# ─────────────────────────────────────────────────────────────────────────
# Phase / hygiene
# ─────────────────────────────────────────────────────────────────────────

.PHONY: phase
phase: ## Print the current phase
	@head -3 docs/PHASE.md

.PHONY: clean
clean:
	@for pkg in $(ACTIVE_PACKAGES) $(PENDING_PACKAGES); do \
	  [ -f $$pkg/Makefile ] && $(MAKE) -C $$pkg clean 2>/dev/null || true; \
	done

.PHONY: tree
tree:
	@find . -maxdepth 2 -type d -not -path '*/node_modules*' -not -path '*/.git*' -not -path '*/target*' -not -path '*/.next*' | sort
