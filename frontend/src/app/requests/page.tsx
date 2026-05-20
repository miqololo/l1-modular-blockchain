import Link from "next/link";
import { listRequests } from "@/lib/indexer";

export const dynamic = "force-dynamic";

export default async function RequestsPage() {
  const list = await listRequests().catch(() => ({ items: [] }));
  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-semibold">Requests</h2>
      {list.items.length === 0 ? (
        <p className="text-sm text-neutral-500">No requests yet.</p>
      ) : (
        <ul className="divide-y divide-neutral-800 border border-neutral-800 rounded">
          {list.items.map((req) => (
            <li key={req.id}>
              <Link href={`/requests/${req.id}`} className="block px-4 py-3 hover:bg-neutral-900">
                <div className="flex justify-between items-baseline">
                  <span className="font-medium">request #{req.id}</span>
                  <span
                    className={
                      req.status === "FINALIZED"
                        ? "text-cyan-400 text-xs"
                        : req.status === "REFUNDED"
                        ? "text-red-400 text-xs"
                        : "text-amber-400 text-xs"
                    }
                  >
                    {req.status}
                  </span>
                </div>
                <p className="text-xs text-neutral-500 mt-1">
                  service: {req.service_id} · {(req.input_text ?? "").slice(0, 60)}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
