---
id: INTENT-M39-trusted-proxy
title: G-7 gin 受信代理收口 — 客户端 IP 不再被 XFF 攻击者任意伪造
status: draft
author: hermes@local (PM)
created: 2026-09-13
outcomes:
  - 空配置（直连部署）下 `ClientIP()` 返回直连对端，**忽略 XFF**，攻击者无法伪造客户端 IP
  - 配置受信代理后，`ClientIP()` 取 XFF 中**最右不可信跳**（不是最左、不是代理本身），符合 RFC 7239 安全语义
  - 四个下游消费者（登录限流桶键 / 审计行 IP / `last_login_ip` / API Key IP 白名单）全部改读真实客户端 IP，限流不再可被无限 XFF 绕过、白名单不再可被 XFF 绕过
  - 非法/过宽 CIDR 条目启动即 fail-fast，运维误配整段 `10.0.0.0/8` 不会被放过
  - 环境变量 `NMP_SERVER_TRUSTED_PROXIES` 能覆盖 yaml 的 `[]`（生产反代容器 IP 可不改 yaml 即生效）
acceptance:
  - id: AC-M39-1
    given: 空 `server.trusted_proxies: []` 配置（直连部署形态）
    when: 同一 `RemoteAddr=203.0.113.9` 客户端第 6 次发请求，每次 `XFF` 都不一样（`1.2.3.4`、`5.6.7.8`、`…`）
    then: 第 6 次返回 429（限流桶键是真实 IP，不被 XFF 绕过）
  - id: AC-M39-2
    given: `server.trusted_proxies: [172.28.0.10/32]`（一个 nginx 反代容器）
    when: 反代转发请求 `RemoteAddr=172.28.0.10` + `XFF="1.2.3.4, 10.0.0.9"`
    then: 审计行 `audit_logs.ip = 10.0.0.9`（最右不可信跳，不是最左、不是代理本身 172.28.0.10）
  - id: AC-M39-3
    given: `config.yaml` 写入非法 CIDR `not-a-cidr` 或 `300.1.1.1`
    when: `config.Load()` + `config.Validate()`
    then: 返回非空 error，进程启动失败（fail-fast，**不会**带错误配置起来）
  - id: AC-M39-4
    given: 配置 `trusted_proxies: ["172.28.0.10"]`（裸 IP，等价 `/32`）
    when: `config.Validate()`
    then: 通过，gin 侧 `SetTrustedProxies` 不会因 `/32` 报错
  - id: AC-M39-5
    given: 配置 `trusted_proxies: ["0.0.0.0/1", "128.0.0.0/1"]`（v4 全覆盖）
    when: `config.Validate()`
    then: 报错（实测这两条即可覆盖全部 v4，必须硬拒；只拒 `/0` 不够）
  - id: AC-M39-6
    given: 配置 `trusted_proxies: ["172.28.0.10"]`
    when: 反代转发请求 `RemoteAddr=172.28.0.10` + `XFF="1.2.3.4"`
    and: API Key 白名单只配 `["1.2.3.4"]`
    then: API Key 鉴权通过（真实客户端 IP = `1.2.3.4`，不在白名单里的攻击者伪造 XFF 会被拒）
  - id: AC-M39-7
    given: `config.yaml` 显式 `server.trusted_proxies: []`（占位）
    and: 环境变量 `NMP_SERVER_TRUSTED_PROXIES=172.28.0.10,10.0.1.5`
    when: `viper.Load()` 后读 `cfg.Server.TrustedProxies`
    then: `= ["172.28.0.10", "10.0.1.5"]`（env 覆盖 yaml；前提是 yaml key 存在）
  - id: AC-M39-8
    given: 配置 `trusted_proxies: ["172.28.0.10/24"]`（**主机位非零**，本意 `172.28.0.10` 单机）
    when: `config.Validate()`
    then: 报错（`net.ParseCIDR` 会静默归一成 `172.28.0.0/24`，本意单机却信任整个网段，必须硬拒）
  - id: AC-M39-9
    given: 配置 `trusted_proxies: ["::ffff:0:0/96"]`（IPv4-mapped IPv6，等效 v4 `/0`）
    when: `config.Validate()`
    then: 报错（按等效 v4 前缀 `ones-96` 判宽，不能用 v6 `/16` 规则放过）
  - id: AC-M39-10
    given: 启动后首次收到带 XFF 但 `trusted_proxies` 为空的请求
    when: gin engine 收到该请求
    then: 一次性运行期 WARN 日志输出（不依赖运维看启动日志，行为触发）
