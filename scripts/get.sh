#!/bin/sh
# Ferrum one-line installer for Linux — downloads the latest (or pinned)
# release, verifies its checksum, and installs it as a systemd service.
# Mirrors the get.docker.com pattern:
#
#   curl -fsSL https://raw.githubusercontent.com/anand34577/ferrum/main/scripts/get.sh | sh
#
# Pin a version and/or skip sudo re-exec (already root) with env vars:
#
#   curl -fsSL .../get.sh | FERRUM_VERSION=v1.2.3 sh
#
# This script only fetches and unpacks the release archive, then hands off
# to the packaged install.sh (see scripts/linux/install.sh) to do the actual
# system setup — the same script you'd run by hand from a downloaded
# archive, so both paths behave identically.
set -eu

REPO="anand34577/ferrum"
RAW_BASE="https://raw.githubusercontent.com/$REPO"
API_BASE="https://api.github.com/repos/$REPO"
RELEASES_BASE="https://github.com/$REPO/releases/download"

log()  { printf '%s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar  >/dev/null 2>&1 || die "tar is required"

# --- root check (mirrors get.docker.com: sudo -E, or run directly if root) ---
if [ "$(id -u)" != "0" ]; then
	if command -v sudo >/dev/null 2>&1; then
		sh_c='sudo -E sh -c'
	elif command -v su >/dev/null 2>&1; then
		sh_c='su -c'
	else
		die "this script must be run as root, and neither sudo nor su is available"
	fi
else
	sh_c='sh -c'
fi

# --- OS / arch detection ---
os="$(uname -s)"
case "$os" in
	Linux) ;;
	*) die "this installer only supports Linux (got: $os) — see the Windows service scripts (scripts/windows/) or README.md for other platforms" ;;
esac

arch="$(uname -m)"
case "$arch" in
	x86_64|amd64) arch="amd64" ;;
	aarch64|arm64) arch="arm64" ;;
	*) die "unsupported architecture: $arch (ferrum publishes linux/amd64 and linux/arm64)" ;;
esac

# --- version resolution ---
version="${FERRUM_VERSION:-}"
if [ -z "$version" ]; then
	log "==> Looking up the latest release"
	version="$(curl -fsSL "$API_BASE/releases/latest" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/')"
	[ -n "$version" ] || die "couldn't determine the latest release — set FERRUM_VERSION=vX.Y.Z to install a specific version"
fi
log "==> Installing ferrum $version ($arch)"

# --- download + verify + extract ---
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

archive="ferrum_${version}_linux_${arch}.tar.gz"
url="$RELEASES_BASE/$version/$archive"
checksums_url="$RELEASES_BASE/$version/checksums.txt"

log "==> Downloading $url"
curl -fsSL -o "$tmpdir/$archive" "$url" || die "download failed — does release $version exist for linux/$arch?"

if curl -fsSL -o "$tmpdir/checksums.txt" "$checksums_url" 2>/dev/null; then
	log "==> Verifying checksum"
	( cd "$tmpdir" && grep " \*\{0,1\}$archive\$" checksums.txt | sha256sum -c - ) \
		|| die "checksum verification failed for $archive"
else
	log "==> Warning: checksums.txt not found for $version, skipping verification"
fi

log "==> Extracting"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"
extracted_dir="$tmpdir/ferrum_${version}_linux_${arch}"
[ -d "$extracted_dir" ] || die "unexpected archive layout (no $extracted_dir)"

# --- install ---
log "==> Running install.sh"
chmod +x "$extracted_dir/install.sh"
$sh_c "cd '$extracted_dir' && ./install.sh"

log "==> Done. ferrum is installed and running as a systemd service."
log "    systemctl status ferrum"
log "    journalctl -u ferrum -f"
log "    edit /etc/ferrum/config.yaml, then: systemctl restart ferrum"
