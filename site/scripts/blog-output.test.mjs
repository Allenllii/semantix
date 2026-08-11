import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const outRoot = path.resolve(import.meta.dirname, "../out");

test("static export contains the Blog index and twenty article pages", async () => {
  const indexHtml = await readFile(path.join(outRoot, "blog", "index.html"), "utf8");
  assert.match(indexHtml, /Semantix Documentation/i);
  assert.match(indexHtml, /Search articles/i);

  const entries = await readdir(path.join(outRoot, "blog"), { withFileTypes: true });
  const articleDirectories = entries.filter(
    (entry) => entry.isDirectory() && !entry.name.startsWith("__next"),
  );
  assert.equal(articleDirectories.length, 20);

  for (const entry of articleDirectories) {
    const html = await readFile(path.join(outRoot, "blog", entry.name, "index.html"), "utf8");
    assert.match(html, /On this page/i, `${entry.name} should render a table of contents`);
    assert.match(html, /Last updated/i, `${entry.name} should render update metadata`);
  }
});