edges:
  - 直连部署（空配置）下 `RemoteAddr` 就是真实客户端 IP，限流/审计按 `RemoteAddr` 工作，**完全正确**——这是空配置的默认形态，不应被 fail-fast 误杀
  - `fe80::/10`（IPv6 link-local）作为「代理本身」太宽，v6 `</16` 规则照拒；但 `127.0.0.0/8`、`10/8`、`172.16/12`、`192.168/16`、`169.254/16` 等合法但「过宽」段**只 WARN 不拒**（v4 `</8` 才拒）
  - `cmd/server/main.go` 用 `srv.ListenAndServe()` 而非 `engine.Run()`，gin 自带的「信任全部代理」告警（`gin.go:379-382`，只在 `Run()` 内）根本不会打印——本轮新加的运行期 WARN 弥补这个盲点
  - 既有测试预期不受影响（已核 `rate_limit_test.go:39`、`routes_integration_test.go:625` 都用 `req.RemoteAddr` 而非 XFF）；新增用例显式断言两个方向（V-1/V-2/V-8）
  - viper 对 slice 的 env 覆盖在 key **缺失于 yaml** 时不生效（`Unmarshal` 走 `AllKeys`，纯 env 键不在其中），故 `backend/config.yaml` 必须显式保留 `trusted_proxies: []` 占位
not_goals:
  - 不引入 Redis 后端限流（`rate_limit.go` 注释里的升级路径是独立任务）
  - 不改 `RemoteIPHeaders` 默认值（`X-Forwarded-For` → `X-Real-IP`）；本仓库 nginx 用 `$proxy_add_x_forwarded_for` 追加真实 `$remote_addr`，gin「XFF 含非法项则 break 并回退 X-Real-IP」路径在当前拓扑不可达
  - 不修 compose 的其它缺陷（G-9 Dockerfile、G-10 env 变量名/upstream 名）——只加 G-7 需要的两处（ipam 固定子网 + `trusted_proxies: [172.28.0.10]`）
  - 不加 `server.behind_proxy` 开关（见 FIX-PLAN §5 R-1 被否的替代方案：空配置对直连部署是正确且安全的，fail-fast 会误杀合法部署）
  - 不动 `audit.ErrorMsg`（全仓无 setter 的死路径，按约定「提一句，别删」）
  - 不动 `audit.Method` 净化（gin 的 method 来自 `http.MethodXxx` 白名单式解析，非法 method 在协议层被拒；实施时用一条用例确认，不靠推理）
evidence:
  - "FIX-PLAN-TRUSTED-PROXY.md §1 实测：默认 gin + RemoteAddr=203.0.113.9 + XFF=1.2.3.4,5.6.7.8 → ClientIP()=1.2.3.4，攻击者完全可控"
  - "FIX-PLAN-TRUSTED-PROXY.md §1 表：四个下游消费者均吃 ClientIP() 值，其中第 4 个是 API Key IP 白名单（安全控制）"
  - "TODO.md G-7 状态：未实现（draft）；安全审计 F1① 已实测「5 req/min 形同虚设」"
  - "FIX-PLAN §2 D-A 配置项 schema；§2 D-B 启动期校验规则；§3 Where 文件清单（9 处）"
---

# M39 — G-7 gin 受信代理收口

## Context

`backend/internal/api/routes.go:118` 用 `gin.Default()` 建引擎，**从未调用 `SetTrustedProxies`**。
gin v1.9.1 的默认受信表是「全部来源」（`defaultTrustedCIDRs` 含 `0.0.0.0/0`、`::/0`），
于是 `Context.ClientIP()` 取 `X-Forwarded-For` **最左值**——所有 IP 都「可信」，等于**攻击者完全可控**。
`cmd/server/main.go:104` 用 `srv.ListenAndServe()` 而非 `engine.Run()`，gin 自带的告警根本不会打印，问题长期无人察觉。

