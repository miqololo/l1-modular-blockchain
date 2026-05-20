---
description: Run the end-to-end demo against the running devnet. Prints a transcript of the flow. Usage — /demo
---

Run the end-to-end happy-path demo against the running devnet.

Preconditions:
- Devnet is up. If `docker compose ps` shows anything not healthy, abort and instruct the user to run `/devnet up`.
- A dev keyring with `alice` (requester) and `bob` (provider) is initialized. The chain init script handles this.

Steps:
1. Print "Step 1/5: Register a service (provider: bob)"
2. Run: `docker compose exec chain aid tx aiservice register-service translate-en-fr 10aios --from bob --keyring-backend test --yes`
3. Wait one block. Capture the `service_id` from the tx response.
4. Print "Step 2/5: Request inference (requester: alice)"
5. Run: `docker compose exec chain aid tx aiservice request-inference <service_id> "Translate hello to French" --max-price 10aios --from alice --keyring-backend test --yes`
6. Capture the `request_id`.
7. Print "Step 3/5: Inference node observes the event and runs inference"
8. Tail inference-node logs for 10s: `docker compose logs --tail=20 inference-node`
9. Print "Step 4/5: Result submitted; immediate finalization"
10. Run: `docker compose exec chain aid query aiservice request <request_id> -o json | jq`
11. Print the result and assert `status == "finalized"`
12. Print "Step 5/5: Indexer reflects the finalized request"
13. Run: `curl -s http://localhost:8081/requests/<request_id> | jq`
14. Final transcript output:

```
## Demo transcript

| Step | Actor | Action | Result |
|---|---|---|---|
| 1 | bob | RegisterService(translate-en-fr, 10aios) | service_id=<N> |
| 2 | alice | RequestInference(service_id, "...") | request_id=<M> |
| 3 | inference-node | observed event, called llama-server | output="..." |
| 4 | bob | SubmitResult | finalized, escrow released |
| 5 | indexer | ingested finalized event | visible at /requests/<M> |
```

If any step fails, stop and print the failing step's exact error.
