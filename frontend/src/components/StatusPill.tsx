// Shared status pill used by request lists and detail pages.
//
// Maps each chain-side RequestStatus to a color that matches its semantic
// meaning in the protocol: green = success path, red = adversarial outcome,
// amber = in-flight, grey = terminal-but-neutral (refund).

const STYLES: Record<string, { bg: string; text: string; label: string; dot: string }> = {
  PENDING:    { bg: "bg-amber-500/10",   text: "text-amber-300",   label: "Pending",    dot: "bg-amber-400" },
  SUBMITTED:  { bg: "bg-sky-500/10",     text: "text-sky-300",     label: "Submitted",  dot: "bg-sky-400" },
  CHALLENGED: { bg: "bg-fuchsia-500/10", text: "text-fuchsia-300", label: "Challenged", dot: "bg-fuchsia-400" },
  FINALIZED:  { bg: "bg-emerald-500/10", text: "text-emerald-300", label: "Finalized",  dot: "bg-emerald-400" },
  SLASHED:    { bg: "bg-rose-500/10",    text: "text-rose-300",    label: "Slashed",    dot: "bg-rose-400" },
  REFUNDED:   { bg: "bg-neutral-500/15", text: "text-neutral-300", label: "Refunded",   dot: "bg-neutral-400" },
};

export function StatusPill({ status }: { status: string }) {
  const s = STYLES[status] ?? {
    bg: "bg-neutral-700/30",
    text: "text-neutral-300",
    label: status,
    dot: "bg-neutral-500",
  };
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium ${s.bg} ${s.text}`}>
      <span className={`inline-block w-1.5 h-1.5 rounded-full ${s.dot}`} />
      {s.label}
    </span>
  );
}
