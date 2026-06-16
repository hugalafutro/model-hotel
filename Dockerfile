# Stage 1: Build frontend
FROM node:26-alpine AS frontend-builder

RUN npm install -g pnpm@10

WORKDIR /app/web

# pnpm-workspace.yaml carries the dependency `overrides` (pnpm 10 reads them here,
# not from package.json), so it must be present for --frozen-lockfile to match the
# lockfile's recorded overrides — otherwise ERR_PNPM_LOCKFILE_CONFIG_MISMATCH.
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,target=/pnpm-store \
    pnpm install --frozen-lockfile --store-dir=/pnpm-store

COPY web/ ./
# build:docker skips `tsc -b` — type-checking is gated by the pre-push hook and
# CI's full `pnpm run build`, so the image build stays off the typecheck path.
RUN pnpm run build:docker

# Stage 2: Build Go binary with embedded frontend + migrations
FROM golang:1.26-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Copy frontend build output into the embed target directory
COPY --from=frontend-builder /app/web/dist ./cmd/server/static/

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-X main.version=$VERSION" -o server ./cmd/server/

# Stage 3: Minimal runtime image
FROM alpine:3.24

# Upgrade base packages so security patches in the 3.24 line (e.g. OpenSSL)
# land even when the alpine:3.24 tag itself lags behind.
# Deliberate trade-off: builds are not reproducible across time (each build picks
# up the current v3.24 patch level). Preferred over pinning package versions,
# which Alpine purges from its repos once superseded, breaking the build.
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates postgresql16-client su-exec

# Create non-root user (uid 1000 matches typical host user)
RUN adduser -D -u 1000 -H appuser

WORKDIR /app

COPY --from=backend-builder /app/server .
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Ensure entrypoint and binary are executable and owned by runtime user
RUN chmod +x /usr/local/bin/docker-entrypoint.sh && \
    chown -R appuser:appuser /app

ENTRYPOINT ["docker-entrypoint.sh"]

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./server"]
