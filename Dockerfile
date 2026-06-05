# Stage 1: Build frontend
FROM node:26-alpine AS frontend-builder

RUN npm install -g pnpm@10

WORKDIR /app/web

COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# Stage 2: Build Go binary with embedded frontend + migrations
FROM golang:1.26-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy frontend build output into the embed target directory
COPY --from=frontend-builder /app/web/dist ./cmd/server/static/

ARG VERSION=dev
RUN go build -ldflags "-X main.version=$VERSION" -o server ./cmd/server/

# Stage 3: Minimal runtime image
FROM alpine:3.23

RUN apk add --no-cache ca-certificates postgresql16-client su-exec

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
