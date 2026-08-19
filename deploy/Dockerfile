# Semantix Gateway 镜像 —— 多阶段构建，非 root 运行（Issue #183）。
#
# 运行层用 alpine（而非设计文档 §5 设想的 scratch/distroless），取舍：
#   ① 网关以 HTTPS 调用上游，需要 CA 证书（scratch 无）；
#   ② docker-compose 的 healthcheck 需要 shell 工具（busybox wget，distroless 无 shell）。
# 镜像仍为单二进制 + 零第三方运行时依赖，体积约 15MB。

# ---- builder ----
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/semantix-gateway ./cmd/semantix-gateway

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 semantix \
    && adduser -S -u 65532 -G semantix semantix \
    && mkdir -p /data \
    && chown 65532:65532 /data
USER 65532:65532
WORKDIR /data
COPY --from=builder /out/semantix-gateway /usr/local/bin/semantix-gateway
EXPOSE 8080
ENTRYPOINT ["semantix-gateway"]
CMD ["--config", "/etc/semantix-gateway.toml"]
