# 修复方案：gin 受信代理收口（TODO G-7）

> 状态 **draft（已过安全审查，修订版）** · 日期 2026-09-09 · 来源 `TODO.md` G-7（安全审计 F1①）
> 分支 `main`（用户已授权直推主干）
> 审查记录：安全审计员（独立 subagent）实测 gin v1.9.1 + viper v1.18.2 行为后提出 5 组意见，
> 本文已按「阻断项全改、建议项择改」修订，处置见 §7。

## 1. 问题（What / Why）

`backend/internal/api/routes.go:118` 用 `gin.Default()` 建引擎，**从未调用 `SetTrustedProxies`**。
gin v1.9.1 的默认受信表是「全部来源」（`gin.go:33-46` 的 `defaultTrustedCIDRs` 含 `0.0.0.0/0`、`::/0`，
在 `gin.go:206` 赋给 engine），于是 `Context.ClientIP()`（`gin.go:validateHeader`）会取
`X-Forwarded-For` 里**最右的不可信 IP**——所有 IP 都「可信」，等于直接返回最左值，即**攻击者完全可控**。
（安全审查实测：默认 gin + `RemoteAddr=203.0.113.9` + `XFF="1.2.3.4, 5.6.7.8"` → `ClientIP()=1.2.3.4`。）

四个下游消费者都吃这个值，其中**第 4 个是安全控制**：

| 消费者 | 位置 | 后果 |
| --- | --- | --- |
| 登录限流桶键 | `middleware/rate_limit.go:37` `ClientIP()+"|"+FullPath()` | 每请求换一个 XFF 即得无限额度（F1① 实测：5 req/min 形同虚设） |
| 审计行 IP | `middleware/audit.go:98` | 取证字段变成攻击者可控，审计价值归零 |
| `last_login_ip` | `handlers/auth_handler.go:122` | 同上 |
| **API Key IP 白名单** | `middleware/auth.go:148-161` | 修前：持有效 Key 者伪造 XFF 即可**从任意 IP 使用**，白名单形同虚设 |

`cmd/server/main.go:104` 用 `srv.ListenAndServe()` 而非 `engine.Run()`，
**gin 自带的「你信任了所有代理」告警（`gin.go:379-382`，只在 `Run()` 内）根本不会打印**——问题长期无人发现。

**约束**：不能简单 `SetTrustedProxies(nil)`。`docker-compose.yml` 里前端（`nmp-web`，nginx 反代 `/api/`）
与后端（`nmp-api`）是**不同容器**，后端看到的直连对端永远是 nginx 容器 IP。
信任全关 → 全站共用一个限流桶（一个攻击者可耗尽所有人的登录配额）+ 审计 IP 全变成代理地址
+ **配了 IP 白名单的 API Key 一律 403**（安全审查发现的连带破坏，见 R-1）。

## 2. 方案（How）

### D-A 配置项：`server.trusted_proxies`

```go
type ServerConfig struct {
	Host           string   `mapstructure:"host"`
	Port           int      `mapstructure:"port"`
	Mode           string   `mapstructure:"mode"`
	MetricsEnabled bool     `mapstructure:"metrics_enabled"`
	TrustedProxies []string `mapstructure:"trusted_proxies"` // 新增
}
```

- 取值：CIDR（`172.28.0.10/32`）或裸 IP（`172.28.0.10`，gin 自动补 `/32`、`/128`，`gin.go:397-409`）。
- **默认空 = 不信任任何来源**（安全默认）。空不是「没配」，而是明确的「我直连，不吃 XFF」——
  直连部署下 `RemoteAddr` 就是真实客户端，空配置**完全正确**。
- 环境变量覆盖：`NMP_SERVER_TRUSTED_PROXIES=172.28.0.10,10.0.1.5`（逗号分隔）。
  依赖 viper 的 `AutomaticEnv` + `StringToSliceHookFunc(",")`；
  **前提是 key 出现在 yaml 里**（viper 的 `Unmarshal` 走 `AllKeys`，纯 env 键不进）——
  故 `backend/config.yaml` 必须显式写 `trusted_proxies: []`（带注释）。此前提由单测钉死（V-6）。

### D-B 启动期校验：非法/过宽条目 fail-fast

