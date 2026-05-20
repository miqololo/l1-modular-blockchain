import Link from "next/link";
import { notFound } from "next/navigation";
import { getDoc, listDocs } from "@/lib/docs";

export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: { params: { slug: string } }) {
  const doc = await getDoc(params.slug);
  return { title: doc ? `${doc.entry.title} — aios docs` : "Documentation — aios" };
}

export default async function DocPage({ params }: { params: { slug: string } }) {
  const [doc, all] = await Promise.all([getDoc(params.slug), listDocs()]);
  if (!doc) notFound();

  return (
    <div className="grid lg:grid-cols-[200px_1fr] gap-8">
      {/* Sidebar — list of all docs */}
      <aside className="space-y-2 lg:sticky lg:top-6 self-start">
        <Link href="/docs" className="text-xs text-neutral-500 hover:text-neutral-300">
          ← all docs
        </Link>
        <nav className="space-y-1 mt-3">
          {all.map((d) => (
            <Link
              key={d.slug}
              href={`/docs/${d.slug}`}
              className={`block px-2 py-1 rounded text-xs transition ${
                d.slug === params.slug
                  ? "bg-emerald-500/10 text-emerald-300"
                  : "text-neutral-400 hover:text-neutral-200 hover:bg-neutral-900"
              }`}
            >
              {d.title}
            </Link>
          ))}
        </nav>
      </aside>

      <article className="min-w-0 prose-docs">
        <header className="mb-6 pb-4 border-b border-neutral-800">
          <h1 className="text-3xl font-bold text-neutral-100">{doc.entry.title}</h1>
          <p className="text-xs text-neutral-500 mt-2 font-mono">{doc.entry.filename}</p>
        </header>
        <MarkdownRendered content={doc.content} />
      </article>
    </div>
  );
}

// Minimal in-house markdown renderer: enough for our docs (headings, lists,
// code blocks, tables, links, bold/italic). No external deps. Server-rendered
// (HTML strings produced once on the server, no client JS).
function MarkdownRendered({ content }: { content: string }) {
  const html = renderMarkdown(content);
  return (
    <div
      className="text-neutral-300 leading-relaxed [&_h1]:text-3xl [&_h1]:font-bold [&_h1]:text-neutral-100 [&_h1]:mt-10 [&_h1]:mb-3 [&_h2]:text-2xl [&_h2]:font-semibold [&_h2]:text-neutral-100 [&_h2]:mt-10 [&_h2]:mb-3 [&_h2]:pb-2 [&_h2]:border-b [&_h2]:border-neutral-800 [&_h3]:text-lg [&_h3]:font-semibold [&_h3]:text-neutral-200 [&_h3]:mt-7 [&_h3]:mb-2 [&_h4]:font-semibold [&_h4]:text-neutral-200 [&_h4]:mt-5 [&_h4]:mb-1 [&_p]:my-3 [&_a]:text-emerald-400 [&_a:hover]:text-emerald-300 [&_a]:underline [&_a]:underline-offset-2 [&_code]:bg-neutral-900 [&_code]:text-emerald-300 [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-[0.85em] [&_pre]:bg-neutral-950 [&_pre]:border [&_pre]:border-neutral-800 [&_pre]:rounded-lg [&_pre]:p-4 [&_pre]:my-4 [&_pre]:overflow-x-auto [&_pre]:text-sm [&_pre_code]:bg-transparent [&_pre_code]:text-neutral-300 [&_pre_code]:p-0 [&_ul]:my-3 [&_ul]:list-disc [&_ul]:pl-6 [&_ul]:space-y-1 [&_ol]:my-3 [&_ol]:list-decimal [&_ol]:pl-6 [&_ol]:space-y-1 [&_li]:text-neutral-300 [&_blockquote]:border-l-2 [&_blockquote]:border-emerald-500/50 [&_blockquote]:pl-4 [&_blockquote]:text-neutral-400 [&_blockquote]:italic [&_blockquote]:my-3 [&_table]:my-4 [&_table]:w-full [&_table]:text-sm [&_th]:text-left [&_th]:font-semibold [&_th]:text-neutral-200 [&_th]:px-3 [&_th]:py-2 [&_th]:border-b [&_th]:border-neutral-700 [&_td]:px-3 [&_td]:py-2 [&_td]:border-b [&_td]:border-neutral-800/50 [&_td]:text-neutral-300 [&_strong]:text-neutral-100 [&_strong]:font-semibold [&_em]:italic [&_hr]:my-8 [&_hr]:border-neutral-800"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

// ── markdown → html (server-only; no untrusted input). Supports:
//    # ## ### #### headings
//    paragraphs
//    - and * bullet lists
//    1. ordered lists
//    > blockquotes
//    ```lang fenced code blocks (escaped)
//    `inline code` (escaped)
//    [text](url) links (target=_blank for absolute http)
//    **bold** *italic*
//    | tables |
//    --- horizontal rule
function renderMarkdown(md: string): string {
  const lines = md.split("\n");
  const out: string[] = [];
  let i = 0;

  const flush = (block: string[], kind: string) => {
    if (block.length === 0) return;
    if (kind === "p") {
      out.push(`<p>${inlineMD(block.join(" ").trim())}</p>`);
    }
    block.length = 0;
  };

  let para: string[] = [];

  while (i < lines.length) {
    const line = lines[i];

    // fenced code block
    if (line.startsWith("```")) {
      flush(para, "p");
      const lang = line.slice(3).trim();
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) {
        code.push(escapeHTML(lines[i]));
        i++;
      }
      i++; // closing fence
      const langClass = lang ? ` class="language-${escapeHTML(lang)}"` : "";
      out.push(`<pre><code${langClass}>${code.join("\n")}</code></pre>`);
      continue;
    }

    // headings
    const h = line.match(/^(#{1,6})\s+(.+)$/);
    if (h) {
      flush(para, "p");
      const level = h[1].length;
      out.push(`<h${level}>${inlineMD(h[2])}</h${level}>`);
      i++;
      continue;
    }

    // horizontal rule
    if (/^---+$/.test(line.trim())) {
      flush(para, "p");
      out.push("<hr/>");
      i++;
      continue;
    }

    // blockquote (single-line; multi-line collapsed)
    if (line.startsWith(">")) {
      flush(para, "p");
      const lines2: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) {
        lines2.push(lines[i].replace(/^>\s?/, ""));
        i++;
      }
      out.push(`<blockquote>${inlineMD(lines2.join(" ").trim())}</blockquote>`);
      continue;
    }

    // table — detect a row of pipes + the alignment row right after
    if (line.includes("|") && i + 1 < lines.length && /^\|?[\s:|-]+\|?$/.test(lines[i + 1])) {
      flush(para, "p");
      const header = splitRow(line);
      i += 2; // skip header + alignment
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
        rows.push(splitRow(lines[i]));
        i++;
      }
      let html = "<table><thead><tr>";
      for (const cell of header) html += `<th>${inlineMD(cell)}</th>`;
      html += "</tr></thead><tbody>";
      for (const row of rows) {
        html += "<tr>";
        for (const cell of row) html += `<td>${inlineMD(cell)}</td>`;
        html += "</tr>";
      }
      html += "</tbody></table>";
      out.push(html);
      continue;
    }

    // unordered list
    if (/^[-*]\s+/.test(line)) {
      flush(para, "p");
      const items: string[] = [];
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        items.push(inlineMD(lines[i].replace(/^[-*]\s+/, "")));
        i++;
      }
      out.push(`<ul>${items.map((it) => `<li>${it}</li>`).join("")}</ul>`);
      continue;
    }

    // ordered list
    if (/^\d+\.\s+/.test(line)) {
      flush(para, "p");
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
        items.push(inlineMD(lines[i].replace(/^\d+\.\s+/, "")));
        i++;
      }
      out.push(`<ol>${items.map((it) => `<li>${it}</li>`).join("")}</ol>`);
      continue;
    }

    // blank line — flush paragraph
    if (line.trim() === "") {
      flush(para, "p");
      i++;
      continue;
    }

    // accumulate paragraph
    para.push(line);
    i++;
  }
  flush(para, "p");

  return out.join("\n");
}

