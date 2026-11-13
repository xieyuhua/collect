# 采集平台 Docker 镜像（内含 Chromium，适合 Linux 无 GUI 环境）
# 运行示例：
#   docker build -t collect .
#   docker run -d -p 8686:8686 -v collect-data:/app/data collect
# 首次启动后请在 data/config.json 里把 browser.no_sandbox 设为 true，
# 并将 browser.bin_path 设为 /usr/bin/chromium-browser；
# 或直接在「系统设置 → 浏览器内核」里指定该路径。
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w" -o /out/collect .

FROM alpine:3.20
RUN apk add --no-cache chromium tzdata ca-certificates fontconfig freetype
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/collect /app/collect
VOLUME ["/app/data"]
EXPOSE 8686
ENTRYPOINT ["/app/collect", "-addr", "0.0.0.0:8686", "-open=false"]