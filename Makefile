# Change these variables as necessary.
MAIN_PACKAGE_PATH := ./
BINARY_NAME := git-mirrorer
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

## build: build the application
.PHONY: build
build:
	${GO_EXECUTABLE} build -o=${TMP_DIR}/bin/${BINARY_NAME} ${MAIN_PACKAGE_PATH}

## run: run the  application
.PHONY: run
run: build
	${TMP_DIR}/bin/${BINARY_NAME}

## run/live: run the application with reloading on file changes
.PHONY: run/live
run/live:
	export GO_EXECUTABLE="${GO_EXECUTABLE}" && export TMP_DIR="${TMP_DIR}" && export BINARY_NAME="${BINARY_NAME}" && bash run_live.sh
