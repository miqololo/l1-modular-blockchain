import Link from "next/link";
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
      <div>
        <Link href="/" className="text-xs text-neutral-500 hover:text-neutral-300">
          ← marketplace
        </Link>
      </div>

      {/* Service header */}
      <section className="border border-neutral-800 rounded-xl bg-gradient-to-br from-neutral-900 to-neutral-950 p-6">
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div className="space-y-1.5">
            <span className="text-[11px] text-neutral-500 uppercase tracking-wider">
              Service #{svc.id}
            </span>
            <h1 className="text-2xl font-semibold">{svc.name}</h1>
            {svc.description && (
              <p className="text-neutral-400 max-w-2xl">{svc.description}</p>
            )}
          </div>
          <div className="text-right">
            <div className="text-[11px] text-neutral-500 uppercase tracking-wider">
              Price
            </div>
            <div className="text-3xl font-bold text-emerald-300 tabular-nums">
              {svc.price_amount}
            </div>
            <div className="text-xs text-neutral-500">{svc.price_denom}</div>
          </div>
        </div>
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-y-3 gap-x-4 mt-6 pt-6 border-t border-neutral-800 text-xs">
          <div>
            <dt className="text-neutral-500 mb-0.5">Owner</dt>
            <dd className="font-mono break-all">{shortAddr(svc.owner)}</dd>
          </div>
          <div>
            <dt className="text-neutral-500 mb-0.5">Active</dt>
            <dd className={svc.active ? "text-emerald-400" : "text-rose-400"}>
              {svc.active ? "yes" : "no"}
            </dd>
          </div>
          <div>
            <dt className="text-neutral-500 mb-0.5">Verification domain</dt>
            <dd>
              {svc.verification_domain_id === 0 ? (
                <span className="text-amber-400">unverified</span>
              ) : (
                <span>#{svc.verification_domain_id}</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-neutral-500 mb-0.5">Registered at</dt>
            <dd className="font-mono">block {svc.created_at_height}</dd>
          </div>
        </dl>
      </section>

      {/* How this works */}
      <section className="border border-neutral-800 rounded-xl bg-neutral-900/30 p-5">
        <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-3">
          What happens when you submit
        </h2>
        <ol className="space-y-2 text-sm text-neutral-300">
          <li className="flex gap-3">
            <span className="text-emerald-400 font-mono shrink-0">1.</span>
            <span>
              Your wallet signs <code className="text-emerald-300">MsgRequestInference</code>. The
              chain escrows <span className="text-emerald-300">{svc.price_amount} {svc.price_denom}</span> from
              your balance.
            </span>
          </li>
          <li className="flex gap-3">
            <span className="text-emerald-400 font-mono shrink-0">2.</span>
            <span>
              The provider's inference-node sees the event, runs the model, and submits
              a signed result. The provider's bond is locked at this point.
            </span>
          </li>
          <li className="flex gap-3">
            <span className="text-emerald-400 font-mono shrink-0">3.</span>
            <span>
              A 45-block challenge window opens. Determinism harnesses re-run the
              inference. If they agree, the request finalizes; if any disagree, a
              dispute resolves the conflict.
            </span>
          </li>
          <li className="flex gap-3">
            <span className="text-emerald-400 font-mono shrink-0">4.</span>
            <span>
              On finalize: you get the output, the provider gets the escrow + bond
              returned. The whole flow is visible on the live event feed.
            </span>
          </li>
        </ol>
      </section>

      {/* Submit form */}
      <section className="border border-emerald-500/20 bg-emerald-500/5 rounded-xl p-6 space-y-4">
        <h2 className="text-xl font-semibold">Submit a request</h2>
        <RequestInferenceForm service={svc} />
      </section>
    </div>
  );
}

function shortAddr(addr: string): string {
  if (addr.length < 16) return addr;
  return `${addr.slice(0, 10)}…${addr.slice(-6)}`;
}
