import Link from "next/link";
import { listDocs } from "@/lib/docs";

export const dynamic = "force-dynamic";
export const metadata = { title: "Documentation — aios" };

export default async function DocsIndexPage() {
  const docs = await listDocs();

  return (
    <div className="space-y-8">
      <header className="space-y-2">
        <Link href="/" className="text-xs text-neutral-500 hover:text-neutral-300">
          ← marketplace
        </Link>
        <h1 className="text-3xl font-bold">Documentation</h1>
        <p className="text-neutral-400 max-w-2xl">
          Everything you need to understand the protocol, integrate with it, or
          verify its claims. Documents are served straight from the repository's{" "}
          <code className="text-emerald-300">docs/</code> folder.
        </p>
      </header>

      {docs.length === 0 ? (
        <div className="border border-neutral-800 rounded-lg p-6 text-sm text-neutral-500">
          No documentation files were found in the deployed container. This usually
          means the image was built without the <code>docs/</code> directory — check{" "}
          <code>frontend/Dockerfile</code>.
        </div>
      ) : (
        <ul className="grid sm:grid-cols-2 gap-3">
          {docs.map((d) => (
            <li key={d.slug}>
              <Link
                href={`/docs/${d.slug}`}
                className="block h-full border border-neutral-800 hover:border-emerald-500/60 rounded-lg p-4 transition group"
              >
                <div className="flex items-baseline justify-between gap-3">
                  <h2 className="font-medium text-neutral-100 group-hover:text-emerald-300">
                    {d.title}
                  </h2>
                  <span className="text-[10px] text-neutral-600 shrink-0">
                    {d.filename}
                  </span>
                </div>
                {d.description && (
                  <p className="text-sm text-neutral-400 mt-1.5">{d.description}</p>
                )}
                <p className="text-[11px] text-neutral-600 mt-2 tabular-nums">
                  {formatSize(d.size)}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
