import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const homepageHtml = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
const crawlerVisibleHtml = homepageHtml.replaceAll("<!-- -->", "");

test("static homepage exposes the visible content update date", () => {
  assert.match(
    crawlerVisibleHtml,
    /<time datetime="2026-08-10"[^>]*>Last updated · 2026-08-10<\/time>/i,
  );
});

test("static homepage exposes the same date as WebPage dateModified", () => {
  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));

  const webpage = jsonLdObjects.find((value) => value["@type"] === "WebPage");

  assert.ok(webpage, "expected homepage WebPage JSON-LD");
  assert.equal(webpage.dateModified, "2026-08-10");
});
