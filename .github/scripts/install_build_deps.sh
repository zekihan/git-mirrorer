#!/bin/sh

set -e

DIR=$(cd -P -- "$(dirname -- "$(command -v -- "$0")")" && pwd -P)

cd "${DIR}/.."

# no dependencies to install
