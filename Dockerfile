# syntax=docker/dockerfile:1

## --platform=$BUILDPLATFORM pins these two stages to the runner's own
# architecture (amd64) instead of the target platform buildx is building for.
# Neither stage needs to run target-arch code — the web build only produces
# static JS/CSS, and Go cross-compiles (GOOS/GOARCH below) without ever
# executing arm64 instructions. Without this pin, buildx runs BOTH stages
# under QEMU user-mode emulation for a linux/arm64 build: npm/Node under
# QEMU is known to hang outright rather than just run slow, which is what
# turned a normal few-minute image build into a 360-minute (GitHub's hard
# cap) stuck job. Only the final base image below stays platform-matched —
# that's just filesystem layers, nothing to execute.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web-build
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /web/dist ./web/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH     go build -trimpath -ldflags="-s -w" -o /out/ferrum ./cmd/ferrum

# "base" (not "static"): Ferrum's own binary is CGO_ENABLED=0/static and
# would run fine on "static", but the bundled Needle 2 CLI (internal/needle)
# is a dynamically-linked glibc binary (needs libc.so.6/libm.so.6/
# libpthread.so.0/libdl.so.2 and the glibc dynamic linker) — "static" ships
# no libc at all, so spawning that subprocess fails outright in this image.
# "base" includes glibc, fixing that, while still carrying no shell/package
# manager.
FROM gcr.io/distroless/base-debian12
LABEL org.opencontainers.image.source="https://github.com/anand34577/ferrum" \
      org.opencontainers.image.description="Ferrum — a fleet-control UI for Proxmox VE" \
      org.opencontainers.image.licenses="MIT"
WORKDIR /app
COPY --from=go-build /out/ferrum /app/ferrum
COPY config.example.yaml /app/config.example.yaml
VOLUME ["/app/data"]
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/app/ferrum", "-healthcheck"]
ENTRYPOINT ["/app/ferrum"]
