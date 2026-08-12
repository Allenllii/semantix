import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const readExport = (path) => readFile(new URL(`../out/${path}`, import.meta.url), "utf8");

const homepageHtml = await readExport("index.html");
const aboutHtml = await readExport("about/index.html");
const contactHtml = await readExport("contact/index.html");
const termsHtml = await readExport("terms/index.html");
const privacyHtml = await readExport("privacy/index.html");
const sitemapXml = await readExport("sitemap.xml");

test("visible contact links use a stable first-party page", () => {
  for (const [name, html] of [
    ["home", homepageHtml],
    ["about", aboutHtml],
    ["terms", termsHtml],
    ["privacy", privacyHtml],
  ]) {
    assert.match(html, /href="\/contact\/?"/, `${name} should link to /contact`);
    assert.doesNotMatch(html, /href="mailto:/, `${name} should not expose a rewritable mailto link`);
    assert.doesNotMatch(html, /cdn-cgi\/l\/email-protection/, `${name} should not ship a Cloudflare link`);
  }
});

test("contact export keeps the official email crawler-readable", () => {
  assert.match(
    contactHtml,
    /aria-label="junhaihuang@aiqueshi\.com"/,
    "contact email should remain available to assistive technology",
  );
  assert.doesNotMatch(contactHtml, />junhaihuang@aiqueshi\.com</, "email should be split to prevent edge rewriting");
  assert.doesNotMatch(contactHtml, /href="mailto:/, "contact page should not expose a rewritable mailto link");
  assert.match(sitemapXml, /<loc>https:\/\/semantix\.ensureok\.ai\/contact<\/loc>/);

  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));
  const organization = jsonLdObjects.find((value) => value["@type"] === "Organization");
  assert.equal(organization.email, "junhaihuang@aiqueshi.com");
  assert.equal(organization.contactPoint.url, "https://semantix.ensureok.ai/contact");
});

test("technical content exposes authorship, evidence, and limitations", async () => {
  const docsHtml = await readExport("docs/guide/index.html");
  const blogHtml = await readExport("blog/open-source-semantic-memory-comparison-guide/index.html");
  const visibleDocsHtml = docsHtml.replaceAll("<!-- -->", "");
  const visibleBlogHtml = blogHtml.replaceAll("<!-- -->", "");

  assert.match(visibleDocsHtml, /作者 · (Gnosil|radianceded|jh10724-dotcom|Allenli1233)/);
  assert.match(visibleDocsHtml, /证据与限制/);
  assert.match(visibleBlogHtml, /By (Gnosil|radianceded|jh10724-dotcom|Allenli1233)/);
  assert.match(visibleBlogHtml, /Evidence and limitations/);
  assert.match(visibleBlogHtml, /View source and revision history/);
});

test("maintainer identity graph uses verifiable Person profiles", () => {
  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));
  const maintainerGraph = jsonLdObjects.find((value) => Array.isArray(value["@graph"]));

  assert.equal(maintainerGraph["@graph"].length, 4);
  for (const person of maintainerGraph["@graph"]) {
    assert.equal(person["@type"], "Person");
    assert.deepEqual(person.sameAs, [person.url]);
    assert.match(person.url, /^https:\/\/github\.com\//);
  }
});

test("first-party navigation does not present GitHub as the canonical docs host", () => {
  assert.doesNotMatch(homepageHtml, />GitHub Docs</);
  assert.match(homepageHtml, /href="\/docs\/?"/);
  assert.match(homepageHtml, /href="\/blog\/?"/);
});
