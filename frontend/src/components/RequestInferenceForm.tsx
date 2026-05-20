"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { sha256 } from "@noble/hashes/sha256";

import { getOrCreateWallet, signTx, type Wallet } from "@/lib/wallet";
import { getAccount, submitTx } from "@/lib/chain";
import type { Service } from "@/lib/indexer";

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

        // Hash the prompt → input_hash.
        const inputHashBytes = sha256(new TextEncoder().encode(prompt));
        const inputHash = Array.from(inputHashBytes).map((b) => b.toString(16).padStart(2, "0")).join("");

        const account = await getAccount(w.address);
        if (account.balance < service.price_amount) {
          throw new Error(
            `Insufficient balance: have ${account.balance} ${service.price_denom}, need ${service.price_amount}. ` +
              `Use one of the funded dev accounts — click "Use dev account" below.`,
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
      const ring = (await res.json()) as { keys: Array<{ name: string; address: string; pub_key_hex: string; priv_key_hex: string }> };
      const k = ring.keys.find((x) => x.name === name);
      if (!k) throw new Error(`dev key ${name} not found`);
      // The Go chain stores ed25519 private key as 64-byte (seed||pub). @noble expects 32-byte seed.
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

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div>
        <label className="block text-sm text-neutral-400 mb-1">Prompt</label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          className="w-full bg-neutral-900 border border-neutral-800 rounded px-3 py-2 text-sm"
          placeholder="Translate 'hello world' to French"
          disabled={pending}
        />
      </div>

      <div className="text-xs text-neutral-500">
        Price: <code>{service.price_amount} {service.price_denom}</code> (escrowed at submit; released on finalize)
      </div>

      {wallet ? (
        <p className="text-xs text-neutral-500">
          Wallet: <code>{wallet.address}</code>
          {walletBalance !== null && (
            <> · balance: <code>{walletBalance} {service.price_denom}</code></>
          )}
        </p>
      ) : (
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => ensureWallet()}
            className="px-3 py-2 text-sm border border-neutral-700 rounded hover:bg-neutral-900"
            disabled={pending}
          >
            Create wallet
          </button>
          <span className="text-xs text-neutral-600">or</span>
          <button type="button" onClick={() => useDevAccount("alice")} className="px-3 py-2 text-sm border border-neutral-700 rounded hover:bg-neutral-900">
            Use dev: alice
          </button>
          <button type="button" onClick={() => useDevAccount("bob")} className="px-3 py-2 text-sm border border-neutral-700 rounded hover:bg-neutral-900">
            Use dev: bob
          </button>
        </div>
      )}

      <button
        type="submit"
        disabled={pending || !prompt.trim()}
        className="px-4 py-2 bg-cyan-700 text-white rounded text-sm hover:bg-cyan-600 disabled:opacity-50"
      >
        {pending ? "Signing and broadcasting…" : "Sign and submit"}
      </button>

      {error && <p className="text-sm text-red-400 mt-2 break-words">{error}</p>}
    </form>
  );
}
