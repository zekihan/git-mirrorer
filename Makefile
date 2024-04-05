# Change these variables as necessary.
MAIN_PACKAGE_PATH := ./
BINARY_NAME := git-mirrorer
TMP_DIR := ./tmp

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

## audit: run quality control checks
.PHONY: audit
audit:
	go mod verify
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go test -race -buildvcs -vet=off ./...


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	go test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	go test -v -race -buildvcs -coverprofile=${TMP_DIR}/coverage.out ./...
	go tool cover -html=${TMP_DIR}/coverage.out

## build: build the application
.PHONY: build
build:
	# Include additional build steps, like TypeScript, SCSS or Tailwind compilation here...
	go build -o=${TMP_DIR}/bin/${BINARY_NAME} ${MAIN_PACKAGE_PATH}

## run: run the  application
.PHONY: run
run: build
	${TMP_DIR}/bin/${BINARY_NAME}

## run/live: run the application with reloading on file changes
.PHONY: run/live
run/live:
	go run github.com/cosmtrek/air@latest \
		--build.cmd "make build" --build.bin "${TMP_DIR}/bin/${BINARY_NAME}" --build.delay "100" \
		--build.exclude_dir "" \
		--build.include_ext "go, jsonc" \
		--misc.clean_on_exit "true"
