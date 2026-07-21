#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EMBED_DIR="$ROOT/internal/frontend/dist"

cd "$ROOT/web"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run build

rm -rf "$EMBED_DIR"
mkdir -p "$(dirname "$EMBED_DIR")"
cp -R "$ROOT/web/dist" "$EMBED_DIR"

cd "$ROOT"
mkdir -p dist
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" \
    -o "dist/jianmen-linux-${arch}-lite" ./cmd/bastion-core
done
