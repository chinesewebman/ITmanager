# 后端镜像：多阶段构建（Go 编译 → 最小运行层）
#
# 生产路径不 import 任何 CGO 包（mattn/go-sqlite3 只被 *_test.go 引用），
# 因此 CGO_ENABLED=0 可产出静态链接二进制，运行层不需要 glibc。
#
# 用 CMD 而非 ENTRYPOINT：镜像里的命令（server/migrate/admin-bootstrap/seed/set-role）
# 需要能被 `docker run <image> ./migrate up` 这样整条覆盖。若用 ENTRYPOINT，
# compose/run 的 command 只会作为参数**拼接**到 ENTRYPOINT 后面
# （实际执行 `./server ./migrate up`），迁移会静默变成启动 server。
FROM golang:1.25-alpine AS build
WORKDIR /src
# 依赖层单独缓存：源码改动不会让 go mod download 重跑
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin-bootstrap ./cmd/admin-bootstrap \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/set-role ./cmd/set-role

FROM alpine:3.20
# busybox 自带 wget applet（健康检查用），无需额外 apk；tzdata 供日志时区
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 nmp
WORKDIR /app
COPY --from=build /out/ /app/
# 占位配置：secret 一律留空，必须由 NMP_* 环境变量注入（见 backend/.env.example）。
# 挂载自定义 config.yaml 时这份会被覆盖。
COPY config.yaml ./
USER nmp
EXPOSE 8080
# start-period 给足 180s：HTTP 端口是在 database.Init → migrate.Up **之后**才监听，
# 空卷 + 慢 IO 上 13 条迁移可能 >85s（旧的 10s+5×15s），一旦被判 unhealthy，
# `docker compose up --wait` 会直接失败（--wait-timeout 救不了 unhealthy）。
HEALTHCHECK --interval=15s --timeout=3s --start-period=180s --retries=20 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
CMD ["./server"]
