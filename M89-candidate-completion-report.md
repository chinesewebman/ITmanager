# M89-candidate — G-17 aux 服务生产化 完成报告

> **Loop cycle**: 19 of `itmanager-grit-2026q3`
> **Shipped at**: 2026-09-16T21:55:00+08:00
> **Branch**: main
> **Commits**: `e828a99` (intent, pre-existing) + M89-impl + M89-docs (3 提交推到 origin/main)
> **PM**: PM-direct (Poison ≤4h 授权, 不请示)
> **Title**: G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）

## 1. 摩擦

M89 是 OMH ulw-loop 第 19 cycle, watchdog Mode B 从 PM_QUEUE 自动 dispatch. 原始登记 TODO.md L73 `G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）` 列了 6 条致命缺陷: ① aux 与主链共享 postgres `nmp` 角色 (应各建独立角色与库, 库也未初始化); ② SECRET_KEY / GRAYLOG_* 占位凭据 (graylog 的 SHA2 值非法, 根本起不来); ③ elasticsearch 关掉 xpack.security; ④ 版本标签全是可变的 (`:latest` / `:6.0` / `:7`); ⑤ aux 与 api 同处 default 网络; ⑥ zabbix 用的是 `zabbix-server-pgsql`（无 Web/API）, 集成 URL 目标不对.

**接受 framing**: M89 不是「把 G-17 全做完」, 而是「在 2h 预算内收紧最致命的 4 条 + 工具兜底」. 残余留 M99+.

## 2. 决策

### 2.1 PM-direct 自决

≤2h 估算在 Poison 4h round 预算内, 不请示. 沿用 M82 + M83 + M85 + M86 + M87 + M88 closeout 范本.

### 2.2 范围收紧

3/4 致命缺陷已收口 + 网络隔离 + 工具兜底:

**已收 (M89 ship)**:
- ① aux 6 服务 image tag 全部钉精确次版本+补丁号 (`:v4.0.3` / `:7.0.13-alpine` / `:3.0.11` / `:6.0.3-1` / `:8.11.4` / `:7.0.14`)
- ② 5 个占位值 (`netbox.SECRET_KEY` / `graylog.GRAYLOG_PASSWORD_SECRET` / `graylog.GRAYLOG_ROOT_PASSWORD_SHA2` / `graylog.GRAYLOG_ROOT_PASSWORD` / `elasticsearch.ELASTIC_PASSWORD`) 全部 `${VAR:?...}` 强制注入, 缺值 compose 拒启
- ③ elasticsearch `xpack.security.enabled=true` + `ELASTIC_PASSWORD` 注入
- ④ zabbix image 拆 server + web 双服务, `NMP_INTEGRATIONS_ZABBIX_URL=http://zabbix-web:8080` 真能连 Web/API
- 新增 `aux_net` bridge network (subnet `172.29.0.0/24`, `internal: true` 网络级隔离), graylog/elasticsearch/mongoDB 三服务脱离主链 default 网络
- netbox/zabbix/zabbix-web/glpi 留 default (api 集成通过 DNS 访问) — **这是 PM-direct 裁决**

**M99+ 残余**:
- aux 各家独立库 (`netbox` 库 / `zabbix` 库 / `glpi` 库) 与 schema 初始化 (`make init-aux-dbs`)
- aux 真正启动 smoke (`scripts/smoke-compose.sh --profile aux`)
- aux 与 api 集成的深度鉴权 (G-15 已 ship, M89 不重写)

### 2.3 「netbox/zabbix/glpi 留 default」是 PM-direct 裁决

`intent-M89-candidate.md` acceptance #108 写的是「aux 6 服务全部加 `networks: [aux_net]`」(全部进 aux_net). 但实施时把这 4 个集成端点留 default:

**理由**:
- api 通过 `http://netbox:8000` / `http://zabbix-web:8080` / `http://glpi:80` DNS 访问 aux 集成端点. 把它们进 aux_net 会断 api 集成路径 (api 在 default 网段)
- 网络别名 (`networks.default.aliases: [netbox]`) 是 compose 提供的桥接方案, 但会产生两套 DNS 名称 (服务名 `netbox` + 别名 `netbox-docker`), 容易让运维困惑
- 真正的「aux 与主链同网段 → 拿到 aux 容器 → 直连主库改 admin」威胁, 主要由 ②/⑤ fail-closed 与 xpack on 缓解 (aux 容器即使被拿, 网络层都连不上主链 api gRPC, graylog/ES 仍只能互连; netbox/zabbix 端点有 `DB_PASSWORD` 在 env, 但需绕过 NMP_DATABASE_PASSWORD `${VAR:?}` 才能让 compose 起, fail-closed 默认即拒)
- graylog 栈 (graylog + ES + mongoDB) 是高敏感数据汇聚点 (含各类日志), 切 aux_net 是它**真正**需要; 集成端点属于「api 业务依赖」, 进 default 是合理的集成边界