四个下游消费者（登录限流桶键 / 审计行 IP / `last_login_ip` / API Key IP 白名单）均吃这个值，
其中**最后一个是安全控制**：持有效 API Key 者伪造 XFF 即可**从任意 IP 使用 Key**，白名单形同虚设。

M39 按 `docs/FIX-PLAN-TRUSTED-PROXY.md` 修订版收口：新增 `ServerConfig.TrustedProxies` + 启动期 fail-fast
校验 + `SetTrustedProxies` 调用 + 一次性运行期 WARN + compose 默认值 + 文档同步。

## Outcomes (5)

详见 frontmatter `outcomes[]`，对应：

- O-1：空配置下 `ClientIP()` 不被 XFF 绕过（直连部署安全默认）
- O-2：配置受信代理后取**最右不可信跳**（RFC 7239 标准语义）
- O-3：四个下游消费者全改读真实客户端 IP（限流、审计、API Key 白名单不再可被绕过）
- O-4：非法/过宽条目 fail-fast（防运维误配）
- O-5：env 覆盖 yaml 生效（生产反代容器 IP 可不改 yaml）

## Acceptance Criteria (10)

详见 frontmatter `acceptance[]`，AC-M39-1 到 AC-M39-10。

## Edge cases (5)

详见 frontmatter `edges[]`：

- 直连部署是合法且安全的（不被 fail-fast 误杀）
- IPv6 link-local 太宽被拒、v4 RFC 1918 段只 WARN 不拒
- gin 自带告警因 `ListenAndServe()` 而不打印；本轮加运行期 WARN 弥补
- 既有测试预期不受影响（已核两处 `RemoteAddr` 用法）
- viper env 覆盖 yaml 缺 key 时静默失效，必须 yaml 保留占位

## Operational constraints

- **必动文件**（来自 FIX-PLAN §3）：`backend/internal/config/config.go`、`backend/config.yaml`、
  `backend/internal/api/routes.go`、`backend/internal/middleware/`（新文件）、
  `backend/internal/config/config_test.go`、`backend/internal/api/routes_integration_test.go`、
  `docker-compose.yml`、`08-部署运维.md`、`TODO.md`
- **不动**：`audit.ErrorMsg`（死路径）、`audit.Method`（协议层拒）、`rate_limit.go`（不引入 Redis）、
  `RemoteIPHeaders` 默认值（XFF → X-Real-IP）
- **不引入新依赖**

## Evidence

详见 frontmatter `evidence[]`，4 条锚点全部已验证（含安全审计 subagent 实测）。

## Round plan (≤4h, PM-direct)

按 FIX-PLAN §3 + §4 拆分 6 小步，每步独立可验证（per-commit push）：

1. `feat(M39): config.ServerConfig.TrustedProxies + Validate() 7 条规则`
2. `feat(M39): config.yaml trusted_proxies: [] 占位 + compose 子网固定`
3. `feat(M39): routes.go SetTrustedProxies fail-closed + 运行期 WARN`
4. `test(M39): config_test.go 7 条用例 (合法/非法/过宽/裸 IP/IPv6-mapped/主机位/env 覆盖)`
5. `test(M39): routes_integration_test.go 3 条用例 (XFF 忽略 / XFF 最右不可信跳 / API Key 白名单)`
6. `docs(M39): 08-部署运维.md 章节 + TODO.md G-7 结案 + CHANGELOG + 报告`

每步 gate（per task-completion-protocol）：
- `cd backend && go build ./...` (exit 0)
- `cd backend && go vet ./...` (exit 0)
- `/home/webman/.local/share/mise/installs/go/1.25.14/bin/gofmt -l` on changed files (empty)
- `cd backend && go test -count=1 ./internal/config/... ./internal/api/...` (green for touched packages)
- `git -c user.email=hermes@local -c user.name=hermes commit` + `git push origin main`

最终 gate：
- `cd backend && go test -count=1 ./...` (26 packages green)
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` (43 cases green, 0 regression)
- **Mutation inversion V-9**：删掉 `SetTrustedProxies` 那行 → AC-M39-1 (V-1) 必须红
- Append M39 section to CHANGELOG.md
- Write `/home/webman/Projects/ITmanager/M39-completion-report.md` (5-section)
