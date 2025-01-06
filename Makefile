# Change these variables as necessary.
MAIN_PACKAGE_PATH := $(shell pwd)
BINARY_NAME := $(shell basename $(MAIN_PACKAGE_PATH))
OS_INFO := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH_INFO := $(shell uname -m | sed 's/x86_64/amd64/')
TMP_DIR := ./tmp
GO_EXECUTABLE := go


# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	${GO_EXECUTABLE} fmt ./...
	${GO_EXECUTABLE} mod tidy -v

## audit: run quality control checks
.PHONY: audit
audit:
	${GO_EXECUTABLE} mod verify
	${GO_EXECUTABLE} vet ./...
	${GO_EXECUTABLE} run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	${GO_EXECUTABLE} run golang.org/x/vuln/cmd/govulncheck@latest ./...
	${GO_EXECUTABLE} test -race -buildvcs -vet=off ./...

# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	${GO_EXECUTABLE} test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	${GO_EXECUTABLE} test -v -race -buildvcs -coverprofile=${TMP_DIR}/coverage.out ./...
	${GO_EXECUTABLE} tool cover -html=${TMP_DIR}/coverage.out

.PHONY: build
build:
	goreleaser build --single-target --clean --snapshot

.PHONY: build/all
build/all:
	goreleaser build --clean --snapshot

.PHONY: build/release
build/release:
	goreleaser build --clean --skip=validate

.PHONY: run
run: build
	./dist/${BINARY_NAME}_${OS_INFO}_${ARCH_INFO}/${BINARY_NAME}

.PHONY: run/release
run/release: build/release
	./dist/${BINARY_NAME}_${OS_INFO}_${ARCH_INFO}/${BINARY_NAME}

.PHONY: release
release:
	goreleaser release --clean

.PHONY: new_version
new_version:
	./scripts/new_version.sh $(VERSION)
