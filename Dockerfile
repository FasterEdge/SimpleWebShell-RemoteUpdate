FROM golang:1.24-alpine AS build
# 通过 goproxy.cn 拉取依赖, 避免在无外网代理环境(如国内网络/受限内网)构建超时
ENV GOPROXY=https://goproxy.cn,direct
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/simplewebshell-remoteupdate .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tar gzip unzip coreutils
COPY --from=build /out/simplewebshell-remoteupdate /usr/local/bin/simplewebshell-remoteupdate
ENTRYPOINT ["/usr/local/bin/simplewebshell-remoteupdate"]
