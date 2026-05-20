# Project Phase

**Current phase: 3.z steps 1–4 + treasury sweep — Voucher sybil resistance complete; escalation layer (step 5) committed via ADR-0004 design**
**Parallel track: Phase 0 — Protocol & determinism** (continues; protocol spec v1 + threat model v1 landed)

Updated: 2026-05-20

## What is shipped (Phase 0.5 → 3.y end-to-end)

### Chain (`chain/`) — custom Go L1 with bbolt persistence
- 11 tx types: `register_service`, `request_inference`, `submit_result`, `register_domain`, `challenge`, `vouch`, `resolve_challenge`, `update_service`, `deactivate_service`, `deactivate_domain`, `transfer`
- 6 statuses: `PENDING`, `SUBMITTED`, `CHALLENGED`, `FINALIZED`, `SLASHED`, `REFUNDED`
- 14 event types covering every state transition + block commit
- Block producer (`RunBlockProducer`) ticks at 1 s, handles deadline/window/resolution-timeout transitions
- Shared `executeSlash` / `executeDismiss` helpers in `chain/internal/state/resolution.go` used by both the block producer and `MsgResolveChallenge`
- Ed25519 over canonical JSON (struct field order — known foot-gun documented in protocol spec §5.2)
- Demo HTTP API on :26657 with `/demo/*` shortcuts that sign and submit each tx type
- Tests in `chain/internal/state/*_test.go` cover all 11 tx handlers + all 3 block-producer-driven transitions

### Inference node (`inference-node/`)
- SSE subscriber to chain events; reconnect on drop
- `llama-server` HTTP executor with per-call attestation
- `MALICIOUS_PROVIDER=1` flag for the Phase 3 demo (fabricates output, exercises challenge path)
- Honest by default; harness exists separately as the challenger

### Determinism harness (`determinism-harness/`)
- Subscribes to chain events, re-runs every inference within its own configured domain
- Verdict reporting at `/report`
- Phase 3.x: files `MsgChallenge` on `DIVERGENT`
- Phase 3.y: files `MsgVouch` (provider-side) when an honest request gets a spurious challenge
- Cross-process determinism check via `make determinism-check` (2 `llama-server` instances × 3 runs; 6 identical hashes as of 2026-05-20 on the demo tuple)

### Indexer (`indexer/`)
- SQLite (Phase 0.5 simplification from Postgres) read model
- REST API for service / request / event lookups
- SSE subscriber with idempotent ingest

### Frontend (`frontend/`)
- Next.js + dev keyring shim (`EXPOSE_DEV_KEYRING=1`) for one-click demo
- Marketplace UI: list services, submit request, watch status
- Phase 3.y panels: dispute view, vouch button

### Docker / one-command demo
- `docker compose up -d` boots model-init → llama-server (×2) → chain → inference-node → determinism-harness → indexer → frontend
- `make demo` — honest path
- `make demo-malicious` — Phase 3 malicious-provider → challenge → slash
- `make demo-spurious` — Phase 3.y honest provider → spurious challenge → voucher dismisses → finalize

### Protocol docs (Phase 0 parallel track, freshly caught up)
- `.claude/internal/protocol/verification-protocol.md` — v1, covers threats, actors, domain, lifecycle, attestation, economics, open problems, code mapping
- `.claude/internal/protocol/threat-model.md` — v1, 11 worked attacks (A1.1–A6.1) with cost/profit/mitigation/residual
- `.claude/internal/prior-art/{ora-opml,gensyn-verde,arbitrum-nitro}.md` — citations + what we borrow + what doesn't transfer
- `.claude/internal/adr/0001` (monorepo), `0002` (optimistic verification), `0003` (runtime + domains), `0004` (dispute game shape — open)

## What's NOT shipped yet

