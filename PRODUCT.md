# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Sysadmins and infrastructure operators running Proxmox VE clusters and standalone nodes — the people on-call for uptime, backups, and capacity. They work from a desktop/laptop browser, often with multiple nodes or clusters open at once, and reach for Ferrum to see fleet-wide state at a glance and act (start/stop/migrate VMs, check backups, manage HA, firewall rules, storage) without jumping between each node's native Proxmox web UI.

## Product Purpose

Ferrum is fleet control for Proxmox VE: a single dashboard that aggregates every cluster and standalone node an operator runs, showing live inventory, health, and dashboards, and centralizing backups, HA, firewall, alerting, storage, users, and tasks. It exists because Proxmox's own UI is per-node/per-cluster; Ferrum's job is to be the one place that spans all of them. Success is an operator trusting Ferrum's view enough to work from it instead of hopping between native Proxmox UIs.

## Positioning

Single-pane-of-glass fleet management across multiple independent Proxmox clusters and standalone nodes — not a theme skin on top of one cluster's UI, but aggregation and control spanning infrastructure Proxmox itself treats as separate.

## Operating Context

- Backend: Go (chi router), SQLite or PostgreSQL. Frontend: React + TypeScript, Vite, Tailwind CSS v4, Radix UI primitives, shadcn-style component layer, TanStack Query/Table, @xyflow/react (topology graph), @novnc/novnc (embedded console), framer-motion.
- Routes/surfaces: Dashboard, Overview, Inventory, Node Detail, Storage, Pools, HA, Firewall, Backups, Tasks, Alerts, Users, Audit, Connections, Topology, Console (embedded noVNC), Settings, Profile, Setup, Login.
- Admin-gated routes exist (`RequireAdmin`) mirroring backend permission checks — the UI must keep visibly distinguishing admin-only areas.
- Long-running/background operations surface as Tasks; live data (rates, status) streams into dashboards — the UI must handle live-updating and loading/error/empty states throughout, not just first paint.
- Primarily used in dark mode; light mode is a secondary/supported theme, not the default assumption.

## Capabilities and Constraints

- Data density is a hard constraint: tables and dashboards (nodes, VMs, tasks, alerts) must stay information-dense and scannable, not padded out like a marketing/landing surface.
- Stack is fixed for this redesign: Tailwind v4 + Radix primitives + the existing shadcn-style component layer. No new UI kit or component library dependency.
- This is an Operate-mode product throughout (task completion, scanability, native web-app conventions outrank decorative expression); brand personality should live in restrained, precise details, not loud marketing-style visuals.
- A redesign was already underway before this session (uncommitted changes across UI primitives, layout, and several pages) — treat that in-progress work as evidence of direction/intent, not as a frozen constraint.

## Evidence on Hand

No existing design tokens doc, brand guide, or marketing assets found in-repo beyond the app's own code. `REMAINING_WORK.md` at repo root tracks in-progress engineering tasks unrelated to this redesign unless it says otherwise.

## Product Principles

1. Fleet-wide clarity over single-node polish — every surface should read as "state across your whole fleet," even when zoomed into one node.
2. Density with hierarchy — pack real operational data tightly, but use type/color/spacing to make the one thing that needs attention (a failing backup, a down node, a firewall drift) unmissable.
3. Dark-first, not dark-only — design and verify in dark mode first; light mode must remain fully coherent, not an afterthought.
4. Trust through precision — status, timestamps, and numbers must feel exact and current; the UI should never look like it's guessing.
5. No visual weight without operational meaning — color, motion, and emphasis are earned by state (critical/warning/healthy/running), not decoration.
