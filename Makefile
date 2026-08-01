.PHONY: test-core bench-core test-core-pbt lint-core-deps test-all build-controller build-server build-cli build-web helm-lint lint docker-build docker-push

## Core Engine targets

test-core:
	go test ./internal/core/... -v -race -count=1

bench-core:
	go test ./internal/core/... -bench=. -benchmem -count=3

test-core-pbt:
	go test ./internal/core/... -v -run=TestProperty -count=1

lint-core-deps:
	@echo "Checking core domain has no external imports..."
	@imports=$$(go list -f '{{join .Imports "\n"}}' ./internal/core/domain/ 2>/dev/null | grep -v "^$$"); \
	bad=$$(echo "$$imports" | grep -v -E "^(fmt|time|sort|strings|math|slices|maps|cmp|errors|github.com/weauratech/aura-power/internal)"); \
	if [ -n "$$bad" ]; then \
		echo "ERROR: Core domain has external dependencies:"; \
		echo "$$bad"; \
		exit 1; \
	fi; \
	echo "OK: Core domain is stdlib-only"

## Lint

lint:
	golangci-lint run
	helm lint ./charts/aura-power

## Build targets

build-server:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/aura-power-server ./cmd/server/

build-controller:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/aura-power-controller ./cmd/controller/

build-cli:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/aura-power ./cmd/aura-power/

build-web:
	cd web && npm run build

build-all: build-server build-controller build-cli build-web

## Docker targets

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

docker-build:
	docker build -f Dockerfile.server -t ghcr.io/weauratech/aura-power-server:$(VERSION) .
	docker build -f Dockerfile.controller -t ghcr.io/weauratech/aura-power-controller:$(VERSION) .

docker-push:
	docker push ghcr.io/weauratech/aura-power-server:$(VERSION)
	docker push ghcr.io/weauratech/aura-power-controller:$(VERSION)

## Helm targets

helm-lint:
	helm lint ./charts/aura-power

helm-template:
	helm template aura-power ./charts/aura-power

## All targets

test-all: lint-core-deps test-core
	go test ./internal/integration/ -count=1
	@echo "All tests passed!"
