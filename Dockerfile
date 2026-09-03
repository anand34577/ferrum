# syntax=docker/dockerfile:1

FROM node:22-alpine AS web-build
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /web/dist ./web/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH     go build -trimpath -ldflags="-s -w" -o /out/ferrum ./cmd/ferrum

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=go-build /out/ferrum /app/ferrum
COPY config.example.yaml /app/config.example.yaml
VOLUME ["/app/data"]
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/app/ferrum", "-healthcheck"]
ENTRYPOINT ["/app/ferrum"]
