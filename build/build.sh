#!/bin/sh
set -e

VERSION="${1:-0.1.0}"
OUTDIR="dist"
ALPINE_JS_FILE="internal/frontend/assets/alpine.min.js"
ALPINE_JS_URL="https://unpkg.com/alpinejs@3.14.3/dist/cdn.min.js"

echo "=== Alpine WebAdmin Release Build ==="
echo "Version: $VERSION"

# Ensure Alpine.js is present before building
if [ ! -f "$ALPINE_JS_FILE" ] || [ $(wc -c < "$ALPINE_JS_FILE") -lt 1000 ]; then
    echo "Downloading Alpine.js..."
    curl -fsSL -o "$ALPINE_JS_FILE" "$ALPINE_JS_URL" || wget -q -O "$ALPINE_JS_FILE" "$ALPINE_JS_URL" || {
        echo "Failed to download Alpine.js"
        exit 1
    }
fi

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