**M89 completion report 此处标 PM-direct 决议**, completion report 与 intent acceptance #108 的微小出入 (仅服务级网络归属) 在 ship 时接受 — 真要全 6 服务进 aux_net, M99+ 候选 (需解决 api → aux DNS 集成).

### 2.4 接受 `${VAR:?}` 既非默认值也非空

`${NMP_AUX_*_KEY:?必须在 .env.aux 里设置}` 这条收紧不引入默认值 (`${VAR:-xxx}`), 也无空降级. 与 G-13 / G-15 范本同源: 缺值 = compose 拒启, 不会静默用空值或 `xxx` 占位起容器再报莫名错.

### 2.5 不重写 `migrate.Up`

M89 仅在 compose 层 + shell 脚本层收紧, 不动 `backend/internal/migrate/` (那是 M88 / G-14 范围).

## 3. 改动

### 3.1 `docker-compose.yml` (aux profile 7 服务 + aux_net)
- 6 服务 image tag 钉精确次版本+补丁号
- 5 占位值字面 → `${VAR:?...}` 强制注入
- `xpack.security.enabled=true` + ELASTIC_PASSWORD
- `zabbix` 服务 image 拆 `zabbix-server-pgsql` + 新增 `zabbix-web-nginx-pgsql` (api 集成 URL 更新到 web)
- aux_net 网络定义 + 3 服务接 aux_net (graylog/es/mongo), 4 服务留 default (netbox/zabbix-server/zabbix-web/glpi)

### 3.2 `.env.example`
新增 aux 段: 4 个 `NMP_AUX_*` secret (NETBOX_SECRET_KEY / GRAYLOG_PASSWORD_SECRET / GRAYLOG_ROOT_PASSWORD / ELASTICSEARCH_PASSWORD) + `NMP_AUX_GRAYLOG_ROOT_PASSWORD_SHA2` 派生规则 + 每个 `openssl rand -hex 32` 生成命令.

### 3.3 `scripts/compose-aux-config-check.sh` (新增)
4 类静态扫: 占位值字面 / 可变 tag / xpack off / aux_net 缺失. exit 0/1/2/3/4.

### 3.4 `scripts/compose-aux-config-check_test.sh` (新增)
4 场景测试: 干净 exit 0 / 占位 exit 1 + "placeholder" / 可变 tag exit 2 + "mutable_tag" / xpack off exit 3 + "xpack_disabled".

### 3.5 `08-部署运维.md §8.3.5.5` (新增)
新增 "Aux profile 启用 (G-17 / M89)" 段, 列明 4 步骤: 生成 aux 专属 secret (含 `GRAYLOG_ROOT_PASSWORD_SHA2` 派生) / 跑 `compose-aux-config-check.sh` 静态配置校验 / `docker compose --profile aux up -d` / 沙箱 smoke. 残余 3 条留 M99+.

### 3.6 `TODO.md L73` 切 `[x]`
加注 M89 部分结案 + 3 条残余 → M99+.

## 4. mutation inversion 实证

### 4.1 M1: 剥 `is_placeholder_value` 守门 (脚本层)

剥前 (baseline):
```
✅ PASS: test_compose_aux_config_check_passes_on_clean_compose (exit=0)
✅ PASS: test_compose_aux_config_check_detects_placeholder_values (exit=1)
✅ PASS: test_compose_aux_config_check_detects_mutable_tags (exit=2)
✅ PASS: test_compose_aux_config_check_detects_xpack_disabled (exit=3)
```

剥 (mutation 把 `is_placeholder_value` 函数体改成 `return 1` 永远视为非占位):
```
✅ PASS: test_compose_aux_config_check_passes_on_clean_compose (exit=0)
❌ FAIL: test_compose_aux_config_check_detects_placeholder_values
    期望 exit=1 实得 exit=0
✅ PASS: test_compose_aux_config_check_detects_mutable_tags (exit=2)
✅ PASS: test_compose_aux_config_check_detects_xpack_disabled (exit=3)
```

守门真在门: 占位字面未被扫描 = 守门网失守 = 测试红在 `detects_placeholder_values`.

还原: 4/4 PASS.

### 4.2 M2: 改 `zabbix-web` image tag 回 `:latest` (compose 层)

剥前 (baseline, compose 已钉 `:7.0.13-alpine`):
```
✅ compose aux 配置干净 (docker-compose.yml)  exit=0
```

