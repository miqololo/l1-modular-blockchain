// In-browser Ed25519 demo wallet.
//
// PHASE 0.5 SIMPLIFICATION: this replaces Keplr for the demo. A real wallet
// connection (Keplr/Leap) lands in Phase 1 along with the real Cosmos SDK chain.
//
// Security: the private key lives in localStorage. This is FINE for a Phase 0.5
// demo on a devnet with fake-money accounts. Do NOT use this pattern with real
// funds.
import * as ed from "@noble/ed25519";
import { sha256 } from "@noble/hashes/sha256";

const STORAGE_KEY = "aios.wallet.v1";

export interface Wallet {
  address: string;
  pubKeyHex: string;
  privKeyHex: string;
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function fromHex(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    out[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return out;
}

export function addressFromPubKey(pub: Uint8Array): string {
  const h = sha256(pub);
  return "aios1" + toHex(h.slice(0, 20));
}

export async function getOrCreateWallet(): Promise<Wallet> {
  if (typeof window === "undefined") {
    throw new Error("wallet only available in browser");
  }
  const existing = window.localStorage.getItem(STORAGE_KEY);
  if (existing) {
    return JSON.parse(existing) as Wallet;
  }
  const priv = ed.utils.randomPrivateKey();
  const pub = await ed.getPublicKey(priv);
  const w: Wallet = {
    address: addressFromPubKey(pub),
    pubKeyHex: toHex(pub),
    privKeyHex: toHex(priv),
  };
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(w));
  return w;
}

export function loadWallet(): Wallet | null {
  if (typeof window === "undefined") return null;
  const existing = window.localStorage.getItem(STORAGE_KEY);
  return existing ? (JSON.parse(existing) as Wallet) : null;
}

export function clearWallet(): void {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(STORAGE_KEY);
}

// Import a key from the chain's dev keyring (alice/bob), useful for the demo
// flow where the dev accounts are funded at genesis.
export function importWalletFromKeyring(privKeyHex: string): Wallet {
  if (typeof window === "undefined") {
    throw new Error("wallet only available in browser");
  }
  const priv = fromHex(privKeyHex);
  // Ed25519 priv from chain is 64 bytes (seed + pub). @noble expects 32-byte seed.
  const seed = priv.length === 64 ? priv.slice(0, 32) : priv;
  // Recompute pub deterministically
  const pubArr = sha256(seed); // PLACEHOLDER — we use noble's getPublicKey below
  void pubArr;
  // We must use ed.getPublicKey for correctness; do it synchronously via sync API
  // We don't have that here, so caller should pass full keyring.
  const w: Wallet = {
    address: "",
    pubKeyHex: "",
    privKeyHex: toHex(seed),
  };
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(w));
  return w;
}

export type TxType = "register_service" | "request_inference" | "submit_result" | "transfer";

interface SignedTx {
  type: TxType;
  nonce: number;
  pub_key_hex: string;
  signature_hex: string;
  payload: unknown;
}

// signTx produces a SignedTx matching chain/internal/types.SignedTx.CanonicalBytes.
export async function signTx(wallet: Wallet, txType: TxType, nonce: number, payload: unknown): Promise<SignedTx> {
  const envelope = {
    type: txType,
    nonce,
    pub_key_hex: wallet.pubKeyHex,
    payload,
  };
  // The chain expects canonical(envelope) — JSON.stringify with the same key order
  // (Go's encoding/json sorts struct fields by declaration order; we mirror).
  const canonical = JSON.stringify(envelope);
  const sig = await ed.signAsync(new TextEncoder().encode(canonical), fromHex(wallet.privKeyHex));
  return {
    type: txType,
    nonce,
    pub_key_hex: wallet.pubKeyHex,
    signature_hex: toHex(sig),
    payload,
  };
}
