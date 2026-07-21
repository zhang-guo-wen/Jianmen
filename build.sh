#!/usr/bin/env bash
# 构建前端 + Windows 二进制 + Linux Lite/RDP 双版本

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
FRONTEND_DIST="$ROOT/web/dist"
EMBED_DIR="$ROOT/internal/frontend/dist"
OUTPUT_DIR="$ROOT/dist"

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "╔══════════════════════════════════════╗"
echo "║   Jianmen 一键构建（Linux 双版本） ║"
echo "╚══════════════════════════════════════╝"
echo ""

echo -e "${CYAN}[1/6] 构建前端...${NC}"
cd "$ROOT/web"
if [[ ! -x node_modules/.bin/vitest || ! -x node_modules/.bin/vue-tsc || ! -x node_modules/.bin/vite ]]; then
  npm ci
fi
npm run build
echo -e "${GREEN}  ✓ 前端构建完成${NC}"

echo -e "${CYAN}[2/6] 复制前端产物到 embed 目录...${NC}"
rm -rf "$EMBED_DIR"
cp -r "$FRONTEND_DIST" "$EMBED_DIR"
file_count=$(find "$EMBED_DIR" -type f | wc -l)
echo -e "${GREEN}  ✓ ${file_count} 个文件已复制${NC}"

echo -e "${CYAN}[3/6] 编译 Windows amd64...${NC}"
cd "$ROOT"
mkdir -p "$OUTPUT_DIR"
GOOS=windows GOARCH=amd64 go build \
  -o "$OUTPUT_DIR/bastion-core-windows-amd64.exe" ./cmd/bastion-core/
win_size=$(du -h "$OUTPUT_DIR/bastion-core-windows-amd64.exe" | cut -f1)
echo -e "${GREEN}  ✓ bastion-core-windows-amd64.exe  (${win_size})${NC}"

echo -e "${CYAN}[4/6] 编译 Linux amd64 Lite...${NC}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o "$OUTPUT_DIR/jianmen-linux-amd64-lite" ./cmd/bastion-core/
lite_size=$(du -h "$OUTPUT_DIR/jianmen-linux-amd64-lite" | cut -f1)
echo -e "${GREEN}  ✓ jianmen-linux-amd64-lite  (${lite_size})${NC}"

echo -e "${CYAN}[5/6] 准备 Linux amd64 guacd 运行时...${NC}"
"$ROOT/scripts/build/prepare-guacd-runtime.sh" amd64

echo -e "${CYAN}[6/6] 编译 Linux amd64 RDP...${NC}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags embedded_guacd \
  -o "$OUTPUT_DIR/jianmen-linux-amd64-rdp" ./cmd/bastion-core/
rdp_size=$(du -h "$OUTPUT_DIR/jianmen-linux-amd64-rdp" | cut -f1)
echo -e "${GREEN}  ✓ jianmen-linux-amd64-rdp  (${rdp_size})${NC}"

echo ""
echo "╔══════════════════════════════════════╗"
echo "║          构建完成，产物如下         ║"
echo "╚══════════════════════════════════════╝"
echo ""
ls -lh "$OUTPUT_DIR"/*.exe "$OUTPUT_DIR"/jianmen-linux-amd64-* 2>/dev/null
echo ""
echo -e "${YELLOW}Linux 默认部署使用: dist/jianmen-linux-amd64-rdp${NC}"
echo -e "${YELLOW}无需远程桌面使用: dist/jianmen-linux-amd64-lite${NC}"