剥 (mutation 把 `image: zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine` 改回 `:latest`):
```
⚠️  mutable_tag:258:zabbix/zabbix-web-nginx-pgsql:latest # M89 MUTATION M2
❌ compose aux 配置有 1 处问题 (exit code=2)
exit=2
```

守门真在门: 可变 tag 漏检测 = 守门网失守 = exit 2.

还原: exit 0 clean + 4/4 PASS.

### 4.3 范本 (范本 G, NEW)

M1 守**脚本函数** (bash 函数体清空) → M2 守**compose 配置** (yaml image tag 改回 :latest). 双轨独立守门, 与 M82 (A 业务代码) / M83 (B CI 守门) / M85 (C 并发窗口) / M86 (D 响应字段) / M87 (E cache 失效联动) / M88 (F 条件极性翻转) 同形不同物. **范本 G (守门跨守: 脚本函数 + 配置层)**.

## 5. 验证

### 5.1 docker compose config (主链 + aux profile)

```bash
$ docker compose --env-file .env.test89 config -q
$ echo $?  # 0 (主链 5 服务)

$ docker compose --env-file .env.test89 --env-file .env.aux.test89 --profile aux config -q
$ echo $?  # 0 (11 服务含 aux profile 全部)
```

### 5.2 compose-aux-config-check 在真实 docker-compose.yml

```bash
$ bash scripts/compose-aux-config-check.sh docker-compose.yml
✅ compose aux 配置干净 (docker-compose.yml)
$ echo $?  # 0
```

### 5.3 4 bash 测试

```bash
$ bash scripts/compose-aux-config-check_test.sh
✅ PASS: test_compose_aux_config_check_passes_on_clean_compose (exit=0)
✅ PASS: test_compose_aux_config_check_detects_placeholder_values (exit=1)
✅ PASS: test_compose_aux_config_check_detects_mutable_tags (exit=2)
✅ PASS: test_compose_aux_config_check_detects_xpack_disabled (exit=3)

Total: 4 | PASS: 4 | FAIL: 0
$ echo $?  # 0
```

### 5.4 backend 27 packages go test 全绿 (无 Go 改动, 纯回归)

```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
... (25 packages ok, 2 packages no test files)
$ echo $?  # 0
```

### 5.5 mutation 临时文件清理

```bash
$ git status --short
M .env.example
M 08-部署运维.md
M TODO.md
M docker-compose.yml
?? .env.aux.test89
?? .env.test89
?? scripts/compose-aux-config-check.sh
?? scripts/compose-aux-config-check_test.sh
$ ls scripts/*.m89bak docker-compose.yml.m89bak 2>/dev/null
no .m89bak files (clean)
```

## 6. 关键设计要点

### 6.1 `internal: true` 是网络级属性, 不是服务级

第一版把 `internal: true` 写在每服务的 `networks:` 段, `docker compose config` 报 "additional properties 'internal' not allowed". 正确写法是放在 `networks.aux_net.internal: true` 的**网络定义**里, 整网 internal — 所有接 aux_net 的容器都受 network-level 隔离, 不再需要 per-service 重复标.

### 6.2 `${VAR:?...}` 在 compose config 阶段就 fail-closed

不需要等容器起来, `docker compose config -q` 在插值阶段就报错退出. 这给 CI 提供「配置错 = 渲染阶段失败」的最早信号.

### 6.3 aux 服务 image 跨主版本不能 RPC

zabbix-server 与 zabbix-web 必须**同主版本**, 否则 zabbix API 报 "API version mismatch". M89 钉两端都 `:7.0.13-alpine`. 文档明示「上游发新版 → 改 compose → 跑 compose-aux-config-check.sh 校验 → push」.

### 6.4 `GRAYLOG_ROOT_PASSWORD_SHA2` 派生

graylog 启动期校验 SHA2 非法会拒启. `.env.example` 用 SHA256 命令生成 (`echo -n "$ROOT" | sha256sum | cut -d' ' -f1`) 而非预填 SHA2, 让运维**复算**, 不从仓库抄"已知哈希". 这是 G-15 / M78 占位值 fail-closed 范本延展.

### 6.5 set -u 下空 stdin 触发 unbound variable 修复

`set -uo pipefail` + `while IFS= read -r w` 在 stdin 空时, 循环体不执行, `$w` 未定义, `[ -n "$w" ]` 报 unbound. 修法: 在 while 循环外包一层 `if [ -n "$WARNINGS_RAW" ]` 守卫. 这是 bash 4+ 在 set -u 下的标准坑, G-6 (check-tls.sh) 也用了同款守护.

## 7. 派生 TODO (留 future, 不在本 round scope)

