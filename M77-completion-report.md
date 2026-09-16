# M77 Completion Report — G-19 web 容器最小权限

> **Loop cycle**: 8 of `itmanager-grit-2026q3`
> **Feat**: (commit pending)
> **Intent**: `intent-M77.md` (in same commit)

## 摩擦

`web` 容器是**唯一对外入口**, 但当前以 root 运行 + 无最小权限. nginx master 以 root 跑
会保留不必要的特权面 (cap_chown / cap_dac_override 全开). 安全审计 P3 长期未结案.

## 改动 (docker + docs, ≤2h)

| 文件 | 改动 |
|---|---|
| `frontend/Dockerfile` | `nginx:1.27-alpine` → `nginxinc/nginx-unprivileged:1.27-alpine` (UID 101) + EXPOSE 8080 + HEALTHCHECK 探 8080 |
| `frontend/nginx.conf` | `listen 80` → `listen 8080` |
| `docker-compose.yml` web 服务 | 6 项硬化: ports `127.0.0.1:3000:80` → `127.0.0.1:3000:8080` + `read_only: true` + 3 tmpfs + `cap_drop: [ALL]` + `security_opt: [no-new-privileges:true]` |
| `08-部署运维.md` §8.3.1 | 同步端口 + G-19 段 + TLS 模板约束 |
| `backend/scripts/test_m77_compose_hardening.py` | **新建** — 8 个钉死硬化项 assertion + mutation inversion 反证 |

## Verify

- `docker compose config` 解析 0 错, web 服务 6 项硬化全部展开 ✓
- `python3 backend/scripts/test_m77_compose_hardening.py` **8/8 PASS** ✓
- **mutation inversion 6 red** (revert read_only / cap_drop / security_opt / port / Dockerfile / listen → 6 tests FAIL) ✓
- 保留 G-7 静态 IP `172.28.0.10` 不破 ✓

## TLS 模板冲突

`nginx-tls.conf.example` 的 `listen 80/443` 需要 root. 用户挂载时**切回 `nginx:1.27-alpine` (root 镜像)**.
doc 同步写明, 见 `08-部署运维.md` §8.3.1.

## 决策点

- **D1**: 用 unprivileged 官方镜像而非自建 USER ✓
- **D2**: nginx.conf listen 80 → 8080 (unprivileged 默认端口) ✓
- **D3**: TLS 模板不动, doc 同步说明用户挂载时用 root 镜像 ✓
- **D4**: 保留 G-7 静态 IP ✓
- **D5**: tmpfs 3 个 (/var/cache/nginx /var/run /tmp) ✓
- **D6**: cap_drop ALL + no-new-privileges ✓

## 风险

- **docker 引擎未跑**: PM-direct sandbox 验证只能 `docker compose config` + pytest assertion. 实际跑通 (`docker compose up`) 需 docker 引擎 — 留给下次 smoke
- **G-7 静态 IP 兼容**: 网络层与 UID 无关, 不冲突 (test_web_static_ip_preserved 验过)
- **read_only + nginx cache**: tmpfs 接管 /var/cache/nginx + /var/run + /tmp, nginx 仍可写
- **HEALTHCHECK 探 8080**: 与 unprivileged listen 端口对齐
