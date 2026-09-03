#!/usr/bin/env bash
# Builds the frontend and cross-compiles the ferrum binary for one or more
# platforms, then packages each as a .tar.gz (or .zip on Windows) under
# dist/ alongside a sha256sum manifest. Used locally and by
# .github/workflows/release.yml — CI just runs this script so a local build
# always matches what a tagged release publishes.
#
# Usage:
#   scripts/build.sh                      # build for the current GOOS/GOARCH
#   scripts/build.sh linux/amd64 windows/amd64 darwin/arm64
#   scripts/build.sh all                  # every target below
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo none)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

ALL_TARGETS=(linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64)

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
  targets=("$(go env GOOS)/$(go env GOARCH)")
elif [ "${targets[0]}" = "all" ]; then
  targets=("${ALL_TARGETS[@]}")
fi

echo "==> Building frontend (web/dist)"
( cd web && npm ci && npm run build )

rm -rf dist
mkdir -p dist

echo "==> Version ${VERSION} (${COMMIT}, ${DATE})"

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"

  out_dir="dist/ferrum_${VERSION}_${goos}_${goarch}"
  mkdir -p "$out_dir"
  bin="$out_dir/ferrum${ext}"

  echo "==> Building ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -ldflags "$LDFLAGS" \
    -o "$bin" ./cmd/ferrum

  cp README.md LICENSE config.example.yaml "$out_dir/"
  if [ "$goos" = "linux" ]; then
    mkdir -p "$out_dir/packaging/systemd"
    cp packaging/systemd/ferrum.service "$out_dir/packaging/systemd/"
    cp scripts/linux/install.sh scripts/linux/uninstall.sh "$out_dir/"
  elif [ "$goos" = "windows" ]; then
    cp scripts/windows/install-service.ps1 scripts/windows/uninstall-service.ps1 "$out_dir/"
  fi

  ( cd dist
    base="ferrum_${VERSION}_${goos}_${goarch}"
    if [ "$goos" = "windows" ]; then
      zip -qr "${base}.zip" "$base"
    else
      tar -czf "${base}.tar.gz" "$base"
    fi
    rm -rf "$base"
  )
done

( cd dist && sha256sum ferrum_* > checksums.txt 2>/dev/null || shasum -a 256 ferrum_* > checksums.txt )

echo "==> Done. Artifacts in dist/:"
ls -la dist
