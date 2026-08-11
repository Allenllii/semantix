# Standalone Blog Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish twenty Markdown articles through an independent `/blog` route with an OpenCode-inspired three-column documentation interface.

**Architecture:** Repository-root `blog/*.md` files are the only Blog content source. A server-only TypeScript library scans and validates frontmatter at build time; static Next.js routes render an index and article pages inside a shared Blog shell with client-side title search and generated page headings.

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript, Tailwind CSS 4, React Markdown, remark-gfm, Node test runner

## Global Constraints

- Move all four existing drafts and sixteen supplied articles into repository-root `blog/`.
- Remove `docs/blog/`; do not place Blog content under `docs/`.
- Preserve article wording and perform only mechanical Markdown formatting.
- Keep legacy `/docs` routes intact while changing the primary `文档 / Docs` navigation target to `/blog`.
- Generate static HTML for `/blog/` and exactly twenty `/blog/<slug>/` pages.
- Do not add a CMS, database, external search service, comments, authentication, analytics, or new runtime dependency.

---

### Task 1: Build the Blog content corpus

**Files:**
- Create: `blog/*.md` (twenty files)
- Delete: `docs/blog/*.md` (four files)

**Interfaces:**
- Produces Markdown documents with required `title`, `description`, `updated`, `group`, and `order` frontmatter.

- [ ] Copy the four existing drafts into `blog/` with their current English slugs.
- [ ] Convert each of the sixteen supplied UTF-8 text attachments into one English-slug Markdown file.
- [ ] Add frontmatter and standard H1/H2/H3 Markdown structure without rewriting prose.
- [ ] Assign one of the five approved groups and deterministic integer ordering.
- [ ] Remove the former `docs/blog/` files after verifying the twenty targets exist.

### Task 2: Add a tested build-time content library

**Files:**
- Create: `site/src/lib/blog.ts`
- Create: `site/scripts/blog-content.test.mjs`

**Interfaces:**
- Produces `BlogPostMeta`, `BlogHeading`, `listBlogPosts()`, `getBlogPost(slug)`, and `readBlogPost(post)`.
- `listBlogPosts()` returns validated metadata sorted by group and order.
- `readBlogPost()` returns body Markdown and extracted H2/H3 headings with stable IDs.

- [ ] Write a Node test that scans `../blog`, expects exactly twenty Markdown files, validates required frontmatter, unique slugs, valid group names, and removal of `../docs/blog`.
- [ ] Run `npm.cmd run test:content` and confirm the new test fails before the library/content integration is complete.
- [ ] Implement frontmatter parsing, validation, sorting, heading extraction, and Markdown heading-ID normalization in `blog.ts`.
- [ ] Extend the test to assert that every document contains one H1 and at least one H2.
- [ ] Run `npm.cmd run test:content` and confirm all content tests pass.

### Task 3: Implement the Blog documentation shell

**Files:**
- Create: `site/src/app/blog/layout.tsx`
- Create: `site/src/components/blog/BlogHeader.tsx`
- Create: `site/src/components/blog/BlogSidebar.tsx`
- Create: `site/src/components/blog/BlogSearch.tsx`
- Create: `site/src/components/blog/BlogTableOfContents.tsx`

**Interfaces:**
- Consumes sorted `BlogPostMeta[]` and active pathname.
- Produces a desktop three-column layout and responsive mobile navigation.

- [ ] Implement a sticky Semantix Blog header with brand link, GitHub link, and search control.
- [ ] Implement grouped left navigation with active-page state and title filtering.
- [ ] Implement a sticky right table of contents for H2/H3 anchors.
- [ ] Implement mobile directory controls that collapse sidebars without hiding article content.
- [ ] Keep visual styling consistent with existing Semantix colors while using the OpenCode structural model.

### Task 4: Implement static Blog routes and Markdown rendering

**Files:**
- Create: `site/src/app/blog/page.tsx`
- Create: `site/src/app/blog/[slug]/page.tsx`
- Modify: `site/src/app/globals.css`

**Interfaces:**
- Consumes `listBlogPosts()`, `getBlogPost()`, and `readBlogPost()`.
- Produces `/blog/` plus twenty statically generated article pages.

- [ ] Build `/blog/` as a documentation landing page with grouped article summaries and a direct start link.
- [ ] Add `generateStaticParams`, `generateMetadata`, and `dynamicParams = false` to the article route.
- [ ] Render Markdown with `react-markdown` and `remark-gfm`, assigning extracted IDs to H2/H3 elements.
- [ ] Add Blog-specific typography, layout, code, table, sidebar, and responsive styles without regressing `.geo-prose`.
- [ ] Add visible updated dates and group metadata to every article page.

### Task 5: Connect navigation, sitemap, and deployment triggers

**Files:**
- Modify: `site/src/components/Nav.tsx`
- Modify: `site/src/components/Footer.tsx`
- Modify: `site/src/app/sitemap.ts`
- Modify: `.github/workflows/deploy-site.yml`

**Interfaces:**
- Primary `文档 / Docs` links point to `/blog`.
- Sitemap includes `/blog/` and twenty article URLs.
- Changes to `blog/**` trigger the existing Cloudflare Pages workflow.

- [ ] Update header and footer primary Docs links from `/docs` to `/blog` while retaining a legacy GitHub Docs link.
- [ ] Generate sitemap entries from Blog metadata with per-article updated dates.
- [ ] Add `blog/**` to the deployment workflow path filter so content-only changes deploy.

### Task 6: Verify output and open local review

**Files:**
- Verify: `site/out/blog/**`

**Interfaces:**
- Produces a running local preview URL for user review.

- [ ] Run `npm.cmd run check` in `site/` and require exit code 0.
- [ ] Assert `site/out/blog/index.html` exists and exactly twenty article `index.html` files exist below it.
- [ ] Start the local development server on an available localhost port.
- [ ] Open `/blog/` in the local browser and visually inspect desktop layout, search, active navigation, article rendering, right-side anchors, and mobile responsiveness.
- [ ] Leave the local server running and provide the review URL to the user.
