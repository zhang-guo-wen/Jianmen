#!/usr/bin/env bash
# Jianmen 一键构建脚本 (Linux / macOS / Git Bash)
# 构建前端 + 编译 Windows/Linux 二进制（内含前端）

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
echo "║   Jianmen 一键构建（含前端嵌入版） ║"
echo "╚══════════════════════════════════════╝"
echo ""

# === 1. 构建前端 ===
echo -e "${CYAN}[1/4] 构建前端...${NC}"
cd "$ROOT/web"
npm run build
echo -e "${GREEN}  ✓ 前端构建完成${NC}"

# === 2. 复制前端到 embed 目录 ===
echo -e "${CYAN}[2/4] 复制前端产物到 embed 目录...${NC}"
rm -rf "$EMBED_DIR"
cp -r "$FRONTEND_DIST" "$EMBED_DIR"
file_count=$(find "$EMBED_DIR" -type f | wc -l)
echo -e "${GREEN}  ✓ ${file_count} 个文件已复制${NC}"

# === 3. 编译 Windows 版本 ===
echo -e "${CYAN}[3/4] 编译 Windows amd64...${NC}"
cd "$ROOT"
mkdir -p "$OUTPUT_DIR"
GOOS=windows GOARCH=amd64 go build -o "$OUTPUT_DIR/bastion-core-windows-amd64.exe" ./cmd/bastion-core/
win_size=$(du -h "$OUTPUT_DIR/bastion-core-windows-amd64.exe" | cut -f1)
echo -e "${GREEN}  ✓ bastion-core-windows-amd64.exe  (${win_size})${NC}"

# === 4. 编译 Linux 版本 ===
echo -e "${CYAN}[4/4] 编译 Linux amd64...${NC}"
cd "$ROOT"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$OUTPUT_DIR/bastion-core-linux-amd64" ./cmd/bastion-core/
linux_size=$(du -h "$OUTPUT_DIR/bastion-core-linux-amd64" | cut -f1)
echo -e "${GREEN}  ✓ bastion-core-linux-amd64  (${linux_size})${NC}"

# === 完成 ===
echo ""
echo "╔══════════════════════════════════════╗"
echo "║          构建完成，产物如下         ║"
echo "╚══════════════════════════════════════╝"
echo ""
ls -lh "$OUTPUT_DIR"/*.exe "$OUTPUT_DIR"/bastion-core-linux-amd64 2>/dev/null
echo ""
echo -e "${YELLOW}Linux 部署: scp dist/bastion-core-linux-amd64 user@server:/opt/jianmen/${NC}"
