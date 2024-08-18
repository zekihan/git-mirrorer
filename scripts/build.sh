#!/bin/sh

set -e

DIR=$( cd -P -- "$(dirname -- "$(command -v -- "$0")")" && pwd -P )

cd "${DIR}/.."

export CGO_ENABLED=0
export GO111MODULE=on

arch=$(uname -m)
if [ "${arch}" = "x86_64" ]; then
	export GOARCH=amd64
elif [ "${arch}" = "aarch64" ]; then
	export GOARCH=arm64
else
	echo "Unsupported architecture: ${arch}"
	exit 1
fi
os=$(uname -s)
if [ "${os}" = "Darwin" ]; then
	export GOOS=darwin
elif [ "${os}" = "Linux" ]; then
	export GOOS=linux
else
	echo "Unsupported OS: ${os}"
	exit 1
fi

if [ -z "${TMP_DIR}" ]; then
	TMP_DIR="./tmp"
fi

if [ -z "${BINARY_NAME}" ]; then
	BINARY_NAME="git-mirrorer"
fi

if [ -z "${MAIN_PACKAGE_PATH}" ]; then
	MAIN_PACKAGE_PATH="./"
fi

if [ -z "${ENVIRONMENT}" ]; then
	ENVIRONMENT="PROD"
fi

if [ -z "${VERSION}" ]; then
	VERSION=$(tr -d '[:space:]' <VERSION)
fi

go build -ldflags="-s -w -X \"github.com/zekihan/git-mirrorer/utils.VERSION=${VERSION}\" -X \"github.com/zekihan/git-mirrorer/utils.ENVIRONMENT=${ENVIRONMENT}\"" -o "${TMP_DIR}/bin/${BINARY_NAME}" "${MAIN_PACKAGE_PATH}"