`Config.Validate()`（`config.go:174`）新增一段，逐项解析并拒绝：

| 规则 | 理由 |
| --- | --- |
| 裸 IP 或合法 CIDR，否则报错 | 与 gin 的解析规则一致（v4→`/32`、v6→`/128`），避免启动放过、运行期 `SetTrustedProxies` 报错 |
| 条目先 `TrimSpace` 并**写回 cfg** | 「校验所见 = gin 所见」。否则 `" 172.28.0.10"` 校验通过、gin 报错，fail-closed 会把整张表清空 |
| v4 前缀 < `/8`、v6 前缀 < `/16` 报错 | 代理身份不可能是整个 `0.0.0.0/1`+`128.0.0.0/1`（实测这两条即可覆盖全部 v4，绕过「只拒 /0」的护栏） |
| IPv4-mapped IPv6 按**等效 v4 前缀**（`ones-96`）判宽 | `::ffff:0:0/96` 等效 v4 `/0`，只按 v6 规则量会被放过，gin 侧却是信任全部 IPv4（实测绕过） |
| 主机位非零报错（如 `172.28.0.10/24`） | `ParseCIDR` 会静默归一成 `172.28.0.0/24`：本意单机却信任整个网段 |
| 不拒 `127.0.0.0/8`、`10/8`、`172.16/12`、`192.168/16`、`169.254/16` | 会误伤 k8s sidecar（127.0.0.1）、docker 默认网段、k8s pod CIDR（`fe80::/10` 不在此列：作为「代理本身」太宽，v6 </16 规则照拒） |

> 过宽前缀只做 **WARN** 吗？不。`/1` 覆盖全网是可实测的绕过路径，必须硬拒；
> 而 `10.0.0.0/8` 这类合法但过宽的段由文档（D-E）提醒「信任的是代理本身，不是网段」。

### D-C 生效点：`SetupRouter`

```go
r := gin.Default()
// 受信代理必须显式配置：gin 默认信任 0.0.0.0/0，会让 ClientIP() 取 XFF 最左值——
// 登录限流可被逐请求换 XFF 绕过、审计 IP 可伪造、API Key IP 白名单可绕过
//（安全审计 F1①，TODO G-7）。
if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
	// fail-closed：gin 出错时可能已把**部分** CIDR 写进 engine（gin.go:438-441），
	// 继续跑等于装了半截信任表。降级为「不信任任何来源」并大声报错。
	slog.Error("SetTrustedProxies 失败，已降级为不信任任何来源", "err", err)
	_ = r.SetTrustedProxies(nil)
}
if len(cfg.Server.TrustedProxies) == 0 {
	slog.Warn("server.trusted_proxies 未配置：忽略 X-Forwarded-For，ClientIP() 取直连对端。" +
		"若部署在反向代理后，登录限流会退化为全站单桶（一个攻击者可耗尽所有人的配额）、" +
		"审计 IP 变成代理地址、配了 IP 白名单的 API Key 将一律 403")
}
```

- 空切片 / nil → gin `parseTrustedProxies` 得到空 CIDR 表 → `isTrustedProxy` 恒 false → `ClientIP() == RemoteAddr`。
- 非空 → 取 XFF 最右不可信跳（gin `validateHeader` 自右向左跳过受信代理）。
- 告警用 `slog`（与 `middleware/audit.go` 一致；`internal/api` 目前无日志调用，新增一处）。

**运行期兜底告警**（实现时按审计意见修订）：挂一个极小的中间件，只要请求带
`X-Forwarded-For` 而**直连对端不在受信表内**就打一条 WARN（进程内仅一次）——
「配置漏了」在日志里留下痕迹，而不是只表现为「Key 突然全 403」。理由：启动告警是静态的，
运维未必会看；这条是行为触发。

> 为什么按「对端是否在表内」而不是「表是否为空」：**配了但配错比不配更危险**——
> 非空配置连启动期 WARN 都不会有，故障完全静默（填错 IP、多跳漏配、代理换 IP、
> 宿主机反代直连容器）。详见 §8。

### D-D 部署配套：compose 固定子网 + 只信任前端容器

