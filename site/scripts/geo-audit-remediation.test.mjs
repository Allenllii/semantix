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
    /<!--email_off-->junhaihuang@aiqueshi\.com<!--\/email_off-->/,
    "contact email should opt out of Cloudflare obfuscation",
  );
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

  assert.match(docsHtml, /Semantix 维护团队撰写/);
  assert.match(docsHtml, /证据与限制/);
  assert.match(blogHtml, /By Semantix maintainers/);
  assert.match(blogHtml, /Evidence and limitations/);
  assert.match(blogHtml, /Source and revision history/);
});
