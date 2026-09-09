# 前端镜像：Vite 构建 → nginx 托管静态文件 + 反代 /api/
#
# 构建期不做任何 secret 注入：前端只用相对路径 /api（见 nginx.conf），
# 所以这份镜像可以在 CI/本地无 .env 的情况下构建。
FROM node:22-alpine AS build
WORKDIR /app
# 依赖层单独缓存；npm ci 按 package-lock.json 精确安装（含 integrity 校验）
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
# 没有 HEALTHCHECK 时 `docker compose up --wait` 只看进程在跑 —— nginx 起来了但
# 站点 500/静态文件没打进去也照样报成功。探 / 能真正等到 nginx 可服务。
# （非 root 运行见 TODO G-19：本阶段仍是 nginx 默认的 root master。）
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
