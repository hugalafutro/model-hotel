.PHONY: build run clean test lint fmt deps docker-up docker-build docker-down docker-logs totp-disable test-db-up test-db-down setup notices frontdesk-build ha-up ha-down ha-logs

VERSION := $(shell cat .version 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)
# Full SHA of the commit this binary is built from, stamped into the API package
# so the dashboard can show exactly which commit a `dev` build corresponds to.
# CI passes the full ${{ github.sha }} too; the backend shortens it for display,
# so every build path feeds the same input and yields the same app_commit.
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

build:
	go build -ldflags "-X main.version=$(VERSION) -X github.com/hugalafutro/model-hotel/internal/api.buildCommit=$(COMMIT)" -o bin/server ./cmd/server/

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
	VERSION=$(VERSION) COMMIT=$(COMMIT) docker compose -f docker-compose.yml -f compose.dev.yml up -d --build

docker-down:
	docker compose -f docker-compose.yml -f compose.dev.yml down

docker-logs:
	docker compose -f docker-compose.yml -f compose.dev.yml logs -f

# -- Front Desk control plane + HA stack --
# frontdesk-build mirrors Dockerfile.frontdesk for a local binary: build the SPA,
# copy it into the //go:embed all:webui target, then build cmd/frontdesk. The
# webui/ contents are gitignored (only .gitkeep/.gitignore are tracked), so the
# copy never dirties the tree.

HA_COMPOSE := deploy/ha/docker-compose.yml

frontdesk-build:
	cd frontdesk/web && pnpm install --frozen-lockfile && pnpm run build:docker
	find internal/frontdesk/webui -mindepth 1 ! -name .gitkeep ! -name .gitignore -delete
	cp -r frontdesk/web/dist/. internal/frontdesk/webui/
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION) -X github.com/hugalafutro/model-hotel/internal/frontdesk.buildCommit=$(COMMIT)" -o bin/frontdesk ./cmd/frontdesk/

ha-up:
	VERSION=$(VERSION) COMMIT=$(COMMIT) docker compose -f $(HA_COMPOSE) up -d --build

ha-down:
	docker compose -f $(HA_COMPOSE) down

ha-logs:
	docker compose -f $(HA_COMPOSE) logs -f

# -- TOTP 2FA emergency escape hatch (operator; deletes the admin_totp row) --

totp-disable:
	@docker compose -f docker-compose.yml -f compose.dev.yml exec -T db psql -U "$${POSTGRES_USER:-modelhotel}" -d "$${POSTGRES_DB:-modelhotel}" -c "DELETE FROM admin_totp_recovery; DELETE FROM admin_totp;"

# -- Test database (ephemeral, no persistent volume) --

test-db-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-db-down:
	docker compose -f docker-compose.test.yml down -v

# -- i18n (see tools/i18n-translate/translate.py) --
# i18n-check is the CI gate: OFFLINE locale-parity validation, no network. New
# user-facing strings are added to en.json and translated into every other
# locale by hand (see AGENTS.md "i18n").

i18n-check:
	python3 tools/i18n-translate/translate.py check

# -- Third-party license notices (see tools/gen-notices) --

notices:
	go run ./tools/gen-notices

# -- One-time setup after cloning --

setup:
	git config core.hooksPath scripts
