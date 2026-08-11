# Standalone Blog Documentation Design

## Goal

Publish all Semantix long-form articles through an independent `/blog` documentation experience. Blog source files live in the repository-root `blog/` directory and never under `docs/`.

## Content Scope

- Migrate the four existing Markdown drafts from `docs/blog/`.
- Import the sixteen supplied Citon text articles.
- Preserve article wording; apply only mechanical Markdown formatting, frontmatter, heading structure, and encoding repair.
- Remove `docs/blog/` after its four articles are migrated.
- The resulting source of truth is exactly twenty Markdown files under `blog/`.

## Public Routes

- `/blog/` is the documentation index and initial article experience.
- `/blog/<slug>/` renders one article as statically generated HTML.
- The existing `/docs` implementation remains intact for legacy GEO documents, but the primary website navigation item labelled `文档 / Docs` points to `/blog/`.
- Blog article URLs never use `/docs/blog/` or `/docs/`.

## Information Architecture

Articles are grouped using frontmatter into:

1. Evaluation Guides
2. Semantic Slices
3. Semantic Cache
4. Scheduling & Harness
5. Go & Framework Independence

Each document contains `title`, `description`, `updated`, `group`, and `order` metadata. Slugs are stable English filenames.

## Interface

The layout takes structural inspiration from the supplied OpenCode documentation screenshot while retaining Semantix branding:

- Sticky top bar with Semantix identity, GitHub link, and article-title search.
- Scrollable left sidebar with grouped article navigation and active-page state.
- Center column with readable Markdown typography, code blocks, tables, lists, and source links.
- Sticky right sidebar generated from the current article's H2 and H3 headings.
- Desktop uses a three-column documentation layout. Mobile collapses both sidebars behind compact controls and keeps the article as the primary surface.
- Search filters the left navigation by title; no external service or full-text index is introduced.

## Components and Data Flow

- `blog/*.md` is the content source.
- A server-only content library scans Markdown, parses frontmatter, validates unique slugs, extracts headings, and returns sorted metadata/content.
- `app/blog/page.tsx` renders the index and directs readers into the article collection.
- `app/blog/[slug]/page.tsx` enumerates all slugs using `generateStaticParams`, renders Markdown, and emits per-article metadata.
- Shared Blog shell components render the header, grouped navigation, search, mobile navigation, and table of contents.
- Sitemap generation includes `/blog/` and all twenty article URLs.

## Failure Handling

- Missing or duplicate slugs fail the build.
- Missing required frontmatter fails the build with the affected filename.
- Unknown article routes return the existing static 404 behavior.
- Empty search results show a local no-results message without affecting article rendering.

## Verification

- Confirm exactly twenty Markdown files exist under `blog/` and `docs/blog/` no longer exists.
- Confirm every article has valid metadata and a unique slug.
- Run lint, typecheck, and production build through `npm run check`.
- Verify static output contains `/blog/index.html` and twenty article pages.
- Inspect desktop and mobile layouts, active navigation, title search, Markdown rendering, and right-side heading links.

## Non-Goals

- Do not rewrite, deduplicate, merge, or fact-check article claims in this change.
- Do not add a CMS, database, external search service, comments, authentication, or analytics.
- Do not remove the existing legacy `/docs` routes.
