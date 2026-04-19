.PHONY: build run clean test deps docker-up docker-down docker-logs

build:
	go build -o server ./cmd/server/

run: build
	./server

clean:
	rm -f server
	rm -rf data/

test:
	go test -v ./...

deps:
	go mod tidy

docker-up:
	docker compose up -d db

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
