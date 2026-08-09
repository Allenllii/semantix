# Semantix — Official Website

The official landing page for [Semantix](https://github.com/Gnosil/semantix), a self-evolving agent kernel.

Built with **Next.js 16** (App Router) + **Tailwind CSS v4** + **shadcn/ui**, white + `#2F967F` theme. Design layout inspired by [reasonix.io](https://reasonix.io/).

## Development

```bash
npm install
npm run dev        # http://localhost:3000
```

## Checks

```bash
npm run typecheck  # tsc --noEmit
npm run lint       # eslint
npm run build      # production build
npm run check      # all three
```

## Structure

```
site/
  src/
    app/           # layout, metadata, globals.css (design tokens), page assembly
    components/    # Nav, Hero, Features, Components, Roadmap, Community, Install, Footer
    lib/           # cn() utility
    types/         # content types
  public/
    seo/           # favicon
```

## License

MIT — same as the Semantix repository.
