#!/usr/bin/env sh
set -e
cd "$(dirname "$0")"
if [ ! -f bin/collect ]; then
  echo "[collect] 未找到 bin/collect，开始编译..."
  command -v go >/dev/null 2>&1 || { echo "[collect] 未安装 Go，请安装后重试"; exit 1; }
  mkdir -p bin
  CGO_ENABLED=0 go build -o bin/collect .
fi
exec ./bin/collect "$@"