import { notFound } from "next/navigation";
import { getService } from "@/lib/indexer";
import { RequestInferenceForm } from "@/components/RequestInferenceForm";

export const dynamic = "force-dynamic";

export default async function ServicePage({ params }: { params: { id: string } }) {
  const id = Number(params.id);
  if (!Number.isFinite(id)) notFound();

  let svc;
  try {
    svc = await getService(id);
  } catch {
    notFound();
  }

  return (
    <div className="space-y-8">
      <section className="border border-neutral-800 rounded p-6">
        <h2 className="text-2xl font-semibold">{svc.name}</h2>
        {svc.description && (
          <p className="text-neutral-400 mt-2">{svc.description}</p>
        )}
        <dl className="grid grid-cols-2 gap-3 mt-6 text-sm">
          <dt className="text-neutral-500">Price</dt>
          <dd>
            {svc.price_amount} {svc.price_denom}
          </dd>
          <dt className="text-neutral-500">Owner</dt>
          <dd>
            <code>{svc.owner}</code>
          </dd>
          <dt className="text-neutral-500">Active</dt>
          <dd>{svc.active ? "yes" : "no"}</dd>
          <dt className="text-neutral-500">Verification</dt>
          <dd>
            {svc.verification_domain_id === 0 ? (
              <span className="text-amber-400">unverified (Phase 0.5)</span>
            ) : (
              <span>domain #{svc.verification_domain_id}</span>
            )}
          </dd>
        </dl>
      </section>

      <section>
        <h3 className="text-xl font-semibold mb-3">Request inference</h3>
        <RequestInferenceForm service={svc} />
      </section>
    </div>
  );
}
