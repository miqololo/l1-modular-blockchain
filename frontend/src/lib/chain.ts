// Direct HTTP client for the aios chain. Used for tx submission and account
// queries (nonce, balance). Read-heavy data (services list, request status)
// goes through the indexer for cleaner separation.
const CHAIN_URL = process.env.NEXT_PUBLIC_CHAIN_URL || "http://localhost:26657";

export interface ChainStatus {
  chain_id: string;
  height: number;
  time: string;
}

export interface Account {
  address: string;
  balance: number;
  nonce: number;
}

export interface TxResponse {
  type: string;
  height: number;
  service_id?: number;
  request_id?: number;
  finalized?: boolean;
}

export async function getStatus(): Promise<ChainStatus> {
  const res = await fetch(`${CHAIN_URL}/status`, { cache: "no-store" });
  if (!res.ok) throw new Error(`chain status: ${res.status}`);
  return res.json();
}

export async function getAccount(addr: string): Promise<Account> {
  const res = await fetch(`${CHAIN_URL}/accounts/${addr}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`account: ${res.status}`);
  return res.json();
}

export async function submitTx(signedTx: unknown): Promise<TxResponse> {
  const res = await fetch(`${CHAIN_URL}/tx`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(signedTx),
  });
  if (!res.ok) {
    const errBody = await res.text();
    throw new Error(`tx failed: ${res.status} ${errBody}`);
  }
  return res.json();
}
