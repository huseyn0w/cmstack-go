# Self-hosted fonts

Per DESIGN_SYSTEM.md §3, fonts are self-hosted (no runtime CDN) and preloaded.
The full variable `.woff2` files are committed here:

- `newsreader-var.woff2` — Newsreader (display/serif), weights 400–500
- `inter-var.woff2` — Inter (sans/UI), weights 400/500/600
- `geist-mono-var.woff2` — Geist Mono (mono/metadata)

Source: the fontsource CDN on jsDelivr (`latin-wght-normal.woff2` variable
builds). They are referenced by:

- `web/templ/layout.templ` — `<link rel="preload">` for Newsreader + Inter (the
  two critical weights); Geist Mono loads via `@font-face` without preload.
- `web/static/tokens.css` — `@font-face` declarations (`font-display: swap`).
- `web/tailwind.css` — `@theme` font families.

Subsetting these to the used weight ranges is deferred to M19; committing the
full variable woff2 now is acceptable.
