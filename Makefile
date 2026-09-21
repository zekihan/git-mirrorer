GO ?= go
BINARY := dist/git-mirrorer
VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run fmt tidy lint test test/cover audit release-check build/release/all release container-test
build:
	mkdir -p dist
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/git-mirrorer

run: build
	./$(BINARY) $(ARGS)

fmt:
	gofmt -w cmd internal

tidy:
	$(GO) mod tidy

lint:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	golangci-lint run

test:
	$(GO) test -race -count=1 ./...

test/cover:
	mkdir -p tmp
	$(GO) test -race -coverprofile=tmp/coverage.out ./...

audit: lint test
	$(GO) mod verify

release-check:
	goreleaser check

build/release/all:
	goreleaser build --clean --snapshot --id all

release:
	goreleaser release --clean

container-test:
	./scripts/container_smoke.sh
