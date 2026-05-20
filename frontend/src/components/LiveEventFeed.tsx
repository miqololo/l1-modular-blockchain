"use client";

import { useEffect, useRef, useState } from "react";

// Live tail of chain events via the SSE endpoint `/events`. Renders the most
// recent N events with a coloured chip per event type. Filters out the noisy
// BlockCommitted keepalive (otherwise the feed scrolls every second with
// "block 503 committed" and obscures the interesting stuff).
//
// Client-only; runs in the browser so it uses NEXT_PUBLIC_CHAIN_URL which the
// Dockerfile bakes into the production bundle.

interface Event {
  type: string;
  block_height: number;
  payload: Record<string, unknown>;
  receivedAt: number;
}

const EVENT_COLORS: Record<string, string> = {
  ServiceRegistered:   "bg-emerald-500/15 text-emerald-300 border-emerald-500/30",
  ServiceUpdated:      "bg-emerald-500/10 text-emerald-300 border-emerald-500/20",
  ServiceDeactivated:  "bg-neutral-500/15 text-neutral-300 border-neutral-500/30",
  DomainRegistered:    "bg-sky-500/15    text-sky-300    border-sky-500/30",
  DomainDeactivated:   "bg-neutral-500/15 text-neutral-300 border-neutral-500/30",
  InferenceRequested:  "bg-sky-500/15    text-sky-300    border-sky-500/30",
  ResultSubmitted:     "bg-cyan-500/15   text-cyan-300   border-cyan-500/30",
  RequestFinalized:    "bg-emerald-500/15 text-emerald-300 border-emerald-500/30",
  RequestRefunded:     "bg-neutral-500/15 text-neutral-300 border-neutral-500/30",
  Challenged:          "bg-fuchsia-500/15 text-fuchsia-300 border-fuchsia-500/30",
  Vouched:             "bg-fuchsia-500/10 text-fuchsia-200 border-fuchsia-500/20",
  RequestSlashed:      "bg-rose-500/15   text-rose-300   border-rose-500/30",
  RequestDismissed:    "bg-emerald-500/10 text-emerald-300 border-emerald-500/20",
  RequestVoided:       "bg-amber-500/15  text-amber-300  border-amber-500/30",
};

export function LiveEventFeed() {
  const [events, setEvents] = useState<Event[]>([]);
  const [status, setStatus] = useState<"connecting" | "open" | "error">("connecting");
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const base = process.env.NEXT_PUBLIC_CHAIN_URL || "http://localhost:26657";
    // Filter out BlockCommitted so the feed shows protocol activity, not heartbeats.
    const url = `${base}/events?types=ServiceRegistered,ServiceUpdated,ServiceDeactivated,DomainRegistered,DomainDeactivated,InferenceRequested,ResultSubmitted,RequestFinalized,RequestRefunded,Challenged,Vouched,RequestSlashed,RequestDismissed,RequestVoided`;
    const es = new EventSource(url);
    esRef.current = es;
    es.onopen = () => setStatus("open");
    es.onerror = () => setStatus("error");

    // The chain SSE delivers one named event per type. Wire each type up;
    // any registered name gets pushed to the same list.
    const handler = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        setEvents((prev) =>
          [{ ...data, receivedAt: Date.now() }, ...prev].slice(0, 20)
        );
      } catch {
        /* ignore malformed */
      }
    };
    for (const type of Object.keys(EVENT_COLORS)) {
      es.addEventListener(type, handler);
    }

    return () => {
      es.close();
    };
  }, []);

  return (
    <div className="border border-neutral-800 rounded-lg bg-neutral-900/30">
      <div className="flex items-center justify-between px-3 py-2 border-b border-neutral-800 text-xs">
        <span className="text-neutral-500">
          {events.length === 0
            ? status === "open"
              ? "Listening… do something on the chain to see events appear."
              : status === "connecting"
                ? "Connecting…"
                : "Disconnected — check the chain URL."
            : `${events.length} recent event${events.length === 1 ? "" : "s"}`}
        </span>
        <span
          className={`inline-flex items-center gap-1.5 ${
            status === "open"
              ? "text-emerald-400"
              : status === "connecting"
                ? "text-amber-400"
                : "text-rose-400"
          }`}
        >
          <span
            className={`inline-block w-1.5 h-1.5 rounded-full ${
              status === "open"
                ? "bg-emerald-400 animate-pulse"
                : status === "connecting"
                  ? "bg-amber-400 animate-pulse"
                  : "bg-rose-400"
            }`}
          />
          {status}
        </span>
      </div>
      <ul className="max-h-72 overflow-y-auto">
        {events.length === 0 ? (
          <li className="px-3 py-6 text-sm text-neutral-600 text-center">No events yet.</li>
        ) : (
          events.map((e, idx) => (
            <li
              key={`${e.block_height}-${idx}`}
              className="flex items-center justify-between gap-3 px-3 py-2 border-b border-neutral-800/50 last:border-b-0"
            >
              <div className="flex items-center gap-2 min-w-0">
                <span
                  className={`inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium border ${
                    EVENT_COLORS[e.type] || "bg-neutral-700/20 text-neutral-400 border-neutral-700"
                  }`}
                >
                  {e.type}
                </span>
                <span className="text-xs text-neutral-500 truncate">
                  {describePayload(e.type, e.payload)}
                </span>
              </div>
              <span className="text-[11px] text-neutral-600 tabular-nums shrink-0">
                block {e.block_height}
              </span>
            </li>
          ))
        )}
      </ul>
    </div>
  );
}

// Best-effort one-line summary per event type. Uses the payload field names
// from chain/internal/types/types.go.
function describePayload(type: string, payload: Record<string, unknown>): string {
  const get = (k: string) => payload?.[k];
  switch (type) {
    case "ServiceRegistered":
      return `${get("name")} (#${get("service_id")})`;
    case "ServiceUpdated":
      return `service #${get("service_id")}`;
    case "ServiceDeactivated":
      return `service #${get("service_id")}`;
    case "DomainRegistered":
      return `domain #${get("domain_id")} · ${get("runtime_id")}`;
    case "DomainDeactivated":
      return `domain #${get("domain_id")} · ${get("requests_voided") || 0} request(s) voided`;
    case "InferenceRequested":
      return `req #${get("request_id")} · svc #${get("service_id")}`;
    case "ResultSubmitted":
      return `req #${get("request_id")} · provider ${shortAddr(get("provider"))}`;
    case "RequestFinalized":
      return `req #${get("request_id")} · paid ${(get("paid") as { amount?: number })?.amount ?? 0}`;
    case "RequestRefunded":
      return `req #${get("request_id")} · refunded`;
    case "Challenged":
      return `req #${get("request_id")} · by ${shortAddr(get("challenger"))}`;
    case "Vouched":
      return `req #${get("request_id")} · ${get("supports_provider") ? "for provider" : "for challenger"}`;
    case "RequestSlashed":
      return `req #${get("request_id")} · provider slashed`;
    case "RequestDismissed":
      return `req #${get("request_id")} · ${get("voucher_count") || 0} voucher(s) defended`;
    case "RequestVoided":
      return `req #${get("request_id")} · prior=${get("prior_status")}`;
    default:
      return "";
  }
}

function shortAddr(addr: unknown): string {
  if (typeof addr !== "string" || addr.length < 12) return String(addr || "");
  return `${addr.slice(0, 9)}…${addr.slice(-4)}`;
}
