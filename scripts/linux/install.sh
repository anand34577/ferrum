#!/usr/bin/env bash
# Installs ferrum as a systemd service: creates a dedicated system user,
# copies the binary to /usr/local/bin, seeds /etc/ferrum/config.yaml from
# config.example.yaml (leaving an existing one untouched), and enables +
# starts the ferrum.service unit.
#
# Run as root from an extracted release archive, where this script sits
# next to ferrum, config.example.yaml, and packaging/systemd/ferrum.service
# (the layout scripts/build.sh produces). Also works from a source checkout,
# where those live two directories up from scripts/linux/.
#
# Usage: sudo ./install.sh [path-to-ferrum-binary]
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "error: must be run as root (sudo ./install.sh)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/packaging/systemd/ferrum.service" ]; then
  PKG_ROOT="$SCRIPT_DIR"                              # flattened release archive
else
  PKG_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"          # repo source checkout
fi

BIN_SRC="${1:-$PKG_ROOT/ferrum}"
UNIT_SRC="$PKG_ROOT/packaging/systemd/ferrum.service"
CONFIG_EXAMPLE="$PKG_ROOT/config.example.yaml"

BIN_DST=/usr/local/bin/ferrum
ETC_DIR=/etc/ferrum
DATA_DIR=/var/lib/ferrum
UNIT_DST=/etc/systemd/system/ferrum.service

for f in "$BIN_SRC" "$UNIT_SRC" "$CONFIG_EXAMPLE"; do
  if [ ! -f "$f" ]; then
    echo "error: expected file not found: $f" >&2
    echo "run this from an extracted ferrum release archive, or pass the binary path explicitly" >&2
    exit 1
  fi
done

echo "==> Creating system user 'ferrum'"
if ! id ferrum >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --no-create-home --shell /usr/sbin/nologin ferrum
fi

echo "==> Installing binary to $BIN_DST"
install -m 0755 -o root -g root "$BIN_SRC" "$BIN_DST"

echo "==> Setting up $ETC_DIR and $DATA_DIR"
mkdir -p "$ETC_DIR" "$DATA_DIR"
if [ ! -f "$ETC_DIR/config.yaml" ]; then
  install -m 0640 -o root -g ferrum "$CONFIG_EXAMPLE" "$ETC_DIR/config.yaml"
  # Point the seeded config at the standard data directory instead of the
  # example's relative ./data path.
  sed -i 's#^\(\s*path:\s*\).*#\1'"$DATA_DIR"'/ferrum.db#' "$ETC_DIR/config.yaml"
  echo "    wrote $ETC_DIR/config.yaml (edit before exposing this beyond localhost)"
else
  echo "    $ETC_DIR/config.yaml already exists, leaving it alone"
fi
chown -R ferrum:ferrum "$DATA_DIR"
chmod 0750 "$DATA_DIR"

echo "==> Installing systemd unit"
install -m 0644 "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload
systemctl enable --now ferrum.service

echo "==> Done. Status:"
systemctl --no-pager status ferrum.service || true
echo
echo "Logs:   journalctl -u ferrum -f"
echo "Config: $ETC_DIR/config.yaml"
echo "Data:   $DATA_DIR"