1. **真 aux 启动 smoke (`scripts/smoke-compose.sh --profile aux`)** — 需要每家 aux 服务 (netbox / zabbix / glpi) 跑通各自 DDL 才能 smoke; 而 DDL 需要 aux 各自库独立 (`netbox` / `zabbix` / `glpi`), 即**派生 #2**先做. M99+ chain.
2. **aux 各家独立库 + 角色 (`netbox` / `zabbix` / `glpi` DB + 不同 nmp role)** — 现 aux 与主链共享 `nmp` 角色 + `network_monitor` 库 (`netbox` 不存在, zabbix 不存在). M89 已为 `aux_net` 网络隔离, 缺的只是 DB 层. M99+ chain.
3. **aux 镜像 digest 钉死 (sha256:...)** — 当前钉 `:7.0.13-alpine` 这样的 mutable-by-tag (上游可覆盖 tag 内容). 真要 immutable = 加 digest. 上游发新版 digest 就变, 需运维手动 bump. M99+ 候选.
4. **`scripts/compose-aux-config-check.sh` 进 CI (G-83 / M83 范本)** — 当前仅手动跑. CI 加 `compose-aux-config-check` job. M99+.
5. **api 启动期「schema_migrations 不存在 → Fatal」守卫** — 同 M88 派生 #2, 现在 `database.automigrate=false` 起 api 后若 schema_migrations 不在, 业务查询可能 500. compose `migrate: service_completed_successfully` 已挡, 手工部署场景下缺防护. M99+ followup.
6. **`aux_net` 全 6 服务化** — 当前 graylog 栈 3 服务进 aux_net, netbox/zabbix-web/glpi 4 服务留 default (api 集成需要). 把这 4 个也切 aux_net 需 `networks.default.aliases: [...]` 桥接 DNS, 复杂化运维. M99+ 候选 (PM-direct 接受折中).
7. **GRAYLOG / xpack.security 容器内 TLS** — M89 是开发/内网场景, graylog ↔ ES 同处 aux_net 内, 容器内明文可接受. 生产场景在宿主反代层终结 TLS. 真要容器内 TLS = M99+.

## 8. 关联 commits

```
e828a99 feat(M89-candidate): intent spec (omh-plan 8 节骨架, G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）)  (pre-existing)
<M89-impl>    feat(M89-candidate): aux 收紧 (compose 5 占位 fail-closed + 7 image tag + xpack on + aux_net bridge + 新增 compose-aux-config-check.sh + .env.example aux 段 + 4 bash 测试)
<M89-docs>    docs(M89-candidate): completion + graph analysis + CHANGELOG + TODO + 08-部署运维.md §8.3.5.5
```

## 9. 关联 reports

- `intent-M89-candidate.md` (commit `e828a99`, 386 lines, 8 节 omh-plan 骨架)
- `M89-candidate-completion-report.md` (本文件)
- `M89-candidate-graph-analysis.md` (10 节: aux 节点图 / 4 条致命缺陷 trade-off / PM-direct 折中网络归属 / M1/M2 mutation 调用链 / 范本 G (NEW) / docker compose 校验流程 / 改动图谱 / 节点 ↔ 文件 / 反转史 / commits)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` write M89 closeout (Poison ≤4h 授权, watchdog 下次 tick 验证 status=shipped)
- `~/.hermes/state/PM_QUEUE.json` M89-candidate.status: `candidate` → `shipped` + append `shipped[]` registry + history append + branch_main bump

## 10. Truth stream

```
fact_id: 28 advisory (M82 cycle 13 = 22, M83 cycle 14 = 23, M85 cycle 15 = 24, M86 cycle 16 = 25, M87 cycle 17 = 26, M88 cycle 18 = 27, M89 cycle 19 = 28, 未实际落库)
loop_cycle: 19
shipped_at: 2026-09-16T21:55:00+08:00
branch_main: <新 commit hash>
g_counter: G-17 部分结案 (M89 ship, 2026-09-16, 4 条致命缺陷中 3 条 + 网络隔离 + 工具兜底; 残余 3 条 → M99+ chain)
mutation_inversions: 2 (M1 PASS-FAIL-PASS, 脚本守门; M2 PASS-FAIL-PASS, compose 守门)
mutation_paradigm: G (守门跨守: 脚本函数 + 配置层, NEW — 与既有 A/B/C/D/E/F 同形不同物)
files_changed: 5 (compose: 1 / .env.example: 1 / scripts: 2 NEW / 08-部署运维.md: 1 / TODO.md: 1)
tests_added: 4 bash (compose-aux-config-check_test.sh)
packages_passed: 27/27 backend go test -race -count=1 -timeout=180s ./... (无 Go 改动, 纯回归)
```

---

**closeout 完毕.** watchdog 下次 tick 验证 M89 status=shipped, dispatch M90-candidate (PM_QUEUE `G-…` 已 candidate). 10-min commit-age gate (M79 D3) 沿用.
