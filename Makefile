.PHONY: build run clean test lint fmt deps docker-up docker-build docker-down docker-logs test-db-up test-db-down

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/server ./cmd/server/

run: build
	./bin/server

clean:
	rm -rf bin/
	rm -rf data/

test:
	go test -v ./...

lint:
	golangci-lint run ./...

fmt:
	find ./internal ./cmd -name '*.go' -type f | xargs gci write -s standard -s default -s "Prefix(github.com/hugalafutro/model-hotel)"
	go fmt ./...

deps:
	go mod tidy

docker-up:
	docker compose up -d

docker-build:
	VERSION=$(VERSION) docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# -- Test database (ephemeral, no persistent volume) --

test-db-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-db-down:
	docker compose -f docker-compose.test.yml down -v
