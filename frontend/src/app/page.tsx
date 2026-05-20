import Link from "next/link";
import { listServices, listRequests, getStats } from "@/lib/indexer";
import { StatusPill } from "@/components/StatusPill";
import { LiveEventFeed } from "@/components/LiveEventFeed";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function HomePage() {
  const [stats, services, recent] = await Promise.all([
    getStats().catch(() => null),
    listServices().catch(() => ({ items: [] })),
    listRequests().catch(() => ({ items: [] })),
  ]);

  const recentSorted = [...recent.items].sort((a, b) => b.id - a.id).slice(0, 8);

  return (
    <div className="space-y-10">
      {/* ── Hero ───────────────────────────────────────────────────────── */}
      <section className="border border-neutral-800 rounded-xl bg-gradient-to-br from-neutral-900 to-neutral-950 p-8">
        <div className="flex items-start justify-between gap-6 flex-wrap">
          <div className="space-y-3 max-w-2xl">
            <span className="inline-flex items-center gap-2 text-xs uppercase tracking-wider text-emerald-400 font-semibold">
              <span className="inline-block w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              Live devnet · TinyLlama 1.1B
            </span>
            <h1 className="text-3xl font-bold leading-tight">
              Verifiable AI inference, on-chain.
            </h1>
            <p className="text-neutral-400">
              Pay a provider to run a model. Watchers re-run the same inference
              independently. Wrong outputs get slashed; spurious challengers lose
              their bond. Below is the live state of the marketplace.
            </p>
          </div>
          {services.items.length > 0 && (
            <Link
              href={`/services/${services.items[0].id}`}
              className="inline-flex items-center gap-2 px-5 py-3 bg-emerald-500 hover:bg-emerald-400 text-neutral-950 font-semibold rounded-lg transition"
            >
              Try a request →
            </Link>
          )}
        </div>
      </section>

      {/* ── Stats ──────────────────────────────────────────────────────── */}
      <section>
        <h2 className="text-sm font-semibold uppercase tracking-wider text-neutral-500 mb-3">
          Marketplace stats
        </h2>
        {stats ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
            <Stat label="Services" value={stats.services_total} accent="emerald" />
            <Stat label="Requests" value={stats.requests_total} accent="sky" />
            <Stat label="Finalized" value={stats.requests_finalized} accent="emerald" />
            <Stat label="Pending" value={stats.requests_pending} accent="amber" />
            <Stat label="Refunded" value={stats.requests_refunded} accent="neutral" />
          </div>
        ) : (
          <UnreachableHint target="indexer" />
        )}
      </section>

      {/* ── Two-column: Services + Recent requests ─────────────────────── */}
      <div className="grid lg:grid-cols-5 gap-8">
        <section className="lg:col-span-3 space-y-3">
          <div className="flex items-baseline justify-between">
            <h2 className="text-sm font-semibold uppercase tracking-wider text-neutral-500">
              Services
            </h2>
            <span className="text-xs text-neutral-600">{services.items.length} registered</span>
          </div>
          {services.items.length === 0 ? (
            <EmptyServices />
          ) : (
            <ul className="space-y-2">
              {services.items
                .filter((s) => s.active)
                .map((svc) => (
                  <li key={svc.id}>
                    <Link
                      href={`/services/${svc.id}`}
                      className="block border border-neutral-800 hover:border-emerald-500/60 rounded-lg px-4 py-3 transition group"
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-neutral-100 group-hover:text-emerald-300">
                              {svc.name}
                            </span>
                            <span className="text-xs text-neutral-600">#{svc.id}</span>
                          </div>
                          {svc.description && (
                            <p className="text-sm text-neutral-400 mt-1 truncate">
                              {svc.description}
                            </p>
                          )}
                        </div>
                        <div className="shrink-0 text-right">
                          <div className="text-sm font-medium text-emerald-300">
                            {svc.price_amount} {svc.price_denom}
                          </div>
                          <div className="text-[11px] text-neutral-600 mt-0.5">
                            {svc.verification_domain_id === 0 ? (
                              <span className="text-amber-400">unverified</span>
                            ) : (
                              <>domain #{svc.verification_domain_id}</>
                            )}
                          </div>
                        </div>
                      </div>
                    </Link>
                  </li>
                ))}
            </ul>
          )}
        </section>

        <section className="lg:col-span-2 space-y-3">
          <div className="flex items-baseline justify-between">
            <h2 className="text-sm font-semibold uppercase tracking-wider text-neutral-500">
              Recent requests
            </h2>
            <Link href="/requests" className="text-xs text-emerald-400 hover:text-emerald-300">
              View all →
            </Link>
          </div>
          {recentSorted.length === 0 ? (
            <div className="border border-neutral-800 rounded-lg p-4 text-sm text-neutral-500">
              No requests yet. Click a service above to submit one.
            </div>
          ) : (
            <ul className="space-y-1.5">
              {recentSorted.map((r) => (
                <li key={r.id}>
                  <Link
                    href={`/requests/${r.id}`}
                    className="flex items-center justify-between px-3 py-2 border border-neutral-800 rounded-lg hover:border-neutral-600 transition group"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="text-xs text-neutral-600 shrink-0">#{r.id}</span>
                      <span className="text-sm text-neutral-300 truncate group-hover:text-neutral-100">
                        {r.input_text || "(no prompt)"}
                      </span>
                    </div>
                    <StatusPill status={r.status} />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      {/* ── Live activity feed ──────────────────────────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-neutral-500">
            Live chain activity
          </h2>
          <span className="text-[11px] text-neutral-600">SSE · /events</span>
        </div>
        <LiveEventFeed />
      </section>

      {/* ── About strip ─────────────────────────────────────────────────── */}
      <section className="border-t border-neutral-800 pt-6 grid sm:grid-cols-3 gap-6 text-sm">
        <div>
          <div className="text-xs uppercase tracking-wider text-neutral-500 mb-1">Honest path</div>
          <p className="text-neutral-400">
            Submit → provider runs the model → watchers verify → finalized after the
            challenge window. You get your output, provider gets paid.
          </p>
        </div>
        <div>
          <div className="text-xs uppercase tracking-wider text-neutral-500 mb-1">
            Fraud caught
          </div>
          <p className="text-neutral-400">
            Provider lies → independent watcher disagrees → files{" "}
            <code className="text-fuchsia-300">MsgChallenge</code> → request slashed,
            requester refunded, provider's bond goes to challenger.
          </p>
        </div>
        <div>
          <div className="text-xs uppercase tracking-wider text-neutral-500 mb-1">
            Honest provider protected
          </div>
          <p className="text-neutral-400">
            Spurious challenger → other watchers <code className="text-fuchsia-300">MsgVouch</code>{" "}
            for the provider → challenge dismissed, challenger loses bond.
          </p>
        </div>
      </section>
    </div>
  );
}

function Stat({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent: "emerald" | "sky" | "amber" | "rose" | "neutral";
}) {
  const accentMap: Record<typeof accent, string> = {
    emerald: "text-emerald-400",
    sky: "text-sky-400",
    amber: "text-amber-400",
    rose: "text-rose-400",
    neutral: "text-neutral-200",
  };
  return (
    <div className="border border-neutral-800 rounded-lg p-4 bg-neutral-900/40">
      <div className="text-[11px] text-neutral-500 uppercase tracking-wider">{label}</div>
      <div className={`text-2xl font-semibold mt-1 tabular-nums ${accentMap[accent]}`}>
        {value}
      </div>
    </div>
  );
}

function UnreachableHint({ target }: { target: string }) {
  return (
    <div className="border border-amber-500/30 bg-amber-500/5 rounded-lg p-4">
      <p className="text-sm text-amber-300">⚠ {target} unreachable</p>
      <p className="text-xs text-neutral-400 mt-1">
        The frontend can't reach the {target}. Check that{" "}
        <code className="text-amber-300">docker compose ps</code> shows the {target} as
        healthy and that the public URL is correct in the environment.
      </p>
    </div>
  );
}

function EmptyServices() {
  return (
    <div className="border border-neutral-800 rounded-lg p-6 text-center space-y-3">
      <p className="text-neutral-300">No services have been seeded yet.</p>
      <p className="text-xs text-neutral-500">
        On the deployment host, run{" "}
        <code className="bg-neutral-900 px-1.5 py-0.5 rounded">make seed</code> or POST to{" "}
        <code className="bg-neutral-900 px-1.5 py-0.5 rounded">/demo/seed</code> on the chain.
      </p>
    </div>
  );
}