```yaml
# docker-compose.yml
services:
  api:
    environment:
      - NMP_SERVER_TRUSTED_PROXIES=172.28.0.10   # 仅信任 nmp-web（G-7）
  web:
    networks:
      default:
        ipv4_address: 172.28.0.10
networks:
  default:
    ipam:
      config:
        - subnet: 172.28.0.0/24
```

- **只信任那一个容器 IP**，不信任整个子网：同网段还有 postgres / redis / netbox 等，
  一旦其中任一被攻陷，子网级信任会立刻变成「XFF 可伪造」。
- 网段选 `172.28.0.0/24`：避开 AWS 默认 VPC 的 `172.31.0.0/16`（网络运维场景碰撞风险真实）。
- **生效前提**：`docker-compose.yml` 现存缺陷 G-9（无 Dockerfile，build 不起来）、
  G-10（env 变量名与 config 体系不符、`frontend/nginx.conf` 的 upstream 名 `backend` 与
  compose 服务名 `api` 不一致）。本节只给「G-7 需要的正确值」，实际跑通依赖 G-9/G-10。

### D-E 文档

`08-部署运维.md` 新增小节：为什么必须配、配什么（反代容器 IP / LB 网段）、
「信任的是代理本身，不是网段」、不配的后果（限流单桶 + 审计 IP 失真 + 白名单 Key 403）、
如何验证（发带 XFF 的请求后看审计行 IP）。

## 3. Where

| 文件 | 改动 |
| --- | --- |
| `backend/internal/config/config.go` | `ServerConfig.TrustedProxies` 字段 + `Validate()` 校验 |
| `backend/config.yaml` | `server.trusted_proxies: []` + 注释（viper env 覆盖的前提） |
| `backend/internal/api/routes.go` | `SetTrustedProxies`（fail-closed）+ 空配置启动告警 |
| `backend/internal/middleware/`（新文件） | XFF 未被信任时的一次性运行期告警 |
| `backend/internal/config/config_test.go` | 校验用例（合法/非法/过宽/env 覆盖） |
| `backend/internal/api/routes_integration_test.go` | 路由级行为用例（XFF 被忽略 / 被采信 / 审计 IP） |
| `docker-compose.yml` | ipam 固定子网 + 只信任 web 容器 |
| `08-部署运维.md` | 新增小节 |
| `TODO.md` | G-7 结案（带证据）；登记 G-10 |

## 4. 验证清单（每条都要有对应测试或实测输出）

| # | 断言 | 方式 |
| --- | --- | --- |
| V-1 | 空配置时 `ClientIP()` 返回直连对端，忽略 XFF | 路由级：同一 RemoteAddr + 每次不同的 XFF，第 6 次仍 429 |
| V-2 | 配置受信代理后取**最右不可信跳**（不是最左、不是代理本身） | 路由级：`RemoteAddr` ∈ 受信网段 + `XFF="1.2.3.4, 10.0.0.9"` → 审计行 IP = `10.0.0.9` |
| V-3 | 非法条目（`not-a-cidr`、`300.1.1.1`）启动即失败 | `config.Validate()` 单测 |
| V-4 | 裸 IP 合法（等价 `/32`） | `config.Validate()` 单测 |
| V-5 | 审计行 IP = 直连对端（不是 XFF） | 集成测试断言 `audit_logs.ip` |
| V-6 | `NMP_SERVER_TRUSTED_PROXIES` 能覆盖 yaml 的 `[]` | config 单测（tmp yaml + `t.Setenv` + `Load`） |
| V-7 | 过宽前缀（`0.0.0.0/1`、`::/1`）被拒 | `config.Validate()` 单测 |
| V-8 | 受信代理配置下，API Key IP 白名单按真实客户端 IP 判定（不被 XFF 绕过） | 集成测试：白名单含 XFF 里的真实 IP → 放行；白名单含代理 IP → 403 |
| V-9 | 变异反证：删掉 `SetTrustedProxies` 那行 → V-1 必须红 | 手工变异后跑测试 |
| V-10 | `go test ./...` 全绿，分支覆盖率不降 | CI + 本地 |

## 5. Risk（≥2 具体失败模式 + 缓解）

