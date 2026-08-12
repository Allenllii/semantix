import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const llmsTxt = await readFile(new URL("../public/llms.txt", import.meta.url), "utf8");
const llmsFullTxt = await readFile(new URL("../public/llms-full.txt", import.meta.url), "utf8");
const robotsTxt = (await readFile(new URL("../public/robots.txt", import.meta.url), "utf8")).replaceAll("\r\n", "\n");
const homepageHtml = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
const faqHtml = await readFile(new URL("../out/docs/faq/index.html", import.meta.url), "utf8");
const sitemapXml = await readFile(new URL("../out/sitemap.xml", import.meta.url), "utf8");

const jsonLdObjects = [...homepageHtml.matchAll(
  /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
)].map((match) => JSON.parse(match[1]));

const docSlugs = ["profile", "profile-en", "guide", "guide-en"];
const docTitles = [
  "Semantix 项目速览",
  "Semantix Project Overview",
  "Semantix 深度解读：从零理解这个项目",
  "Semantix Deep Dive: Understanding the Project from Scratch",
];

test("llms.txt follows the llmstxt.org link-index format", () => {
  const linkLines = [...llmsTxt.matchAll(/^[-*]\s*\[[^\]]+\]\([^)]+\): .+$/gm)].map((m) => m[0]);
  assert.ok(llmsTxt.includes("## Project"), "llms.txt should have a Project section");
  assert.ok(llmsTxt.includes("## Documentation"), "llms.txt should have a Documentation section");
  assert.ok(llmsTxt.includes("## Site"), "llms.txt should have a Site section");
  assert.ok(linkLines.length >= 8, `expected >= 8 described links, got ${linkLines.length}`);
  assert.match(llmsTxt, /\[About Semantix\]\(https:\/\/semantix\.ensureok\.ai\/about\)/, "about link");
  for (const slug of docSlugs) {
    assert.ok(
      llmsTxt.includes(`https://semantix.ensureok.ai/docs/${slug}`),
      `llms.txt should link /docs/${slug}`,
    );
  }
});

test("llms-full.txt aggregates all four documents with an index", () => {
  assert.ok(llmsFullTxt.startsWith("# "), "llms-full.txt should start with an H1 title");
  assert.ok(llmsFullTxt.includes("## Documentation"), "llms-full.txt should have a Documentation index");
  assert.ok(llmsFullTxt.includes("## Full text"), "llms-full.txt should have a Full text section");
  for (const title of docTitles) {
    assert.ok(llmsFullTxt.includes(title), `llms-full.txt should contain "${title}"`);
  }
});

test("robots.txt explicitly allows the major AI crawlers", () => {
  for (const crawler of ["PerplexityBot", "GPTBot", "ClaudeBot", "Google-Extended", "CCBot"]) {
    assert.ok(
      robotsTxt.includes(`User-agent: ${crawler}\nAllow: /`),
      `robots.txt should allow ${crawler}`,
    );
  }
  assert.match(robotsTxt, /Sitemap: https:\/\/semantix\.ensureok\.ai\/sitemap\.xml/, "sitemap line");
});

test("homepage export ships WebSite and SoftwareApplication JSON-LD", () => {
  const website = jsonLdObjects.find((value) => value["@type"] === "WebSite");
  assert.ok(website, "expected WebSite JSON-LD");
  assert.equal(website.name, "Semantix");
  assert.deepEqual(website.publisher, { "@id": "https://www.ensureok.ai/#organization" });

  const softwareApplication = jsonLdObjects.find((value) => value["@type"] === "SoftwareApplication");
  assert.ok(softwareApplication, "expected SoftwareApplication JSON-LD");
  assert.equal(softwareApplication.applicationCategory, "DeveloperApplication");
  assert.equal(softwareApplication.codeRepository, "https://github.com/Gnosil/semantix");
});

test("docs FAQ page visible content matches FAQPage JSON-LD", () => {
  const faqJsonLdObjects = [...faqHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));
  const faq = faqJsonLdObjects.find((value) => value["@type"] === "FAQPage");
  assert.ok(faq, "expected FAQPage JSON-LD");
  assert.equal(faq.mainEntity.length, 5);

  const visibleHtml = faqHtml.replaceAll("<!-- -->", "");
  for (const question of faq.mainEntity) {
    assert.ok(
      visibleHtml.includes(question.name),
      `visible docs FAQ should contain the question "${question.name}"`,
    );
    assert.ok(
      visibleHtml.includes(question.acceptedAnswer.text),
      `visible docs FAQ should contain the answer for "${question.name}"`,
    );
  }
});

test("docs exports ship TechArticle JSON-LD with correct dates", async () => {
  for (const slug of docSlugs) {
    const docHtml = await readFile(new URL(`../out/docs/${slug}/index.html`, import.meta.url), "utf8");
    const docJsonLd = [...docHtml.matchAll(
      /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
    )].map((match) => JSON.parse(match[1]));

    const article = docJsonLd.find((value) => value["@type"] === "TechArticle");
    assert.ok(article, `docs/${slug} should ship TechArticle JSON-LD`);
    assert.equal(article.datePublished, "2026-08-10", `docs/${slug} datePublished`);
    assert.equal(article.dateModified, "2026-08-12", `docs/${slug} dateModified`);
    assert.equal(article.author["@type"], "Person", `docs/${slug} author type`);
    assert.match(article.author.url, /^https:\/\/github\.com\//, `docs/${slug} author profile`);
    assert.equal(article.mainEntityOfPage, `https://semantix.ensureok.ai/docs/${slug}`);
  }
});

test("sitemap lists all static docs pages", () => {
  for (const slug of docSlugs) {
    assert.ok(
      sitemapXml.includes(`<loc>https://semantix.ensureok.ai/docs/${slug}</loc>`),
      `sitemap should contain /docs/${slug}`,
    );
  }
  assert.ok(
    sitemapXml.includes("<loc>https://semantix.ensureok.ai/docs/faq</loc>"),
    "sitemap should contain /docs/faq",
  );
});
