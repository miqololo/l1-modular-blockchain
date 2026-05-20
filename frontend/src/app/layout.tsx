import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = {
  title: "aios — decentralized AI marketplace",
  description:
    "Optimistic fraud-proof verification of AI inference on a modular L1. Demo with TinyLlama.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-neutral-950 text-neutral-100 min-h-screen flex flex-col">
        <header className="border-b border-neutral-800 bg-neutral-900/50 backdrop-blur">
          <div className="max-w-6xl mx-auto px-6 py-4 flex items-center justify-between gap-6">
            <Link href="/" className="flex items-center gap-3 group">
              <span className="inline-flex w-8 h-8 items-center justify-center rounded-md bg-gradient-to-br from-emerald-500 to-sky-500 text-neutral-950 font-bold text-sm">
                ai
              </span>
              <div className="leading-tight">
                <div className="text-base font-semibold group-hover:text-emerald-300 transition">
                  aios
                </div>
                <div className="text-[11px] text-neutral-500">
                  decentralized AI marketplace
                </div>
              </div>
            </Link>
            <nav className="flex items-center gap-5 text-sm">
              <Link href="/" className="text-neutral-300 hover:text-emerald-300 transition">
                Services
              </Link>
              <Link href="/requests" className="text-neutral-300 hover:text-emerald-300 transition">
                Requests
              </Link>
              <Link
                href="/docs"
                className="text-neutral-300 hover:text-emerald-300 transition"
              >
                Docs
              </Link>
            </nav>
          </div>
        </header>

        <main className="flex-1 max-w-6xl w-full mx-auto px-6 py-8">{children}</main>

        <footer className="border-t border-neutral-800 mt-12">
          <div className="max-w-6xl mx-auto px-6 py-6 text-xs text-neutral-500 flex flex-col sm:flex-row gap-2 sm:items-center sm:justify-between">
            <p>
              Phase 3.z marketplace demo · real inference, real signatures, real
              dispute economics.
            </p>
            <p className="text-neutral-600">
              Fraud-proof game shipped: bonds + vouchers + sybil resistance + cross-host determinism.
            </p>
          </div>
        </footer>
      </body>
    </html>
  );
}
