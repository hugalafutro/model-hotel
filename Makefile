.PHONY: build run clean test lint fmt deps docker-up docker-build docker-down docker-logs totp-disable test-db-up test-db-down setup notices

VERSION := $(shell cat .version 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)

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
	docker compose -f docker-compose.yml -f compose.dev.yml up -d

docker-build:
	docker compose -f docker-compose.yml -f compose.dev.yml down
	VERSION=$(VERSION) docker compose -f docker-compose.yml -f compose.dev.yml up -d --build

docker-down:
	docker compose -f docker-compose.yml -f compose.dev.yml down

docker-logs:
	docker compose -f docker-compose.yml -f compose.dev.yml logs -f

# -- TOTP 2FA emergency escape hatch (operator; deletes the admin_totp row) --

totp-disable:
	@docker compose -f docker-compose.yml -f compose.dev.yml exec -T db psql -U "$${POSTGRES_USER:-modelhotel}" -d "$${POSTGRES_DB:-modelhotel}" -c "DELETE FROM admin_totp_recovery; DELETE FROM admin_totp;"

# -- Test database (ephemeral, no persistent volume) --

test-db-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-db-down:
	docker compose -f docker-compose.test.yml down -v

# -- i18n (see tools/i18n-translate/translate.py) --
# i18n-check is the CI gate: OFFLINE locale-parity validation, no network/DeepL.
# i18n-fill/bootstrap call DeepL (DEEPL_API_KEY) and are LEGACY/optional: the
# free DeepL quota is exhausted (HTTP 456 until ~2027), so translate new keys by
# hand into all locales instead (see AGENTS.md "i18n"). check still passes offline.

i18n-check:
	python3 tools/i18n-translate/translate.py check

i18n-fill:
	python3 tools/i18n-translate/translate.py fill

# -- Third-party license notices (see tools/gen-notices) --

notices:
	go run ./tools/gen-notices

# -- One-time setup after cloning --

setup:
	git config core.hooksPath scripts
