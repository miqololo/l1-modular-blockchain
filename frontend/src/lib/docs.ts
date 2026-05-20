import "server-only";
import { promises as fs } from "fs";
import path from "path";

// Server-side helpers for the in-app /docs route. Markdown files are shipped
// in the container at /app/docs-source/ (see frontend/Dockerfile's COPY step
// that reads from the project's docs/ folder).

const DOCS_DIR = process.env.DOCS_DIR || "/app/docs-source";

export interface DocEntry {
  slug: string;       // URL-safe id (filename minus .md)
  filename: string;   // original filename for display
  title: string;      // first heading or fallback to filename
  description: string; // first paragraph (truncated)
  size: number;       // bytes
}

/** Curated ordering + descriptions; anything not listed appears below as auto-discovered. */
const CURATED: Array<{ slug: string; title: string; description: string }> = [
  { slug: "README",                  title: "Documentation index",       description: "Map of all integration docs — start here." },
  { slug: "TUTORIAL",                title: "Tutorial for reviewers",    description: "End-to-end walkthrough validating every protocol claim." },
  { slug: "REPRODUCIBILITY",         title: "Reproducibility checklist", description: "Step-by-step independent verification with sign-off." },
  { slug: "TUTORIAL.ru",             title: "Руководство (RU)",          description: "Полное руководство на русском." },
  { slug: "getting-started",         title: "Getting started",           description: "Boot the stack, hit the UI, run the demo." },
  { slug: "architecture",            title: "Architecture",              description: "What each component does and how they communicate." },
  { slug: "data-model",              title: "Data model",                description: "Service, Request, Result, Attestation, Event shapes." },
  { slug: "chain-api",               title: "Chain API",                 description: "HTTP + SSE reference for the L1." },
  { slug: "indexer-api",             title: "Indexer API",               description: "REST reference for denormalized reads." },
  { slug: "signing",                 title: "Signing",                   description: "Ed25519 + canonical encoding rules." },
  { slug: "tutorial-end-to-end",     title: "End-to-end flow",           description: "Sign a tx, follow it through every component." },
  { slug: "integrate-a-service",     title: "Integrate a service",       description: "Register an AI model for paid inference." },
  { slug: "integrate-an-inference-node", title: "Integrate an inference node", description: "Earn fees by serving requests." },
  { slug: "integrate-a-client",      title: "Integrate a client",        description: "Build a wallet / dApp on top." },
  { slug: "phases-and-roadmap",      title: "Phases & roadmap",          description: "What's shipped, what's coming." },
  { slug: "glossary",                title: "Glossary",                  description: "Vocabulary cheat-sheet." },
];

/** Lists all .md files in /app/docs-source, applying the curated order first. */
export async function listDocs(): Promise<DocEntry[]> {
  let files: string[] = [];
  try {
    files = (await fs.readdir(DOCS_DIR)).filter((f) => f.endsWith(".md"));
  } catch {
    return [];
  }

  const entries = await Promise.all(
    files.map(async (filename) => {
      const slug = filename.replace(/\.md$/, "");
      const full = path.join(DOCS_DIR, filename);
      let body = "";
      let size = 0;
      try {
        const stat = await fs.stat(full);
        size = stat.size;
        body = await fs.readFile(full, "utf8");
      } catch {
        /* skip */
      }
      const curated = CURATED.find((c) => c.slug === slug);
      const title =
        curated?.title ??
        extractFirstHeading(body) ??
        filename.replace(/\.md$/, "").replace(/[-_]/g, " ");
      const description = curated?.description ?? extractFirstParagraph(body);
      return { slug, filename, title, description, size };
    }),
  );

  // Sort: curated order first, then alphabetical for the rest.
  const order = new Map(CURATED.map((c, i) => [c.slug, i]));
  return entries.sort((a, b) => {
    const ai = order.get(a.slug) ?? 1000;
    const bi = order.get(b.slug) ?? 1000;
    if (ai !== bi) return ai - bi;
    return a.slug.localeCompare(b.slug);
  });
}

export async function getDoc(slug: string): Promise<{ content: string; entry: DocEntry } | null> {
  // Slug whitelist: no slashes, no .. — straight letter/number/dash/dot/underscore only.
  if (!/^[A-Za-z0-9._-]+$/.test(slug)) return null;
  const full = path.join(DOCS_DIR, `${slug}.md`);
  try {
    const content = await fs.readFile(full, "utf8");
    const stat = await fs.stat(full);
    const curated = CURATED.find((c) => c.slug === slug);
    const title =
      curated?.title ??
      extractFirstHeading(content) ??
      slug.replace(/[-_]/g, " ");
    const description = curated?.description ?? extractFirstParagraph(content);
    return {
      content,
      entry: { slug, filename: `${slug}.md`, title, description, size: stat.size },
    };
  } catch {
    return null;
  }
}

function extractFirstHeading(md: string): string | null {
  const m = md.match(/^#\s+(.+)$/m);
  return m ? m[1].trim() : null;
}

function extractFirstParagraph(md: string): string {
  const lines = md.split("\n");
  let buf: string[] = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith("#")) continue;       // skip headings
    if (trimmed.startsWith("```")) continue;     // skip code-block fences
    if (trimmed.startsWith(">")) continue;       // skip quotes
    if (!trimmed) {
      if (buf.length) break;
      continue;
    }
    buf.push(trimmed);
    if (buf.join(" ").length > 240) break;
  }
  const text = buf.join(" ").replace(/\*\*?|`/g, "");
  if (text.length <= 200) return text;
  return text.slice(0, 197) + "…";
}
