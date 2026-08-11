# GEO Week 2 #39: Visible Last-Updated Metadata

## Goal

Make the homepage content date visible near the top of the page and expose the same date as machine-readable `dateModified` metadata. Reuse the existing manually maintained `siteIdentity.lastUpdated` value so normal builds never invent a new date.

## Scope

- Display `Last updated · YYYY-MM-DD` in the homepage Hero, after the Chinese subtitle and before the descriptive paragraph.
- Display each documentation page's own `Last updated · YYYY-MM-DD` in the docs detail header and on the docs index cards.
- Render every date with a semantic `<time datetime="YYYY-MM-DD">` element.
- Add homepage `WebPage` JSON-LD with `dateModified` sourced from the same identity value.
- Preserve the About page date display.
- Do not introduce automatic build timestamps, per-anchor dates, or unrelated homepage copy changes.

## Design

The existing `siteIdentity.lastUpdated` field remains the single source of truth for the homepage. `Hero.tsx` imports it and renders a visually secondary monospace line. `page.tsx` emits a `WebPage` JSON-LD block containing the homepage URL, name, and `dateModified`; JSON serialization escapes `<` consistently with the existing Organization JSON-LD implementation.

Documentation pages carry their own `lastUpdated` in the `geoDocuments` metadata (`site/src/lib/geo-docs.ts`), formatted identically (`YYYY-MM-DD`) and manually maintained alongside the content, so each document's date reflects its own material update time rather than the site-wide date.

All homepage anchors (`#features`, `#components`, `#roadmap`, `#community`, and `#start`) belong to this one document, so one prominent page-level date covers them without repeating identical metadata in every section.

## Validation

- A regression test checks that Hero renders a semantic visible date from `siteIdentity.lastUpdated`.
- A regression test checks that homepage WebPage structured data uses the same value for `dateModified`.
- A regression test checks every exported docs detail page (`/docs/{profile,profile-en,guide,guide-en}`) exposes its own visible date.
- `npm run check` verifies lint, types, static export, and the content regression tests.
- The exported homepage and docs HTML are inspected to confirm the visible dates and structured metadata are present without client interaction.

## Acceptance Mapping

- Homepage date near the top: Hero placement.
- Major content anchors dated: they are sections of the dated homepage document.
- Docs pages dated: each `/docs/{slug}` page and docs index card shows its own `Last updated`.
- Centralized and stable source: `siteIdentity.lastUpdated` (homepage) and `geoDocuments[].lastUpdated` (docs) are manually maintained.
- Maintenance consistency: About, Hero, homepage structured data, and docs pages consume the same explicit content date where applicable.
- Crawler-readable text: static server-rendered `<time>` output.
