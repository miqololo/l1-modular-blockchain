import Link from "next/link";
import { listServices, getStats } from "@/lib/indexer";

export const dynamic = "force-dynamic";

export default async function HomePage() {
  const [stats, services] = await Promise.all([
    getStats().catch(() => null),
    listServices().catch(() => ({ items: [] })),
  ]);

  return (
    <div className="space-y-8">
      <section>
        <h2 className="text-2xl font-semibold mb-4">Stats</h2>
        {stats ? (
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            <Stat label="Services" value={stats.services_total} />
            <Stat label="Requests total" value={stats.requests_total} />
            <Stat label="Finalized" value={stats.requests_finalized} />
            <Stat label="Pending" value={stats.requests_pending} />
            <Stat label="Refunded" value={stats.requests_refunded} />
          </div>
        ) : (
          <p className="text-sm text-neutral-500">indexer unreachable.</p>
        )}
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Services</h2>
        {services.items.length === 0 ? (
          <div className="text-sm text-neutral-500 space-y-2">
            <p>No services registered yet.</p>
            <p>
              Use the seed script from the host:{" "}
              <code className="bg-neutral-900 px-1.5 py-0.5 rounded">make seed</code>
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-neutral-800 border border-neutral-800 rounded">
            {services.items.map((svc) => (
              <li key={svc.id}>
                <Link href={`/services/${svc.id}`} className="block px-4 py-3 hover:bg-neutral-900">
                  <div className="flex items-baseline justify-between">
                    <span className="font-medium">{svc.name}</span>
                    <span className="text-sm text-neutral-500">
                      {svc.price_amount} {svc.price_denom}
                    </span>
                  </div>
                  {svc.description && <p className="text-sm text-neutral-400 mt-1">{svc.description}</p>}
                  <p className="text-xs text-neutral-600 mt-1">
                    owner: <code>{svc.owner}</code> · id: {svc.id}
                    {svc.verification_domain_id === 0 && (
                      <span className="ml-2 text-amber-400">unverified</span>
                    )}
                  </p>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="border border-neutral-800 rounded p-4">
      <div className="text-xs text-neutral-500 uppercase tracking-wide">{label}</div>
      <div className="text-2xl font-semibold mt-1">{value}</div>
    </div>
  );
}
