.PHONY: build run clean test lint fmt deps docker-up docker-build docker-down docker-logs test-db-up test-db-down release patch minor major

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

# -- Test database (ephemeral, no persistent volume) --

test-db-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-db-down:
	docker compose -f docker-compose.test.yml down -v

# -- Release: bump version, commit, tag, atomic-push commit+tag together --
#
# Usage:
#   make release          # patch bump (default)
#   make release LEVEL=minor
#   make release LEVEL=major
#   make patch / minor / major   # shortcuts
#
# All paths land in `_bump`, which guarantees commit + tag ship in ONE push.
# Docker workflow triggers on the tag and builds from the same commit.

LEVEL ?= patch

release: _bump

patch:
	@$(MAKE) _bump LEVEL=patch

minor:
	@$(MAKE) _bump LEVEL=minor

major:
	@$(MAKE) _bump LEVEL=major

_bump:
	@if [ -n "$$(git status --porcelain)" ]; then \
	  echo "release: working tree not clean. Commit or stash other changes first:"; \
	  git status --short; \
	  exit 1; \
	fi
	@BRANCH=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$BRANCH" != "master" ]; then \
	  echo "release: refusing to bump from branch '$$BRANCH' (must be master)"; \
	  exit 1; \
	fi
	@CURRENT=$$(cat .version) && \
	IFS='.' read -r MAJ MIN PAT <<<"$$CURRENT" && \
	case "$(LEVEL)" in \
	  patch) PAT=$$((PAT + 1)) ;; \
	  minor) MIN=$$((MIN + 1)); PAT=0 ;; \
	  major) MAJ=$$((MAJ + 1)); MIN=0; PAT=0 ;; \
	  *) echo "release: invalid LEVEL=$(LEVEL) (use patch|minor|major)"; exit 1 ;; \
	esac && \
	NEW="$$MAJ.$$MIN.$$PAT" && \
	echo "release: $$CURRENT -> $$NEW" && \
	echo "$$NEW" > .version && \
	git add .version && \
	git commit -m "v$$NEW" && \
	git tag "v$$NEW" && \
	git push --atomic origin master "v$$NEW" && \
	echo "release: pushed v$$NEW (commit + tag in one push)"
