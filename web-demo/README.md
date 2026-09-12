# Ferrum — demo build

This is a copy of `../web` (the real product frontend) with its API layer
swapped for an in-memory mock (`src/lib/demoApi.ts` + `src/lib/demoWorld.ts`)
so it runs as static files with no backend, no database, and no real Proxmox
host — used for the live demo published at `/docs/demo`.

Everything else — every page, every component, the build tooling — is the
unmodified real app. Only three things differ from `../web`:

- `src/lib/demoApi.ts` / `src/lib/demoWorld.ts` (new files): mock the REST
  API and the AI Assistant's chat stream from an in-memory fleet.
- `src/main.tsx`: installs the mock before anything renders, and uses
  `HashRouter` instead of `BrowserRouter` (GitHub Pages serves static files,
  so client-side routes need to live in the hash, not the path).
- `vite.config.ts`: relative `base` (served from a subpath, not the domain
  root) and no dev-server proxy (there's nothing to proxy to).

To rebuild after a change here, or after porting a change from `../web`:

```bash
npm install
npx vite build
```

Then copy `dist/` over `../docs/demo/`. Re-run this whenever `../web` changes
in a way the demo should reflect — this copy does not update on its own.
