#!/usr/bin/env sh
# 多平台打包脚本：与 build.ps1 等价的 Linux/macOS 版本。
#
# 产物：
#   bin/collect              当前平台二进制（本地直接运行用）
#   dist/collect-<版本>-<平台>-<架构>.<ext>   全平台产物 + checksums.txt
#
# 用法：
#   ./build.sh                  # 打包全部平台
#   ./build.sh -o release       # 指定输出目录
#   ./build.sh -v v1.2.0        # 指定版本号（默认 git describe 或 dev-日期）
#   ./build.sh -only linux,amd64   # 只编译某个平台（逗号分隔 GOOS,GOARCH）
set -e
cd "$(dirname "$0")"

OUT_DIR="dist"
VERSION=""
ONLY=""

# ---- 简易参数解析（兼容 -key value 与 --key value）----
while [ $# -gt 0 ]; do
  case "$1" in
    -o|--out) OUT_DIR="$2"; shift 2 ;;
    -v|--version) VERSION="$2"; shift 2 ;;
    -only|--only) ONLY="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "$0" | sed -n '2,14p' | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "未知参数: $1（-h 查看帮助）"; exit 1 ;;
  esac
done

command -v go >/dev/null 2>&1 || { echo "[build] 未安装 Go，请先安装 https://go.dev/dl/"; exit 1; }

# ---- 版本号：显式指定 > git describe > dev-日期 ----
if [ -z "$VERSION" ]; then
  VERSION=$(git describe --tags --always --dirty 2>/dev/null || true)
fi
if [ -z "$VERSION" ]; then
  VERSION="dev-$(date +%Y%m%d)"
fi
# 版本号只用于文件名，去掉可能存在的 v 前缀空格等
VERSION=$(echo "$VERSION" | tr -d ' ')

mkdir -p "$OUT_DIR" bin

# ---- 目标平台列表：GOOS GOARCH 后缀名（ext 为空字符串表示无后缀）----
build_one() {
  goos="$1"; goarch="$2"; ext="$3"
  if [ -n "$ONLY" ]; then
    # -only linux,amd64 / -only linux
    case ",$ONLY," in
      *",$goos,$goarch,"*|*",$goos,"*) ;;
      *) return 0 ;;
    esac
  fi
  name="collect-${VERSION}-${goos}-${goarch}${ext}"
  echo "[build] $goos/$goarch -> $OUT_DIR/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w" -o "$OUT_DIR/$name" .
}

# 本地平台二进制（bin/collect 或 bin/collect.exe），不带版本号，方便 start.sh 直接用
LOCAL_GOOS=$(go env GOOS)
LOCAL_GOARCH=$(go env GOARCH)
local_ext=""
[ "$LOCAL_GOOS" = "windows" ] && local_ext=".exe"
echo "[build] $LOCAL_GOOS/$LOCAL_GOARCH -> bin/collect$local_ext"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "bin/collect$local_ext" .

# 全平台产物（用 for 循环而不是管道 while，避免子 shell 丢变量）
for pair in \
  "windows amd64 .exe" \
  "windows arm64 .exe" \
  "linux amd64" \
  "linux arm64" \
  "darwin amd64" \
  "darwin arm64"; do
  set -- $pair
  build_one "$1" "$2" "${3:-}"
done

# ---- 校验和：对所有带版本号的产物生成 checksums.txt（有 sha256sum 用之，macOS 退回 shasum）----
sum_cmd=""
command -v sha256sum >/dev/null 2>&1 && sum_cmd="sha256sum"
[ -z "$sum_cmd" ] && command -v shasum >/dev/null 2>&1 && sum_cmd="shasum -a 256"
if [ -n "$sum_cmd" ]; then
  (cd "$OUT_DIR" && $sum_cmd collect-${VERSION}-* > checksums.txt)
fi

echo
echo "产物："
ls -lh "$OUT_DIR" | awk 'NR>1 {printf "  %-45s %8s\n", $NF, $5}'
echo "  bin/collect$local_ext"
echo
echo "部署提示：Linux 服务器首次运行前把 config.json 的 browser.no_sandbox 设为 true（无 GUI 环境）；"
echo "或直接用 deploy/docker-run.sh 以容器方式运行（镜像内已带 Chromium）。"
