#!/usr/bin/env bash
# Stops and removes the ferrum systemd service and binary. Configuration
# (/etc/ferrum) and data (/var/lib/ferrum) are kept by default — pass
# --purge to remove those too, and --purge-user to also drop the 'ferrum'
# system user.
#
# Usage: sudo ./uninstall.sh [--purge] [--purge-user]
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "error: must be run as root (sudo ./uninstall.sh)" >&2
  exit 1
fi

purge=false
purge_user=false
for arg in "$@"; do
  case "$arg" in
    --purge) purge=true ;;
    --purge-user) purge_user=true ;;
    *) echo "unknown argument: $arg" >&2; exit 1 ;;
  esac
done

echo "==> Stopping and disabling ferrum.service"
systemctl disable --now ferrum.service 2>/dev/null || true

echo "==> Removing systemd unit and binary"
rm -f /etc/systemd/system/ferrum.service
systemctl daemon-reload
rm -f /usr/local/bin/ferrum

if $purge; then
  echo "==> Purging /etc/ferrum and /var/lib/ferrum"
  rm -rf /etc/ferrum /var/lib/ferrum
else
  echo "==> Keeping /etc/ferrum and /var/lib/ferrum (pass --purge to remove them)"
fi

if $purge_user; then
  echo "==> Removing system user 'ferrum'"
  userdel ferrum 2>/dev/null || true
fi

echo "==> Done."
