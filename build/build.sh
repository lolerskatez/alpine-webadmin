#!/bin/sh
set -e

VERSION="${1:-0.1.0}"
OUTDIR="dist"

echo "=== Alpine WebAdmin Release Build ==="
echo "Version: $VERSION"

mkdir -p "$OUTDIR"

export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

LDFLAGS="-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=$VERSION"

echo "Building webadmin..."
go build -trimpath -ldflags "$LDFLAGS" -o "$OUTDIR/webadmin" ./cmd/webadmin

echo "Building roothelper..."
go build -trimpath -ldflags "$LDFLAGS" -o "$OUTDIR/roothelper" ./cmd/roothelper

echo "Generating checksums..."
cd "$OUTDIR"
sha256sum webadmin roothelper > checksums.txt
cd ..

echo "=== Done ==="
ls -la "$OUTDIR/"
