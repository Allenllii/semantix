import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const homepageHtml = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
const crawlerVisibleHtml = homepageHtml.replaceAll("<!-- -->", "");

// Single source for the expected date so a site update touches exactly one literal.
const expectedLastUpdated = "2026-08-10";

test("static homepage exposes the visible content update date", () => {
  assert.match(
    crawlerVisibleHtml,
    new RegExp(
      `<time datetime="${expectedLastUpdated}"[^>]*>Last updated · ${expectedLastUpdated}</time>`,
      "i",
    ),
  );
});

test("static homepage exposes the same date as WebPage dateModified", () => {
  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));

  const webpage = jsonLdObjects.find((value) => value["@type"] === "WebPage");

  assert.ok(webpage, "expected homepage WebPage JSON-LD");
  assert.equal(webpage.dateModified, expectedLastUpdated);
});
