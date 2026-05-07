#!/bin/bash
set -euo pipefail

mkdir -p bin

# Monotonic build id: one more per git commit; suffix identifies tree + dirty state.
if git rev-parse --git-dir >/dev/null 2>&1; then
	n="$(git rev-list --count HEAD 2>/dev/null || echo 0)"
	s="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
	VERSION="${n}-${s}"
	if ! git diff-index --quiet HEAD -- 2>/dev/null; then
		VERSION="${VERSION}-dirty"
	fi
else
	VERSION="0-nogit"
fi

LDFLAGS=(-ldflags "-X main.version=${VERSION}")

GOOS=linux GOARCH=arm go build "${LDFLAGS[@]}" -o bin/systemd-web-arm systemd-web.go
GOOS=linux GOARCH=arm64 go build "${LDFLAGS[@]}" -o bin/systemd-web-arm64 systemd-web.go
#go build "${LDFLAGS[@]}" -o bin/systemd-web systemd-web.go

echo "Built version ${VERSION}"
