# 采集平台 Docker 镜像（内含 Chromium，适合 Linux 无 GUI 环境）
# 运行示例：
#   docker build -t collect .
#   docker run -d -p 8686:8686 -v collect-data:/app/data collect
# 首次启动后请在 data/config.json 里把 browser.no_sandbox 设为 true，
# 并将 browser.bin_path 设为 /usr/bin/chromium-browser；
# 或直接在「系统设置 → 浏览器内核」里指定该路径。
#
# 国内网络可用镜像源构建（deploy/docker-run.sh 会自动处理）：
#   docker build --build-arg REGISTRY=docker.m.daocloud.io/library -t collect .
# 构建参数：
#   REGISTRY  Docker Hub 镜像源前缀（默认为空 = 官方源）
#   GOPROXY   Go 模块代理（默认为空 = 官方 proxy.golang.org）
ARG REGISTRY=""
ARG GOPROXY=""

FROM ${REGISTRY:+$REGISTRY/}golang:1.26-alpine AS build
ARG GOPROXY
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0
RUN [ -n "$GOPROXY" ] && go env -w GOPROXY="$GOPROXY" || true
RUN go build -trimpath -ldflags "-s -w" -o /out/collect .

FROM ${REGISTRY:+$REGISTRY/}alpine:3.20
RUN apk add --no-cache chromium tzdata ca-certificates fontconfig freetype
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/collect /app/collect
VOLUME ["/app/data"]
EXPOSE 8686
ENTRYPOINT ["/app/collect", "-addr", "0.0.0.0:8686", "-open=false"]