**R-1 部署未配 `trusted_proxies` → 三类后果同时发生（可用性 + 安全控制失效）**
失败形态：生产反代后漏配。① 5 req/min 的登录额度被所有用户共享，一个攻击者 5 次请求即可让全体用户
5 分钟内无法登录；② 审计 IP 全变成 nginx 容器 IP，取证价值归零；③ **配了 IP 白名单的 API Key 一律 403**
（白名单比对的是代理 IP）——这是功能破坏，且表象与「Key 被吊销」难区分。
修前是「限流/白名单可绕过」，修后漏配则是「限流变 DoS、白名单变全拒」——方向相反，同样坏。
缓解：① 启动 WARN 明说三项后果；② 运行期首次见 XFF 再 WARN 一次（行为触发，不依赖运维看启动日志）；
③ compose 默认给出正确值（D-D）；④ 文档写清代价。
**被否的替代方案**：release 模式下空配置直接 fail-fast。否掉的理由：空配置对**直连部署**是正确且安全的
（无 XFF 可吃，`RemoteAddr` 即真实客户端），fail-fast 会误杀合法部署；要区分二者必须新增
`server.behind_proxy` 开关，属于为单点问题扩配置面。若将来漏配事故真的发生，再考虑加开关。

**R-2 受信网段开太宽（如整段 `172.16.0.0/12`）→ 同网段任何容器/主机可伪造 XFF**
失败形态：运维图省事把整个 docker 网段写进配置，被攻陷的任一容器（如 netbox）即可伪造审计 IP、
绕过登录限流与 API Key 白名单——修复形同虚设。
缓解：① D-D 只信任单个容器 IP；② 文档强调「信任的是代理本身，不是网段」；
③ `Validate` 硬拒 v4 </8、v6 </16（实测 `0.0.0.0/1`+`128.0.0.0/1` 可覆盖全部 v4，只拒 `/0` 不够）。

**R-3 viper 对 slice 的 env 覆盖不生效（静默回到空配置）**
失败形态：`NMP_SERVER_TRUSTED_PROXIES` 被 `AutomaticEnv` 忽略（viper 已知限制：`Unmarshal` 走
`AllKeys`，纯 env 键不在其中），进程带着空配置起来——表现与「没配」相同，且没有任何报错。
缓解：V-6 单测直接钉住 env 覆盖（审查已实测：yaml 含 `trusted_proxies: []` 时覆盖生效，键缺失则被忽略）；
`config.yaml` 显式保留占位；若单测发现覆盖不生效，改用 `viper.BindEnv("server.trusted_proxies")`。

**R-4 既有测试依赖 XFF 行为而静默变红/变绿**
失败形态：本仓库测试都用 `req.RemoteAddr` 而非 XFF（已核 `rate_limit_test.go:39`、
`routes_integration_test.go:625`），故预期不受影响；但若有遗漏，改动会改掉限流桶键。
缓解：改动后跑全量 `go test ./...`；新增用例显式断言两个方向（V-1/V-2/V-8）。

## 6. 不做（划界）

- 不引入 Redis 后端限流（`rate_limit.go` 注释里的升级路径是独立任务）。
- 不改 `RemoteIPHeaders` 默认值（`X-Forwarded-For` → `X-Real-IP`）。
  限定条件：**本仓库 nginx 用 `$proxy_add_x_forwarded_for` 追加真实 `$remote_addr`**
  （`frontend/nginx.conf:19-20`），最右项恒可解析，故 gin「XFF 含非法项则 break 并回退 X-Real-IP」
  的路径（审查实测存在）在当前拓扑不可达。换用「原样透传 XFF」的代理时需重评。
- 不修 compose 的其它缺陷（G-9 Dockerfile、G-10 env 变量名/upstream 名）——只加 G-7 需要的两处。
- 不加 `server.behind_proxy` 开关（见 R-1 被否的替代方案）。

## 7. 安全审查处置

