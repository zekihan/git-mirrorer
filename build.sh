#!/bin/bash

export CGO_ENABLED=0
export GO111MODULE=on
export GOOS=linux

if [ -z "${TMP_DIR}" ]; then
	TMP_DIR="./tmp"
fi

if [ -z "${BINARY_NAME}" ]; then
	BINARY_NAME="git-mirrorer"
fi

if [ -z "${MAIN_PACKAGE_PATH}" ]; then
	MAIN_PACKAGE_PATH="."
fi

vers=$(tr -d '[:space:]' <VERSION)
go build -ldflags="-s -w -X \"github.com/zekihan/git-mirrorer/util.VERSION=${vers}\" -X \"github.com/zekihan/git-mirrorer/util.ENVIRONMENT=PROD\"" -o "${TMP_DIR}/bin/${BINARY_NAME}" "${MAIN_PACKAGE_PATH}"
