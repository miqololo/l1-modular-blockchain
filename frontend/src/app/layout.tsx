import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "aios — decentralized AI marketplace",
  description: "Phase 0.5 demo: request inference, see results finalized on-chain.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <header className="border-b border-neutral-800 px-6 py-4 flex items-center justify-between">
          <h1 className="text-xl font-semibold">
            <a href="/" className="hover:underline">aios marketplace</a>
          </h1>
          <span className="text-xs text-neutral-500">
            Phase 0.5 — real inference, unverified
          </span>
        </header>
        <main className="px-6 py-8 max-w-5xl mx-auto">{children}</main>
        <footer className="px-6 py-6 text-xs text-neutral-500 border-t border-neutral-800 mt-12">
          <p>
            Phase 0.5 inference is NOT verifiable. Fraud-proof challenges arrive in Phase 3.
            See <code>docs/PHASE.md</code> in the repo for the full roadmap.
          </p>
        </footer>
      </body>
    </html>
  );
}
