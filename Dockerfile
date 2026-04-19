# Backend build stage
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go mod tidy
RUN go build -o server ./cmd/server/

# Frontend build stage
FROM node:20-alpine AS frontend-builder

WORKDIR /app/web

COPY web/package.json web/package-lock.json ./
RUN npm install

COPY web/ ./
RUN npm run build

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy backend
COPY --from=backend-builder /app/server .
COPY --from=backend-builder /app/internal/db/migrations /app/migrations

# Copy frontend
COPY --from=frontend-builder /app/web/dist /app/web/dist

EXPOSE 8080

CMD ["./server"]
