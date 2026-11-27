#!/usr/bin/env bash
# 制作「Linux 离线 Docker 部署包」：
#   构建镜像（本机 Docker）→ docker save 导出 tar → 连同 docker-compose.yml、
#   配置、load/run 脚本一起打成 zip。拷到 Linux 服务器解压后：
#
#   ./load.sh          # 导入镜像（离线，不访问任何 registry）
#   docker compose up -d   # 启动
#
# 产物：dist/collect-docker-<版本>-<架构>.zip
#
# 用法（在仓库根执行）：
#   ./deploy/package.sh                     # 默认 linux/amd64
#   ./deploy/package.sh -v v1.2.0           # 指定版本号
#   ./deploy/package.sh -arch arm64         # 目标架构 amd64 / arm64
#   ./deploy/package.sh -registry docker.m.daocloud.io/library \
#                      -goproxy https://goproxy.cn,direct   # 国内网络走镜像源
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION=""
ARCH="amd64"
REGISTRY="${REGISTRY:-}"
GOPROXY="${GOPROXY:-}"
OUT_DIR="dist"
APP_IMAGE_NAME="collect"

while [ $# -gt 0 ]; do
  case "$1" in
    -v|--version) VERSION="$2"; shift 2 ;;
    -arch|--arch) ARCH="$2"; shift 2 ;;
    -registry|--registry) REGISTRY="$2"; shift 2 ;;
    -goproxy|--goproxy) GOPROXY="$2"; shift 2 ;;
    -h|--help) grep '^#' "$0" | sed -n '2,20p' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1（-h 查看帮助）"; exit 1 ;;
  esac
done

command -v docker >/dev/null 2>&1 || { echo "未安装 docker，无法构建镜像"; exit 1; }

if [ -z "$VERSION" ]; then
  VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev-$(date +%Y%m%d)")
fi

IMAGE="$APP_IMAGE_NAME:$VERSION"
STAGE="$OUT_DIR/collect-docker-$VERSION-linux-$ARCH"
PKG="$STAGE.zip"

echo "[package] 版本 $VERSION · 架构 linux/$ARCH"

# ---- 1. 构建镜像（REGISTRY/GOPROXY 传给 Dockerfile 的构建参数）----
build_args=()
[ -n "$REGISTRY" ] && build_args+=(--build-arg "REGISTRY=$REGISTRY")
[ -n "$GOPROXY" ] && build_args+=(--build-arg "GOPROXY=$GOPROXY")
echo "[package] 构建镜像 $IMAGE ..."
docker build "${build_args[@]}" -t "$IMAGE" .

# ---- 2. 导出镜像 tar（离线导入用；docker save 本身不压缩，再 gzip 一次省一半体积）----
mkdir -p "$STAGE"
echo "[package] 导出镜像 tar（约 200MB，需一会儿）..."
docker save "$IMAGE" -o "$STAGE/collect-image.tar"
gzip -f "$STAGE/collect-image.tar"
# 得到 collect-image.tar.gz；load.sh 里 gunzip -k 还原成 .tar 给 docker load

# ---- 3. docker-compose.yml（数据卷落在包目录 data/）----
cat > "$STAGE/docker-compose.yml" <<EOF
services:
  collect:
    image: $IMAGE
    container_name: collect
    restart: unless-stopped
    ports:
      - "\${COLLECT_PORT:-8686}:8686"
    volumes:
      - ./data:/app/data
    environment:
      - TZ=Asia/Shanghai
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8686/api/health >/dev/null 2>&1 || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
EOF

# ---- 4. config.json（容器内路径，首次部署生成一次；已有 data/config.json 时 compose 卷优先）----
cat > "$STAGE/config.json" <<'EOF'
{
  "addr": "0.0.0.0:8686",
  "data_dir": "/app/data",
  "browser": {
    "bin_path": "/usr/bin/chromium-browser",
    "headless": true,
    "proxy": "",
    "download_host": "npm",
    "custom_host": "",
    "revision": 0,
    "no_sandbox": true
  },
  "max_body_kb": 256,
  "proxy": "",
  "timeout_sec": 60,
  "run_workers": 2
}
EOF

# ---- 5. load.sh：导入镜像 + 提示启动 ----
cat > "$STAGE/load.sh" <<EOF
#!/usr/bin/env bash
# 离线导入镜像并启动（在解压目录里执行）
set -e
cd "\$(dirname "\$0")"

echo "[load] 导入镜像 $IMAGE ..."
gunzip -k collect-image.tar.gz
docker load -i collect-image.tar
rm -f collect-image.tar

# 首次部署：把 config.json 放进数据目录（compose 已把 ./data 挂到 /app/data）
mkdir -p data
[ -f data/config.json ] || cp config.json data/config.json

echo "[load] 完成。启动："
echo "  docker compose up -d          # 需要已安装 docker compose 插件"
echo "  或旧版：docker-compose up -d"
echo "  访问: http://<服务器IP>:\${COLLECT_PORT:-8686}"
EOF
chmod +x "$STAGE/load.sh"

# ---- 6. README ----
cat > "$STAGE/README.txt" <<EOF
采集平台 离线 Docker 部署包（linux/$ARCH，镜像 $IMAGE）

部署步骤（服务器需已安装 Docker 与 docker compose 插件）：
  1. unzip 解压本包
  2. ./load.sh          导入镜像并准备 data/config.json
  3. docker compose up -d
  4. 浏览器访问 http://<服务器IP>:8686

常用操作：
  docker compose logs -f        # 看日志
  docker compose restart        # 重启
  docker compose down           # 停止（数据保留在 ./data）
  COLLECT_PORT=9000 docker compose up -d   # 改端口

升级：换新包后重复 1-2，然后 docker compose up -d；./data 里的数据库/导出/日志全部保留。

浏览器内核：镜像内自带 Chromium（/usr/bin/chromium-browser），无需额外安装。
EOF

# ---- 7. 打 zip ----
rm -f "$PKG"
if command -v zip >/dev/null 2>&1; then
  (cd "$OUT_DIR" && zip -qr "$(basename "$PKG")" "$(basename "$STAGE")")
else
  echo "[package] 未找到 zip，改用 tar.gz"
  tar -czf "${PKG%.zip}.tar.gz" -C "$OUT_DIR" "$(basename "$STAGE")"
  PKG="${PKG%.zip}.tar.gz"
fi

echo
echo "部署包已生成: $PKG"
ls -lh "$PKG" | awk '{printf "  大小: %s\n", $5}'
echo "服务器上: 解压 → ./load.sh → docker compose up -d"
