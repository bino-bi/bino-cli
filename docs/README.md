# bino CLI Documentation

Source for the bino CLI documentation site at https://cli.bino.bi.

Built with [Astro](https://astro.build/) and [Starlight](https://starlight.astro.build/).

## Development

All commands are run from the `docs/` directory:

```
npm install          # Install dependencies
npm run dev          # Start dev server at localhost:4321
npm run build        # Build production site
npm run preview      # Preview build locally
```

## Project Structure

- `src/content/docs/` — Documentation pages as `.mdx` files. Each file maps to a route based on its file name.
- `src/assets/` — Images and other assets embedded in documentation pages.
- `public/` — Static assets (favicons, etc.) served as-is.
- `astro.config.mjs` — Astro and Starlight configuration.
