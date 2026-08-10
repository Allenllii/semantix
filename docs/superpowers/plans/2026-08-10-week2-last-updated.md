# Visible Last-Updated Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render stable, visible update dates (homepage + documentation pages) and matching machine-readable metadata for GEO Week 2 #39.

**Architecture:** Keep `siteIdentity.lastUpdated` as the manual single source of truth for the homepage, and `geoDocuments[].lastUpdated` for each documentation page. Consume them in the server-rendered Hero, docs pages, and homepage WebPage JSON-LD, then verify the static export as the crawler-visible boundary.

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript, Node.js built-in test runner.

## Global Constraints

- The visible copy is exactly `Last updated · YYYY-MM-DD`.
- The visible homepage date uses `<time datetime="YYYY-MM-DD">` near the top of Hero.
- Each `/docs/{slug}` page and docs index card shows its own date from `geoDocuments[].lastUpdated`.
- `dateModified` uses the same `siteIdentity.lastUpdated` value.
- No build timestamp, new dependency, per-anchor repetition, or unrelated copy change.

---

### Task 1: Static-export regression test

**Files:**
- Create: `site/scripts/last-updated.test.mjs`
- Modify: `site/package.json`

**Interfaces:**
- Consumes: `site/out/index.html` produced by `npm run build`.
- Produces: `npm run test:content`, asserting crawler-visible HTML.

- [x] Write a Node test that reads `out/index.html`, expects `<time datetime="2026-08-10">Last updated · 2026-08-10</time>`, extracts JSON-LD scripts, and expects a `WebPage` object with literal `dateModified: "2026-08-10"`.
- [x] Add a second test asserting every exported docs detail page (`out/docs/{profile,profile-en,guide,guide-en}.html`) exposes its own visible date.
- [x] Add `"test:content": "node --test scripts/*.test.mjs"` to `package.json` and fold it into `npm run check` after `build`.
- [x] Run `npm run build && npm run test:content`; verify both homepage assertions fail before the implementation lands.

### Task 2: Visible and structured homepage metadata

**Files:**
- Modify: `site/src/components/Hero.tsx`
- Modify: `site/src/app/page.tsx`

**Interfaces:**
- Consumes: `siteIdentity.lastUpdated: string`.
- Produces: static `<time>` markup and one `WebPage` JSON-LD script.

- [x] Import `siteIdentity` in Hero and render `<time dateTime={siteIdentity.lastUpdated}>Last updated · {siteIdentity.lastUpdated}</time>` after the Chinese subtitle.
- [x] Define homepage WebPage JSON-LD in `page.tsx` with `url`, `name`, and `dateModified`, and serialize it using the existing `<` escaping pattern.
- [x] Run `npm run build && npm run test:content`; verify both tests pass.

### Task 2b: Visible documentation-page dates

**Files:**
- Modify: `site/src/lib/geo-docs.ts`
- Modify: `site/src/app/docs/[slug]/page.tsx`
- Modify: `site/src/app/docs/page.tsx`

**Interfaces:**
- Consumes: `geoDocuments[].lastUpdated: string`.
- Produces: one semantic `<time>` per docs detail page and per docs index card.

- [x] Add `lastUpdated: "2026-08-10"` to every entry in `geoDocuments` (same `YYYY-MM-DD` format as the homepage).
- [x] Render `<time dateTime={document.lastUpdated}>Last updated · {document.lastUpdated}</time>` in the docs detail header and on the docs index cards.
- [x] Run `npm run build && npm run test:content`; verify the docs date test passes.

### Task 3: Review and publish

**Files:**
- Review all branch changes against Issue #39 and the design spec.

**Interfaces:**
- Consumes: verified branch diff.
- Produces: pushed `codex/week2-last-updated` and a Draft PR closing #39.

- [x] Review for correctness, accessibility, structured-data validity, and scope containment.
- [x] Run fresh full verification after review changes (`npm run check` green).
- [x] Push the branch and create a Draft PR against `Gnosil/semantix:main` with an acceptance checklist and `Closes #39`.
