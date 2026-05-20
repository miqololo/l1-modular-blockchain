"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams, notFound } from "next/navigation";
import type { InferenceRequest } from "@/lib/indexer";
import { StatusPill } from "@/components/StatusPill";

export default function RequestPage() {
  const params = useParams<{ id: string }>();
  const id = Number(params.id);
  if (!Number.isFinite(id)) notFound();

  const [req, setReq] = useState<InferenceRequest | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let stop = false;
    async function poll() {
      try {
        const base = process.env.NEXT_PUBLIC_INDEXER_URL || "http://localhost:8081";
        const res = await fetch(`${base}/requests/${id}`, { cache: "no-store" });
        if (!res.ok) {
          if (res.status === 404) {
            setError("Not found yet — the indexer may still be catching up.");
            return;
          }
          throw new Error(`indexer: ${res.status}`);
        }
        const data = (await res.json()) as InferenceRequest;
        if (!stop) setReq(data);
        return data.status === "FINALIZED" || data.status === "REFUNDED" || data.status === "SLASHED";
      } catch (e) {
        if (!stop) setError((e as Error).message);
      }
      return false;
    }
    (async () => {
      let done = false;
      while (!stop && !done) {
        done = (await poll()) ?? false;
        if (!done) await new Promise((r) => setTimeout(r, 2000));
      }
    })();
    return () => {
      stop = true;
    };
  }, [id]);

  if (!req) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold">Request #{id}</h1>
        {error ? (
          <p className="text-sm text-amber-400">{error}</p>
        ) : (
          <p className="text-sm text-neutral-500">Loading…</p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-baseline justify-between gap-3 flex-wrap">
        <div>
          <Link
            href="/requests"
            className="text-xs text-neutral-500 hover:text-neutral-300"
          >
            ← all requests
          </Link>
          <h1 className="text-2xl font-semibold mt-1">Request #{req.id}</h1>
        </div>
        <StatusPill status={req.status} />
      </div>

      {/* Lifecycle visualization */}
      <Lifecycle status={req.status} />

      {/* Two-column: input/metadata + output */}
      <div className="grid lg:grid-cols-2 gap-6">
        {/* Input + metadata */}
        <section className="border border-neutral-800 rounded-xl bg-neutral-900/30 p-5 space-y-4">
          <div>
            <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-2">Prompt</h2>
            <p className="text-sm whitespace-pre-wrap text-neutral-200">{req.input_text || "(empty)"}</p>
          </div>
          <dl className="grid grid-cols-2 gap-y-2 gap-x-3 text-sm pt-3 border-t border-neutral-800">
            <dt className="text-neutral-500">Service</dt>
            <dd className="font-mono text-xs">
              <Link
                href={`/services/${req.service_id}`}
                className="text-emerald-400 hover:text-emerald-300"
              >
                #{req.service_id}
              </Link>
            </dd>

            <dt className="text-neutral-500">Requester</dt>
            <dd className="font-mono text-xs break-all">{req.requester}</dd>

            <dt className="text-neutral-500">Escrow</dt>
            <dd className="font-mono text-xs">{req.escrow_amount} {req.escrow_denom}</dd>

            <dt className="text-neutral-500">Created at</dt>
            <dd className="font-mono text-xs">block {req.created_at_height}</dd>

            <dt className="text-neutral-500">Deadline</dt>
            <dd className="font-mono text-xs">block {req.deadline_height}</dd>

            {req.finalized_at_height && (
              <>
                <dt className="text-neutral-500">Finalized at</dt>
                <dd className="font-mono text-xs">block {req.finalized_at_height}</dd>
              </>
            )}

            <dt className="text-neutral-500">Input hash</dt>
            <dd className="font-mono text-[10px] break-all leading-relaxed col-span-1">
              {req.input_hash.slice(0, 16)}…
            </dd>
          </dl>
        </section>

        {/* Output */}
        <section className="border border-neutral-800 rounded-xl bg-neutral-900/30 p-5 space-y-4">
          <h2 className="text-xs uppercase tracking-wider text-neutral-500">
            {req.status === "FINALIZED" ? "Verified output" : "Output"}
          </h2>
          {req.output_text ? (
            <div className="space-y-3">
              <div className="text-sm whitespace-pre-wrap text-neutral-200 bg-neutral-950 border border-neutral-800 rounded-lg p-3 max-h-72 overflow-y-auto">
                {req.output_text}
              </div>
              <dl className="grid grid-cols-3 gap-y-1.5 gap-x-3 text-xs pt-3 border-t border-neutral-800">
                <dt className="text-neutral-500">Provider</dt>
                <dd className="col-span-2 font-mono text-[11px] break-all">{req.provider}</dd>
                <dt className="text-neutral-500">Paid</dt>
                <dd className="col-span-2 font-mono">
                  {req.paid_amount ?? 0} {req.paid_denom ?? req.escrow_denom}
                </dd>
                <dt className="text-neutral-500">Output hash</dt>
                <dd className="col-span-2 font-mono text-[10px] break-all">
                  {req.output_hash}
                </dd>
              </dl>
            </div>
          ) : req.status === "PENDING" ? (
            <PendingPlaceholder />
          ) : req.status === "SUBMITTED" ? (
            <p className="text-sm text-sky-300">
              Provider has submitted. Waiting for the challenge window to expire (~45s).
            </p>
          ) : req.status === "CHALLENGED" ? (
            <p className="text-sm text-fuchsia-300">
              This request is under dispute. Vouchers are weighing in; resolution
              happens after the window closes.
            </p>
          ) : req.status === "SLASHED" ? (
            <p className="text-sm text-rose-300">
              Result was slashed. The chain refunded the requester and transferred the
              provider's bond to the challenger.
            </p>
          ) : req.status === "REFUNDED" ? (
            <p className="text-sm text-neutral-400">
              Request did not finalize; the requester was refunded.
            </p>
          ) : (
            <p className="text-sm text-neutral-500">No output yet.</p>
          )}
        </section>
      </div>

      {!isTerminal(req.status) && (
        <p className="text-xs text-neutral-500">
          Auto-refreshing every 2 s. The page will stop polling once the request reaches a
          terminal state.
        </p>
      )}
    </div>
  );
}

