import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const baseUrl = "https://semantix.ensureok.ai";

const documents = [
  { slug: "profile", file: "overview.md" },
  { slug: "profile-en", file: "overview.en.md" },
  { slug: "guide", file: "deep-dive.md" },
  { slug: "guide-en", file: "deep-dive.en.md" },
];

const header = `# Semantix — Full Site Text for AI Assistants

> This file aggregates the full text of the official Semantix documentation for AI assistants, complementing the link index at /llms.txt. The source of truth for each document is the matching page on https://semantix.ensureok.ai/docs/.

## Documentation

- [Semantix website](${baseUrl}/): Official homepage — positioning, features, components, roadmap, and install instructions.
- [Semantix repository](https://github.com/Gnosil/semantix): Source code, tests, design documents, and issue tracker.

## Full text

`;

const parts = [];
for (const doc of documents) {
  const body = await readFile(path.join(root, "content", "geo", doc.file), "utf8");
  parts.push(
    `---\n\n> Source: ${baseUrl}/docs/${doc.slug}\n\n${body.trim()}\n`
  );
}

const output = header + parts.join("\n");
await writeFile(path.join(root, "public", "llms-full.txt"), output, "utf8");
console.log(
  `Wrote public/llms-full.txt (${output.length} bytes, ${parts.length} documents)`
);
