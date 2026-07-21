#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${1:-dev}"
FRONTEND_DIST="$ROOT/web/dist"
EMBED_DIR="$ROOT/internal/frontend/dist"
OUTPUT_DIR="$ROOT/dist/release"

build_archive() {
  local os="$1"
  local arch="$2"
  local variant="${3:-}"
  local executable="jianmen"
  local suffix="${os}-${arch}"
  local build_tags=()

  if [[ "$os" == "windows" ]]; then
    executable="jianmen.exe"
  elif [[ -n "$variant" ]]; then
    suffix="${suffix}-${variant}"
  fi

  if [[ "$variant" == "rdp" ]]; then
    "$ROOT/scripts/build/prepare-guacd-runtime.sh" "$arch"
    build_tags=(-tags embedded_guacd)
  fi

  local archive_name="jianmen-${VERSION}-${suffix}"
  local package_dir="$OUTPUT_DIR/$archive_name"
  rm -rf "$package_dir"
  mkdir -p "$package_dir"

  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build "${build_tags[@]}" -trimpath -ldflags="-s -w" \
    -o "$package_dir/$executable" ./cmd/jianmen

  cp "$ROOT/configs/config.example.json" "$package_dir/config.example.json"
  cp "$ROOT/LICENSE" "$package_dir/LICENSE"
  cp "$ROOT/README.md" "$package_dir/README.md"
  if [[ "$variant" == "rdp" ]]; then
    cp "$ROOT/THIRD_PARTY_NOTICES.md" "$package_dir/THIRD_PARTY_NOTICES.md"
  fi

  if [[ "$os" == "windows" ]]; then
    python - "$package_dir" "$OUTPUT_DIR/${archive_name}.zip" <<'PY'
from pathlib import Path
import sys
import zipfile

source = Path(sys.argv[1])
destination = Path(sys.argv[2])
with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(source.rglob("*")):
        if path.is_file():
            archive.write(path, path.relative_to(source.parent))
PY
  else
    tar -C "$OUTPUT_DIR" -czf "$OUTPUT_DIR/${archive_name}.tar.gz" "$archive_name"
  fi

  rm -rf "$package_dir"
}

cd "$ROOT/web"
npm ci
npm run build

rm -rf "$EMBED_DIR"
mkdir -p "$(dirname "$EMBED_DIR")"
cp -R "$FRONTEND_DIST" "$EMBED_DIR"

cd "$ROOT"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

build_archive windows amd64
build_archive windows arm64
for arch in amd64 arm64; do
  build_archive linux "$arch" lite
  build_archive linux "$arch" rdp
done

(
  cd "$OUTPUT_DIR"
  sha256sum *.zip *.tar.gz >checksums.txt
)

echo "Release packages created in $OUTPUT_DIR"
