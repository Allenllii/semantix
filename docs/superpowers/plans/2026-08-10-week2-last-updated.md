# Visible Last-Updated Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render one stable, visible homepage update date and matching machine-readable metadata for GEO Week 2 #39.

**Architecture:** Keep `siteIdentity.lastUpdated` as the manual single source of truth. Consume it in the server-rendered Hero and homepage WebPage JSON-LD, then verify the static export as the crawler-visible boundary.

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript, Node.js built-in test runner.

## Global Constraints

- The visible copy is exactly `Last updated · YYYY-MM-DD`.
- The visible date uses `<time datetime="YYYY-MM-DD">` near the top of Hero.
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

- [ ] Write a Node test that reads `out/index.html`, expects `<time datetime="2026-08-10">Last updated · 2026-08-10</time>`, extracts JSON-LD scripts, and expects a `WebPage` object with literal `dateModified: "2026-08-10"`.
- [ ] Add `"test:content": "node --test scripts/*.test.mjs"` to `package.json`.
- [ ] Run `npm run build && npm run test:content`; verify both assertions fail because the visible time and WebPage metadata are absent.

### Task 2: Visible and structured homepage metadata

**Files:**
- Modify: `site/src/components/Hero.tsx`
- Modify: `site/src/app/page.tsx`

**Interfaces:**
- Consumes: `siteIdentity.lastUpdated: string`.
- Produces: static `<time>` markup and one `WebPage` JSON-LD script.

- [ ] Import `siteIdentity` in Hero and render `<time dateTime={siteIdentity.lastUpdated}>Last updated · {siteIdentity.lastUpdated}</time>` after the Chinese subtitle.
- [ ] Define homepage WebPage JSON-LD in `page.tsx` with `url`, `name`, and `dateModified`, and serialize it using the existing `<` escaping pattern.
- [ ] Run `npm run build && npm run test:content`; verify both tests pass.
- [ ] Run `npm run check` and inspect exported `out/index.html`.
- [ ] Commit implementation and tests.

### Task 3: Review and publish

**Files:**
- Review all branch changes against Issue #39 and the design spec.

**Interfaces:**
- Consumes: verified branch diff.
- Produces: pushed `codex/week2-last-updated` and a Draft PR closing #39.

- [ ] Review for correctness, accessibility, structured-data validity, and scope containment.
- [ ] Run fresh full verification after review changes.
- [ ] Push the branch and create a Draft PR against `Gnosil/semantix:main` with an acceptance checklist and `Closes #39`.
