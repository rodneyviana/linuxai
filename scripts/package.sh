#!/usr/bin/env bash
# Builds static amd64 + arm64 binaries and packages them, install.sh, and
# .env.example into a single self-extracting makeself installer.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v makeself >/dev/null 2>&1; then
	echo "package.sh: makeself is not installed or not on PATH" >&2
	exit 1
fi

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
echo "==> Version: $VERSION"
if ! git describe --tags >/dev/null 2>&1; then
	echo "    (no tags yet; run 'git tag v0.1.0' and push it to get a real version)"
fi

OUT="${1:-linuxai-installer-${VERSION}.run}"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

LDFLAGS="-X main.version=$VERSION"

echo "==> Building linuxai-amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$STAGE/linuxai-amd64" ./cmd/linuxai

echo "==> Building linuxai-arm64"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$STAGE/linuxai-arm64" ./cmd/linuxai

cp scripts/install.sh "$STAGE/install.sh"
chmod +x "$STAGE/install.sh"
cp .env.example "$STAGE/.env.example"

echo "==> Packaging $OUT"
makeself --gzip "$STAGE" "$OUT" "linuxai installer" ./install.sh

echo "==> Done: $OUT"
