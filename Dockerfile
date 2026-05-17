# Frontend build stage
FROM node:20-alpine AS frontend-builder

RUN corepack enable && corepack prepare pnpm@latest --activate

WORKDIR /app/web

COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# Backend build stage
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go mod tidy

# Copy frontend build into static directory for embedding
COPY --from=frontend-builder /app/web/dist ./cmd/server/static/

ARG VERSION=dev
RUN go build -ldflags "-X main.version=$VERSION" -o server ./cmd/server/

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates postgresql16-client

WORKDIR /app

# Copy backend binary
COPY --from=backend-builder /app/server .

# Also copy frontend files for filesystem fallback
COPY --from=frontend-builder /app/web/dist ./web/dist/

# Copy migrations (embedded in binary but also available for reference)
COPY --from=backend-builder /app/internal/db/migrations ./migrations/

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./server"]