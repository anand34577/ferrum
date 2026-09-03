# Ferrum

Fleet control for Proxmox VE — a single dashboard for every cluster and standalone node you run, with live inventory, dashboards, backups, HA, firewall, alerting, and more.

- **Repository:** https://github.com/anand34577/ferrum
- **Backend:** Go (chi router), SQLite or PostgreSQL
- **Frontend:** React + TypeScript, Vite, Tailwind CSS v4

## Getting started

```bash
go build ./cmd/ferrum
./ferrum -config config.example.yaml
```

The web UI is served from the same binary (see `web/embed.go`). For frontend development:

```bash
cd web
npm install
npm run dev
```

See [REMAINING_WORK.md](REMAINING_WORK.md) for the current in-progress task list.
