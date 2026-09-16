# M77 — G-19 web 容器最小权限（OMH ulw-loop 第 8 cycle）

> **Loop cycle**: 8 of `itmanager-grit-2026q3`

## Goal

`web` 容器是**唯一对外入口**, 但当前以 root 运行 + 无最小权限. 把 nginx master 切到非 root 用户 + 加 read_only/tmpfs/cap_drop/no-new-privileges 容器级硬化.

## Non-goals

- 不改 `backend/Dockerfile` (G-7 已 ship 非 root 10001)
- 不动 `nginx-tls.conf.example` 的 listen 80/443 模板 (TLS 终止天然要 root; 用户挂载时自行决定, 见 G-19 doc 同步)
- 不引 `docker-compose` 新依赖 (仅用 compose 内置语法)
- 不重写 nginx 配置 (只改 listen 端口 80 → 8080 + 调整 pid/temp 路径)

## Assumptions

- `nginxinc/nginx-unprivileged:1.27-alpine` 是官方 non-root 镜像 (UID 101), master listen 8080
- 不动 TLS 模板 (用户挂载时用宿主机 nginx, 不进容器)
- compose web 容器当前 `127.0.0.1:3000:80` 需改 `127.0.0.1:3000:8080`
- pid / temp 路径默认 `/tmp/nginx.pid` / `/var/cache/nginx` / `/var/run` 容器内可写 (root 改 user 后 nginx.conf 仍写 `/var/run/nginx.pid`, unprivileged 镜像改 `/tmp/nginx.pid`)
- read_only + tmpfs 不会破坏 nginx (cache/run/pid 都在 tmpfs 内)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `frontend/Dockerfile` 改 `nginxinc/nginx-unprivileged:1.27-alpine` | grep verify |
| `frontend/nginx.conf` listen 80 → 8080 | grep verify |
| `docker-compose.yml` web 服务 6 项硬化 (read_only + 2 tmpfs + cap_drop + security_opt + 端口改) | yaml parse + grep |
| 容器内运行 `nginx` 的 UID 非 0 | `docker compose exec web id` (实际跑通时验) |
| 文档 `08-部署运维.md` §8.3.1 sync | section patch |
| 不破坏 G-7 静态 IP `172.28.0.10` 兼容 | yaml 仍含 networks.ipv4_address |
| 已有 healthcheck 不破 | HEALTHCHECK 改 probe 8080 |
| fact_store fact_id=16 | shipped record |

## Verification

- `docker compose config` 解析无错
- `docker compose build web` 成功 (本地构建; 不能跑就在 sandbox 验 yaml)
- 实际跑通需 docker 引擎, PM-direct sandbox 没 docker — 用 `docker compose config --quiet` 验证 + 静态分析
- YAML parse 通过 (python yaml.safe_load)
- nginx.conf 仍 parse: `nginx -t -c <conf>` (本地无 nginx 容器, 用 python 语法检查 fallback)

## Risks

- **TLS 模板冲突**: nginx-tls.conf.example 的 `listen 80` / `listen 443 ssl` 需要 root (≤ 1024). 用户挂载时必须用 `nginx:1.27-alpine` (root) 镜像而非 unprivileged. doc 同步写明
- **静态 IP + unprivileged UID**: web 容器在 172.28.0.10, UID 101 (nginx user) — 网络层与 UID 无关, 不冲突
- **read_only + nginx.conf 写 cache**: `/var/cache/nginx` 与 `/var/run` 走 tmpfs, nginx 仍可写
- **G-7 静态 IP**: 保留 `networks.default.ipv4_address: 172.28.0.10`, 不动
- **healthcheck 探 80 → 8080**: HEALTHCHECK 改成 wget http://127.0.0.1:8080/

## Plan

1. `frontend/Dockerfile`: 改 `nginx:1.27-alpine` → `nginxinc/nginx-unprivileged:1.27-alpine`
2. `frontend/nginx.conf`: `listen 80` → `listen 8080` + pid `/tmp/nginx.pid` 注释
3. `docker-compose.yml` web 服务:
   - ports `127.0.0.1:3000:80` → `127.0.0.1:3000:8080`
   - 加 `read_only: true`
   - 加 `tmpfs: [/var/cache/nginx, /var/run, /tmp]`
   - 加 `cap_drop: [ALL]`
   - 加 `security_opt: [no-new-privileges:true]`
   - healthcheck 改 probe 8080
4. `08-部署运维.md` §8.3.1 同步端口 3000:80 → 3000:8080 + 加 G-19 段
5. fact_store + docs commit

## Decision gate

- **D1**: 用 unprivileged 官方镜像而非自建 USER — 避免自建 USER 时 pid/temp 路径漏改 ✓
- **D2**: nginx.conf listen 80 → 8080 (unprivileged 默认端口) — 不动 1024 以下端口 ✓
- **D3**: TLS 模板不动, doc 同步说明用户挂载时用 root 镜像 — TLS 终止天然要 root ✓
- **D4**: 保留 G-7 静态 IP `172.28.0.10` — 网络层与 UID 无关 ✓
- **D5**: tmpfs 3 个 (/var/cache/nginx /var/run /tmp) — nginx 写 cache/pid/temp 都能跑 ✓
- **D6**: cap_drop ALL + no-new-privileges — 容器级硬化最低门槛 ✓
