# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS node

# Build the application, embedded browser assets, and health checker.
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath

# Keep Node/npm in the builder only; use the same asset pipeline as development.
COPY --from=node /usr/local/ /usr/local/

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    npm run generate \
    && npm run css \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o bin/ticketopia ./cmd/ticketopia \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o bin/healthcheck healthcheck.go

# Includes CA certificates and timezone data, without a shell/package manager.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build --chown=65532:65532 /app/bin/ticketopia ./
COPY --from=build --chown=65532:65532 /app/bin/healthcheck /healthcheck

USER 65532:65532
EXPOSE 8080

HEALTHCHECK --interval=30s \
    --timeout=3s \
    --start-period=10s \
    --retries=3 \
    CMD ["/healthcheck"]

ENTRYPOINT ["/app/ticketopia"]
