# GEO Week 3: Platform-Specific Optimization — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise Perplexity / Google AI Overviews / Gemini platform readiness (16.8 / 25.0 / 25.8) via `llms.txt` + `llms-full.txt`, structured data, explicit AI-crawler allowance, and a complete sitemap (GEO Week 3).

**Architecture:** Hand-maintained `llms.txt` (link index); build-generated `llms-full.txt` from `content/geo/*.md`; JSON-LD blocks sourced from existing `siteIdentity` / `geoDocuments` constants; static-export regression tests on the committed files and `out/`.

**Tech Stack:** Next.js 16 App Router (static export), React 19, TypeScript, Node.js built-in test runner. No new dependencies.

## Global Constraints

- No new dependencies, no build timestamps, no fabricated facts.
- Every schema object maps to visible/true content (FAQPage ⇄ visible FAQ section).
- Dates and URLs come only from `siteIdentity` / `geoDocuments`.
- FAQ copy stays factual, single-language-per-item, non-promotional.
- `npm run check` green at the end.

---

### Task 1: Spec-compliant llms.txt

**Files:**
- Modify: `site/public/llms.txt`

**Interfaces:**
- Consumes: existing site URLs and document titles.
- Produces: llmstxt.org-format index: H1 + blockquote summary + `##` sections, every link `[Title](URL): one-line description`.

- [ ] Rewrite `llms.txt`: `## Project` (/, /about, GitHub), `## Documentation` (/docs + 4 docs pages), `## Site` (/terms, /privacy), each line with a description.
- [ ] Keep bilingual summary only in the blockquote; link descriptions English only.

### Task 2: llms-full.txt build-time generator

**Files:**
- Create: `site/scripts/generate-llms-full.mjs`
- Create: `site/public/llms-full.txt` (committed output)
- Modify: `site/package.json`

**Interfaces:**
- Consumes: `site/content/geo/*.md` (source of truth) + document titles from the docs index.
- Produces: deterministic `public/llms-full.txt` (title, summary, link index, then full text of the 4 documents, English and Chinese kept as-is).

- [ ] Write generator: reads the 4 markdown files, emits header + `## Documentation` link index + full bodies, writes `public/llms-full.txt`.
- [ ] Add `"prebuild": "node scripts/generate-llms-full.mjs"` to `package.json` (runs before `next build`, so `out/` ships it).
- [ ] Run the generator once and commit the output file.

### Task 3: WebSite + SoftwareApplication structured data

**Files:**
- Modify: `site/src/lib/site-identity.ts` (optional: add `licenseUrl`, `inLanguage` if cleaner)
- Modify: `site/src/app/layout.tsx`

**Interfaces:**
- Consumes: `siteIdentity` fields.
- Produces: two extra JSON-LD scripts in every page `<head>`/body.

- [ ] Add `WebSite` JSON-LD (`@id` `<productUrl>/#website`, `name`, `url`, `inLanguage: ["zh-CN","en"]`, `publisher` → Organization `@id`).
- [ ] Add `SoftwareApplication` JSON-LD (`name`, `applicationCategory: DeveloperApplication`, `operatingSystem`, `license`, `codeRepository`, `offers` (free/OSS), `publisher` → Organization `@id`, `description`).
- [ ] Serialize with the existing `<`-escaping pattern.

### Task 4: Visible FAQ section + FAQPage JSON-LD (homepage)

**Files:**
- Create: `site/src/components/Faq.tsx`
- Modify: `site/src/app/page.tsx`

**Interfaces:**
- Consumes: factual Q&A pairs (from `docs/GEO.md` FAQ, condensed).
- Produces: visible `<section>` on homepage + matching `FAQPage` JSON-LD.

- [ ] Create `Faq.tsx` with 5 factual Q&A pairs (what is Semantix / problem it solves / L1-L3 cache meaning / self-evolution meaning / how to participate) — single-language copy, no slogans.
- [ ] Render `Faq` between `Install` and `Footer` in `page.tsx`.
- [ ] Add `FAQPage` JSON-LD with `mainEntity` array matching the visible questions verbatim.

### Task 5: TechnicalArticle JSON-LD on docs pages

**Files:**
- Modify: `site/src/app/docs/[slug]/page.tsx`

**Interfaces:**
- Consumes: `geoDocuments[].title/description/lastUpdated/language`.
- Produces: per-page `TechnicalArticle` JSON-LD.

- [ ] Add `TechnicalArticle` JSON-LD (`headline` = title, `description`, `dateModified` = `document.lastUpdated`, `inLanguage`, `author`/`publisher` → Organization `@id`, `mainEntityOfPage` = page URL).

### Task 6: Explicit AI-crawler allowance in robots.txt

**Files:**
- Modify: `site/public/robots.txt`

**Interfaces:**
- Consumes: AI crawler list from the audit (page 8).
- Produces: explicit `User-agent:` groups, catch-all last.

- [ ] Add explicit groups (`Allow: /`) for PerplexityBot, GPTBot, ChatGPT-User, OAI-SearchBot, ClaudeBot, anthropic-ai, Google-Extended, GoogleOther, CCBot, Amazonbot, cohere-ai, Bytespider, Applebot-Extended.
- [ ] Keep `User-agent: *` block after the named groups; keep the Sitemap line.

### Task 7: Complete the sitemap

**Files:**
- Modify: `site/src/app/sitemap.ts`

**Interfaces:**
- Consumes: `geoDocuments[].lastUpdated` + slugs.
- Produces: 4 extra `<url>` entries in `out/sitemap.xml`.

- [ ] Add `/docs/profile`, `/docs/profile-en`, `/docs/guide`, `/docs/guide-en` with `lastModified` from `geoDocuments`, `weekly`, priority 0.8.

### Task 8: Week 3 regression tests

**Files:**
- Create: `site/scripts/week3-platform.test.mjs`

**Interfaces:**
- Consumes: `public/llms.txt`, `public/llms-full.txt`, `public/robots.txt`, `out/` HTML + sitemap produced by `npm run build`.
- Produces: `npm run test:content` coverage for all Week 3 artifacts.

- [ ] Test `llms.txt`: has `##` sections; ≥8 `[Title](URL):` lines; links `/about` and all 4 docs pages.
- [ ] Test `llms-full.txt`: contains the 4 document titles; contains a `## Documentation` index; file starts with a title line.
- [ ] Test `robots.txt`: names PerplexityBot, GPTBot, ClaudeBot with `Allow: /`.
- [ ] Test homepage export: JSON-LD includes `WebSite` and `SoftwareApplication`; visible FAQ heading present; `FAQPage` JSON-LD present with the same 5 questions as the visible section.
- [ ] Test each docs export: contains `TechnicalArticle` JSON-LD with the document's `dateModified`.
- [ ] Test `out/sitemap.xml`: contains all 4 docs URLs.
- [ ] Run `npm run build && npm run test:content`; confirm new tests fail before Tasks 1–7 land, pass after.

### Task 9: Verify and review

**Files:**
- Review all branch changes against the design spec and the Week 3 findings.

**Interfaces:**
- Consumes: verified branch diff.
- Produces: green `npm run check` and a ready-to-review PR (or pushed branch).

- [ ] Run `npm run check` green (lint, typecheck, build, content tests).
- [ ] Spot-check `out/llms.txt`, `out/llms-full.txt`, `out/robots.txt` and exported JSON-LD blocks manually.
- [ ] Review diff for scope containment, no fabricated data, no unrelated copy changes.
- [ ] Note in the PR: re-run Citon on the live domain after deploy to measure Week 3 score movement.
