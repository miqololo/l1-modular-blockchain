"use client";

import { useEffect, useState } from "react";
import { useParams, notFound } from "next/navigation";
import type { InferenceRequest } from "@/lib/indexer";

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
        const url = `${process.env.NEXT_PUBLIC_INDEXER_URL || "http://localhost:8081"}/requests/${id}`;
        const res = await fetch(url, { cache: "no-store" });
        if (!res.ok) {
          if (res.status === 404) {
            setError("Not found yet — the indexer may still be catching up.");
            return;
          }
          throw new Error(`indexer: ${res.status}`);
        }
        const data = (await res.json()) as InferenceRequest;
        if (!stop) setReq(data);
        return data.status === "FINALIZED" || data.status === "REFUNDED";
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
        <h2 className="text-2xl font-semibold">request #{id}</h2>
        {error ? (
          <p className="text-sm text-amber-400">{error}</p>
        ) : (
          <p className="text-sm text-neutral-500">Loading…</p>
        )}
      </div>
    );
  }

  const isFinalized = req.status === "FINALIZED";
  return (
    <div className="space-y-6">
      <header className="flex items-baseline justify-between">
        <h2 className="text-2xl font-semibold">request #{req.id}</h2>
        <span className={isFinalized ? "text-cyan-400 text-sm" : "text-amber-400 text-sm"}>
          {req.status}
        </span>
      </header>

      <dl className="grid grid-cols-3 gap-3 text-sm border border-neutral-800 rounded p-4">
        <dt className="text-neutral-500">Service</dt>
        <dd className="col-span-2">{req.service_id}</dd>

        <dt className="text-neutral-500">Requester</dt>
        <dd className="col-span-2"><code>{req.requester}</code></dd>

        <dt className="text-neutral-500">Prompt</dt>
        <dd className="col-span-2 whitespace-pre-wrap">{req.input_text}</dd>

        <dt className="text-neutral-500">Input hash</dt>
        <dd className="col-span-2 break-all"><code className="text-xs">{req.input_hash}</code></dd>

        <dt className="text-neutral-500">Escrow</dt>
        <dd className="col-span-2">{req.escrow_amount} {req.escrow_denom}</dd>

        <dt className="text-neutral-500">Deadline</dt>
        <dd className="col-span-2">block {req.deadline_height}</dd>

        {isFinalized && (
          <>
            <dt className="text-neutral-500">Output</dt>
            <dd className="col-span-2 whitespace-pre-wrap bg-neutral-900 rounded p-3">
              {req.output_text}
            </dd>
            <dt className="text-neutral-500">Output hash</dt>
            <dd className="col-span-2 break-all"><code className="text-xs">{req.output_hash}</code></dd>
            <dt className="text-neutral-500">Provider</dt>
            <dd className="col-span-2"><code>{req.provider}</code></dd>
            <dt className="text-neutral-500">Paid</dt>
            <dd className="col-span-2">{req.paid_amount} {req.paid_denom}</dd>
          </>
        )}
      </dl>

      {!isFinalized && (
        <p className="text-sm text-neutral-500">
          Waiting for inference-node to submit a result. Auto-refresh every 2s.
        </p>
      )}
    </div>
  );
}
