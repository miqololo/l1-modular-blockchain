// Typed fetch wrappers around the indexer's REST API.
//
// Schema mirrors indexer/internal/store. Zod parsing flags drift loudly.
import { z } from "zod";

const INDEXER_URL = process.env.NEXT_PUBLIC_INDEXER_URL || "http://localhost:8081";

export const Service = z.object({
  id: z.number(),
  owner: z.string(),
  name: z.string(),
  description: z.string(),
  price_denom: z.string(),
  price_amount: z.number(),
  verification_domain_id: z.number(),
  active: z.boolean(),
  created_at_height: z.number(),
});
export type Service = z.infer<typeof Service>;

export const InferenceRequest = z.object({
  id: z.number(),
  service_id: z.number(),
  requester: z.string(),
  input_hash: z.string(),
  input_uri: z.string(),
  input_text: z.string(),
  escrow_denom: z.string(),
  escrow_amount: z.number(),
  deadline_height: z.number(),
  status: z.string(),
  created_at_height: z.number(),
  finalized_at_height: z.number().nullable().optional(),
  output_hash: z.string().nullable().optional(),
  output_uri: z.string().nullable().optional(),
  output_text: z.string().nullable().optional(),
  provider: z.string().nullable().optional(),
  paid_denom: z.string().nullable().optional(),
  paid_amount: z.number().nullable().optional(),
});
export type InferenceRequest = z.infer<typeof InferenceRequest>;

export const Stats = z.object({
  services_total: z.number(),
  requests_total: z.number(),
  requests_finalized: z.number(),
  requests_pending: z.number(),
  requests_refunded: z.number(),
});
export type Stats = z.infer<typeof Stats>;

const ListEnvelope = <T extends z.ZodTypeAny>(item: T) =>
  z.object({ items: z.array(item) });

async function fetchJSON<T>(path: string, schema: z.ZodType<T>): Promise<T> {
  const res = await fetch(`${INDEXER_URL}${path}`, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`indexer ${path}: ${res.status}`);
  }
  return schema.parse(await res.json());
}

export async function listServices() {
  return fetchJSON("/services", ListEnvelope(Service));
}

export async function getService(id: number) {
  return fetchJSON(`/services/${id}`, Service);
}

export async function listRequests(opts?: { serviceId?: number; status?: string }) {
  const params = new URLSearchParams();
  if (opts?.serviceId) params.set("service_id", String(opts.serviceId));
  if (opts?.status) params.set("status", opts.status);
  const qs = params.toString();
  const suffix = qs ? `?${qs}` : "";
  return fetchJSON(`/requests${suffix}`, ListEnvelope(InferenceRequest));
}

export async function getRequest(id: number) {
  return fetchJSON(`/requests/${id}`, InferenceRequest);
}

export async function getStats() {
  return fetchJSON(`/stats/summary`, Stats);
}