function isTerminal(status: string): boolean {
  return status === "FINALIZED" || status === "SLASHED" || status === "REFUNDED";
}

/** Visual lifecycle bar. Highlights the steps the request has passed through. */
function Lifecycle({ status }: { status: string }) {
  const steps = ["PENDING", "SUBMITTED", "FINALIZED"] as const;
  type Step = (typeof steps)[number];

  // Which "rail" step are we currently at? Default mapping:
  //   PENDING → pending
  //   SUBMITTED, CHALLENGED → submitted (in-flight after submission)
  //   FINALIZED → finalized
  //   SLASHED → finalized-as-failure
  //   REFUNDED → pending-failed (never reached submitted)
  const reached = (s: Step) => {
    if (s === "PENDING") return true;
    if (s === "SUBMITTED") return status !== "PENDING" && status !== "REFUNDED";
    return status === "FINALIZED" || status === "SLASHED";
  };
  const failedAt: Step | null =
    status === "REFUNDED" ? "SUBMITTED" : status === "SLASHED" ? "FINALIZED" : null;

  return (
    <div className="border border-neutral-800 rounded-xl bg-neutral-900/30 p-4">
      <ol className="flex items-center justify-between gap-2">
        {steps.map((s, idx) => {
          const isReached = reached(s);
          const isFailed = failedAt === s;
          const color = isFailed
            ? "border-rose-500 bg-rose-500/10 text-rose-300"
            : isReached
              ? "border-emerald-500 bg-emerald-500/10 text-emerald-300"
              : "border-neutral-700 bg-neutral-900 text-neutral-500";
          const railColor =
            idx < steps.length - 1
              ? reached(steps[idx + 1])
                ? failedAt === steps[idx + 1]
                  ? "bg-rose-500/40"
                  : "bg-emerald-500/40"
                : "bg-neutral-800"
              : "";
          return (
            <li key={s} className="flex items-center gap-2 flex-1 last:flex-initial">
              <div className={`flex items-center gap-2.5 px-3 py-1.5 rounded-full border ${color} shrink-0`}>
                <span className="text-[10px] font-bold">{idx + 1}</span>
                <span className="text-xs">
                  {isFailed ? (failedAt === "FINALIZED" ? "SLASHED" : "REFUNDED") : s}
                </span>
              </div>
              {idx < steps.length - 1 && <div className={`flex-1 h-px ${railColor}`} />}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function PendingPlaceholder() {
  return (
    <div className="space-y-2">
      <div className="h-3 bg-neutral-800 rounded animate-pulse w-3/4" />
      <div className="h-3 bg-neutral-800 rounded animate-pulse w-1/2" />
      <div className="h-3 bg-neutral-800 rounded animate-pulse w-2/3" />
      <p className="text-xs text-neutral-500 pt-2">
        Provider is running the model. This typically takes 5–15 seconds.
      </p>
    </div>
  );
}
