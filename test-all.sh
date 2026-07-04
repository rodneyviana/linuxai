#!/usr/bin/env bash
# Runs the full static check suite: gofmt, go vet, go test, and a static
# cross-compile build for both shipped architectures.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

cleanup() {
	rm -f linuxai-amd64 linuxai-arm64
}
trap cleanup EXIT

echo "==> gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt found unformatted files:"
	echo "$unformatted"
	exit 1
fi

echo "==> go vet"
go vet ./...

echo "==> go test"
go test ./...

echo "==> build amd64 (static)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o linuxai-amd64 ./cmd/linuxai
if ldd linuxai-amd64 >/dev/null 2>&1; then
	echo "error: linuxai-amd64 is dynamically linked"
	exit 1
fi

echo "==> build arm64 (static)"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o linuxai-arm64 ./cmd/linuxai

echo "==> all checks passed"
