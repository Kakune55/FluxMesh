# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
ENV GO111MODULE=on \
	GOPROXY=https://goproxy.cn
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/fluxmesh ./cmd/fluxmesh

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/fluxmesh /usr/local/bin/fluxmesh

# 开发联调场景默认使用 root，避免宿主机 bind mount 目录权限导致 etcd 数据目录无法创建。
# 生产环境建议改回非 root 运行，并在启动前对挂载目录做好 chown/chmod。
ENTRYPOINT ["fluxmesh"]
