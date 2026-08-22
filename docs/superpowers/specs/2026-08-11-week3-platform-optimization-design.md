# GEO Week 3: Platform-Specific Optimization

## Goal

Raise GEO platform readiness for the three weakest engines — Perplexity (16.8/100), Google AI Overviews (25.0/100), Gemini (25.8/100) — by strengthening the machine-consumable signals each platform actually uses: a spec-compliant `llms.txt`, a full-text `llms-full.txt`, richer structured data, explicit AI-crawler allowances, and a complete sitemap. ChatGPT (50.0) and Bing Copilot (57.5) already pass; they are preserved, not regressed.

Source: Citon GEO audit 2026-08-10 (semantix.ensureok.ai), 30-Day Action Plan Week 3.

## Background Facts (from the audit)

- `llms.txt` exists but is 11 lines / 3 links — does not follow the llmstxt.org format (missing per-link descriptions, no section structure). This is the single biggest Perplexity lever.
- No `llms-full.txt` at all.
- Schema currently shipped: Organization (layout) + WebPage (home). Missing: WebSite, SoftwareApplication, FAQPage, TechnicalArticle.
- robots.txt is `User-agent: *` only — every AI crawler is `NOT_MENTIONED` (works today, but leaves readiness to default behavior).
- Sitemap lists only site-level entries; the 4 static docs pages (`/docs/profile`, `/docs/profile-en`, `/docs/guide`, `/docs/guide-en`) are omitted.

## Design Decisions

### 1. `llms.txt` — hand-maintained, llmstxt.org compliant

Rewrite `site/public/llms.txt` as: H1 title, blockquote summary (English first, one-line Chinese), then `##` sections (`Project`, `Documentation`, `Site`) where every link line is `[Title](URL): one-line description`. Keep it human-maintained: it is a small link index, and it must match reality (5 docs links, 2 legal links, 2 external links).

### 2. `llms-full.txt` — generated at build time, committed

Perplexity explicitly recommends `llms-full.txt` for full-text grounding. Content = the four GEO documents (`content/geo/*.md`) plus a link index header. Sources of truth stay in `content/geo/`; a Node script (`site/scripts/generate-llms-full.mjs`) concatenates them deterministically into `site/public/llms-full.txt`, wired as a `prebuild` step so the static export always ships the current text. The generated file is committed so diffs are reviewable.

### 3. Structured data — additive, no fabrication

- `layout.tsx`: add `WebSite` JSON-LD (`name`, `url`, `inLanguage`, `publisher` → existing Organization `@id`) and `SoftwareApplication` JSON-LD (`applicationCategory: DeveloperApplication`, `operatingSystem`, `license: MIT`, `codeRepository`, `offers` free/open-source, `publisher` → Organization `@id`).
- Homepage: add a visible compact FAQ section (5 factual Q&A pairs taken from the authoritative `docs/GEO.md` FAQ) **and** matching `FAQPage` JSON-LD in `page.tsx`. Policy rule: FAQPage structured data must correspond to visible content — so the visible section lands first.
- `/docs/[slug]`: add `TechnicalArticle` JSON-LD per page (`headline`, `description`, `dateModified` from `geoDocuments[].lastUpdated`, `inLanguage`, `author`/`publisher` → Organization `@id`).
- `sameAs`: only real links exist (GitHub + company site). Add the company site link to Organization `sameAs`; do not invent social profiles to chase the "5 links" heuristic — fabricated signals are worse than none for E-E-A-T.
- Person schema: skipped — no named individual maintainer exists on the site; inventing one would be fabrication.

### 4. robots.txt — explicit AI crawler allowance

Add explicit groups (`Allow: /`) for: `PerplexityBot`, `GPTBot`, `ChatGPT-User`, `OAI-SearchBot`, `ClaudeBot`, `anthropic-ai`, `Google-Extended`, `GoogleOther`, `CCBot`, `Amazonbot`, `cohere-ai`, `Bytespider`, `Applebot-Extended`. Keep the catch-all `User-agent: *` group last and the Sitemap line.

### 5. Sitemap — complete the static inventory

Add the 4 docs pages with `lastModified` from `geoDocuments[].lastUpdated` (single source of truth), docs pages `weekly`/priority 0.8.

### 6. Regression tests

New `site/scripts/week3-platform.test.mjs` (Node built-in runner, same pattern as `last-updated.test.mjs`), asserting on committed files and the `out/` static export:
- `llms.txt`: has `##` sections, ≥8 link lines, every link line contains `(URL):`.
- `llms-full.txt`: contains all 4 document titles and a `## Documentation` index.
- `robots.txt`: names `PerplexityBot`, `GPTBot`, `ClaudeBot`; `Allow: /` groups present.
- Homepage export: JSON-LD contains `WebSite` and `SoftwareApplication` objects.
- Homepage export: visible FAQ section present AND `FAQPage` JSON-LD present.
- Each docs export: `TechnicalArticle` JSON-LD with correct `dateModified`.
- Sitemap export: contains all 4 docs URLs.

## Constraints

- No new dependencies; no build timestamps; no fabricated facts (links, people, metrics).
- Every schema addition maps to visible/true content.
- `siteIdentity` / `geoDocuments` remain the single sources of truth for dates, names, URLs.
- Visible homepage FAQ copy must stay factual and non-promotional (audit criticizes marketing filler — do not add more).
- Keep bilingual duplication out of the FAQ section (audit flagged bilingual repetition).

## Out of Scope

- Re-running Citon (needs operator's tool/credentials; suggested post-deploy verification step).
- Google Search Console verification / Bing Webmaster (needs operator account access).
- Moving docs off GitHub to the own domain (Week 4+ / discovery longer-term item).
- Content E-E-A-T rewrite (Week 2 territory; partially landed).

## Validation

- `npm run check` (lint + typecheck + build + content tests) green.
- Manual spot-check of `out/llms.txt`, `out/llms-full.txt`, `out/robots.txt`, exported JSON-LD blocks.
- Post-deploy: re-run Citon audit on the live domain and compare Week 3 scores; record results in the PR.

## Acceptance Mapping

- Perplexity (16.8): spec-compliant `llms.txt` + `llms-full.txt` + explicit `PerplexityBot` allowance.
- Google AI Overviews (25.0): visible FAQ + `FAQPage` JSON-LD + `WebSite` JSON-LD + existing E-E-A-T work.
- Gemini (25.8): `WebSite`/`SoftwareApplication`/`TechnicalArticle` schema + complete sitemap.
- No regression on ChatGPT / Bing Copilot.
