# Ferrum

Fleet control for Proxmox VE — a single dashboard for every cluster and standalone node you run, with live inventory, dashboards, backups, HA, firewall, alerting, and more.

- **Repository:** https://github.com/anand34577/ferrum
- **Backend:** Go (chi router), SQLite or PostgreSQL
- **Frontend:** React + TypeScript, Vite, Tailwind CSS v4

## Screenshots

Captured against a mock Proxmox cluster (`prod-cluster`: 3 nodes, 16 VMs/LXCs, Ceph + NFS storage) to show the UI populated the way it looks on a real fleet.

### Fleet overview

![Fleet overview dashboard](docs/screenshots/dashboard.png)

### Inventory

![Inventory — nodes and guests](docs/screenshots/inventory.png)

### Topology

![Topology graph](docs/screenshots/topology.png)

### Storage

![Storage pools and Ceph health](docs/screenshots/storage.png)

### Backups & replication

![Backup jobs and replication](docs/screenshots/backups.png)

### High availability

![HA resources and groups](docs/screenshots/high-availability.png)

### Firewall

![Cluster firewall rules](docs/screenshots/firewall.png)

### Look & feel

Three selectable UI themes — Enterprise, Proxmox-native, and Terminal — each with light/dark and an accent color.

![Appearance settings](docs/screenshots/settings-appearance.png)

### First-run setup

![First-run admin setup](docs/screenshots/setup.png)

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

On first run, open the UI and create the initial admin account, then add a Proxmox connection (host, port, and either an API token or username/password) from **Connections**.

### Docker

```bash
docker build -t ferrum .
docker run -p 8080:8080 -v ferrum-data:/app/data ferrum
```

## Deploying a release build

Every [GitHub release](https://github.com/anand34577/ferrum/releases) ships prebuilt, statically-linked archives for Linux (amd64/arm64), Windows (amd64/arm64), and macOS (amd64/arm64) — no Go toolchain or CGO dependencies needed on the target machine. Each archive bundles the binary, `config.example.yaml`, and the install script for its OS.

### Linux (systemd)

One-liner, same idea as `get.docker.com` — downloads the latest release for your architecture, verifies its checksum, and installs it as a systemd service:

```bash
curl -fsSL https://raw.githubusercontent.com/anand34577/ferrum/main/scripts/get.sh | sudo sh
```

Pin a specific version with `FERRUM_VERSION`:

```bash
curl -fsSL https://raw.githubusercontent.com/anand34577/ferrum/main/scripts/get.sh | FERRUM_VERSION=v1.2.3 sudo sh
```

Or do it by hand from a downloaded archive — `scripts/get.sh` just automates these same steps:

```bash
tar -xzf ferrum_*_linux_amd64.tar.gz
cd ferrum_*_linux_amd64
sudo ./install.sh
```

Either way, this creates a dedicated `ferrum` system user, installs the binary to `/usr/local/bin/ferrum`, seeds `/etc/ferrum/config.yaml`, and enables + starts the `ferrum.service` systemd unit (`packaging/systemd/ferrum.service`) — data lives in `/var/lib/ferrum`, logs go to `journalctl -u ferrum -f`.

To uninstall, grab [`scripts/linux/uninstall.sh`](scripts/linux/uninstall.sh) and run it as root — add `-- --purge` to also remove config/data:

```bash
curl -fsSL https://raw.githubusercontent.com/anand34577/ferrum/main/scripts/linux/uninstall.sh | sudo bash -s --
```

### Windows (Windows Service)

```powershell
Expand-Archive ferrum_*_windows_amd64.zip
cd ferrum_*_windows_amd64
.\install-service.ps1   # run as Administrator
```

This installs the binary to `%ProgramFiles%\Ferrum`, seeds `%ProgramData%\Ferrum\config.yaml`, and registers a "Ferrum" Windows service — `ferrum.exe` detects it's running under the Service Control Manager and manages its own start/stop lifecycle, no wrapper (NSSM etc.) needed. Since Windows services don't capture stdout/stderr the way systemd does, logs are written to `%ProgramData%\Ferrum\ferrum.log`. Uninstall with `.\uninstall-service.ps1` (add `-Purge` to also remove config/data).

### Building from source

```bash
scripts/build.sh                       # current platform only, output in dist/
scripts/build.sh linux/amd64 windows/amd64
scripts/build.sh all                   # every platform the release workflow builds
```

```powershell
.\scripts\build.ps1                    # Windows-native equivalent, current platform only
```

Both build the frontend, cross-compile with version info baked in (`ferrum -version`), and package each target as a `.tar.gz`/`.zip` with its install script — the same thing [`.github/workflows/release.yml`](.github/workflows/release.yml) runs when a `vX.Y.Z` tag is pushed, publishing the resulting archives (and a `checksums.txt`) as GitHub Release assets.

## Configuration

Copy `config.example.yaml` to `config.yaml` and adjust as needed, or set the equivalent `FERRUM_*` environment variables (see the comments in that file for the full list, including SQLite/PostgreSQL, TLS cookie behavior, reverse-proxy support, and optional OIDC single sign-on).

## License

[MIT](LICENSE)
