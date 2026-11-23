#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Devoplus
#
# Builds the release binaries. The provider is pure Go, so CGO is disabled and
# the binaries are fully static without a cross compilation toolchain.

set -o errexit
set -o nounset
set -o pipefail

PROVIDER_NAME="garm-provider-vultr"
MODULE="github.com/devoplus/garm-provider-vultr"

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$SOURCE_DIR"

VERSION="${VERSION:-$(git describe --tags --match='v[0-9]*' --dirty --always 2>/dev/null || echo 'v0.0.0-unknown')}"
OUTPUT_DIR="${OUTPUT_DIR:-$SOURCE_DIR/bin/$VERSION}"

LDFLAGS="-s -w -X ${MODULE}/provider.Version=${VERSION} -X main.Version=${VERSION}"

export CGO_ENABLED=0

build() {
    local goos="$1"
    local goarch="$2"
    local suffix="${3:-}"
    local target="${OUTPUT_DIR}/${goos}/${goarch}/${PROVIDER_NAME}${suffix}"

    mkdir -p "$(dirname "$target")"
    echo "building ${goos}/${goarch} -> ${target}"
    GOOS="$goos" GOARCH="$goarch" go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "$target" \
        .
}

build linux amd64
build linux arm64

echo
echo "version: ${VERSION}"
echo "binaries are available in ${OUTPUT_DIR}"
