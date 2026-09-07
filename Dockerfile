FROM golang:1.25-alpine AS build
# 通过 goproxy.cn 拉取依赖, 避免在无外网代理环境(如国内网络/受限内网)构建超时
ENV GOPROXY=https://goproxy.cn,direct
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/simplewebshell-remoteupdate .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tar gzip unzip coreutils && \
    adduser -D -H -u 1000 app && mkdir -p /app && chown -R app:app /app
COPY --from=build /out/simplewebshell-remoteupdate /usr/local/bin/simplewebshell-remoteupdate
# 以非 root 运行: 更新工作流会执行发布包内脚本/解包 tar, root 权限下若上游被攻破
# 等于把整个容器交出去; app 用户只持有 /app(App.Root 默认值)的写权限。
WORKDIR /app
USER app
ENTRYPOINT ["/usr/local/bin/simplewebshell-remoteupdate"]
