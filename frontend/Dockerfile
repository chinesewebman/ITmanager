# 前端镜像：Vite 构建 → nginx 托管静态文件 + 反代 /api/
#
# 构建期不做任何 secret 注入：前端只用相对路径 /api（见 nginx.conf），
# 所以这份镜像可以在 CI/本地无 .env 的情况下构建。
#
# G-19 / M77：换成 `nginxinc/nginx-unprivileged:1.27-alpine`（官方 non-root 镜像，
# UID 101）。容器级硬化在 compose：read_only + tmpfs(/var/cache/nginx, /var/run, /tmp)
# + cap_drop [ALL] + no-new-privileges。**非 root 是硬要求** —— `web` 是唯一对外
# 入口，nginx master 以 root 跑会保留不必要的特权面（cap_chown/cap_dac_override 等
# 全开）。listen 端口对应改 8080（非 root 默认不可绑 < 1024），compose 端口映射同步
# 改 127.0.0.1:3000:8080。注意：TLS 模板 `nginx-tls.conf.example` 的 listen 80/443
# 需要 root，挂载该模板时**仍用 root 镜像**（见 08-部署运维.md G-19 段）。
FROM node:22-alpine AS build
WORKDIR /app
# 依赖层单独缓存；npm ci 按 package-lock.json 精确安装（含 integrity 校验）
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# 官方 unprivileged 镜像（UID 101 = nginx）。相比自建 `USER nginx` 块：
#   - pid / temp / cache 路径已就位（/tmp/nginx.pid）
#   - listen 端口默认 8080
#   - 与 nginx.conf 的 `listen 8080` 对齐（G-19）
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
# HEALTHCHECK 探 8080（与 nginx.conf listen 对齐）。没有 HEALTHCHECK 时
# `docker compose up --wait` 只看进程在跑 —— nginx 起来了但站点 500/静态文件
# 没打进去也照样报成功。探 / 能真正等到 nginx 可服务。
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1
