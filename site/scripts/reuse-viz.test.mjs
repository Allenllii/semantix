import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const srcRoot = path.resolve(import.meta.dirname, "../src");
const repoRoot = path.resolve(import.meta.dirname, "../..");

test("homepage ships a reuse-visualization terminal mockup", async () => {
  const componentsSource = await readFile(
    path.join(srcRoot, "components/Components.tsx"),
    "utf8",
  );

  for (const marker of [
    "REUSE VISUALIZATION",
    "semantix dashboard",
    "💰 Cost saved",
    "🎯 Hit rate",
    "🗂 Zone distribution",
    "📦 Reused slices",
    "from:session-",
    "3/10 hits in 2 sessions",
  ]) {
    assert.ok(componentsSource.includes(marker), `Components.tsx should show ${marker}`);
  }
  assert.match(componentsSource, /示意输出/);
  assert.match(componentsSource, /真实数据以 semantix verify 门禁报告为准/);
});

test("README documents the dashboard example and zone legend", async () => {
  const readme = await readFile(path.join(repoRoot, "README.md"), "utf8");

  assert.match(readme, /# Reuse Visualization/);
  assert.match(readme, /semantix dashboard/);
  assert.match(readme, /💰 Cost saved/);
  assert.match(readme, /🎯 Hit rate/);
  assert.match(readme, /🗂 Zone distribution/);
  assert.match(readme, /📦 Reused slices/);

  for (const legendRow of ["✅", "🟡", "❌"]) {
    assert.ok(readme.includes(legendRow), `README zone legend should include ${legendRow}`);
  }
  assert.match(readme, /hit \/ PASS/);
  assert.match(readme, /grey \/ WARN/);
  assert.match(readme, /miss \/ FAIL/);
  assert.match(readme, /CLI v2 \(U19–U27\) ✅/);
});

test("homepage export renders the reuse dashboard", async () => {
  const home = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
  const visible = home.replaceAll("<!-- -->", "");

  assert.ok(visible.includes("REUSE VISUALIZATION"), "export should contain the mockup header");
  assert.ok(visible.includes("semantix dashboard"), "export should show the dashboard command");
  assert.ok(visible.includes("Cost saved"), "export should show the cost-savings bar");
});