function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|")) s = s.slice(0, -1);
  return s.split("|").map((c) => c.trim());
}

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function inlineMD(s: string): string {
  let out = escapeHTML(s);
  // inline code (escape inside)
  out = out.replace(/`([^`]+)`/g, (_, code) => `<code>${code}</code>`);
  // bold
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  // italic (single asterisk; avoid mid-word collisions by requiring non-alphanum boundaries)
  out = out.replace(/(^|[^\w*])\*([^*\n]+)\*(?=[^\w*]|$)/g, "$1<em>$2</em>");
  // links [text](url)
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, text, url) => {
    const isAbs = /^https?:\/\//.test(url);
    const safeUrl = url.replace(/"/g, "&quot;");
    return isAbs
      ? `<a href="${safeUrl}" target="_blank" rel="noopener noreferrer">${text}</a>`
      : `<a href="${rewriteRelativeLink(safeUrl)}">${text}</a>`;
  });
  return out;
}

// Rewrite repo-relative markdown links so they work inside /docs/<slug>.
// Examples:
//   `signing.md` → `/docs/signing`
//   `./signing.md` → `/docs/signing`
//   `../README.md` → `/docs/README`
// Links into folders that aren't shipped in the container (e.g. an internal
// development workspace) become harmless in-page anchors.
function rewriteRelativeLink(url: string): string {
  // Strip leading ./ or ../
  const u = url.replace(/^\.\//, "").replace(/^\.\.\//, "");
  // Dot-prefixed paths point at folders not present in the container build;
  // turn them into anchor links so they don't 404 — the original path is
  // visible in the href for traceability.
  if (u.startsWith(".")) return `#${u}`;
  // Same-folder .md → /docs/slug
  const m = u.match(/^([A-Za-z0-9._-]+)\.md(#.*)?$/);
  if (m) return `/docs/${m[1]}${m[2] || ""}`;
  return u;
}
