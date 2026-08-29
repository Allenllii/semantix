import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const siteRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const registryPath = path.join(siteRoot, "src", "lib", "docs.ts");

function manifestEntries(source) {
  return [...source.matchAll(/slug: "([^"]+)",\s+fileName: "([^"]+)"/g)].map((match) => ({
    slug: match[1],
    fileName: match[2],
  }));
}

test("documentation registry exposes the complete task-based information architecture", async () => {
  const source = await readFile(registryPath, "utf8");
  const entries = manifestEntries(source);
  const slugs = entries.map(({ slug }) => slug);

  assert.ok(entries.length >= 17, `expected at least 17 documents, found ${entries.length}`);
  assert.equal(new Set(slugs).size, slugs.length, "document slugs must be unique");

  for (const section of ["开始使用", "核心工作流", "理解机制", "集成与运维", "项目背景"]) {
    assert.match(source, new RegExp(`section: "${section}"`));
  }

  for (const entry of entries) {
    await access(path.join(siteRoot, "content", "docs", entry.fileName));
  }
});

test("user-facing workflow documents cite repository sources", async () => {
  const required = [
    "quickstart.md",
    "agent-integration.md",
    "configuration.md",
    "extract-search.md",
    "lookup-inject.md",
    "verify-observe.md",
    "cli-reference.md",
    "slices-and-cache.md",
    "retrieval-safety.md",
    "scheduling-evolution.md",
    "gateway.md",
    "storage-maintenance.md",
    "evidence-and-status.md",
  ];

  for (const fileName of required) {
    const content = await readFile(path.join(siteRoot, "content", "docs", fileName), "utf8");
    assert.match(content, /^# /, `${fileName} must start with a title`);
    assert.match(content, /## 对应仓库来源/, `${fileName} must identify its repository sources`);
  }
});

test("the full-text generator and route registry stay aligned", async () => {
  const registry = manifestEntries(await readFile(registryPath, "utf8"));
  const generator = manifestEntries(
    await readFile(path.join(siteRoot, "scripts", "generate-llms-full.mjs"), "utf8"),
  );

  assert.deepEqual(generator, registry);
});

test("exported documentation keeps navigation and copyable code blocks usable", async () => {
  const indexHtml = await readFile(path.join(siteRoot, "out", "docs", "index.html"), "utf8");
  const quickstartHtml = await readFile(
    path.join(siteRoot, "out", "docs", "quickstart", "index.html"),
    "utf8",
  );

  for (const section of ["开始使用", "核心工作流", "理解机制", "集成与运维", "项目背景"]) {
    assert.ok(indexHtml.includes(section), `docs index should expose ${section}`);
  }
  assert.match(quickstartHtml, /文档目录/);
  assert.match(quickstartHtml, /aria-label="复制代码"/);
  assert.match(quickstartHtml, /semantix extract/);
});
