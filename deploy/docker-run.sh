#!/usr/bin/env bash
# 采集平台 Linux Docker 独立目录运行脚本。
#
# 把二进制 + config 放在一个独立目录（宿主机上与程序目录解耦），
# 用仓库根的 Dockerfile 构建镜像并运行容器，数据落在该目录的 data/ 子目录。
#
# 目录约定（--dir 指定，默认 ./collect-app）：
#   collect-app/
#   ├── config.json      # 首次 setup 自动生成（no_sandbox=true、chromium 路径已配好）
#   └── data/            # 数据库 / 导出 / 日志 / 浏览器（挂载为容器卷，升级不丢）
#
# 用法（在仓库根或任意位置执行）：
#   ./deploy/docker-run.sh setup                 # 初始化独立目录 + 构建镜像
#   ./deploy/docker-run.sh start [-p 8686:8686]  # 启动容器（可重复调用，端口可改）
#   ./deploy/docker-run.sh stop                  # 停止并移除容器
#   ./deploy/docker-run.sh restart               # 重启
#   ./deploy/docker-run.sh logs [-f]             # 看容器日志
#   ./deploy/docker-run.sh status                # 运行状态 + 健康检查
#   ./deploy/docker-run.sh upgrade               # 重新构建镜像并滚动升级（数据保留）
#   ./deploy/docker-run.sh all                   # setup + start 一步到位
#
# 环境变量：
#   APP_DIR     独立目录（默认 ./collect-app）
#   IMAGE       镜像名（默认 collect:latest）
#   PORT        默认端口映射（默认 8686:8686）
#   REGISTRY    Docker Hub 镜像源前缀，国内网络可设 docker.m.daocloud.io/library
#   GOPROXY     Go 模块代理，国内网络可设 https://goproxy.cn,direct
set -euo pipefail

REGISTRY="${REGISTRY:-}"
GOPROXY="${GOPROXY:-}"

# 组装 build-arg 参数（未设置时为空数组）
build_args=()
[ -n "$REGISTRY" ] && build_args+=(--build-arg "REGISTRY=$REGISTRY")
[ -n "$GOPROXY" ] && build_args+=(--build-arg "GOPROXY=$GOPROXY")

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Dockerfile 在仓库根（deploy/ 的上一级）；脚本被复制走时退回当前目录
if [ -f "$SCRIPT_DIR/../Dockerfile" ]; then
  REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  REPO_ROOT="$(pwd)"
fi

APP_DIR="${APP_DIR:-$REPO_ROOT/collect-app}"
IMAGE="${IMAGE:-collect:latest}"
PORT="${PORT:-8686:8686}"
CONTAINER="collect"

log()  { echo "[docker-run] $*"; }
die()  { echo "[docker-run] 错误: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "未找到 $1，请先安装"; }

# ---- 独立目录 + config.json ----
make_config() {
  cat > "$APP_DIR/config.json" <<EOF
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
}

cmd_setup() {
  need docker
  mkdir -p "$APP_DIR/data"
  if [ -f "$APP_DIR/config.json" ]; then
    log "config.json 已存在，跳过生成（如需重置请先删除）"
  else
    make_config
    log "已生成 $APP_DIR/config.json（no_sandbox=true、chromium 路径已指向镜像内浏览器）"
  fi
  log "构建镜像 $IMAGE ..."
  docker build "${build_args[@]}" -t "$IMAGE" "$REPO_ROOT"
  log "镜像构建完成"
}

container_running() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$CONTAINER"
}

cmd_start() {
  need docker
  [ -f "$APP_DIR/config.json" ] || die "先执行 setup：$0 setup"
  if container_running; then
    log "容器已在运行（$CONTAINER），如需改端口请先 stop 再 start"
    return 0
  fi
  # 清理上次异常退出留下的停止容器
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  log "启动容器 $CONTAINER（端口 $PORT，数据目录 $APP_DIR/data）"
  docker run -d \
    --name "$CONTAINER" \
    --restart unless-stopped \
    -p "$PORT" \
    -v "$APP_DIR/data:/app/data" \
    -e TZ=Asia/Shanghai \
    --health-cmd "wget -qO- http://127.0.0.1:8686/api/health >/dev/null 2>&1 || exit 1" \
    --health-interval 30s \
    --health-timeout 5s \
    --health-retries 3 \
    --health-start-period 20s \
    "$IMAGE" >/dev/null
  log "已启动。浏览器访问 http://<主机IP>:${PORT%%:*}"
}

cmd_stop() {
  need docker
  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    log "停止并移除容器 $CONTAINER"
    docker rm -f "$CONTAINER" >/dev/null
  else
    log "容器不存在，无需停止"
  fi
}

cmd_restart() {
  cmd_stop
  cmd_start
}

cmd_logs() {
  need docker
  docker logs "$@" "$CONTAINER"
}

cmd_status() {
  need docker
  if container_running; then
    log "容器 $CONTAINER 运行中"
    docker ps --filter "name=$CONTAINER" --format '  状态: {{.Status}}  端口: {{.Ports}}'
    if command -v curl >/dev/null 2>&1; then
      host_port="${PORT%%:*}"
      if curl -sf --max-time 5 "http://127.0.0.1:$host_port/api/health" >/dev/null 2>&1; then
        log "健康检查通过: http://127.0.0.1:$host_port"
      else
        log "健康检查未通过（可能仍在启动，稍等后重试 status）"
      fi
    fi
  else
    log "容器未运行。执行 $0 start 启动"
    exit 1
  fi
}

# 升级：重建镜像 → 停旧容器 → 用同样的端口/卷起新容器。data/ 在宿主机，数据不丢。
cmd_upgrade() {
  need docker
  old_port="$PORT"
  if container_running; then
    # 运行中的容器实际端口优先（start 时可能用过不同端口）
    mapped=$(docker port "$CONTAINER" 8686/tcp 2>/dev/null | head -1 | sed 's/.*://')
    [ -n "$mapped" ] && old_port="$mapped:8686"
  fi
  PORT="$old_port"
  log "重建镜像 $IMAGE ..."
  docker build "${build_args[@]}" -t "$IMAGE" "$REPO_ROOT"
  cmd_stop
  cmd_start
  log "升级完成，数据目录未改动: $APP_DIR/data"
}

cmd_all() {
  cmd_setup
  cmd_start
  cmd_status
}

usage() {
  grep '^# ' "$0" | sed -n '2,40p' | sed 's/^# \{0,1\}//'
  exit 0
}

case "${1:-}" in
  setup)   shift; cmd_setup "$@" ;;
  start)   shift; cmd_start "$@" ;;
  stop)    cmd_stop ;;
  restart) cmd_restart ;;
  logs)    shift; cmd_logs "$@" ;;
  status)  cmd_status ;;
  upgrade) cmd_upgrade ;;
  all)     cmd_all ;;
  -h|--help|help|"") usage ;;
  *) die "未知命令: $1（-h 查看帮助）" ;;
esac