| Item | Blocked on | Phase |
|---|---|---|
| Cross-host (not cross-process) determinism check | Need a second host runner | 1 |
| Replace `llama_http` with a verified-runtime executor | Determinism gate above | 1 |
| CometBFT consensus (replace single-producer goroutine) | Cosmos SDK port | 1 |
| ~~Refund-on-`MsgDeactivateDomain`~~ | Shipped 2026-05-20; cascade auto-deactivates services + voids open requests + refunds all locked stakes | done |
| ~~`VoucherMargin > 0` enforcement~~ | Per-domain override shipped; demo unchanged (margin=0); production operators raise per-domain margin as 2-watcher market lands | done |
| ~~Voucher bond scales with provider bond~~ | `VoucherBondScaleBP` shipped 2026-05-20; default 5000 BP preserves legacy ratios | done |
| ~~Forfeit-bond treasury sweep~~ | `ModuleTreasuryAddr` + reroute shipped 2026-05-20; withdrawal routing deferred to future ADR | done |
| Federated re-execution committee (Phase 3.z step 5 escalation) | ADR-0004 commits the design; impl is Phase 4 | future |
| ~~Full voucher-flow integration test~~ | Shipped 2026-05-20 in `dispute_integration_test.go` | done |
| ~~Tokenizer pinning in the domain tuple~~ | Shipped 2026-05-20; `tokenizer_id` field on `VerificationDomain` + `Attestation`; back-compat preserves legacy domains | done |
| Authority key → multisig → governance | Phase 4 design | 4 |
| Generative agents (`agents/`) | Phase 4 |
| CosmWasm extensions (`contracts/`) | Phase 5 |
| Celestia DA migration | Phase 5 |

## Gate to phase 4 (next forward step)

Phase 3.y → 4 requires:

