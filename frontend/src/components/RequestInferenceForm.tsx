"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { sha256 } from "@noble/hashes/sha256";

import { getOrCreateWallet, signTx, type Wallet } from "@/lib/wallet";
import { getAccount, submitTx } from "@/lib/chain";
import type { Service } from "@/lib/indexer";

const SAMPLE_PROMPTS = [
  "Translate 'hello world' to French.",
  "Write a haiku about cryptography.",
  "Explain optimistic rollups to a teenager.",
];

export function RequestInferenceForm({ service }: { service: Service }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [prompt, setPrompt] = useState("");
  const [wallet, setWallet] = useState<Wallet | null>(null);
  const [walletBalance, setWalletBalance] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function ensureWallet(): Promise<Wallet> {
    if (wallet) return wallet;
    const w = await getOrCreateWallet();
    setWallet(w);
    try {
      const acc = await getAccount(w.address);
      setWalletBalance(acc.balance);
    } catch {
      setWalletBalance(0);
    }
    return w;
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!prompt.trim()) {
      setError("Prompt cannot be empty.");
      return;
    }
    startTransition(async () => {
      try {
        const w = await ensureWallet();
        const inputHashBytes = sha256(new TextEncoder().encode(prompt));
        const inputHash = Array.from(inputHashBytes)
          .map((b) => b.toString(16).padStart(2, "0"))
          .join("");

        const account = await getAccount(w.address);
        if (account.balance < service.price_amount) {
          throw new Error(
            `Insufficient balance: have ${account.balance} ${service.price_denom}, need ${service.price_amount}. ` +
              `Pick a dev account (alice/bob) below — they're pre-funded.`,
          );
        }

        const payload = {
          requester: w.address,
          service_id: service.id,
          input_hash: inputHash,
          input_uri: "inline:" + inputHash,
          input_text: prompt,
          max_price: { denom: service.price_denom, amount: service.price_amount },
          deadline_height: 0,
        };
        const tx = await signTx(w, "request_inference", account.nonce, payload);
        const res = await submitTx(tx);
        router.push(`/requests/${res.request_id ?? ""}`);
      } catch (err) {
        setError((err as Error).message);
      }
    });
  }

  async function useDevAccount(name: "alice" | "bob") {
    setError(null);
    try {
      const res = await fetch("/api/dev-keyring");
      if (!res.ok) throw new Error(`/api/dev-keyring: ${res.status}`);
      const ring = (await res.json()) as {
        keys: Array<{ name: string; address: string; pub_key_hex: string; priv_key_hex: string }>;
      };
      const k = ring.keys.find((x) => x.name === name);
      if (!k) throw new Error(`dev key ${name} not found`);
      // Chain stores 64-byte (seed || pub); @noble expects 32-byte seed.
      const seed = k.priv_key_hex.length === 128 ? k.priv_key_hex.slice(0, 64) : k.priv_key_hex;
      const w: Wallet = { address: k.address, pubKeyHex: k.pub_key_hex, privKeyHex: seed };
      window.localStorage.setItem("aios.wallet.v1", JSON.stringify(w));
      setWallet(w);
      const acc = await getAccount(w.address);
      setWalletBalance(acc.balance);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  const canSubmit = !pending && prompt.trim().length > 0;

  return (
    <form onSubmit={onSubmit} className="space-y-5">
      {/* Sample prompts */}
      <div className="flex flex-wrap gap-2">
        {SAMPLE_PROMPTS.map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => setPrompt(p)}
            disabled={pending}
            className="px-3 py-1 text-xs border border-neutral-800 rounded-full text-neutral-400 hover:border-emerald-500/50 hover:text-emerald-300 transition"
          >
            {p}
          </button>
        ))}
      </div>

      <div>
        <label className="block text-xs uppercase tracking-wider text-neutral-500 mb-2">
          Prompt
        </label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          className="w-full bg-neutral-950 border border-neutral-800 focus:border-emerald-500/60 focus:outline-none rounded-lg px-3 py-2 text-sm transition resize-y"
          placeholder="What should the model produce?"
          disabled={pending}
        />
      </div>

      {/* Wallet panel */}
      <div className="border border-neutral-800 rounded-lg p-3 space-y-2">
        {wallet ? (
          <div className="flex items-center justify-between gap-3 flex-wrap">
            <div className="space-y-0.5">
              <div className="text-[11px] text-neutral-500 uppercase tracking-wider">Signer</div>
              <div className="text-xs font-mono">{wallet.address}</div>
            </div>
            <div className="text-right">
              <div className="text-[11px] text-neutral-500 uppercase tracking-wider">Balance</div>
              <div className="text-sm font-mono">
                {walletBalance ?? "—"} {service.price_denom}
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-2">
            <p className="text-xs text-neutral-500">
              No wallet connected. Pick a dev account (pre-funded) to sign the request.
            </p>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => useDevAccount("alice")}
                disabled={pending}
                className="px-3 py-1.5 text-xs border border-neutral-700 rounded hover:border-emerald-500/60 hover:bg-emerald-500/5 transition"
              >
                Use dev: alice
              </button>
              <button
                type="button"
                onClick={() => useDevAccount("bob")}
                disabled={pending}
                className="px-3 py-1.5 text-xs border border-neutral-700 rounded hover:border-emerald-500/60 hover:bg-emerald-500/5 transition"
              >
                Use dev: bob
              </button>
              <button
                type="button"
                onClick={() => ensureWallet()}
                disabled={pending}
                className="px-3 py-1.5 text-xs border border-neutral-700 rounded hover:bg-neutral-800/50 transition"
              >
                Create fresh wallet
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Cost summary + submit */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="text-xs text-neutral-500">
          Escrow at submit:{" "}
          <span className="text-emerald-300 font-mono">
            {service.price_amount} {service.price_denom}
          </span>
          {" "}· released to provider on finalize
        </div>
        <button
          type="submit"
          disabled={!canSubmit}
          className="inline-flex items-center gap-2 px-5 py-2 bg-emerald-500 hover:bg-emerald-400 disabled:bg-neutral-800 disabled:text-neutral-500 text-neutral-950 font-semibold rounded-lg transition"
        >
          {pending ? (
            <>
              <Spinner /> Signing and broadcasting…
            </>
          ) : (
            <>Sign and submit →</>
          )}
        </button>
      </div>

      {error && (
        <div className="border border-rose-500/30 bg-rose-500/5 rounded-lg p-3 text-sm text-rose-300 break-words">
          {error}
        </div>
      )}
    </form>
  );
}

function Spinner() {
  return (
    <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.25" strokeWidth="4" />
      <path
        d="M4 12a8 8 0 018-8"
        stroke="currentColor"
        strokeWidth="4"
        strokeLinecap="round"
      />
    </svg>
  );
}
