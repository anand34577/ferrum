# Design

<!-- impeccable:design-schema 1 -->

## World

Glass Flight Deck — an aviation cockpit/EICAS instrument panel, not a rounded-card SaaS admin theme. Built code-led (no image generation available in this environment); the direction contract lives at `.impeccable/surfaces/web-src.md` (seed key `026d7572`, chosen card: IMPECCABLE'S PICK, "Glass Flight Deck").

## Palette

- Ground: near-black graphite, `--bg #090b0e` → `--bg-surface #12151b` → `--bg-elevated #161a22` (dark, primary theme). Light theme kept fully coherent as the secondary theme (`--bg #f8fafc` etc.), unchanged from the pre-existing ramp.
- Caution hierarchy (never decorative — earned only by state): `--status-ok` nominal green, `--status-warn` amber, `--status-error` master-caution red (rarest color on screen, reserved), `--status-info` live-data cyan (shifted from the previous blue so "telemetry updating" never reads as a link).
- Brand accent: oxide (rust/iron, unchanged) — carries primary actions and the one-off "Ferrum" identity (BrandMark, active nav), kept distinct from the caution colors exactly like a real avionics brand strip is distinct from its warning lights.

## Type

- `--font-sans` Inter (body copy only), `--font-display` **Oswald** (self-hosted `/fonts/oswald-variable.woff2`, replacing Space Grotesk), `--font-mono` JetBrains Mono. Oswald is a genuinely condensed grotesk — the single highest-leverage move in this pass, since `font-display` is already used by `CardTitle`/`DialogTitle`/`WidgetChrome` and 15 page files, so the swap re-typeset the whole app's headings at once.
- `panel-label` utility (`index.css`) now sets `font-family: var(--font-display)` explicitly (not just uppercase+tracking on inherited Inter) — every page title, nav item, group label, badge, and tab renders in true condensed Oswald caps. Page titles sit at `2.25rem` with tight `0.02em` tracking (condensed faces don't need heavy tracking at display size).
- All numeric/data fields render in `font-mono` with `.tabular` (tabular-nums) — inputs, selects, KPI values, table cells.

## Signature motifs

- **`corner-frame` utility**: two diagonal targeting-reticle corner brackets (top-left + bottom-right), used sparingly on instrument readouts only — every KPI tile and the page-header icon plate — never as blanket card decoration.
- **Master-caution bar** is now *persistent*, not just reactive: a quiet green "ALL SYSTEMS NOMINAL" strip pinned above the header when the fleet is clean, banded amber/red when it isn't. It never disappears, so the redesign's signature element is visible on every page regardless of fleet state.
- **Blueprint grid ground**: the dot-grid background was replaced with a layered fine (8px) + bold (32px) line grid — reads as a technical drawing surface, applied identically on the authenticated app and the login/setup screen.

## Radius & elevation

- `--radius-sm/md/lg/xl` tightened to 2/3/4/6px (from 6/8/12/16px) — this single token change flattened every `rounded-sm/md/lg/xl` utility app-wide to instrument-panel bezels. `rounded-2xl`/`rounded-full` usages that bypassed the tokens (badges, buttons, the command palette, track-fill bars) were hand-converted to rectangular/`rounded-sm`.
- Glossy two-tone gradients removed from Button, BrandMark, the header avatar chip, and the sidebar active-nav treatment — replaced with a flat panel color plus one hairline inset top-highlight (the physical bezel edge), never a gradient sweep.
- Card's `rail` prop no longer paints a colored `border-left` (a banned pattern) — it keeps only the faint background tint; explicit signal is a `<StatusDot>`/`<Badge>` pairing instead.

## Components

- **Badge**: rectangular annunciator chip (`rounded-sm`, bordered, `panel-label`), not a pill.
- **Switch**: rectangular panel-toggle track/thumb, not an iOS pill.
- **KpiCard**: digit-bank readout — `panel-label` micro-label, `font-mono` tabular value, flat bordered tile, squared progress rail with a caution glow only past a warn/error threshold (`--caution-glow-warn/error`).
- **MasterCautionBar** (new, `components/layout/MasterCautionBar.tsx`): reads `useFleetOverview()`/`summarizeFleet()` fleet-wide; renders nothing when nominal, a banded (`caution-band` utility) amber/red strip pinned above the header when critical alerts, offline connections, or warnings exist anywhere in the fleet. Links to `/alerts`.
- **DataTable / Input / Select / Tabs**: unchanged structurally, retuned to the flattened radius and (Input/Select) `font-mono`.

## Verification

Visually verified in-browser (Playwright, dark mode) against a live build of the app: Login, first-run Setup, Fleet Overview, Inventory (empty state), Settings (Appearance), Custom Dashboard, and Overview at a 390px mobile width. Not individually screenshotted this pass: Storage, Pools, HA, Firewall, Backups, Tasks, Alerts, Users, Audit, Connections, Topology, Node Detail, Console, Profile — these inherit the redesigned shared primitives/tokens and compile clean, but were only reviewed via component-level diffs, not rendered. The master-caution bar's *firing* (amber/red) state was reasoned through code, not observed live — the only test fleet available had zero alerts.

## Open follow-ups

- Screenshot the remaining pages listed above and fix anything page-specific the shared-component pass didn't reach.
- Force or seed a critical/warning alert to visually confirm the master-caution bar's banded state and its `/alerts` link.
- If a genuinely condensed grotesk becomes worth the added font weight later, it would sharpen the panel-label register beyond tracked-caps Inter — not required now.
