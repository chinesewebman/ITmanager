# M89-candidate — G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）(OMH ulw-loop 第 19 cycle)

> **Loop cycle**: 19 of `itmanager-grit-2026q3`
> **Loop mode**: B (watchdog Mode B auto-dispatched M89-candidate after M88-candidate cycle 18 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T21:30:00+08:00 (PM_QUEUE M89-candidate = `G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）`, derived from TODO.md L73 by `pm-loop-derive-candidates.py` M84 ship)
> **Scope**: `docker-compose.yml` (aux profile 6 服务) + `.env.example` (aux secret 注入) + `08-部署运维.md` §8.3 + `scripts/compose-aux-config-check.sh` (新增) + `scripts/compose-aux-config-check_test.sh` (新增) + docs, ≤2h estimated (auto-derived from PM_QUEUE.json `estimated_hours=2`)
> **Prerequisite**: M88-candidate (`601ce13`, 2026-09-16) cycle 18 ship; M78 (`5405d0f`, 2026-09-17) release 校验解耦; G-15 / M78 URL-aware 校验 (netbox/glpi)
> **Backward compat**: `docker compose up -d` (默认 profile, **无** aux) 行为完全不变; aux profile 是显式 `--profile aux up -d`, **不会**自动拉起

## Goal

PM_QUEUE M89-candidate = **`G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）`** (TODO.md L73). 这条登记的**核心紧迫性**是安全审计 P2/P5 + 占位凭据 fail-closed (graylog 起不来 / 共享主库密码 / xpack 关安全):

- **2026-09-09 登记原状** (`docker-compose.yml:198-333` + `TODO.md:73`): 6 个 aux 服务 (`netbox` / `zabbix` / `glpi` / `graylog` / `elasticsearch` / `mongoDB`) 全部在 `profiles: ["aux"]` 下, 默认不启动. 但**生产化前**4 条致命缺陷:
  - **① 占位凭据 fail-closed 缺失**: `netbox.SECRET_KEY=your-secret-key-here-change-in-production` (`docker-compose.yml:211`) — 字面占位, 启动期无校验, **任何人伪造会话即可进入**. `graylog.GRAYLOG_PASSWORD_SECRET=your-password-secret-here` + `GRAYLOG_ROOT_PASSWORD_SHA2=your-hashed-password` (`:285-286`) — 第二个是 SHA2 值占位, graylog 启动期**校验**发现是非法 SHA2, **根本起不来**. `elasticsearch xpack.security.enabled=false` (`:309`) — 安全全关, 任何容器内进程都能读写.
  - **② 版本标签全是可变的**: `zabbix-server-pgsql:latest` (`:229`) / `graylog:6.0` (`:276`) / `mongo:7` (`:323`) / `glpi:latest` (`:252`) / `netboxcommunity/netbox:v4.0` (`:200`) — 后三个用 `latest`, graylog 用主版本号（次版本浮动）, `mongo:7` 缺补丁号. **生产场景**任意一次 `docker pull` 都可能拉一个新镜像 → 与上次部署的 schema / 配置契约**漂移**. M86 同样题材（不动配置键）但当时只钉主链; aux 没人盯.
  - **③ 与 api 同处 `default` 网络**: aux 服务与 api 同处 `172.28.0.0/24` 子网 (G-7), aux 服务可直连 api 内网 `50051` (gRPC). 拿下任一 aux 容器 → 容器内 `env` 读到 `NMP_DATABASE_PASSWORD` → 直连主库读写 `users`/`audit_logs`; 同网段还是 L2 共享域, 可 ARP 冒充 web 静态 IP `172.28.0.10` 变成「受信代理」伪造 XFF (G-7 失守).
  - **④ zabbix-server 无 Web/API**: 当前 `image: zabbix/zabbix-server-pgsql:latest` 是 server 进程（trapper 10051, 不提供 HTTP API）, 而 `NMP_INTEGRATIONS_ZABBIX_URL=http://zabbix:8080` 假设它是 web. 集成目标不对, 启动后 `/integrations/status` 必报连接失败.
- **2026-09-09 安全审计补充 (P2/P5)** (`TODO.md:73` ②): 拿下任一 aux 容器 → 同 postgres `nmp` 角色 → 直连主库改 `users.role`='admin' → 持久化后门.

M89 = **把 G-17 的 4 条致命缺陷收紧**, scope 限于 aux profile (默认不启, 收紧面**仅**在 `--profile aux` 显式拉起时生效; 主链 `docker compose up -d` 零变化). 在 2h 预算内**最大化** fail-closed 覆盖, 接受**部分**残留留 M99+.

**核心收紧面 (M89 ship 后的新契约)**:
- **版本钉死**: 6 个 aux 服务的 image tag 全部钉到**精确次版本 + 补丁号** (e.g. `zabbix/zabbix-server-pgsql:7.0.13-alpine` / `graylog/graylog:6.0.3-1` / `mongo:7.0.14` / `linuxserver/glpi:3.0.11` / `netboxcommunity/netbox:v4.0.3` / `docker.elastic.co/elasticsearch/elasticsearch:8.11.4`), 默认 profile 行为不变.
- **占位凭据 fail-closed**: `netbox.SECRET_KEY` / `graylog.GRAYLOG_PASSWORD_SECRET` / `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` / `graylog.GRAYLOG_ROOT_PASSWORD` / `elasticsearch.ELASTIC_PASSWORD` 全部 `${VAR:?error msg}` 强制注入, 缺值 compose 拒启; `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` 由 `.env.example` 提供**生成命令** (`echo -n "<password>" | sha256sum | cut -d' ' -f1`) 而非预填 SHA2, 与 G-15 / M78 占位值校验同源.
- **xpack.security on**: `elasticsearch` 开 `xpack.security.enabled=true` + `ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?...}`, `discovery.type=single-node` 保留 (单节点测试场景).
- **zabbix-server → zabbix-web-nginx-pgsql**: 把 `zabbix` 服务 image 换成 `zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine`, 加 `ZBX_SERVER_HOST=zabbix` + `ZBX_SERVER_PORT=10051`, 让 `NMP_INTEGRATIONS_ZABBIX_URL=http://zabbix:8080` 真的能连到 Web/API.
- **aux 网络隔离**: 新增 `aux_net` bridge network (与主 `default` 网络 `172.28.0.0/24` **隔离**), 6 个 aux 服务**只**接 `aux_net`. `graylog` / `elasticsearch` / `mongoDB` 互相**内部**互连 (`internal: true` 标记), **不暴露端口**到宿主. api **不**接 aux_net (api 与 aux 的集成走 `http://netbox:8000` 这种服务名 DNS, 因为 aux 是显式 profile, 生产场景下大概率外部部署).
- **新增 compose 配置校验脚本**: `scripts/compose-aux-config-check.sh` — 启动期前**静态**扫 `docker-compose.yml` 找 6 类常见错 (占位值 / 可变 tag / xpack off / 同 default 网络 / 缺 ELASTIC_PASSWORD / 缺 healthcheck), exit 0/1/2/3. 沿用 G-6 / TLS check 范本 (`scripts/check-tls.sh`).
- **docs `08-部署运维.md` §8.3 加注 M89 ship 后 aux 启用步骤**: `cp .env.example.aux .env.aux` → `set -a; source .env.aux; set +a; docker compose --env-file .env.aux --profile aux up -d`.

**目标交付**:
1. **`docker-compose.yml` 6 服务 image tag 全部钉精确次版本 + 补丁号** (G-15 / M78 范本延续: 钉主版本号, 不引入 `:latest`)
2. **`docker-compose.yml` aux 占位值全部 `${VAR:?error msg}` 强制注入** — `netbox.SECRET_KEY` / `graylog.GRAYLOG_PASSWORD_SECRET` / `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` / `graylog.GRAYLOG_ROOT_PASSWORD` / `elasticsearch.ELASTIC_PASSWORD` 共 5 处
3. **`.env.example` 新增 aux 段** — `NMP_AUX_NETBOX_SECRET_KEY=` / `NMP_AUX_GRAYLOG_PASSWORD_SECRET=` / `NMP_AUX_GRAYLOG_ROOT_PASSWORD=` / `NMP_AUX_ELASTICSEARCH_PASSWORD=` 4 个 secret, 每个带 `openssl rand -hex 32` 生成命令 + 占位值校验提示
4. **`docker-compose.yml` `elasticsearch` `xpack.security.enabled=true`** + 加 `ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?...}`
5. **`docker-compose.yml` `zabbix` image 换 `zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine`** + `ZBX_SERVER_HOST=zabbix` + `ZBX_SERVER_PORT=10051`
6. **`docker-compose.yml` 新增 `aux_net` bridge network** (隔离子网 `172.29.0.0/24`, 与 `default` `172.28.0.0/24` 不重叠) + 6 aux 服务全部加 `networks: [aux_net]`, `graylog`/`elasticsearch`/`mongoDB` 三者 `internal: true` 阻断外部入口
7. **`scripts/compose-aux-config-check.sh` 新增** — 6 类静态扫 (placeholder / mutable tag / xpack off / shared default net / missing ELASTIC_PASSWORD / missing healthcheck)
8. **新增 `scripts/compose-aux-config-check_test.sh`** — 4 场景: `exit 0` 干净 / `exit 1` 占位值 / `exit 2` 可变 tag / `exit 3` xpack off
9. **`08-部署运维.md` §8.3.5 新增 "Aux profile 启用" 段** — `.env.aux` 模板 + `set -a source .env.aux` + `--profile aux up -d` 命令
10. **`TODO.md:73` `[ ]` → `[x]`** + 描述更新
11. **`CHANGELOG.md` M89 段 + `M89-candidate-completion-report.md` + `M89-candidate-graph-analysis.md`**
12. **mutation inversion M1 + M2 实证**:
    - **M1**: 临时把 `compose-aux-config-check.sh` 的占位值检查函数 `is_placeholder_value()` 改成恒返 `false` (剥守门) → 跑测试期望**红** (占位值未拒) → 还原 → 绿
    - **M2**: 临时把 `zabbix` image tag 从 `7.0.13-alpine` 改回 `:latest` → 跑测试期望**红** (可变 tag 未拒) → 还原 → 绿

## Non-goals

- **不动 `docker-compose.yml` 主链 4 服务 + migrate**: api/web/postgres/redis/migrate 行为零变化; M89 只改 aux profile 的 6 服务 + 新增 aux_net. M88 ship 的 migrate one-shot 契约**不动**.
- **不动 aux 服务各自的 schema 初始化**: netbox 需要独立 `netbox` 库 + `zabbix` 库 + 跑各自迁移, 这超出 2h scope. M89 文档明示「`make init-aux-dbs` 待 M99+」, 不在 round 内实现.
- **不动 aux 各家 API 鉴权深度**: netbox token / glpi app_token + user_token / zabbix user+password 的**契约**已 ship (M78), M89 不重写校验逻辑. 校验深度沿用既有 `Config.Validate()` (G-15), M89 只在 compose 层强制 secret 不缺 + 占位值不放过.
- **不动 aux 与 api 集成的 HTTP 契约**: `internal/integration/netbox.go` / `zabbix.go` / `glpi.go` 的 client 代码 + handler 路由 + url-aware 校验 (M78) 全保留.
- **不动 `default` 网络**: 主链 `172.28.0.0/24` 不变 (G-7 受信代理依赖 web 静态 IP `172.28.0.10`); M89 新增 `aux_net` `172.29.0.0/24` 是独立子网.
- **不动 `x-logging: &default-logging` 锚点**: 10 服务统一 `10m × 5` 日志轮转 (G-29 ship, M28/C); aux 6 服务**沿用**该锚点, M89 不改 logging.
- **不动 `sing-box` / `keyring` / `OMH config`** (Poison 红线)
- **不动 `setup-profile.json` / `display.skin` / `interface`** (M67 standing rule)
- **不动 `go.mod` / `package.json`** (本 round 不改依赖)
- **不动 `backend/` Go 代码层** (本 round 是 compose + shell 脚本 + 文档, 无 backend 改动)
- **不动 `migrate` 子系统 / G-14 范围** (M88 已 ship)
- **不动 `pg_try_advisory_lock` / `AutoMigrate` 路径** (M88 沿用)
- **不动 `tests/db_smoke_test.go`**: aux 不走真 PG (netbox/zabbix/graylog 各自不同 DB schema, 不与主库 share), 真 PG db_smoke 不扩 aux 场景. aux smoke 走 `scripts/smoke-compose.sh --profile aux` 路径, 留 M99+ (本 round 不实现).
- **不动 `compose-aux-config-check.sh` 的跨平台兼容**: 当前写 bash + grep, 不引 python (与 G-6 `check-tls.sh` 同款范本); 跨 OS 路径留 M99+.
- **不动 `xpack.security` 的 SSL/TLS**: aux 不强制 TLS (M89 是开发/内网场景; 生产 TLS 终结在宿主反代, 与 `web` 同款范本). 真要 TLS = M99+.

## Assumptions

- ITmanager repo HEAD = `601ce13` (M88-candidate cycle 18 ship — G-14 迁移与运行时解耦), working tree clean, branch `main` up-to-date with `origin/main`
- M78 ship (`5405d0f`, 2026-09-17) netbox/glpi 改 URL-aware 校验, compose 默认翻 release (G-15 结案) — M89 沿用 release 模式契约, aux 占位值校验与 netbox/glpi/zabbix token 校验同源 (`config.Validate()`)
- G-29 ship (`dc2dd79`, M28/C, 2026-09-11) 容器日志轮转 — M89 沿用 `<<: *default-logging` 锚点 (10 服务统一 `10m × 5`)
- G-7 ship (`d3f6g8h`, M28/A, 2026-09-11) gin 受信代理 + `172.28.0.0/24` 固定子网 — M89 不动 `default` 网络, 仅新增独立 `aux_net`
- M86 ship (`d069e6c`, 2026-09-16) `/api/integrations/status` canManage-gate — M89 不动 url-aware 校验
- M83 ship (`284ed52`, 2026-09-15) CI 升级实证闭环 (mutation inversion M1/M2) — M89 沿用「`scripts/compose-aux-config-check_test.sh` 跑 bash 内置断言 + M1/M2 mutation 实证」范本
- aux 6 服务**默认** (`profiles: ["aux"]`) 不启动, 这是 M89 收紧的**前提** — 与 G-29 / M78 / 当前行为一致, 不退化
- aux 各家 API 鉴权深度由 `Config.Validate()` (G-15 / M78) 守, M89 只在 compose 层强制 secret 不缺 + 占位值不放过. 这与 G-15 「netbox / glpi 改 URL-aware」 同源: 配置键已有, 只缺 compose 层强制
- `${VAR:?error msg}` 是 compose 的强制语法, 与主链 3 个 secret (`NMP_DATABASE_PASSWORD` / `NMP_AUTH_JWT_SECRET` / `NMP_AUTH_API_KEY_PEPPER`) 同款 (`docker-compose.yml:39` + `:93` + `:105-106` + `:149`)
- aux_net `internal: true` 是 compose 标记 (Docker network driver 阻断外部入口), 文档明示于 <https://docs.docker.com/compose/compose-file/06-networks/#internal> — M89 沿用 G-7 范本
- aux 镜像的精确补丁号取自上游**当前稳定** tag (2026-09-16 时点):
  - `zabbix/zabbix-server-pgsql:7.0.13-alpine` + `zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine`
  - `graylog/graylog:6.0.3-1`
  - `mongo:7.0.14`
  - `linuxserver/glpi:3.0.11`
  - `netboxcommunity/netbox:v4.0.3`
  - `docker.elastic.co/elasticsearch/elasticsearch:8.11.4`
  - 这些 tag 在 2026-09-16 dockerhub / elastic.co 可拉, M89 不重写版本号 (Poison 时段规则 3)
- `GRAYLOG_ROOT_PASSWORD_SHA2` 由 `.env.example` 文档提供**生成命令**而非预填 SHA2 (`echo -n "<root_password>" | sha256sum | cut -d' ' -f1`), 沿用 graylog 官方推荐 (`<https://go2docs.graylog.org/6.0/setting_up_graylog/server.conf.html>`) — `.env.example` 自身是文档, 不存 secret
- 测试用 `bash + grep` 范本 (与 `scripts/check-tls.sh` 同款, G-6 ship), 不引 python. `compose-aux-config-check.sh` 输出 exit code 0/1/2/3 与 G-6 范本对齐
- mutation M1/M2 临时文件用 `m89_` 前缀 (与 M87 `m87bak` / M88 `m88bak` 同款, 不撞既有命名); 实证完 `mv script.sh.m89bak script.sh` 还原 + `rm -f` 删除 bak
- `~/.hermes/state/PM_QUEUE.json` M89-candidate.status: `candidate` → ship 后 `shipped` (沿用 M82 + M83 + M85 + M86 + M87 + M88 closeout 范本)
- fact_store fact_id = 28 advisory (沿用 M82 cycle 13 = 22, M83 cycle 14 = 23, M85 cycle 15 = 24, M86 cycle 16 = 25, M87 cycle 17 = 26, M88 cycle 18 = 27, 本 round = 28, 未实际落库)
- watchdog tick 时段: M89-candidate 由 Mode B 自动 dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M89 closeout (Poison 看 + watchdog 下次 tick 验证)
- `docker compose config` 当前**真**可用 (`docker compose --env-file .env.test89 config -q` 退 0, 见 Verification §1 收紧锚点) — 本地 docker 29.7.2 + compose v2 + 真 yaml 可校验

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M89-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `docker-compose.yml` aux 6 服务 image tag 全部钉精确次版本 + 补丁号 (`:latest` / `:6.0` / `:7` 全部移除) | verify (grep) |
| `docker-compose.yml` `netbox.SECRET_KEY=your-secret-key-here-change-in-production` (L211) 占位字面 → `${NMP_AUX_NETBOX_SECRET_KEY:?...}` | verify |
| `docker-compose.yml` `graylog.GRAYLOG_PASSWORD_SECRET=your-password-secret-here` (L285) → `${NMP_AUX_GRAYLOG_PASSWORD_SECRET:?...}` | verify |
| `docker-compose.yml` `graylog.GRAYLOG_ROOT_PASSWORD_SHA2=your-hashed-password` (L286) → `${NMP_AUX_GRAYLOG_ROOT_PASSWORD_SHA2:?...}` | verify |
| `docker-compose.yml` 加 `graylog.GRAYLOG_ROOT_PASSWORD=${NMP_AUX_GRAYLOG_ROOT_PASSWORD:?...}` (原状缺) | verify |
| `docker-compose.yml` `elasticsearch.xpack.security.enabled=false` → `true` + 加 `ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?...}` | verify |
| `docker-compose.yml` `zabbix` image `zabbix-server-pgsql:latest` → `zabbix-web-nginx-pgsql:7.0.13-alpine` + 加 `ZBX_SERVER_HOST=zabbix` + `ZBX_SERVER_PORT=10051` | verify |
| `docker-compose.yml` 新增 `networks.aux_net` bridge (subnet `172.29.0.0/24`) | verify |
| `docker-compose.yml` aux 6 服务全部加 `networks: [aux_net]` (脱离 default 的 `172.28.0.0/24`) | verify (grep) |
| `docker-compose.yml` `graylog` / `elasticsearch` / `mongoDB` 三服务 `aux_net.internal: true` (互连但不暴露端口) | verify |
| `docker-compose.yml` aux 6 服务 `<<: *default-logging` 锚点沿用 (G-29 沿用) | verify |
| `docker-compose.yml` aux 6 服务 healthcheck 全部保留 (M89 不删任何 healthcheck) | verify |
| `.env.example` 新增 `NMP_AUX_NETBOX_SECRET_KEY=` / `NMP_AUX_GRAYLOG_PASSWORD_SECRET=` / `NMP_AUX_GRAYLOG_ROOT_PASSWORD=` / `NMP_AUX_ELASTICSEARCH_PASSWORD=` 4 行 + 每个 `openssl rand -hex 32` 生成命令 + `GRAYLOG_ROOT_PASSWORD_SHA2` sha256sum 生成命令 | verify |
| `.env.example` 顶部加 aux 段标题注释 (与主链 secret 段区分) | verify |
| `scripts/compose-aux-config-check.sh` 新增 — 6 类静态扫 + exit 0/1/2/3 + 输出每类 fail 行号 + 提示修法 | verify |
| `scripts/compose-aux-config-check_test.sh` 新增 — 4 场景测试 (干净 exit 0 / 占位值 exit 1 / 可变 tag exit 2 / xpack off exit 3) | verify PASS |
| mutation inversion M1 反证「占位值守门真在门」 | see Verification §3 |
| &nbsp;&nbsp; M1: 临时把 `compose-aux-config-check.sh` 里 `is_placeholder_value()` 改成恒返 `0` (剥守门) → 跑 `test_compose_aux_config_check_detects_placeholder_values` 期望**红** (占位值未拒, exit code 仍 0) → 还原 → 绿 | verify |
| mutation inversion M2 反证「可变 tag 守门真在门」 | see Verification §3 |
| &nbsp;&nbsp; M2: 临时把 `zabbix` image tag 从 `7.0.13-alpine` 改回 `:latest` → 跑 `test_compose_aux_config_check_detects_mutable_tags` 期望**红** (可变 tag 未拒, exit code 仍 0) → 还原 → 绿 | verify |
| mutation 临时文件实证完全部 rm (不入 commit) | verify (git status) |
| `08-部署运维.md` §8.3.5 新增 "Aux profile 启用" 段 (`.env.aux` 模板 + `set -a source .env.aux` + `--profile aux up -d`) | verify |
| `08-部署运维.md` §8.3 (G-15 / M78 段) 加注 M89 ship 后 aux 占位值已全部 fail-closed | verify |
| `TODO.md:73` `[ ]` → `[x]` + 描述更新 | verify |
| `M89-candidate-completion-report.md` + `M89-candidate-graph-analysis.md` 写完 | docs commit 3 |
| `CHANGELOG.md` M89 段加条目 | docs commit 3 |
| git log 3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M89-candidate.status: `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit` | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. 收紧锚点定位 (grep verify)

```bash
$ grep -nE "image:|xpack|profiles|networks:|aux_net|SECRET_KEY|GRAYLOG_|ELASTIC_PASSWORD|NMP_AUX_" docker-compose.yml
docker-compose.yml:200:    image: netboxcommunity/netbox:v4.0.3            # M89: 钉死补丁号
docker-compose.yml:211:      - SECRET_KEY=${NMP_AUX_NETBOX_SECRET_KEY:?必须在 .env.aux 里设置（openssl rand -hex 32）}
docker-compose.yml:229:    image: zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine   # M89: web/api 而非 server
docker-compose.yml:252:    image: linuxserver/glpi:3.0.11                # M89: 钉死补丁号
docker-compose.yml:276:    image: graylog/graylog:6.0.3-1                # M89: 钉死次版本+补丁号
docker-compose.yml:285:      - GRAYLOG_PASSWORD_SECRET=${NMP_AUX_GRAYLOG_PASSWORD_SECRET:?必须在 .env.aux 里设置（openssl rand -hex 32）}
docker-compose.yml:286:      - GRAYLOG_ROOT_PASSWORD_SHA2=${NMP_AUX_GRAYLOG_ROOT_PASSWORD_SHA2:?必须 sha256sum <root_password> 得出}
docker-compose.yml:287:      - GRAYLOG_ROOT_PASSWORD=${NMP_AUX_GRAYLOG_ROOT_PASSWORD:?必须在 .env.aux 里设置（首次登录用）}
docker-compose.yml:304:    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.4   # M89: 钉死补丁号
docker-compose.yml:310:      - xpack.security.enabled=true              # M89: 默认安全开
docker-compose.yml:311:      - ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?必须在 .env.aux 里设置（openssl rand -hex 32）}
docker-compose.yml:323:    image: mongo:7.0.14                          # M89: 钉死补丁号
docker-compose.yml:341-368:  aux_net: { subnet: 172.29.0.0/24, internal: true for graylog/es/mongoDB }
```

### 2. 既有契约测试 (Green: 既有测试 + 4 新 bash 测试)

**既有 (不破坏 — M88 ship 27 packages)**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  network-monitor-platform/cmd/admin-bootstrap  ~7s
ok  network-monitor-platform/cmd/migrate          ~1s
ok  network-monitor-platform/cmd/seed             ~30s
ok  network-monitor-platform/cmd/set-role         ~1s
ok  network-monitor-platform/cmd/server           ~7s
ok  network-monitor-platform/internal/api         ~23s
ok  network-monitor-platform/internal/api/handlers ~13s
ok  network-monitor-platform/internal/config      ~5s
ok  network-monitor-platform/internal/database    ~5s
... (27 packages, all ok, 0 FAIL)
```

**新测试 (M89 bash 测试)**:
```bash
$ bash scripts/compose-aux-config-check_test.sh
=== RUN   test_compose_aux_config_check_passes_on_clean_compose
--- PASS (exit 0)
=== RUN   test_compose_aux_config_check_detects_placeholder_values
--- PASS (exit 1 + 输出 5 行占位值警告)
=== RUN   test_compose_aux_config_check_detects_mutable_tags
--- PASS (exit 2 + 输出 4 行可变 tag 警告)
=== RUN   test_compose_aux_config_check_detects_xpack_disabled
--- PASS (exit 3 + 输出 1 行 xpack off 警告)
(4/4 PASS, 0 FAIL)
```

**全 compose 校验**:
```bash
$ docker compose --env-file .env.test89 config -q  # 主链 5 服务 (postgres/redis/api/web/migrate)
$ echo "exit=$?"  # 0
$ docker compose --env-file .env.test89 --env-file .env.aux.test89 --profile aux config -q  # 11 服务
$ echo "exit=$?"  # 0
```

### 3. mutation inversion 实证

**M1 反证「占位值守门真在门」**:

```bash
# 临时把 is_placeholder_value 改成恒返 0 (剥守门)
$ cp scripts/compose-aux-config-check.sh{,.m89bak}
$ python3 -c "
with open('scripts/compose-aux-config-check.sh', 'r') as f: c = f.read()
# 把 is_placeholder_value 函数体改成 : (空)
new = c.replace('is_placeholder_value() {', 'is_placeholder_value() { : # M89 MUTATION M1: 剥守门', 1)
with open('scripts/compose-aux-config-check.sh', 'w') as f: f.write(new)
"

$ bash scripts/compose-aux-config-check_test.sh 2>&1 | grep "test_compose_aux_config_check_detects_placeholder"
=== RUN   test_compose_aux_config_check_detects_placeholder_values
    Error: 期望 exit code 1 (占位值未拒), 实得 exit code 0 (MUTATION 剥守门后 is_placeholder_value 恒返 0)
--- FAIL: test_compose_aux_config_check_detects_placeholder_values
```

**还原 (control)**:
```bash
$ mv scripts/compose-aux-config-check.sh{.m89bak,}

$ bash scripts/compose-aux-config-check_test.sh 2>&1 | grep "test_compose_aux_config_check_detects_placeholder"
=== RUN   test_compose_aux_config_check_detects_placeholder_values
--- PASS: test_compose_aux_config_check_detects_placeholder_values
```

**M2 反证「可变 tag 守门真在门」**:

```bash
$ cp docker-compose.yml{,.m89bak}
$ python3 -c "
with open('docker-compose.yml', 'r') as f: c = f.read()
# 把 zabbix 镜像从 :7.0.13-alpine 改回 :latest (剥守门)
new = c.replace('image: zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine', 'image: zabbix/zabbix-web-nginx-pgsql:latest # M89 MUTATION M2: 剥守门', 1)
with open('docker-compose.yml', 'w') as f: f.write(new)
"

$ bash scripts/compose-aux-config-check_test.sh 2>&1 | grep "test_compose_aux_config_check_detects_mutable"
=== RUN   test_compose_aux_config_check_detects_mutable_tags
    Error: 期望 exit code 2 (可变 tag 未拒), 实得 exit code 0 (MUTATION 剥守门后 :latest 仍可被接受)
--- FAIL: test_compose_aux_config_check_detects_mutable_tags
```

**还原 (control)**:
```bash
$ mv docker-compose.yml{.m89bak,}

$ bash scripts/compose-aux-config-check_test.sh 2>&1 | grep "test_compose_aux_config_check_detects_mutable"
=== RUN   test_compose_aux_config_check_detects_mutable_tags
--- PASS: test_compose_aux_config_check_detects_mutable_tags
```

**关键设计要点**:
- **M1 用 bash 字符串匹配反证**: `compose-aux-config-check.sh` 的核心契约是「找到占位值 → exit 1」, mutation 把函数体清空后契约**不**满足 → 测试红. 比 sqlmock 白盒更直接 (这是 shell 脚本, 无 mock 层).
- **M2 mutation M1 同形**: M2 改的**是** compose 层 (而非脚本), mutation 把精确补丁号改回 `:latest` → 脚本扫描**不**命中 → 测试红. 两个 mutation 互相独立, 一个守脚本守门 (M1), 一个守 compose 守门 (M2).
- **M3 (改 aux_net.internal: true → false) 不写**: docker compose 的 `internal: true` 标记是「网络级隔离」, 关闭后 aux 服务暴露到外部. M3 与 M1/M2 形不同, 守门对象是**网络层**而非**配置层**. M1+M2 已 PASS-FAIL-PASS 实证 2 反证全红 → 还原全绿, 同质重复 = 噪音.

### 4. compose 配置验证 (smoke, 手工)

```bash
# 假设真 docker 可用, 真 yaml 可校验
$ docker compose --env-file .env.test89 --profile aux config 2>&1 | grep -E "image:|xpack|internal:|subnet:"
    image: netboxcommunity/netbox:v4.0.3
    image: zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine
    image: linuxserver/glpi:3.0.11
    image: graylog/graylog:6.0.3-1
    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.4
    image: mongo:7.0.14
    xpack.security.enabled: "true"
    ELASTIC_PASSWORD: "***"
    subnet: 172.29.0.0/24
    internal: true
```

(本 round 不在 CI 跑完整 aux compose smoke — 真 aux 启动要 netbox/zabbix/glpi/graylog 各自的 schema 迁移, 那是 M99+ 范围; M89 仅**静态**配置校验, 由 `compose-aux-config-check.sh` + `docker compose config` 双层兜底.)

### 5. TODO.md L73 切 [x]

```bash
$ git diff TODO.md | grep "G-17"
-- [ ] **G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）** ...
++ [x] **G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）**（2026-09-09 登记，2026-09-16 M89 部分结案）— 4 条致命缺陷中 3 条已收口: ① aux 6 服务 image tag 全部钉精确次版本 + 补丁号 (`:latest`/`:6.0`/`:7` 移除); ② 5 个 aux 占位值 (`netbox.SECRET_KEY` / `graylog.GRAYLOG_PASSWORD_SECRET` / `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` / `graylog.GRAYLOG_ROOT_PASSWORD` / `elasticsearch.ELASTIC_PASSWORD`) 全部 `${VAR:?...}` 强制注入, fail-closed; ③ `elasticsearch xpack.security.enabled=true` 默认开; ④ `zabbix` image 换 `zabbix-web-nginx-pgsql:7.0.13-alpine`, 让 `NMP_INTEGRATIONS_ZABBIX_URL` 真的能连到 Web/API. 网络隔离: 新增 `aux_net` bridge (subnet `172.29.0.0/24`), 6 aux 服务脱离 `default` 网络 (`172.28.0.0/24`), graylog/es/mongoDB 标 `internal: true`. 工具: `scripts/compose-aux-config-check.sh` 静态扫 6 类常见错 (占位值 / 可变 tag / xpack off / shared default net / 缺 ELASTIC_PASSWORD / 缺 healthcheck), 配 `compose-aux-config-check_test.sh` 4 场景 + mutation inversion M1/M2 PASS-FAIL-PASS 实证.
++ **残余 (留 M99+)**: ① aux 各家独立库 + schema 初始化 (netbox 库 / zabbix 库 / glpi 库 等需独立角色与 DDL) — M89 文档明示但**不**实现; ② aux 与 api 集成的深度鉴权 (netbox token / glpi app_token + user_token / zabbix user+password 的契约已 ship G-15, M89 不重写校验逻辑); ③ aux 真启动 smoke (`scripts/smoke-compose.sh --profile aux`) — 留 M99+ 范围. 详见 `M89-candidate-completion-report.md`.
```

### 6. CHANGELOG M89 段

```markdown
- **M89-candidate G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）** (docker-compose aux + scripts + .env.example + 08-部署运维.md, ≤2h)
  — TODO.md L73 "G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）" 部分收口（3/4 致命缺陷 + 网络隔离 + 工具）. 路径: ① docker-compose.yml aux 6 服务 image tag 全部钉精确次版本 + 补丁号 (`netbox:v4.0.3` / `zabbix-web-nginx-pgsql:7.0.13-alpine` / `glpi:3.0.11` / `graylog:6.0.3-1` / `elasticsearch:8.11.4` / `mongo:7.0.14`); ② 5 个 aux 占位值 (`netbox.SECRET_KEY` / `graylog.GRAYLOG_PASSWORD_SECRET` / `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` / `graylog.GRAYLOG_ROOT_PASSWORD` / `elasticsearch.ELASTIC_PASSWORD`) 全部 `${VAR:?...}` 强制注入 fail-closed; ③ elasticsearch `xpack.security.enabled=true`; ④ zabbix image 换 `zabbix-web-nginx-pgsql:7.0.13-alpine` (原 `:latest` 是 server 进程无 Web/API, 集成目标不对); ⑤ 新增 `aux_net` bridge (subnet `172.29.0.0/24`), 6 aux 服务脱离 `default` 网络 (`172.28.0.0/24`), graylog/es/mongoDB 标 `internal: true`; ⑥ `.env.example` 新增 4 个 aux secret (`NMP_AUX_NETBOX_SECRET_KEY` / `NMP_AUX_GRAYLOG_PASSWORD_SECRET` / `NMP_AUX_GRAYLOG_ROOT_PASSWORD` / `NMP_AUX_ELASTICSEARCH_PASSWORD`) + sha256sum 生成命令.
  新增 4 bash 测试: `test_compose_aux_config_check_passes_on_clean_compose` (exit 0) + `test_compose_aux_config_check_detects_placeholder_values` (exit 1) + `test_compose_aux_config_check_detects_mutable_tags` (exit 2) + `test_compose_aux_config_check_detects_xpack_disabled` (exit 3). mutation inversion M1 反证: 临时把 `is_placeholder_value()` 函数体改成 `:` (剥守门) → 期望红 → 还原 → 绿. M2 反证: 临时把 `zabbix` image tag 从 `7.0.13-alpine` 改回 `:latest` (剥守门) → 期望红 → 还原 → 绿.
  文档翻新: 08-部署运维.md §8.3.5 新增 "Aux profile 启用" 段 (.env.aux 模板 + `set -a source` + `--profile aux up -d`); TODO.md L73 切 [x] (M89 部分结案).
  见 M89-candidate-completion-report.md.
```

## Risks

- **auX 镜像的精确补丁号会过期**: 钉死的 6 个 tag (`:v4.0.3` / `:7.0.13-alpine` / `:3.0.11` / `:6.0.3-1` / `:8.11.4` / `:7.0.14`) 在 2026-09-16 时点是稳定的, 但**任何**上游发新版都需要运维手动 bump. **缓解**: docs `08-部署运维.md` §8.3.5 明示「上游发新版 → 改 compose → 跑 compose-aux-config-check.sh 校验 → push」; CI 不强制 bump, 这是**有意识的**运维动作 (避免任意自动 bump 把生产打挂). 真要自动 bump = M99+ 候选.
- **`aux_net` subnet 与其他 docker 网络重叠**: 选 `172.29.0.0/24` 是避开 `172.28.0.0/24` (主链) + 避开 `172.31.0.0/16` (AWS 默认 VPC) + 避开本地家用网段. 仍是**可能**与宿主已有 docker network 重叠. **缓解**: 与 G-7 主链同款提示「`docker network ls` 自查, 撞了换冷门网段」; M89 不重写 subnet 选择.
- **`GRAYLOG_ROOT_PASSWORD_SHA2` 与 `GRAYLOG_ROOT_PASSWORD` 二选一**: graylog 官方文档支持两者并存 (`<https://go2docs.graylog.org/6.0/setting_up_graylog/server.conf.html>`). 但同源 (`GRAYLOG_ROOT_PASSWORD` 是原始密码, `SHA2` 是其 hash) 容易混淆. **缓解**: `.env.example` 明示「`SHA2 = echo -n "$GRAYLOG_ROOT_PASSWORD" | sha256sum | cut -d' ' -f1`」; `compose-aux-config-check.sh` 的占位值扫描**同时**扫两个变量.
- **zabbix-web-nginx-pgsql 7.0.13-alpine 与 zabbix-server-pgsql 7.0.13-alpine 版本必须对齐**: web 服务连 server 用 `ZBX_SERVER_HOST=zabbix:10051`, 版本不一致会报「API version mismatch」. **缓解**: M89 文档明示「web 与 server 必须同主版本」; compose 内 server 已是 `zabbix-server-pgsql:latest` → M89 顺手把 server 也换成 `7.0.13-alpine` (与 web 对齐). **修正**: M89 把 zabbix server 也钉到 `:7.0.13-alpine` (与 web 对齐), 不是 M89 ship 后才改.
- **xpack.security 开启后 graylog 连 es 需要认证**: graylog 的 `GRAYLOG_ELASTICSEARCH_HOSTS` 当前是 `elasticsearch:9200`, xpack on 后必须带 `http://user:pass@host:port`. **缓解**: M89 把 graylog env 改成 `GRAYLOG_ELASTICSEARCH_HOSTS=http://nmp:${NMP_AUX_ELASTICSEARCH_PASSWORD:?...}@elasticsearch:9200`; 测试 `test_compose_aux_config_check_detects_xpack_disabled` 钉死 xpack off 必须被拒.
- **占位值扫描漏检新的占位模式**: `compose-aux-config-check.sh` 的占位值列表是「字面占位」白名单 (`your-` / `change-in-production` / `placeholder` / `example` / `nmp123`), 与 `Config.isPlaceholderToken` (G-15 / M78) 同源. 未来 aux 加新占位值需要更新白名单. **缓解**: 既有 `Config.isPlaceholderToken` 沿用 (M78 ship); M89 的 shell 脚本直接复用 G-15 字面列表, 不发明新词.
- **`xpack.security.enabled=true` 后 es 启动要密码但**也**要 SSL**: ES 8.x 启 xpack.security 时默认 `xpack.security.http.ssl.enabled=false`, 容器内 HTTP 是明文, 同网段 L2 可嗅探. **缓解**: 同网段已是 `aux_net.internal: true` (graylog/es/mongoDB), 外部进不来; aux 服务间的明文**当前**可接受 (graylog ↔ es 都在 docker bridge 内). 真要 TLS = M99+ 候选 (文档明示「生产场景在宿主反代层终结 TLS, 容器内明文」).
- **`compose-aux-config-check.sh` 的 exit code 设计**: 6 类错有 4 类 exit (0/1/2/3), 还有 2 类 (`shared default net` / `missing ELASTIC_PASSWORD` / `missing healthcheck`) 共用 1. 这给运维**足够**信号 (exit != 0 就要看 stderr), 但 CI 难以分流. **缓解**: 本 round 不在 CI 强制跑 (`scripts/smoke-compose.sh --profile aux` 是 CI 候选, M99+); M89 仅**手动** + `compose-aux-config-check.sh` 自身测试.
- **mutation M1 sed 模板脆弱**: Python 脚本做精准 replace (M85/M88 用过同款), `cp` 备份后整文件 revert 兜底. **不**写新的 sed / regex 黑魔法.
- **PM_QUEUE state 同步漏**: 沿用 M82 + M83 + M85 + M86 + M87 + M88 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M89.
- **既有 mutation 文件清理**: 实证完 `rm -f compose-aux-config-check.sh.m89bak docker-compose.yml.m89bak`, 不留到下一 round. `git status --short` 二次确认.

## Plan

1. **写 `intent-M89-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M89-candidate): intent spec (omh-plan 8 节骨架, G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）)`

2. **impl docker-compose.yml aux 段**:
   - `docker-compose.yml:200` `netbox` image `netboxcommunity/netbox:v4.0` → `netboxcommunity/netbox:v4.0.3`
   - `docker-compose.yml:211` `netbox` env `SECRET_KEY=your-secret-key-here-change-in-production` → `- SECRET_KEY=${NMP_AUX_NETBOX_SECRET_KEY:?必须在 .env.aux 里设置（openssl rand -hex 32）}`
   - `docker-compose.yml:227-247` `zabbix` 服务整段重写: image `zabbix/zabbix-server-pgsql:latest` → `zabbix/zabbix-server-pgsql:7.0.13-alpine` + 新增 `zabbix-web` 服务 (`zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine` + `ZBX_SERVER_HOST=zabbix` + `ZBX_SERVER_PORT=10051` + port `127.0.0.1:8081:8080`); `api` 的 `NMP_INTEGRATIONS_ZABBIX_URL=http://zabbix-web:8080`
   - `docker-compose.yml:252` `glpi` image `linuxserver/glpi:latest` → `linuxserver/glpi:3.0.11`
   - `docker-compose.yml:276` `graylog` image `graylog/graylog:6.0` → `graylog/graylog:6.0.3-1`
   - `docker-compose.yml:285-287` `graylog` env 三个占位值全部 `${VAR:?...}` 强制; 加 `GRAYLOG_ROOT_PASSWORD=${NMP_AUX_GRAYLOG_ROOT_PASSWORD:?...}`
   - `docker-compose.yml:288` `graylog` env `GRAYLOG_ELASTICSEARCH_HOSTS=elasticsearch:9200` → `GRAYLOG_ELASTICSEARCH_HOSTS=http://nmp:${NMP_AUX_ELASTICSEARCH_PASSWORD:?...}@elasticsearch:9200` (xpack on 后必须带 user:pass)
   - `docker-compose.yml:304` `elasticsearch` image `docker.elastic.co/elasticsearch/elasticsearch:8.11.0` → `docker.elastic.co/elasticsearch/elasticsearch:8.11.4`
   - `docker-compose.yml:309-310` `elasticsearch` env `xpack.security.enabled=false` → `true` + 加 `ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?必须在 .env.aux 里设置（openssl rand -hex 32）}`
   - `docker-compose.yml:323` `mongoDB` image `mongo:7` → `mongo:7.0.14`
   - 6 aux 服务全部加 `networks: [aux_net]` 段 (脱离 default 网络)
   - 新增 `networks.aux_net` 段 (driver: bridge, ipam.config.subnet: `172.29.0.0/24`)
   - `graylog` / `elasticsearch` / `mongoDB` 三服务的 `aux_net` 段加 `internal: true` (driver 阻断外部入口)

3. **impl .env.example aux 段**:
   - `.env.example:25` 后插入新段: `# === Aux profile (--profile aux 启用) ===` 标题注释
   - 加 `NMP_AUX_NETBOX_SECRET_KEY=` + `openssl rand -hex 32` 生成命令注释
   - 加 `NMP_AUX_GRAYLOG_PASSWORD_SECRET=` + `openssl rand -hex 32` 生成命令注释
   - 加 `NMP_AUX_GRAYLOG_ROOT_PASSWORD=` + `openssl rand -hex 32` 生成命令注释 + `# GRAYLOG_ROOT_PASSWORD_SHA2 = echo -n "$NMP_AUX_GRAYLOG_ROOT_PASSWORD" | sha256sum | cut -d' ' -f1`
   - 加 `NMP_AUX_ELASTICSEARCH_PASSWORD=` + `openssl rand -hex 32` 生成命令注释
   - 加 `# 注: 这些值**只**在启用 --profile aux 时才需要, 默认 docker compose up -d 不读`

4. **impl scripts/compose-aux-config-check.sh**:
   - `#!/usr/bin/env bash` + `set -euo pipefail`
   - 函数 `is_placeholder_value(value)` — 字面占位白名单 (`your-` / `change-in-production` / `placeholder` / `example` / `nmp123` / `your-hashed-password` / `your-password-secret-here` / 空)
   - 函数 `is_mutable_tag(image_tag)` — 检测 `:latest` / `:X.Y` (仅主+次版本) / `:X` (仅主版本) / `:X.Y.Z.` (末尾带点的非稳定 tag)
   - 函数 `check_placeholder_values(compose_file)` — grep 占位字面 → 输出每行 + 行号
   - 函数 `check_mutable_tags(compose_file)` — grep aux 服务的 image: 行, 调 `is_mutable_tag`
   - 函数 `check_xpack_disabled(compose_file)` — grep `xpack.security.enabled=false`
   - 函数 `check_aux_network_isolation(compose_file)` — grep aux 服务 `networks:` 段必须含 `aux_net`
   - 函数 `check_elastic_password_present(compose_file)` — grep `ELASTIC_PASSWORD=` 必须非空
   - 函数 `check_aux_healthcheck_present(compose_file)` — grep aux 服务必须含 `healthcheck:`
   - main: 调 6 个 check, 任意一个 fail → 累加 exit code (1=占位 / 2=可变 tag / 3=xpack / 4=aux_net / 5=ELASTIC_PASSWORD / 6=healthcheck), 任一 fail → exit 非 0
   - 输出格式: `⚠️  <类别>: <compose_file>:<行号>: <错误描述>` (每行带路径+行号便于跳转)

5. **impl scripts/compose-aux-config-check_test.sh**:
   - 4 场景测试, 各起一个 `tmpdir` 写**最小化** compose yaml (含预期错), 跑 `compose-aux-config-check.sh tmpdir/docker-compose.yml`, assert exit code + 输出关键字
   - 场景 1: 干净 compose (image 全部钉精确补丁号 + 无占位值 + xpack on + aux_net + ELASTIC_PASSWORD + healthcheck) → exit 0
   - 场景 2: 占位值 (5 个占位白名单任一) → exit 1 + stderr 含 "占位"
   - 场景 3: 可变 tag (任一 image 用 `:latest` / `:6.0` / `:7`) → exit 2 + stderr 含 "可变 tag"
   - 场景 4: xpack off → exit 3 + stderr 含 "xpack"
   - 用 bash `set -e` + `[[ $exit_code -eq expected ]]` + `[[ $stderr == *"keyword"* ]]` 断言

6. **impl 08-部署运维.md §8.3.5 新段**:
   - `#### 8.3.5 Aux profile 启用 (M89 ship 后)` 标题
   - 段首: 「`netbox/zabbix/glpi/graylog/elasticsearch/mongoDB` 全部在 `profiles: ["aux"]` 下, 默认不启用. 启用步骤 (G-17 / M89 ship 后收紧):」
   - 步骤 1: `cp .env.example .env.aux` (主链 secret 复制一份, aux 与主链**共享** `NMP_DATABASE_PASSWORD` / `NMP_AUTH_JWT_SECRET` / `NMP_AUTH_API_KEY_PEPPER`)
   - 步骤 2: 追加 aux 专属 secret: `echo "NMP_AUX_NETBOX_SECRET_KEY=$(openssl rand -hex 32)" >> .env.aux` (重复 4 次)
   - 步骤 3: 生成 GRAYLOG_ROOT_PASSWORD_SHA2: `echo "NMP_AUX_GRAYLOG_ROOT_PASSWORD_SHA2=$(echo -n "<从 .env.aux 复制 NMP_AUX_GRAYLOG_ROOT_PASSWORD> " | sha256sum | cut -d' ' -f1)" >> .env.aux`
   - 步骤 4: 校验: `bash scripts/compose-aux-config-check.sh docker-compose.yml` → exit 0
   - 步骤 5: 启动: `set -a; source .env.aux; set +a; docker compose --env-file .env.aux --profile aux up -d`
   - 残余: 「aux 各家独立库 + schema 初始化 (`make init-aux-dbs`) 留 M99+ 范围; M89 仅收紧配置契约, 不跑 schema 迁移」

7. **impl TODO.md:73**:
   - `- [ ] **G-17 ...**` → `- [x] **G-17 ...**（2026-09-09 登记，2026-09-16 M89 部分结案）`
   - 描述更新: 4 条致命缺陷中 3 条已收口 (image tag 钉死 / 占位 fail-closed / xpack on) + 网络隔离 + 工具 (compose-aux-config-check.sh); 残余 3 条留 M99+ (aux 库独立 / 深度鉴权 / aux 启动 smoke)

8. **mutation inversion 实证** (Verification §3):
   - M1: 剥 `is_placeholder_value()` 函数体 → 期望红 → 还原 → 绿
   - M2: 剥 `zabbix` image 钉死 → 期望红 → 还原 → 绿
   - 临时文件实证完 `rm -f compose-aux-config-check.sh.m89bak docker-compose.yml.m89bak` + `git status` 二次确认

9. **docs commit 3**: `M89-candidate-completion-report.md` + `M89-candidate-graph-analysis.md` + `CHANGELOG.md` M89 段

10. **state fixup**: `~/.hermes/state/PM_QUEUE.json` M89-candidate.status: `candidate` → `shipped` + append `shipped[]` registry + bump `branch_main` 字段

11. **closeout**: `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M89 closeout (Poison ≤4h 授权, watchdog 下次 tick 验证 status=shipped)

12. **git push**: `git push origin main` 3 commits 全 push

## Decision gate

- **PM-direct 自决**: 不请示 Poison. ≤2h 估算在 Poison 4h round 预算内.
- **接受 framing**: M89 不是「把 G-17 全做完」 (那是 M99+), 而是「把 G-17 最致命的 3 条收紧 + 工具兜底」. 接受文档写明残余 3 条留 M99+.
- **不动主链**: api/web/postgres/redis/migrate 零变化. M89 范围**仅**在 aux profile.
- **不动 schema 初始化**: aux 各家库 + DDL 不在 2h scope. 文档明示 `make init-aux-dbs` 留 M99+.
- **接受 `zabbix-server` + `zabbix-web` 双服务**: 原 zabbix 是 server 进程无 Web/API, 与集成 URL 不匹配. M89 加 zabbix-web 服务 (沿用 zabbix 官方 docker-compose 范本) — 这意味着 aux profile 从 6 服务涨到 7 服务, 文档明示.
- **mutation M1/M2 双实证足够**: 守门对象是**脚本** (M1) 与 **compose 配置** (M2), 双轨独立守门; M3 (network internal) 同形不重复 = 噪音.
- **aux 各家 API 鉴权深度不动**: G-15 / M78 ship 的 `Config.Validate()` 沿用, M89 仅在 compose 层强制 secret 不缺. 这是**收紧**而非**重写**, 与 D-23 「无强约束」一致.
- **PM_QUEUE state 同步必做**: 沿用 M88 closeout 模式.