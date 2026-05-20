import Link from "next/link";
import { listRequests } from "@/lib/indexer";
import { StatusPill } from "@/components/StatusPill";

export const dynamic = "force-dynamic";

export default async function RequestsPage() {
  const list = await listRequests().catch(() => ({ items: [] }));
  const sorted = [...list.items].sort((a, b) => b.id - a.id);

  // Tally by status for the header chips.
  const counts: Record<string, number> = {};
  for (const r of sorted) counts[r.status] = (counts[r.status] || 0) + 1;
  const orderedStatuses = [
    "PENDING",
    "SUBMITTED",
    "CHALLENGED",
    "FINALIZED",
    "SLASHED",
    "REFUNDED",
  ];

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold">Requests</h1>
        <div className="text-xs text-neutral-500">{sorted.length} total</div>
      </div>

      {sorted.length === 0 ? (
        <div className="border border-neutral-800 rounded-xl p-8 text-center space-y-3">
          <p className="text-neutral-300">No requests yet.</p>
          <p className="text-xs text-neutral-500">
            Click a service on{" "}
            <Link href="/" className="text-emerald-400 hover:text-emerald-300">
              the marketplace
            </Link>{" "}
            to submit one.
          </p>
        </div>
      ) : (
        <>
          {/* Status tally */}
          <div className="flex flex-wrap gap-2">
            {orderedStatuses
              .filter((s) => counts[s])
              .map((s) => (
                <span
                  key={s}
                  className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full border border-neutral-800 text-xs"
                >
                  <StatusPill status={s} />
                  <span className="text-neutral-400 tabular-nums">{counts[s]}</span>
                </span>
              ))}
          </div>

          <ul className="space-y-2">
            {sorted.map((req) => (
              <li key={req.id}>
                <Link
                  href={`/requests/${req.id}`}
                  className="block border border-neutral-800 hover:border-neutral-600 rounded-lg px-4 py-3 transition group"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-neutral-100 group-hover:text-emerald-300">
                          #{req.id}
                        </span>
                        <span className="text-[11px] text-neutral-600">
                          service #{req.service_id}
                        </span>
                      </div>
                      <p className="text-sm text-neutral-400 truncate max-w-2xl">
                        {req.input_text || "(no prompt)"}
                      </p>
                    </div>
                    <StatusPill status={req.status} />
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
