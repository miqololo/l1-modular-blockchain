# frontend/ — Next.js + Keplr marketplace UI

**Status: phase 4.** Do not start until phase 2 is complete and `aiservice` module is functional on devnet.

## Stack (target)

- Next.js 14 (App Router)
- Keplr + Leap wallet via CosmJS
- TanStack Query for chain reads
- Tailwind + shadcn/ui
- Vitest + React Testing Library + Playwright

## Rules specific to this package

- All chain reads go through the indexer's REST API. Never query the node RPC directly from the browser.
- All chain writes use CosmJS signing client with the connected wallet.
- No `any`. No `@ts-ignore` without a comment.
- Server components by default; client components only when interactivity demands it.
- Loading and error states are required, not optional. No bare `await` without a fallback UI.

## TDD checklist

- Components: behavior test with RTL, not snapshot
- Hooks: render-hook test
- Critical flows (connect wallet → request inference → see result): Playwright e2e against devnet
