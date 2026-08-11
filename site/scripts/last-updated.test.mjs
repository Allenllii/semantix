import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const homepageHtml = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
const crawlerVisibleHtml = homepageHtml.replaceAll("<!-- -->", "");

// Single source for the expected date so a site update touches exactly one literal.
const expectedLastUpdated = "2026-08-11";

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

test("static docs pages expose each document's last-updated date", async () => {
  const docSlugs = ["profile", "profile-en", "guide", "guide-en"];

  for (const slug of docSlugs) {
    const docHtml = await readFile(new URL(`../out/docs/${slug}/index.html`, import.meta.url), "utf8");
    const crawlerVisibleDocHtml = docHtml.replaceAll("<!-- -->", "");

    assert.match(
      crawlerVisibleDocHtml,
      new RegExp(
        `<time dateTime="${expectedLastUpdated}"[^>]*>Last updated · ${expectedLastUpdated}</time>`,
        "i",
      ),
      `docs/${slug} should expose the visible last-updated date`,
    );
  }
});
