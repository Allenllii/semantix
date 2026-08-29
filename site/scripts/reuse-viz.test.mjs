import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const repoRoot = path.resolve(import.meta.dirname, "../..");

test("README keeps the zone icons and points to the reuse walkthrough", async () => {
  const readme = await readFile(path.join(repoRoot, "README.md"), "utf8");

  assert.match(readme, /🟢 hit · 🟡 grey · ⚪ miss/);
  assert.match(readme, /TECHNICAL-OVERVIEW\.md#reuse-visualization/);

  const readmeZh = await readFile(
    path.join(repoRoot, "README.zh-CN.md"),
    "utf8",
  );
  assert.match(readmeZh, /🟢 hit · 🟡 grey · ⚪ miss/);
  assert.match(readmeZh, /TECHNICAL-OVERVIEW\.zh-CN\.md#复用可视化/);
});

test("technical overview documents the full reuse walkthrough and zone legend", async () => {
  const doc = await readFile(
    path.join(repoRoot, "docs", "TECHNICAL-OVERVIEW.md"),
    "utf8",
  );

  assert.match(doc, /# Reuse Visualization/);
  assert.match(doc, /semantix dashboard/);
  assert.match(doc, /💰 Cost savings/);
  assert.match(doc, /🎯 Cache hit rate \(L3\/L2\)/);
  assert.match(doc, /🗂 Zone distribution/);
  assert.match(doc, /📦 Slice library/);

  // Two icon families: retrieval zones vs. the verify gate.
  assert.match(doc, /🟢 hit · 🟡 grey · ⚪ miss/);
  assert.match(doc, /✅hit · 🟡grey · ❌miss/);
  assert.match(doc, /✅ PASS · ⚠ WARN · ❌ FAIL/);
  assert.match(doc, /# ✅ PASS relevance=75\.0% \(≥70%\)/);
  assert.match(doc, /🎯 3\/3 hits in 3 sessions/);

  assert.match(doc, /CLI v2 \(U19–U27\) ✅/);
  assert.match(doc, /CLI reuse viz \(U28–U31\) ✅/);
});
