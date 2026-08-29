import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const baseUrl = "https://semantix.ensureok.ai";

const documents = [
  { slug: "quickstart", fileName: "quickstart.md" },
  { slug: "agent-integration", fileName: "agent-integration.md" },
  { slug: "configuration", fileName: "configuration.md" },
  { slug: "extract-search", fileName: "extract-search.md" },
  { slug: "lookup-inject", fileName: "lookup-inject.md" },
  { slug: "verify-observe", fileName: "verify-observe.md" },
  { slug: "cli-reference", fileName: "cli-reference.md" },
  { slug: "slices-and-cache", fileName: "slices-and-cache.md" },
  { slug: "retrieval-safety", fileName: "retrieval-safety.md" },
  { slug: "scheduling-evolution", fileName: "scheduling-evolution.md" },
  { slug: "gateway", fileName: "gateway.md" },
  { slug: "storage-maintenance", fileName: "storage-maintenance.md" },
  { slug: "evidence-and-status", fileName: "evidence-and-status.md" },
  { slug: "profile", fileName: "overview.md" },
  { slug: "guide", fileName: "deep-dive.md" },
  { slug: "profile-en", fileName: "overview.en.md" },
  { slug: "guide-en", fileName: "deep-dive.en.md" },
];

const header = `# Semantix — Full Site Text for AI Assistants

> This file aggregates the full text of the official Semantix documentation for AI assistants, complementing the link index at /llms.txt. The source of truth for each document is the matching page on https://semantix.ensureok.ai/docs/.

## Documentation

- [Semantix website](${baseUrl}/): Official homepage — positioning, features, components, roadmap, and install instructions.
- [Semantix repository](https://github.com/Gnosil/semantix): Source code, tests, design documents, and issue tracker.
- [Benchmarks and evidence](${baseUrl}/benchmarks): Reproducible commands, repository test boundaries, and the labeled retrieval fixture.
- [Evidence methodology](${baseUrl}/evidence/methodology): E0–E3 labels explaining what each result does and does not establish.

## Full text

`;

const parts = [];
for (const doc of documents) {
  const body = await readFile(path.join(root, "content", "docs", doc.fileName), "utf8");
  parts.push(
    `---\n\n> Source: ${baseUrl}/docs/${doc.slug}\n\n${body.trim()}\n`
  );
}

const output = header + parts.join("\n");
await writeFile(path.join(root, "public", "llms-full.txt"), output, "utf8");
console.log(
  `Wrote public/llms-full.txt (${output.length} bytes, ${parts.length} documents)`
);
