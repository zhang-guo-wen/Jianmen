#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARCH="${1:?usage: prepare-guacd-runtime.sh <amd64|arm64> [output]}"
OUTPUT="${2:-$ROOT/internal/guacdruntime/assets/guacd-linux-${ARCH}.tar.gz}"
IMAGE_REPOSITORY="guacamole/guacd"
IMAGE_VERSION="1.6.0"

case "$ARCH" in
  amd64)
    IMAGE_DIGEST="sha256:f39258e35244b6bf79bc6ac4e60eee176aea6f6a5adb13e8c3090e48df8ae515"
    ;;
  arm64)
    IMAGE_DIGEST="sha256:769987c20e99f59578305505ffa23418c24da73d579364f097cdf01d9866e5e5"
    ;;
  *)
    echo "unsupported guacd runtime architecture: $ARCH" >&2
    exit 2
    ;;
esac

IMAGE="${IMAGE_REPOSITORY}:${IMAGE_VERSION}@${IMAGE_DIGEST}"

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