- [x] All 11 tx types implemented + tested
- [x] Voucher mechanism shipped and demonstrated end-to-end (`make demo-spurious`)
- [x] Cross-process determinism demonstrated (`make determinism-check`)
- [x] Protocol spec v1 + threat model v1 + 3 prior-art notes landed
- [x] ADR-0007 step 1 (voucher eligibility + `VoucherMargin` param) shipped
- [x] ADR-0007 step 2 (service-registration bond + active-eligibility) shipped
- [x] Test-funding helpers (`(*State).SetParams`, `(*State).Mint`) shipped
- [x] Block-tick helper (`(*State).Tick`) shipped — drives time-based resolution in tests
- [x] Full dispute-game integration tests shipped (`dispute_integration_test.go`): dismissal-by-voucher, slash-without-voucher, eligibility-rejection paths verified end-to-end with exact balance assertions
- [x] `MsgDeactivateDomain` cascade shipped — auto-deactivates dependent services + voids open requests + refunds all locked stakes; `RequestVoided` event added; `domain_voiding_test.go` covers PENDING / SUBMITTED / CHALLENGED / FINALIZED-untouched paths with exact balance assertions
- [x] ADR-0007 step 3 (per-domain `VoucherMargin` override) shipped
- [x] ADR-0007 step 4 (`VoucherBondScaleBP` — voucher bond scales with provider bond) shipped
- [x] Treasury sweep shipped — `ModuleTreasuryAddr` + `(*State).TreasuryBalance`; early-deactivation forfeits and losing-side voucher bonds on slash routed there instead of accumulating in module escrow
- [x] Immediate-finalize provider-bond bug fixed (`ChallengeWindowBlocks == 0` branch in `applySubmitResult` now returns the bond)
- [x] ADR-0004 committed — "federated re-execution committee" as escalation layer; Nitro-style bisection rejected with rationale (chain can't re-execute one inference step)
- [x] Tokenizer pinning in the domain tuple shipped — Phase 1 deliverable; closes verification-protocol §7.7 open problem; back-compat preserves legacy domains
- [x] **MVP item #2 — multi-watcher demo shipped.** Second dev key `harness-b`; second container `determinism-harness-b` pointed at `llama-server-b`; second witness service registered at seed; voucher quorum game now demonstrable for real, not theatrical. `make demo-multi-watcher` exercises both harnesses on a single request and shows they independently agree.
- [x] **MVP item #3 — falsifiability demos shipped.** `make demo-no-watcher` stops both harnesses, runs a malicious provider, and shows the request finalizes with a fabricated hash (the documented failure mode when no honest watcher exists). `make demo-tokenizer-mismatch` shows the chain rejecting a `MsgSubmitResult` whose `TokenizerID` differs from the domain's, ending the request in `REFUNDED`. Both demos make the security claim falsifiable: a reviewer can break the assumption and see the protocol behave as the threat model predicts.
- [x] **MVP item #4 — End-to-end CI shipped.** Three GitHub Actions workflows: `lint.yml` (gofmt+vet+golangci-lint per package, ~1 min), `test.yml` (go test -race -count=1 per package, ~2 min), `demos.yml` (seven matrix jobs covering honest/malicious/spurious/no-watcher/tokenizer-mismatch/determinism-check/multi-watcher, ~15 min). Helper `scripts/ci-assert-status.sh` polls chain state and asserts terminal status with full request-payload dump on failure. README has CI badges + section documenting what each workflow proves.
- [x] **MVP item #5 — English reviewer walkthrough shipped.** `docs/TUTORIAL.md` written for skeptical reviewers, structured around falsifiability (the 7 demos + 2 falsifiability demos), explicit "what is NOT being claimed" section, threat coverage table mapping each attack to its mitigation status. Counterpart to `TUTORIAL.ru.md` but reorganized for verification rather than exposition.
- [x] **MVP item #6 — Reproducibility statement shipped.** `docs/REPRODUCIBILITY.md` is a one-page certification checklist with pinned configuration (model SHA, runtime image, all bond defaults, sampling params), eight-step verification procedure, sign-off block for the verifier (CPU model, OS, commit SHA), and open audit items the document explicitly does NOT certify (cross-host determinism, censorship resistance, authority compromise, soak tests).
- [ ] **Cross-host determinism demonstrated** (Phase 1 hard requirement; not yet)
- [ ] ADR-0007 step 3 — enforce `VoucherMargin > 0` once a 2-watcher market exists
- [ ] ADR-0007 step 4 — voucher bond scales with provider bond
- [ ] **Bisection fallback for unresolvable disputes** (ADR-0004 commits + impl)
- [ ] CometBFT migration (Phase 1, blocks 3.z testnet)

The catch-up gap (between "Phase 3.y shipped" and "everything from Phase 0–3 actually done") closed today. Forward work is now Phase 1 (cross-host determinism + CometBFT) and Phase 3.z (sybil-resistant vouchers + bisection).

## Active packages by phase

| Phase | Active packages |
|---|---|
| 0 (parallel) | `determinism-harness`, `.claude/internal/protocol`, `.claude/internal/prior-art`, `.claude/internal/adr` |
| 0.5 (done) | `chain`, `inference-node`, `indexer`, `frontend`, `proto`, `determinism-harness` |
| 1 (next) | Add: verified-runtime executor in `inference-node`; Cosmos SDK port of `chain` |
| 2 (done — folded into 3.x catch-up) | `chain` lifecycle hardening (UpdateService, DeactivateService, DeactivateDomain) |
| 3 (done — 3.y) | `chain` dispute mechanism + harness as challenger/voucher |
| 3.z (next) | Sybil resistance + bisection fallback within `chain` |
| 4 | + `agents/`, expanded analytics, public testnet |
| 5 | + `contracts/` (CosmWasm); Celestia DA |

## Phase plan summary

| Phase | Title | Status | Gate |
|---|---|---|---|
| 0 | Protocol & determinism (parallel) | spec v1 + cross-process check landed | Cross-host check passes |
| 0.5 | Demo slice | shipped 2026-04 | docker compose end-to-end |
| 1 | Verified inference node | next | Two hosts produce identical signatures |
| 2 | Marketplace hardening | folded into 3.x catch-up | n/a |
| 3 | Challenge & fraud proofs (3.x simple + 3.y voucher) | shipped 2026-05 | `make demo-malicious` + `make demo-spurious` pass |
| 3.z step 1 | Voucher eligibility + `VoucherMargin` param | shipped 2026-05-20 | ADR-0007 step 1; `voucher_eligibility_test.go` |
| 3.z step 2 | Service-registration bond + active-eligibility | shipped 2026-05-20 | ADR-0007 step 2; `registration_bond_test.go` |
| 3.z step 3 | Per-domain `VoucherMargin` override | shipped 2026-05-20 | ADR-0007 step 3; `phase_3z_345_test.go` |
| 3.z step 4 | Voucher bond scales with provider bond | shipped 2026-05-20 | ADR-0007 step 4; `phase_3z_345_test.go` |
| 3.z treasury | Forfeit-bond treasury sweep | shipped 2026-05-20 | `phase_3z_345_test.go::TestTreasury_*` |
| 3.z step 5 (design) | Federated re-execution committee | design accepted in ADR-0004 | impl is Phase 4 |
| 4 | Testnet + agents + analytics | open | — |
| 5 | Celestia DA + CosmWasm | open | — |
