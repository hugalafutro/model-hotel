# Frontend build stage
FROM node:20-alpine AS frontend-builder

WORKDIR /app/web

COPY web/package.json web/package-lock.json ./
RUN npm install

COPY web/ ./
RUN npm run build

# Backend build stage
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go mod tidy

# Copy frontend build into static directory for embedding
COPY --from=frontend-builder /app/web/dist ./cmd/server/static/

RUN go build -o server ./cmd/server/

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy backend binary
COPY --from=backend-builder /app/server .

# Also copy frontend files for filesystem fallback
COPY --from=frontend-builder /app/web/dist ./web/dist/

# Copy migrations (embedded in binary but also available for reference)
COPY --from=backend-builder /app/internal/db/migrations ./migrations/

EXPOSE 8080

CMD ["./server"]