| 审查意见 | 处置 |
| --- | --- |
| ① 漏了 API Key IP 白名单这个安全控制消费者 | **采纳**：§1 表补第 4 行，R-1 ③、V-8 新增 |
| ② 默认空 → 单桶；建议加 `behind_proxy` 开关 + 错误时 fail-closed | **部分采纳**：fail-closed 采纳（D-C）；开关**否掉**（理由见 R-1）；单桶后果扩写进 R-1 并加运行期告警 |
| ③ compose 单 IP 可靠性（网络重建/IP 冲突/AWS 网段重叠） | **采纳**：网段改 `172.28.0.0/24`；补「依赖 G-9/G-10 才真跑通」标注 |
| ④ 只拒 `/0` 不够（`/1` 对可覆盖全网） | **采纳**：改为 v4 </8、v6 </16 硬拒；不误伤私有段 |
| ⑤ overclaim（gin 行号、V-2 未断言最右跳、nginx upstream 名） | **采纳**：行号改为 `gin.go:33-46`/`:206`/`:444-455`；V-2 改为断言最右不可信跳；D-D 标注 upstream 名不一致 |

## 8. 实现后审计处置（2026-09-09）

实现完成后再过两名独立审计员（正确性/一致性、安全/回归），结论均为「需修订」。
逐条处置：

| 发现 | 级别 | 处置 |
| --- | --- | --- |
| `::ffff:0:0/96` 等 IPv4-mapped 写法绕过过宽护栏（gin 侧等效信任全部 IPv4） | 高 | **采纳**：按等效 v4 前缀（`ones-96`）判宽；实测绕过用例 + 变异反证 |
| 带空白条目：Validate 放过、gin 报错 → fail-closed 把整张表清空且不再告警 | 中 | **采纳**：`Validate` 先 `TrimSpace` 并写回 cfg，保证「校验所见 = gin 所见」 |
| fail-closed 分支零覆盖 | 中 | **采纳**：新增 `TestRoutes_受信代理配置非法时failClosed`（半截表 + XFF 换桶，变异反证红） |
| 文档称不拒 `fe80::/10`，代码拒了 | 中 | **采纳（改文档）**：/10 link-local 作为「代理本身」太宽，拒得对 |
| 「配了但配错」无任何告警（比不配更隐蔽） | 高 | **采纳**：告警中间件改为按「对端是否在受信表内」判定，恒挂载；启动期非空时补 Info 打印生效表 |
| 旧 `config.yaml` 无该键时 env 被静默忽略（升级场景） | 信息 | **采纳**：`Load()` 加 `viper.SetDefault("server.trusted_proxies", []string{})`；新增 yaml 无键的 env 覆盖用例 + 变异反证 |
| V-8 首条断言 `NotEqual(403)` 偏弱 | 低 | **采纳**：改 `Equal(200)` |
| 「只告警一次」只断言 flag、不断言日志条数 | 低 | **采纳**：注入 `slog.Handler` 断言 3 次请求仅 1 条 WARN |
| 主机位非零被静默归一为整网段 | 中 | **采纳**：显式报错并给出 `172.28.0.0/24` / `172.28.0.10/32` 两种写法 |
| `::fffe:0:0/95` 这类更宽 v6 网段 | — | **不改**：其基址已不是 v4-mapped，gin 的 `IPNet.Contains` 对 IPv4 客户端匹配不上（实测），不构成绕过；拦了反而是误拒 |
| 文档行号失效（§1 `routes.go:117`、D-A `gin.go:438-451`） | 低 | **采纳**：改为 `:118`、`gin.go:397-409` |
| `integrationTestTrustedProxies` 包级可变状态 | 低 | **不改**：api 包无 `t.Parallel()`，`t.Cleanup` 复位，`-count=2` 实测绿；重构 `setupTestRouter` 超出本次范围 |
| compose 静态 IP 与 `--scale web=2` 互斥；宿主机若已占用 172.28.0.0/24 会起不来 | 信息 | **采纳（文档）**：08 文档补自检与互斥说明 |
| gin「最右不可信跳」在 XFF 全受信时回退最左值 | 信息 | **采纳（文档）**：08 文档补「前提是代理追加了真实客户端地址」 |
| 既有：API Key 白名单接受 CIDR 但按精确串比对（永不命中） | 信息 | **登记** TODO G-11（非本次引入） |
| 既有：`routes.go` 重复挂 `gin.Default()` 已含的 Logger/Recovery；`healthCheck` 死函数 | 信息 | 记录，不在本次改动范围 |
