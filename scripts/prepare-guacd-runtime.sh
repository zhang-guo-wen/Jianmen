#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="${1:?usage: prepare-guacd-runtime.sh <amd64|arm64> [output]}"
OUTPUT="${2:-$ROOT/internal/guacdruntime/assets/guacd-linux-${ARCH}.tar.gz}"
IMAGE="guacamole/guacd:1.6.0@sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f"

case "$ARCH" in
  amd64|arm64) ;;
  *)
    echo "unsupported guacd runtime architecture: $ARCH" >&2
    exit 2
    ;;
esac

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required to prepare the embedded guacd runtime" >&2
  exit 1
}
command -v gzip >/dev/null 2>&1 || {
  echo "gzip is required to prepare the embedded guacd runtime" >&2
  exit 1
}

mkdir -p "$(dirname "$OUTPUT")"
temporary="${OUTPUT}.tmp.$$"
container=""
cleanup() {
  rm -f "$temporary"
  if [[ -n "$container" ]]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

docker pull --platform "linux/$ARCH" "$IMAGE" >/dev/null
container="$(docker create --platform "linux/$ARCH" "$IMAGE")"
docker export "$container" | gzip -n -9 >"$temporary"
mv -f "$temporary" "$OUTPUT"

size="$(du -h "$OUTPUT" | cut -f1)"
echo "Prepared guacd $ARCH runtime: $OUTPUT ($size)"
