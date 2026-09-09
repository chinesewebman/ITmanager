# 网络运维监控平台 - 开发待办

## 当前状态 (2026-09-09, `git describe` = v2.1.2-12-gdad2b6f)

> **真实基线**：`v2.1.2-12-gdad2b6f`（最新 tag **v2.1.2**，最后提交 **2026-07-01**）。v2.2 / v2.3 系列（B/C 系列）已在 v2.1.2 之后合入，尚未打 tag。
> **下一步：v3 架构优化**（R5 → R2 → R1 → R3 → R4），依据 [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md)。
>
> 历史状态 (2026-06-17, v1.0.2)：v1.0.0 → v1.0.1 → v1.0.2 三连发布（10 PR + 一键部署 + README badges）。详见 [CHANGELOG.md](CHANGELOG.md)。
> 优化方向见 [docs/优化路线图.md](docs/优化路线图.md)。

### v2.1.2 之后已合入（B/C 系列，未打 tag）

- [x] **B1-4 TRAPS.md** (`e7c1a0e`) — 集中 27 个项目 trap
- [x] **B4 资产软退役 + IP 释放** (`223c11e` 后端 + `20723a3` 前端退役/恢复 UI)
- [x] **C6 docker-compose healthcheck** (`77bfdd9`) — 7 服务 healthcheck + `depends_on: service_healthy`
- [x] **C7 首次登录强改密** (`c848ff5` + `dad2b6f`) — 改密页 + 首次跳引导
- [x] 其余（Zabbix / NetBox / GLPI runtime config UI、Zabbix metric fallback worker、Go 1.25 fmt 重排）见 [CHANGELOG.md](CHANGELOG.md) 「未发布」节

### v3 改造待办（2026-09-09 新增，依据 [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md)）

优先级顺序：**R5 → R2 → R1 → R3 → R4**

- [x] **R5 文档与仓库卫生**（2026-09-09 完成）— 版本号对齐 `git describe`；`schema.sql` 拆分（16 张已实现表，39 张设计表移入 `docs/schema-planned.sql`）；ADR-0002 的 gRPC 部分作废（ADR-0003）；`.gitignore:10` `server` → `/server` 修复源码误伤
- [ ] **R2 专线归属 NetBox Circuits**（3-4h）— 废弃自建 `lines` 四表，专线以 NetBox `Circuit` + `CircuitTermination` 建模，ITmanager 只读渲染
- [ ] **R1 AI 模块重定位**（4-6h）— 砍自建 LLM 问答/知识库，改为 ITmanager 作为 HolmesGPT 数据源（toolset 端点，只读 token）
- [ ] **R3 vCenter 纳管**（6-8h）— vCenter → NetBox sync，ITmanager 从 NetBox 读 VM（先 dry-run 一周再开自动清理）
- [ ] **R4 告警与 Keep 划界**（2h 文档 + 后续实施）— 跨源去重/关联归 Keep；ITmanager 保留人工抑制（维护窗口）+ 值班升级
- [x] **R6 工单 SoT 归位**（2026-09-09 完成）— `05-运维工单.md` 重写：ITmanager `tickets` 为唯一真值，GLPI 降级为可选只读参考；新增 `docs/adr/0004-工单SoT决策.md`；明确不做 ITIL 审批 / SLA / 满意度

### 待修复缺陷（2026-09-09 实测，见 [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md) §9）

- [x] **D-1（阻断级）`tickets` 三套 schema 不一致** — 修复轮 `2ec518c`：补齐式迁移 `000013_schema_align`（非破坏、幂等、带类型守卫）+ `db_smoke` CI job（全新安装 + 存量升级两条路径）
- [x] **D-2 工单号碰撞** — `2ec518c`：按当日前缀计数 + 进位字母标签，唯一索引兜底
- [ ] **D-3 `alerts.ticket_id` 悬空** — 字段语义已修（`000013` 补列 + 模型对齐），**写入方仍缺**：随「告警 → 一键建单」落地
- [x] **D-4（阻断级）`users` 表缺 `role` / `deleted_at` 列** — `2ec518c` + `000013`：先加列后回填再设 DEFAULT（避免存量 admin 降级）
- [x] **D-5 `audit_logs` 列名漂移** — `2ec518c`：`000013` RENAME 表/列对齐模型
- [x] **D-6 `RequireRole` 未挂载** — `2ec518c` 挂载 17 条 admin 专属路由；**2026-09-09 进一步升级为能力矩阵**（见下方 AUTHZ 条目）
- [x] **D-7 API Key `permissions` 未校验** — `2ec518c`：校验自身 scope，关联用户 inactive 立即失效

### AUTHZ 角色词表归一 + 权限矩阵（2026-09-09 完成，对应 FIX-PLAN-D1-D7 §7 R-2）

- [x] **权威词表单点化** — `backend/internal/middleware/roles.go`：`admin/ops_admin/ops_user/auditor/readonly/user`，遗留别名 `operator`→`ops_user`、`viewer`→`readonly`（只读入不写出，v4 清理）
- [x] **能力矩阵鉴权** — `read/write/manage/audit/identity` 五档；`RequireRole("admin")` 全部替换为 `RequireCapability(...)`；`read` 是地板（未被更高能力覆盖的端点默认放行）
- [x] **路由清单双向 diff** — `TestRoutes_非GET路由都已分类` 用 `r.Routes()` 反向兜底，新增非 GET 路由忘挂门禁即失败
- [x] **`/auth/me` 下发 `capabilities`** — 前端不再复制矩阵
- [x] **`cmd/set-role`** — 打通 `ops_admin`/`ops_user`/`auditor` 的生产分配路径，含「拒绝降级最后一个 admin」防自锁
- [x] **ADR-0005 + 06 章同步** — `docs/adr/0005-角色词表与权限矩阵.md`、`06-用户权限.md` §6.0.2/§6.2.2
- [x] **前端 6 页 token 失效** — `AlertSuppressions`/`Oncall`/`Runbook`/`Topology`/`MetricSnapshot`/`AssetTimeline` 用 `localStorage.getItem('token') ?? ''` 拼 Bearer，而该键自 C-F5 改 httpOnly cookie 后无人写入 → 恒 401 + 静默回退 mock。**已修复（2026-09-09）**：收敛为 `services/api.ts` 的共享 `apiGet`/`apiSend`（4 份副本已漂移过一次）+ 修 `/api/v1` 前缀 + 拦截器不再把 204/blob 当失败 + Runbook 写操作不再谎报成功 + ESLint `no-restricted-syntax` 回归守卫。见 `docs/FIX-PLAN-FRONTEND-TOKEN.md`

**AUTHZ 审计发现的既有缺陷（本轮记录，未修）**：

- [x] **`POST /api/auth/skip-password-change` 可被空 body 绕过** — 服务端只拒绝 `reason == "first_login"`；body 为空时 `req.Reason` 为空即清除强改密标记 → seed 的 `admin/admin123` 可保留弱口令。**已修复（2026-09-09）**：判据改为 DB 的 `must_change_password`（flag=true 一律 400，不看客户端自报 reason）；前端 `Login.tsx` 跳转参数 `first-login`→`first_login`（原先与 `ChangePassword.tsx` 的判定不一致，跳过按钮在强改密时照样显示）；该路由移入 `protected` 组以复用 AuditLog；不再写假的 `password_set_at`。见 `docs/FIX-PLAN-AUTHZ-LEFTOVER.md`
- [x] **write scope 的 API Key 挂在 admin 账号上可签发新 Key** — `apiKeyAllows` 只按 HTTP 方法判定，`identity` 路由的能力来自关联用户角色 → 可铸造远期 Key 作持久化后门。**已修复（2026-09-09）**：新增 `middleware.RejectAPIKeyAuth`（任何 API Key 一律 403，比原计划「要求 Key 权限含 admin」更严——admin scope 的 Key 同样能自我复制），整组挂在 `/auth/api-keys`，并覆盖同类路径 `PUT /auth/password`。见 `docs/FIX-PLAN-AUTHZ-LEFTOVER.md`
- [ ] **`must_change_password=true` 未在服务端全局强制**（AUTHZ-LEFTOVER G-1）— 登录后拿到的 JWT 仍可调任意端点，「强改密」目前是前端 UX + skip 端点拦截 + **`CreateAPIKey` 定点拒绝**（2026-09-09 审计 F-1 收窄，见 `docs/FIX-PLAN-AUTHZ-LEFTOVER.md` §8.1）。彻底方案需在 `AuthMiddleware` 中查库判定，属性能/架构决策
- [x] **API Key 路径不检查 `LockedUntil`**（AUTHZ-LEFTOVER G-2）— **结案：不修**（2026-09-09 决策，见 `docs/FIX-PLAN-AUTHZ-CLOSURE.md` §2 D-A）。理由：① 因果错位，`LockedUntil` 是「密码被猜」的信号，与「Key 是否被盗」无因果关系；② 耦合会**阻断受害者自救**（锁定判定在密码校验之前，而吊销 Key / 改密必须走登录会话）且任何人可用错密触发 30 分钟停摆；③ 正确补充是「看得见」——已新增登录尝试入审计（D-E）。账号级止血开关是 `status=inactive`（两条路径都立即失效）。连带新增 G-4/G-5
- [x] **前端密钥管理打不到后端**（AUTHZ-LEFTOVER G-3）— **已修复（2026-09-09）**：`api.ts` 四条路径 `/api-keys`→`/auth/api-keys`；`Settings.tsx` 读 `res.data.data.key`→`api_key`（原先一次性明文 Key 永不显示）；过期时间提示 RFC3339→`YYYY-MM-DD`（后端只收 `2006-01-02`）；403 改为区块内 Alert「当前账号无密钥管理权限」而非静默空白。见 `docs/FIX-PLAN-AUTHZ-CLOSURE.md`
- [x] **API Key 可读写通知渠道凭据 / 改写集成出站地址**（F-3 + F-5）— **已修复（2026-09-09）**：`/notification-channels` 整组 + 三条 `PUT /integrations/*` 挂 `RejectAPIKeyAuth`；`/test` 与 `/sync` 保留（堵住 PUT 后只能打管理员配置过的地址）。见 `docs/FIX-PLAN-AUTHZ-CLOSURE.md` §1 S-3/S-4
- [ ] **`/auth/me` 不在 `openapi.yaml`** — `capabilities` 进不了 `gen:api` 生成类型，前端接入前需先补 spec
- [ ] **`cmd/set-role` 并发窗口** — 防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）；该命令是人工运维操作，暂不修
- [ ] **`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名** — 不含 token，属读地板；若需收紧另立任务
- [ ] **G-4 账号处置无产品化入口**（2026-09-09 新增，AUTHZ-CLOSURE §2 D-A）— `users.status` / `locked_until` 只能改库；`/users` 仅 `GET`，`cmd/` 只有 `admin-bootstrap`/`migrate`/`seed`/`server`/`set-role`。建议下一轮做最小集：`cmd/disable-user` + `PUT /users/:id/status`，并一并覆盖 G-5
- [ ] **G-5 已签发 JWT 不查库**（2026-09-09 新增）— `status=inactive` 对 API Key 立即生效（`middleware/auth.go:187`），但对**已签发会话 JWT 最长 24h 才生效**（`middleware/auth.go:112-124` 只用 claims；`auth.jwt.expire=86400`）。「禁用账号」对会话失窃不是即时止血。属独立架构决策（每请求查库 vs 短 TTL + 刷新）
- [x] **G-7 gin 受信代理未配置 → 限流可绕过、审计 IP 可伪造**（2026-09-09 新增 → 当日结案，安全审计 F1①）— 原状：未调 `SetTrustedProxies`，gin 默认信任全部来源，`ClientIP()` 取 XFF **最左值**（攻击者可控）→ ① 登录限流每请求换 XFF 即无限额度；② 审计 IP 可伪造；③ **API Key IP 白名单可绕过**（安全审查补出的第 4 个消费者，`middleware/auth.go:148-161`）。**已交付**（`docs/FIX-PLAN-TRUSTED-PROXY.md`）：① `server.trusted_proxies []string`，默认空 = 不信任任何来源（直连部署的正确值），`NMP_SERVER_TRUSTED_PROXIES` 可覆盖；② `Config.Validate()` 启动期校验——非法条目与**过宽前缀**（v4 < /8、v6 < /16，`0.0.0.0/1`+`128.0.0.0/1` 可覆盖全网，只拒 `/0` 不够）一律 fail-fast；③ `SetupRouter` 调 `SetTrustedProxies`，出错 fail-closed 降级为不信任并 `slog.Error`；空配置启动 WARN + 运行期首次见 XFF 再 WARN 一次（`middleware/trusted_proxy_warn.go`）；④ compose 固定子网 `172.28.0.0/24` + web 静态 IP `172.28.0.10`，api 只信任该 IP（不信任整段——同网段还有 pg/redis/netbox）；⑤ 文档 `08-部署运维.md` §8.2.3。**验证**：V-1 XFF 被忽略（6 次同对端不同 XFF → 第 6 次 429）、V-2/V-5 审计 IP = XFF 最右不可信跳、V-8 白名单按真实客户端判定；变异反证 2 组（删 `SetTrustedProxies` → V-1/V-2/V-8 红；校验空转 → 非法/过宽用例红）；全量 `go test ./...` 26 包绿。**审计后修订（两名独立审计员，处置表见方案 §8）**：① 校验补三类绕过——IPv4-mapped 等效前缀（`::ffff:0:0/96` 曾绕过 /16 门槛，实测 gin 侧信任全部 IPv4）、主机位非零拒绝（`172.28.0.10/24` 被静默归一为整段）、条目 `TrimSpace` 写回 cfg（原先「校验所见 ≠ gin 所见」，会让 fail-closed 清空整张表）；② 运行期告警改为按「直连对端是否在受信表内」判定并**恒挂载**（原只在空配置时挂 → 「配了但配错」零告警，比不配更隐蔽），启动期非空时 Info 打印生效表；③ `Load()` 加 `viper.SetDefault("server.trusted_proxies", []string{})`——旧 `config.yaml` 无该键时 `NMP_SERVER_TRUSTED_PROXIES` 原会被静默忽略（升级场景）；④ 新增 fail-closed 判别用例（半截信任表 + XFF 换桶，变异反证红）与「只告警一次」的日志条数断言；⑤ `.env.example` 补该项。**审计否决 1 条**：`::fffe:0:0/95` 不拦——基址已非 v4-mapped，gin 的 `IPNet.Contains` 对 IPv4 客户端匹配不上（实测），拦了是误拒。**被否的替代**：release 模式空配置 fail-fast——直连部署下空配置是正确且安全的，fail-fast 会误杀，需新增 `server.behind_proxy` 开关才能区分（理由见方案 R-1）。**残余**：compose 实际跑通仍依赖 G-9/G-10
- [ ] **G-8 `RejectAPIKeyAuth` 未挂载时静默失效**（2026-09-09 新增，安全审计 F6）— 该中间件靠 `c.GetString("auth_type") == "apikey"` 判定，若某路由忘了先挂 `AuthMiddleware`，`auth_type` 为空 → 直接放行，守卫**看起来在、实际不设防**。当前所有用法都在 `protected` 组下（已挂 `AuthMiddleware`），属潜在陷阱而非现存漏洞。待办：改为「无 `auth_type` 即 500 + 启动期自检」，或让路由注册阶段校验中间件顺序
- [ ] **G-11 API Key 白名单接受 CIDR 但按字符串精确比对 → CIDR 条目永不命中**（2026-09-09 新增，G-7 审计连带发现，非本次引入）— 创建/更新 API Key 时 `handlers/api_key_handler.go:82-90` 只做 `net.ParseIP` 或 CIDR 格式校验（接受 `10.0.0.0/8`），而鉴权侧 `middleware/auth.go:148-153` 用 `entry == clientIP` 精确比较——填 CIDR 的 Key 永远匹配不上，表现为「白名单莫名 403」。IPv6 文本形式差异（`::1` vs `0:0:0:0:0:0:0:1`）同理。待办：① 要么两侧统一为「CIDR/裸 IP 都按网段匹配」（`net.ParseCIDR` + `Contains`，裸 IP 补 /32、/128）；② 要么创建时就拒绝 CIDR 并写清「只接受单个 IP」。**倾向 ①**（与 `server.trusted_proxies` 的语义一致），但需同时决定 IPv6 规范化。关联 G-7（同一个 `ClientIP()` 消费者）
- [x] **G-6 Web 界面 TLS 最低版本无强制、无验证**（2026-09-09 新增 → 当日结案，PCI DSS 4.2.1）— 原状：仓库内没有任何 TLS 终止或最低版本约束（`frontend/nginx.conf` 只有 `listen 80`，Go 后端 `cmd/server/main.go:104` 明文 `ListenAndServe()`），「只支持 TLS 1.2+」完全依赖外部反代、无法自证。**已交付**：① `08-部署运维.md` §8.2.2「TLS 终止与最低版本」——终止点（nginx/`nmp-web`）、最低版本、套件/HSTS/票据策略、80→443、后端明文不得直接暴露、自证命令；② `scripts/check-tls.sh`——用 `openssl s_client` **主动发起** TLS 1.0/1.1 握手断言被拒（curl 无法降级尝试），退出码 0/1/2，OpenSSL 3.x 缺 legacy provider 时按「无法判定」退出而非假通过；③ `frontend/nginx-tls.conf.example`——443 + `ssl_protocols TLSv1.2 TLSv1.3` + HSTS + 80→443 模板（opt-in，需自备证书）。**已验证**：真实 nginx:alpine 套模板 → 脚本退出 0；故意放开 `ssl_protocols`+`SECLEVEL=0` 的 nginx → 脚本退出 1 并逐条点名 TLS 1.0/1.1。**残余（属部署方动作，非代码）**：生产必须套模板（或外部反代等效配置）并跑一次 `check-tls.sh`，退出码须为 0
- [ ] **G-12 `routes.go` 重复挂载 Logger/Recovery + `healthCheck` 死函数**（2026-09-09 新增，G-7 审计连带发现，非本次引入）— ① `gin.Default()` 已包含 `gin.Logger()` + `gin.Recovery()`，`routes.go:139-140` 又 `r.Use` 了一次，实测每请求访问日志打两遍 **→ ① 已由 G-16 轮修复（2026-09-09）：`gin.New()` + 显式挂载，且 Logger/Recovery 各只挂一次**；② `routes.go:451` 的 `healthCheck` 无任何调用点、0% 覆盖（仍待办）。待办：确认 `healthCheck` 无外部引用后删除
- [x] **G-10 compose 的 env 变量名与 config 体系不符 + 反代 upstream 名与服务名不一致**（2026-09-09 新增，G-7 连带发现）— `docker-compose.yml:46-50` 给 api 传的是 `DATABASE_URL` / `REDIS_URL` / `NETBOX_URL` / `ZABBIX_URL` / `GLPI_URL`，而 `config.Load()` 只认 `NMP_` 前缀 + mapstructure 路径（`NMP_DATABASE_PASSWORD`、`NMP_INTEGRATIONS_NETBOX_URL`…），**这五个变量一个都不会生效**；且缺 `NMP_AUTH_JWT_SECRET` / `NMP_DATABASE_PASSWORD` / `NMP_API_KEY_PEPPER`，release 模式下 `Validate()` 必然拒启。另：`frontend/nginx.conf:14` 的 `proxy_pass http://backend:8080/` 指向服务名 `backend`，compose 里该服务叫 `api`（`container_name: nmp-api`）——DNS 解析不到。待办：① 逐项改成 `NMP_` 变量（含 `NMP_SERVER_TRUSTED_PROXIES`，G-7 已加；`backend/.env.example` 同样缺 `NMP_AUTH_API_KEY_PEPPER`——照抄它会因 `Validate()` 拒启）；② 统一 upstream 名（改 nginx.conf 为 `api` 或给 api 加网络别名 `backend`）；③ 与 G-9 的 Dockerfile 一起做一次 `docker compose up` 真跑通并记录 **已交付（2026-09-09）**：① compose 传给 api 的变量全部改为 `NMP_*`（逐项与 mapstructure 键核对：`NMP_DATABASE_{HOST,PORT,USER,PASSWORD,NAME,SSLMODE}` / `NMP_REDIS_HOST` / `NMP_INTEGRATIONS_{NETBOX,ZABBIX,GLPI}_URL` / `NMP_AUTH_JWT_SECRET` / `NMP_AUTH_API_KEY_PEPPER`）；② 三个必需 secret 用 `${VAR:?}` 强制，缺值 compose 直接报错退出（不再有 `nmp123` 硬编码进仓库）；③ upstream 名用**网络别名** `backend` 对齐（实测 compose 网络里 `container_name` 不注册 DNS，只有服务名/别名可以；不改 `nginx.conf`，它同时是非 compose 部署模板）；④ 端口收敛：postgres 只绑环回 `127.0.0.1:5432`（宿主机 `make deploy` 的 db-migrate/db-seed 要连它）、redis 不发布、api 只绑 `127.0.0.1:8080`、web 也只绑 `127.0.0.1:3000`（它提供明文 HTTP，对外必须前置 TLS 终结——安全审计 P1；顺带解决宿主机已占用 6379 导致的 `port is already allocated`）；⑤ aux 服务加 `profiles: ["aux"]`；⑥ `backend/.env.example` 补 `NMP_AUTH_API_KEY_PEPPER`（照抄旧文件会因 Validate 拒启），根目录新增 `.env.example`；⑦ 文档 §8.3 重写——旧示例（image 拉取 + 全量发布端口 + 非 NMP_ 变量名）与仓库文件漂移，已废弃。
- [x] **G-13 `auth.api_key_pepper` 在 shipped config.yaml 里没有键 → env 被静默忽略 + 报错文案写错变量名**（2026-09-09 新增 → 当日结案，commit `954c79f`）— 实测：`NMP_AUTH_JWT_SECRET`/`NMP_AUTH_API_KEY_PEPPER`/`NMP_DATABASE_PASSWORD` 三个 env 齐备，`go run ./cmd/migrate status` 仍报「auth.api_key_pepper 不能为空（通过 **NMP_API_KEY_PEPPER** 注入）」——两个独立缺陷：① viper 的 `Unmarshal` 只遍历 `AllKeys`（yaml 键 + `SetDefault` 键），纯 env 键不进 AllKeys，shipped yaml 缺 `auth.api_key_pepper` 键 → 该 secret 无论怎么注入都读不进来（fail-closed，但文档化部署路径是死的；生产挂载自定义/旧 config.yaml 同样中招）；② 报错文案写的 `NMP_API_KEY_PEPPER` 不是 viper 认的名字（正确是 `NMP_AUTH_API_KEY_PEPPER`），运维照提示改仍然起不来。**已交付**：yaml 补 `api_key_pepper: ""` 占位 + `viper.SetDefault("auth.api_key_pepper", "")`（覆盖「挂载的 yaml 无该键」场景）+ 报错文案改正 + 三条单测（shipped yaml + env 能启动、旧 yaml 无键时 env 仍生效、报错文案变量名）。变异反证：删 `SetDefault` → 红；文案改回错名 → 红。已逐字段核对：**唯一缺口就是 pepper**。
- [ ] **G-14 迁移与运行时解耦（多副本部署前置）**（2026-09-09 新增，compose 轮审查发现）— `database.Init` **无条件**执行 `migrate.Up`（`internal/database/database.go:61-66` + `cmd/server/main.go:34` 注入 `MigrationsFS`），没有开关；迁移锁是非阻塞 `pg_try_advisory_lock`（`migrate/migrate.go:70-78`），**多副本同时冷启动时抢不到锁的副本会启动失败并反复重启**。本轮只把「api 单副本」写进文档，未改代码（改动需新配置键 `database.automigrate`，而新键又要防 G-13 的 viper AllKeys 坑）。待办：① yaml + `SetDefault` 落 `database.automigrate` 占位；② `database.Init` 读该开关；③ compose 加独立 one-shot `migrate` 服务 + `depends_on: service_completed_successfully`（注意 `cmd/migrate` 走全量 `config.Load`→`Validate`，容器必须注入全部必需 secret，否则 gate 永不满足）；④ 多副本部署文档 + `--scale api=N` 验证。
- [ ] **G-15 release 校验与「集成可选」耦合 → compose 默认模式只能留在 debug**（2026-09-09 新增，compose 轮审查发现）— `Config.Validate()` 在 release 下**无条件**要求 `integrations.netbox.token` 与 `glpi.*_token`（`config.go:222-237`，zabbix 已是「URL 非空才校验」的模式），于是 `docker compose` 默认`NMP_SERVER_MODE=debug` 才不会拒启；而 debug 的代价是：弱集成凭据不被启动期拒绝、登录 cookie 的 `Secure` 不开（`auth_handler.go:134-135`）。待办：① netbox/glpi 改成与 zabbix 一致的「URL 配置了才校验」；② 校验解耦后把 compose 默认翻成 `release`；③ 文档同步。
- [x] **G-16 GORM logger 硬编码 `LogLevel: logger.Info` → release 也全量打印每条 SQL**（2026-09-09 新增，compose 轮审查发现）— `internal/database/database.go:38-46` 固定 Info 级别（含参数展开），容器 stdout 会落全量 SQL，生产上是敏感信息与噪声双输。待办：把 GORM 级别接到 `cfg.Log.Level`（warn/release 下静默或只报慢查询），并加单测钉住「release → 不打印 SQL」。**2026-09-09 安全审计补充实证（P4）**：会落日志的敏感语句包括 `middleware/auth.go:134` 的 `WHERE key_hash = '<...>'` 与 `auth_handler.go:280` 改密后的 `UPDATE users SET password_hash='$2a$10$...'`；`/tmp/smoke-compose2.log:184` 可见完整 bcrypt 值被打进容器日志 → `docker compose logs api` 或转发到 Graylog/Splunk 后，任何只读日志账号都能拿到哈希离线爆破。**已交付（2026-09-09，`docs/FIX-PLAN-LOG-HYGIENE.md`，三视角审查处置见其 §7）**：① gorm 级别接 `cfg.Log.Level`（`mapGormLogLevel`：`debug`→Info、`info`/`warn`/空串/非法值→Warn、`error`→Error；包级默认**显式** `Warn` —— gorm 的零值比 Silent 还小，`Trace` 首行就 return，会一条都不打），5 个 main 各加一行 `database.SetGormLogLevel(cfg.Log.Level)`；② `ParameterizedQueries: true` 恒定 + `RecorderParamsFilter` 覆盖 —— **`DB.Scan` 走 `traceRecorder` 的包级钩子，只设 `ParameterizedQueries` 挡不住**（`finisher_api.go:527-533` + `logger.go:220-225`，实测泄漏）；③ `Colorful: false`（ANSI 不再进日志）；④ **gin Recovery 不再 dump 请求头** —— `gin.Recovery()` **和** `gin.CustomRecovery` 都会在 `CustomRecoveryWithWriter` 内部把 `httputil.DumpRequest` 写进 `DefaultErrorWriter`、且只屏蔽 `Authorization`，**Cookie 里的 `auth_token`（JWT，可直接重放）明文落盘**（默认 `server.mode: debug`），改为手写 recover（`middleware/recovery.go`）+ `gin.Default()`→`gin.New()` 顺带去掉重复挂载的 Logger/Recovery（G-12 的 ① 已一并修）；⑤ `pkg/logger` 级别归一化 —— 原先只认大写，而 `config.yaml` 写的是小写 `info` → `order["info"]==0` 等价 DEBUG，**级别过滤完全失效**；同时补 `viper.SetDefault("log.level","info")`；⑥ seed 不再打印明文默认密码。**验证**：10 项变异反证全红且都红在断言上（删 `RecorderParamsFilter` → 实测打出 `password_hash = "$2a$10$…"`；routes 换回 `gin.Recovery()` → 实测 dump 出 `Cookie: auth_token=eyJ…`；改用 `gin.CustomRecovery` → `DefaultErrorWriter` 非空红；级别映射/默认值/参数化/颜色/归一化/`SetDefault` 各一条）；`go test ./...` 26 包绿 + `go vet` 干净。**残余**：错误文本旁路（PG 约束冲突消息、上游响应体、钉钉 webhook token）→ 见 G-28。
- [ ] **G-17 aux 服务生产化（netbox/zabbix/glpi/graylog/elasticsearch/mongoDB）**（2026-09-09 新增，compose 轮审查发现）— 本轮只做了 `profiles: ["aux"]` 隔离（默认不启动）+ DB 密码与主链同源，其余仍是占位/不安全默认：① 与主链共享 postgres `nmp` 角色（应各建独立角色与库，库也未初始化）；② `SECRET_KEY`/`GRAYLOG_*` 占位凭据（graylog 的 SHA2 值非法，根本起不来）；③ `elasticsearch` 关掉 `xpack.security`；④ 版本标签全是可变的（`latest`/`6.0`/`7`）；⑤ 与 api 同处 default 网络（可直连内部 gRPC 50051）；⑥ zabbix 用的是 `zabbix-server-pgsql`（无 Web/API），`NMP_INTEGRATIONS_ZABBIX_URL` 目标不对，需 `zabbix-web-nginx-pgsql`。**2026-09-09 安全审计补充（P2/P5）**：⑦ aux 与主链共用同一个 postgres 超级用户 `nmp` 与同一个 `default` 网络 → 拿下任一 aux 容器（netbox 的 `SECRET_KEY` 是公开占位值，可伪造会话）即从容器内 `env` 读到主库密码，直连读写 `users`/`audit_logs`；同网段还是 L2 共享域，可 ARP 冒充 web 的静态 IP `172.28.0.10` 从而变成「受信代理」伪造 XFF。加固方向：aux 各建独立角色/库 + 独立 network + `cap_drop: [ALL]` + `no-new-privileges`；ES 至少开 `xpack.security`。⑧ 版本标签钉死（`latest-pg16`/`latest` 会静默换版，带持久卷的 postgres 尤其危险）。
- [x] **G-9 `docker-compose.yml` 引用的 Dockerfile 在仓库中不存在 → `docker compose build` 必失败**（2026-09-09 新增，TLS 任务连带发现）— `docker-compose.yml:40-41` 与 `:59-61` 分别声明 `context: ./backend` / `./frontend` + `dockerfile: Dockerfile`，但 `git ls-files | grep -i dockerfile` 与全盘 `find` 均为空——**仓库里没有任何 Dockerfile**。后果：README/文档里的 `docker compose up -d` 开箱即失败；`nmp-api` / `nmp-web` 两个容器名（G-7 的受信代理讨论、§8.2.2 的 TLS 终止点都按它写的）在当前仓库状态下根本起不来。待办：① 补 `backend/Dockerfile`（Go 多阶段构建：`golang:1.22-alpine` 编译 → `alpine`/`distroless` 运行，非 root 用户）；② 补 `frontend/Dockerfile`（`node` 构建 → `nginx:alpine` 托管，默认站点用 `frontend/nginx.conf`，TLS 场景见 §8.2.2）；③ CI 增一步 `docker compose config`（不 build）防止引用漂移；④ 若决定不提供 Dockerfile，则从 compose 中删掉 `build:` 段并改用镜像名 **已交付（2026-09-09）**：① `backend/Dockerfile`（golang:1.25-alpine 编译 → alpine:3.20 运行，`CGO_ENABLED=0` 静态二进制，非 root uid 10001，busybox 自带 wget 做健康检查，build `server`/`migrate`/`admin-bootstrap` 三个命令；用 `CMD` 而非 `ENTRYPOINT`——ENTRYPOINT + compose `command` 会拼接成 `./server ./migrate up`，迁移会静默变成起 server）；② `frontend/Dockerfile`（node:22-alpine → nginx:1.27-alpine）+ 两个 `.dockerignore`；③ **根因之一：`.gitignore:48-49` 的裸 `Dockerfile`/`.dockerignore` 规则匹配任意深度，当年就算写了也进不了仓库**——已删除该规则（`git check-ignore` 复核）；④ CI 新增 `compose-config` job（`test -f` 四文件 + `docker compose config -q`，作用范围按实证收窄：config 不 stat Dockerfile、也不懂 `NMP_` 语义，env 名对齐靠 backend 单测）；⑤ `scripts/smoke-compose.sh` 真跑通并端到端验证 G-7。方案 `docs/FIX-PLAN-COMPOSE-RUNTIME.md`。

- [ ] **G-18 `database.Init` 的 gorm `AutoMigrate` 兜底在真实 postgres 上不可用**（2026-09-09 新增，compose smoke 实测）— `MigrationsFS` 未注入时 `Init` 走 else 分支的 `autoMigrate()`（`internal/database/database.go:67-73`，函数体 `:79-`）。实测：空库上它建表但**不建迁移里的种子数据**（roles 无 admin → `admin-bootstrap` 报「未找到 admin 角色」）；已迁移的库上它与迁移 DDL 漂移，`AutoMigrate` 直接报 `insufficient arguments`（`database.go:106`）。受影响的三个 CLI （`cmd/admin-bootstrap`、`cmd/seed`、`cmd/set-role`）已在 compose 轮注入 `MigrationsFS` 修复（D-I），但兜底本身仍在：待办 ① 删掉兜底改为显式报错（「请注入 MigrationsFS」），或 ② 限定为 sqlite 测试专用（加注释 + 在 postgres 驱动下拒绝进入该分支）。**注意**：现有单测依赖 sqlite 走这条兜底，改动需同步。

- [ ] **G-19 `web` 容器以 root 运行、无最小权限**（2026-09-09 新增，安全审计 P3）— `frontend/Dockerfile` 的 `FROM nginx:1.27-alpine` 之后没有 `USER`，nginx master 以 root 跑（默认 caps 全开），而它是**唯一对外入口**；对比 `backend/Dockerfile` 的 `adduser -u 10001` / `USER nmp`。本轮已补 `HEALTHCHECK`（`--wait` 才真的等到 nginx 可用）。待办：换 `nginxinc/nginx-unprivileged:1.27-alpine`（或自行 `USER nginx` + 改 pid/temp 路径 + `listen 8080`），并加 `read_only: true` + `tmpfs: [/var/cache/nginx, /var/run]` + `cap_drop: [ALL]`。注意 `frontend/nginx.conf` 同时是非 compose 部署模板，改监听端口要同步 `08-部署运维.md` 与 `frontend/nginx-tls.conf.example`。

- [x] **G-20 `models.Asset.CustomFields` 零值 `''` 在真 Postgres 上是非法 JSON → 建资产失败**（2026-09-09 新增 → 当日结案，CLI 真库回归实测）— 原状：`internal/models/asset.go:48-49` 两列 `type:jsonb` + Go `string`，零值 `""` 被写进 INSERT → PG `invalid input syntax for type json (22P02)`；`cmd/seed` 每条资产都建不出来却 `exit 0`（静默半失败），`POST /api/assets` 不带 `custom_fields` 直接 500；sqlite 不校验 JSON 所以单测全绿。**已交付**（`docs/FIX-PLAN-ASSET-JSONB.md`，三视角审查处置见其 §7）：① `models.Asset.BeforeSave` 把空值归一为 `[]`/`{}`（覆盖 Create/Save，sqlite 默认门禁可断言）；② `migrations/000014_asset_jsonb_defaults.{up,down}.sql` —— 回填 NULL + `SET DEFAULT`（**UPDATE 排在 ALTER 之前**并加 `lock_timeout='5s'`：`execInTx` 单事务内 `ALTER` 的 ACCESS EXCLUSIVE 持有到 COMMIT，顺序反了会让回填期间 `assets` 全表不可读写），down 只 DROP DEFAULT 不动数据；③ `cmd/seed` 失败计数 → 非零退出（`make deploy`/CI 不再把半种子当成功）；④ `integration/service.go:127` 的 `Tags: "{}"` → `"[]"`（列语义统一）；⑤ `scripts/db_smoke.sh` 升级库按**版本号**过滤旧迁移（原 `! -name '000013_*'` 会把 000014 也预应用 → 回填恒命中 0 行的假绿）+ 预置 NULL/非 NULL 两行存量资产；⑥ `TestDBSmoke_DownPreservesLegacyColumns` 先回滚 14 再回滚 13（`migrate.Down` 只回滚最新版本，否则用例空转）；⑦ `ci.yml` seed 步断言资产数 > 0 且无 NULL jsonb；⑧ `smoke-compose.sh` 5b 资产断言 + 6b 经 nginx 建资产（201 且 tags=[] custom_fields={}）。⑨ **连带发现（已修）**：`cmd/seed` 演示资产 `asset_tag` 补 `rack.Name` —— `unique_asset` 经 000013 把 `assets.idc_id` 改名为 `site_id` 后语义是「站内唯一」，而 tag 只含 `site.Code`（`AST-DC-BJ-01-001`），同站点 4 个机柜撞键 → 27 服务器 + 9 交换机 `23505`，**48 个演示资产实际只落库 12 个**；G-20 前这些失败只打日志、`exit 0`，CI 的 `assets > 0` 照样通过。配套 `cmd/seed/main_test.go` 新增「站内唯一 + 48 资产」断言，测试 schema 补 `UNIQUE (asset_tag, site_id)` 镜像真库约束。**验证**：`go test ./...` 26 包绿；`scripts/db_smoke.sh` 两条路径绿；变异反证（删钩子 → V-1/V-2/V-6 红；删 `SET DEFAULT` → V-3 红；删回填 → V-4 红；退回旧 `asset_tag` → 单测红 + 真 PG `exit 1`/36 处失败；删 000014 down 的 DROP DEFAULT → V-8 红）；本机真 PG 全新库跑 seed → `exit 0`、48 资产、0 NULL jsonb、幂等重跑 `exit 0`。**否决**：① 不加 `NOT NULL`（全表校验 + 重写 = 长 ACCESS EXCLUSIVE 锁，且 `PUT {"tags": null}` 仍能绕过，DB 约束换不来真不变量）；② 入参规范化不并入本轮（→ G-21，避免扩大爆炸半径）。**实测边界**：钩子只覆盖 Create/Save，`Updates` 一族全部绕过（结构体零值被 gorm 跳过 = 静默 no-op；`Select(...).Updates` / `Updates(map)` 写出 `''` → 22P02；map 传数组被渲染成 `('x')`、传 null 写入 NULL）→ G-21。

- [ ] **G-21 资产 jsonb 入参规范化（`UpdateAsset` 的 `Updates(map)` 语义）**（2026-09-09 新增，G-20 边界实测）— `handlers/asset_handler.go:113-119` 把请求体直接当 `map[string]interface{}` 交给 gorm，于是 `{"tags": ""}`、`{"tags": []}`（合法 JSON 数组，客户端最自然的写法，gorm 渲染成 `('x')`）、`{"custom_fields": null}` 分别得到 22P02 500 / 22P02 500 / 写入 NULL（破坏 000014 回填出的「无 NULL」）。另外结构体 `Updates` 的零值被 gorm 静默跳过，`PATCH {"tags": ""}` 看起来成功其实没改。待办：定「归一 vs 400」的语义（建议空串/数组/null 一律归一为 `[]`/`{}`，非法 JSON 才 400），在 `UpdateAsset`/`CreateAsset` 层做，并让 `internal/models/hooks_test.go` 的特征化用例改为断言归一结果。关联 G-20（同一族的「零值悄悄变成非法 JSON」）。

- [x] **G-22 NetBox 同步 upsert 用 Go 字段名当列名 → `42703 column "Name" does not exist`**（2026-09-09 新增，G-20 审查连带发现，既存缺陷 → 当日结案）— 原状：`integration/upsert.go:13-22` 把 Go 字段名直接塞进 `ON CONFLICT ("netbox_id") DO UPDATE SET "Name"=EXCLUDED.Name, ...`（`clause.Assignments` 不做字段→列名映射），实测首次插入即失败；与 jsonb 无关。**已交付**（`docs/FIX-PLAN-NETBOX-UPSERT.md`，三视角审查处置见其 §1.1 F-4/F-6/F-7 与 §1.3 F-5、§4 V-0 实测表）：① `buildUpsertClause` 改用 `clause.AssignmentColumns`（参数语义钉死为 **DB 列名**），空列表 → `DoNothing`（gorm 空 `Set` 会渲染 `SET "id"="id"`，PG 上 `42702 ambiguous`，实测）；② 三条路径**全修**：NetBox 走真 upsert（冲突目标 `net_box_id`，更新列 `name, asset_type, brand, model, sn, site_name, updated_at`），Zabbix/GLPI 去掉 `ON CONFLICT`（Zabbix 侧加回必然 42P10 —— `alerts.trigger_id` 无唯一索引，且唯一还会吃掉告警历史；GLPI 侧加回是 42702 —— `SET "id"="id"` 在解析期就歧义，早于仲裁索引检查）；③ `migrations/000015_asset_netbox_unique.{up,down}.sql` 把 `idx_assets_net_box_id` 改成 UNIQUE（PG 唯一索引允许多 NULL → 手工资产不受影响；`DROP INDEX` 的 ACCESS EXCLUSIVE 持有到 COMMIT，读写全阻塞，注释写明大表走维护窗口）；`Asset.NetBoxID` 标签 `index` → `uniqueIndex` 对齐；④ 删掉 `SyncFromNetBox` 的「预查询 + 回填 ID」（其预查询写 `netbox_id IN ?` 也是错列名 → F-6）。**连带修复**：更新列原含 `status`，而 `ConvertToAsset` 硬编码 `"active"` —— 会把本地已退役资产静默改回 active 且留下 `retired_at`（F-7）；漏了 `site_name`（换机房后本地永陈旧）。**验证**：`go test ./...` 26 包绿 + `go vet` 干净；`scripts/db_smoke.sh` 两条路径绿（新增 `TestDBSmoke_NetBoxUpsert`：索引唯一性 / 重复拒绝 / 多 NULL / 真同步端到端 / 脏数据挡住迁移的负循环）；**变异反证 10/10 变红**（M1 手拼 EXCLUDED.、M2 SET 退回 Go 字段名、M3 冲突目标退回 `netbox_id`、M4 测试库去唯一索引、M5/M6 Zabbix/GLPI 加回 ON CONFLICT、M7 000015 建普通索引、M8 更新列加回 `status`、M9 去掉 `site_name`、M10 退回「两次 Down」的旧版本）。**M10 的注意点**：反向变异（只删最后一次 Down）**不会变红、会静默空转** —— 末尾断言恒真，这正是必须显式补第三次 Down 的理由（V-6）。**实测教训**：sqlite 列名解析大小写不敏感 → `SET "Name"=EXCLUDED.Name` 在 sqlite 上**静默成功**，故单测必须断言渲染出的 SQL 字符串（见 T-30）；迁移失败日志里**没有 PG 的 DETAIL**（驱动 `PgError.Error()` 只拼 Severity/Message/SQLSTATE，本仓库未开 gorm `TranslateError`），只剩 `ERROR: could not create unique index … (SQLSTATE 23505)` → 定位重复值只能靠升级前自检 SQL。**第二轮（测试有效性 + 正确性审计，2026-09-09）**：F-A（最严重）修复 —— 删掉 000015 时两套用例原本**全 SKIP + 脚本 EXIT=0**，现改为「前置缺失 → `Fatalf`」+ `MigrateRunner` 守恒断言（`applied == embed 内 *.up.sql 数` 且 15 已记录），实测变异 N1/N1b 均红（`docs/FIX-PLAN-NETBOX-UPSERT.md` §4 V-11/V-12）；F-D/F-2 修复 —— 更新列提为 `netboxUpdateCols` 并由单测**引用该变量**断言（不再另抄），加 `tags`/`custom_fields`/`rack_name` 不被覆盖的断言（N2 实测红）；F-C/F-5/F-3 修复 —— dbsmoke 预置人工标签断言不被覆盖、批次内按 `net_box_id` 去重（PG 同批重复 id 会 21000 整批回滚，N5 实测红）、`now` 改 `time.Now().UTC()` 对齐 gorm `NowFunc`；F-8 顺手修 —— `PATCH /assets/:id` 撞唯一索引从 500 改 409；F-1 划界 —— Zabbix 预过滤只认 `status='problem'`，本地已 ack 的告警会被重复插入，属语义决策 → **G-27**，并用断言钉住当前行为；F-7 补 `t.Cleanup` 恢复共享库状态。新增用例：混合批次、同批重复 id、空列表/错误透传、ack 边界、`Update` 409；`SyncFromNetBox` 语句覆盖 78.6% → **94.4%**（`SyncFromZabbix` 82.8%、`SyncFromGLPI` 85.2%）。变异反证累计 **15 项全红**（M1–M11 + N1/N1b/N2/N5），并新增 T-31（两类假绿：前置 Skip / 测试抄一份生产清单；变异要红在断言上而非编译上）。

- [ ] **G-24 seed 演示网络接口的 IP 非法（`192.168.A.10`）**（2026-09-09 新增，G-20 退出码连带暴露，未修）— `cmd/seed/main.go:161-163` 用 `fmt.Sprintf("192.168.%s.10", rack.Row)` 拼 IP，而 `rack.Row` 是字母（`A`/`B`）→ 生成不可路由的 `192.168.A.10`。000013 把 `asset_networks.ipv4_address` 从 inet 改成 varchar(45) 之前，这类插入是**硬失败**（被静默吞掉）；改名后静默入库。影响面：演示数据可信度（告警里的 `HostIP` 同样是 `192.168.A.10`），不阻塞 G-20。待办：改成 `192.168.<rowIndex>.10` 或直接用保留测试网段，并顺带修 `alerts` 里的同源假 IP。
- [ ] **G-23 `ticket.Tags` 表示一致性**（2026-09-09 新增，G-20 审查划界）— `models/ticket.go:25` 的 `default:'[]'` 对 Create 有效（gorm 参数替换），`ticket_service.go:104-105` 另有 `""→"[]"` 归一，所以插入路径不落 NULL；属「表示不一致」而非缺陷。待办：与 G-21 的归一策略一起收敛（或统一改用钩子）。
- [ ] **G-25 GLPI 同步一次新增 ≥2 张工单必失败（`ticket_number` 撞号）**（2026-09-09 新增，G-22 修复实测暴露，未修）— `models/ticket.go` 的 `Ticket.BeforeCreate → generateTicketNumber` 按「**当天已建条数**」算号（`TICKET-YYYYMMDD-<序号>`），`CreateInBatches` 里每行的 hook 都在**同一批**插入前执行、查到的条数相同 → 整批拿到同一个号 → `tickets.ticket_number` 唯一索引（`idx_tickets_ticket_number`）拒绝整批，`SyncFromGLPI` 返回 error、`SyncAll` 记 glpi 失败。**实测**：一次 `CreateInBatches` 2 张票 → `UNIQUE constraint failed: tickets.ticket_number`（两行都是 `TICKET-20260909-A`）；逐条 `Create` 正常。影响面：GLPI 首次同步（或任何新增 ≥2 张票的同步）全部失败。G-22 轮**刻意不修**（边界见 `docs/FIX-PLAN-NETBOX-UPSERT.md` §6），`TestSyncFromGLPI_两次同步不重复` 只喂 1 张票以免红在编号上。待办：批量插入前显式给每行分配唯一号（或按 `external_id` 派生、或改逐条插入 + 冲突重试），并补一条 ≥2 张票的回归用例。
- [ ] **G-26 `migrations/000014` 的 `SET lock_timeout` 会泄漏到连接池的连接上**（2026-09-09 新增，G-22 审查发现，未修）— `000014_asset_jsonb_defaults.up.sql` 用会话级 `SET lock_timeout='5s'`（不是 `SET LOCAL`），迁移结束后该连接被 gorm 放回连接池，**同一个连接后续所有语句都带 5s 锁超时** → 高并发/长事务下业务写入可能莫名 `55P03 lock_timeout`，且报错点与迁移毫无关联，极难定位。待办：改成 `SET LOCAL lock_timeout`（需在事务内、执行器本来就按事务跑，天然满足），或迁移结束前 `RESET lock_timeout`。

- [ ] **G-27 Zabbix 同步会把「本地已确认」的告警重复插入（同一 trigger 两行）**（2026-09-09 新增，G-22 正确性审查发现，未修）— `integration/service.go:168` 的预过滤只认 `status = 'problem'`，而运维在 ITmanager 里点「确认」后本地行变成 `status='acknowledged'`（`service/alert_service.go:212`）。Zabbix 侧 `trigger.get` 带 `only_true:true` + `filter:{value:1}`（`integration/zabbix.go:166-168`），trigger 未恢复就一直在结果里 → 下次同步预过滤查不到该行 → **再插一行 `status='problem'`**，同一 trigger 在告警列表出现两行、ack 状态丢失。修复前 Zabbix 同步在真 PG 上必 42703/42P10（从未写过数据），属「修复解锁」的既存缺陷。**两条路**：① 预过滤改 `status IN ('problem','acknowledged')`（承认「本地已 ack 的进行中告警不再重复插」）；② 维持现状 + 明写边界。取舍点：ITmanager **没有恢复同步**（只插 problem、从不把行改成 resolved），所以本地 ack 行对应的仍是 Zabbix 侧同一个进行中告警，①更贴近语义；但 Zabbix「恢复后再触发」的场景下本地 ack 行会挡住新告警（②反而能出第二行，代价是丢 ack）。属**语义决策**，G-22 轮不做，`TestSyncFromZabbix_本地已确认的告警会重复插入` 已把当前行为钉住（改语义时该断言要一起改）。

- [x] **G-28 错误文本旁路：凭据随 `*url.Error` 落进应用日志与 `notification_logs.error_msg`**（2026-09-09 新增，G-16 三视角审查发现，未修）— G-16 只堵住了 **SQL 参数**，**错误文本**是另一条路。实测链路：`notification/sender.go:135` 把钉钉 webhook（`https://oapi.dingtalk.com/robot/send?access_token=…`，token 在 query 里）交给 `http.NewRequestWithContext`、`:140` 的 `http.Client.Do`；失败时 Go 返回 `*url.Error`，`Error()` 就是 `Post "https://…?access_token=SECRET": …` —— 完整 URL 连凭据一起。它同时走两个出口：① `worker.go:153` 的 `log.Printf("… send err for channel %s: %v")` → 应用日志（stdout，`log.level` 默认 info 也照打）；② `worker.go:246/253` 的 `markFailed(ctx, id, err.Error())` → `notification_logs.error_msg`（`models/alert.go:130`，varchar(500)，进备份、进只读 DB 账号视野）。钉钉机器人 token 可发任意消息到运维群（告警抑制 / 钓鱼）。同类：`apierr.go:38-41` 把 `internalErr.Error()` 原文写进 `gin.DefaultErrorWriter`（stderr）——包装过的驱动错误、上游错误原文都会落盘（**链路 A**：`handlers/channel_handler.go:85` 的 `apierr.Internal(c, "测试发送失败", err)` ← `service/channel_service.go:96` ← 同一个 `*url.Error`，即人工点「测试发送」也能触发，不必等告警）。第三条面：`middleware/recovery.go:27` 的 `slog.Any("panic", err)` 会把 panic 的 error 值整块打出（当前无带凭据的 panic 源，属潜在面）。**已核实的非向量**：PG 的 `DETAIL`（`Key (col)=(value)`）**不会**出现，pgx v5.5.1 的 `PgError.Error()` 只拼 `Severity: Message (SQLSTATE)`（`pgconn/errors.go:51-53`）。待办：① 通知发送错误按「渠道类型 + HTTP 状态」重写，**不带 URL**（`url.Error` 用 `errors.As` 拆出 `Op`/`Err`，`URL` 只留 scheme+host+path，query 一律丢弃或脱敏）；② `error_msg` 入库前过一遍脱敏（query 里的 `access_token`/`token`/`key`/`secret` 值替换为 `***`）；③ 给 `apierr.Respond` 的 5xx 分支加同样的脱敏，或改为只记 `code` + 类型化摘要；④ 回归用例钉住「含 token 的 URL 不出现在 error / 日志 / error_msg」。**已交付（2026-09-09，`docs/FIX-PLAN-ERROR-REDACT.md` rev2，三视角审查处置见其 §7、实现记录见 §8）**：① 新增纯函数包 `internal/redact`——`URL()` 把任何 URL 塌缩成 `scheme://host`（丢 userinfo/path/query，解析失败 → `<invalid-url>`），`Text()` 三条**结构性**规则（URL 形状 / `Authorization: Bearer|Basic` / 键值形态保留键名换 `***`），不按参数名做黑名单（黑名单必漏 path、header、userinfo 三种形态）；② `notification/sender.go` 新增 `urlErrCause`（`errors.As` 递归剥 `*url.Error`，上限 4 层，内层 nil/超限 → 固定文案 `"未知错误"`，**绝不回传原串**），钉钉/自定义 webhook 各 2 处 return 改为「`scheme://host` + 底层 cause」；非 2xx 与 email 文案**未动**；③ 四个出口全部接上：`worker.go:149/156` 两行日志、`markFailed` 入库前（先脱敏**再按 rune 截断**到 500——修 S-1 的字节截断切断 UTF-8 导致 PG 22021 拒收、行永远 pending）、`apierr.go` 5xx 内部日志、`integration_handler.go:121/174/213` 三处连通测试 400 文案；`markFailed` 的 UPDATE 错误不再静默丢弃（修 S-2）；④ 验证：`go test ./...` 26 包绿 + `-race` 绿 + `go vet`/`gofmt` 干净；`redact`/`urlErrCause`/`markFailed` 语句覆盖 100%；**变异反证 9/9 全红在断言上**（含 M3 `Text` 恒等、M8 日志去掉脱敏、M10 退回字节截断）；审查建议的 M5「先截断后脱敏」经四组构造实测**不可观测**，已从变异表移除并写明理由（顺序不是安全边界）。**残余**：`recovery.go` 的 `slog.Any("panic", err)`（无已知带凭据源）、`httpx.go:167` 拼上游 4xx 响应体 → 见 **G-31**；gorm/pgx 编码错误文本（`%#v`）→ **G-32**；seed 企微渠道键写错 + 钉钉 `SignSecret` 未参与签名 → **G-33**。 **第二轮（2026-09-09 晚，安全审计 + 正确性审计回执，处置见 `docs/FIX-PLAN-ERROR-REDACT.md` §9）**：① **H-1（高，已修）**——`integration/service.go:282/289/296` 与 `metric_sync.go:111` 四处 `log.Printf("%v", err)` 没接脱敏，集成 URL 的 query/path 凭据原文落 stderr；定案**在 httpx 出口收口**（`redactedErr`：`Error()` 过 `redact.Text`、`Unwrap()` 保错误链），一处覆盖全部消费者，避免下次新增日志点又漏；② **M-2/M-3（已修）**——规则 1 加 scheme-relative 分支（`//host/…`，authority 限「带点域名 / localhost / [IPv6]」防误伤路径）、值类不再排除 `'`/`\`、`URL()` 对 `Host` 尾冒号（`https://user:`）返回 `<invalid-url>`；③ **正确性 P1（已修）**——规则 3 值类放宽（不再排除 `}` `]` `<` `>`）、规则 2 放宽（不再排除 `,`），修 `{"password":"ab}c"}` 漏尾与 `password=}SECRET` 整条不匹配；④ **P4（已修）**——`markFailed` 加 `strings.ToValidUTF8`，修「≤500 rune 的非法 UTF-8 被 PG 22021 拒收 → 行永远 pending 无限重发」；⑤ 验证：**27 包全绿**、`-race` 绿、`redact`/`URL`/`markFailed` 100%，第二轮变异 **M11–M16 六项全红在断言上**。新增残余 → **G-34**（值边界）与 **G-35**（过度脱敏）。 **第三轮（2026-09-09 深夜，测试有效性审计回执，处置见同文档 §9.5）**：① **HIGH-1** webhook 的 parse 失败 return 零覆盖 → 补 `TestWebhookSender_非法URL不泄漏原串`（U1 变异红在断言，泄漏原文含 `http://[::1/services/T000/B000/SECRETPATH`）；② **HIGH-2** NetBox/GLPI 的 400 回显脱敏零覆盖 → 补两条用例（**U3′ 组合变异**——同时去掉 handler 层与 httpx 层脱敏——红在断言；**单层**去 handler 脱敏仍绿，因 httpx 已源头收口，登记 **T-35**）；③ **MED-1** `worker.go` resolver 日志零覆盖 → 补 `TestHandleAlertEvent_resolver错误日志不泄漏URL凭据`（`WorkerConfig.Resolver` 注入带凭据错误，U2 红）；④ **MED-2** `urlErrCause` 深度 4 丢 cause（文档称「上限 4 层」实际只处理 3 层）→ 把有界判定移到解包前，V-11 补 depth=4（U4 红）；⑤ LOW-1 恒真断言保留为哨兵、LOW-2 值类耦合保留（注释已说明）；验证：**27 包全绿**、测试函数 **959**、`notification` 覆盖 **73.0%**。

- [ ] **G-29 容器日志无轮转（`json-file` 默认无上限）**（2026-09-09 新增，G-16 审查发现，未修）— `docker-compose.yml` 全仓没有 `logging:` 段，Docker 默认 `json-file` 驱动**不轮转**：`api`（gin 访问日志 + gorm 慢查询/错误 + 应用日志）与 `web`（nginx access log）长期运行会把宿主盘写满，写满后 postgres 写入失败、容器被 OOM/异常重启，且 `docker compose logs` 本身也会变得不可用。G-16 把 SQL 参数化后单条日志变短，但不改变「无上限」这件事。待办：给所有服务加 `logging: {driver: json-file, options: {max-size: "10m", max-file: "5"}}`（或统一走 journald/远端采集），并把上限写进 `08-部署运维.md` §8.4.3。

- [ ] **G-30 冒烟脚本把数据库口令放进进程参数与环境**（2026-09-09 新增，G-16 审查发现，未修，低危）— `scripts/db_smoke.sh:93` 用 `docker run -e POSTGRES_PASSWORD="$DB_PASS"`：`-e` 的值进 `docker run` 的 **argv**，同机任意用户 `ps aux` 即可看到（`/proc/<pid>/cmdline` 默认全局可读）；`:83-84` 的 `PGPASSWORD="$DB_PASS" psql` 与 `:175-176` 构造、`:181-190` 执行的 `TEST_DATABASE_URL="postgres://user:pw@…" go test` 则落在子进程环境里（同 uid 可读 `/proc/<pid>/environ`）。影响面限于本地一次性冒烟库，且 `DB_PASS` 默认值是**固定**的 `smoke_pw`（`db_smoke.sh:41`，可被 `SMOKE_DB_PASS` 覆盖；随机的只有 `smoke-compose.sh:66` 的 `ADMIN_PW`），故低危。待办：`POSTGRES_PASSWORD` 改用 `--env-file`（0600 临时文件、`trap` 删除）或 stdin 传 `-e PGPASSWORD` 之外的方式；DSN 走 `PGPASSFILE`/`.pgpass` 而非 URL 里的明文口令。

- [ ] **G-31 同类错误回显点未逐一收口（HTTP 400 文案 / 上游响应体 / panic 值）**（2026-09-09 新增，G-28 审查发现，未修）— G-28 只修了**已核实带凭据**的四处出口，同类写法还有：① `apierr.BadRequest(c, "...: "+err.Error())` 全仓约 30 处（`grep -rn 'BadRequest(c, .*err.Error()' internal/`），绝大多数是 DB/参数校验错误（不含 URL），但**形态相同**——将来任何一处接上第三方客户端错误就会复现 G-28；② ~~`internal/httpx/httpx.go:167` 的 4xx 分支把**上游响应体原文**拼进错误~~（**2026-09-09 晚已闭合**：httpx 出口统一过 `redact.Text`，见 G-28 第二轮 ①）（`httpx: %s %s → %d: %s`），上游若回显了凭据（例如把请求 URL/DSN 放进错误 JSON）会连带落日志；③ `middleware/recovery.go` 的 `slog.Any("panic", err)` 把 panic 的 error 值整块打出（当前无带凭据的 panic 源）。待办：把「错误文本进日志/响应」收敛到一个统一出口（例如 `apierr` 里集中脱敏 + httpx 只留状态码与截断后的摘要），或用 lint/测试钉住「新调用点必须过 redact」。 **补充（2026-09-09，G-33 M1 rev3 正确性审计 L-1）**：`ErrInvalidInput` 的哨兵文本是英文 `invalid input`，凡 `BadRequest(c, err.Error())` 的路径用户都会看到 `invalid input: 渠道名称不能为空` 这类中英混排（10+ handler 同形）。属本条的「错误文案」面，随统一出口一并处理；单独改共享哨兵会波及全仓文案断言，故不在 G-33 轮动。
- [ ] **G-32 gorm/pgx 的编码错误文本可能带值（`%#v`）**（2026-09-09 新增，G-28 审查发现，未修）— pgx v5 在类型编码失败时会把**值本身**用 `%#v` 拼进错误（`pgtype/pgtype.go:1905` 附近），而 gorm 的 logger 只参数化了 SQL 文本、不处理错误文本。当前无「凭据 → 非字符串列」的调用点，故非活缺陷；若将来把 token/口令写进非 text 列（如 `jsonb`、`inet`、`uuid`）并触发编码错误，凭据会随错误文本落进 G-16 已接级别的 gorm 日志。待办：评估自定义 gorm logger 只打 SQLSTATE + 表名，或对 `Error()` 出口统一过 `redact.Text`。
- [ ] **G-33 seed 企微渠道配置键写错 + 钉钉 `SignSecret` 解析后从未使用**（2026-09-09 新增，G-28 审查发现，功能缺陷非安全）— `cmd/seed/main.go` 给企业微信渠道写的键是 `webhook_url`，而 `WebhookSender` 只认 `url`（`sender.go` 的 `channelConfig.URL`）→ 该渠道恒报 `webhook: url is required`，通知永远发不出去；另 `channelConfig.SignSecret`（钉钉加签）解析后**没有任何代码使用**，配了也不生效。待办：seed 改键名 + 钉钉实现加签（或从 schema/UI 里去掉该字段以免误导）。**M1 已交付（2026-09-09，`docs/FIX-PLAN-NOTIFY-CHANNEL.md` rev2，三视角审查处置见其 §7）**：把问题扩到「配置契约错位」全域并修 M1——① 后端写入端 fail-fast：新增 `validateChannelConfig`（`notification.NewSender` 试构造，**不发起网络 I/O**，失败原因在 service 侧过 `redact.Text` 后随 `ErrInvalidInput` 返回——400 出口没有兜底，`apierr.BadRequest` 的 internalErr 恒 nil）；`Create` 校验名称+配置；`Update` 用**合并后的 (type, config)** 校验（只改 name/is_enabled 才跳过），`config`/`type` 非字符串一律 fail-closed（否则 gorm `Updates(map)` 会把 `float64`/`bool` 静默写成 `"12345.0"`/`"1"`，见 T-36）；② handler：`Create` 400 回显原因（原硬编码「渠道名称不能为空」会吞掉构造器原因）、`Update` 补 `ErrInvalidInput` → 400（原来会落成 500）；③ 前端 `Settings.tsx` 表单键名对齐后端 tag（原用 `smtp`/`port`/`username`/`password`/`webhook`，全是坏键）、端口改 `InputNumber`（`<Input type="number">` 产出字符串 → `SMTPPort int` 反序列化失败，M1 上线会把 UI 自己保存的邮件渠道全挡）、email 补 `from`/`to`（`Select mode="tags"`）、下拉去掉后端未实现的 `wechat`、编辑不再硬编码 `is_enabled: true`（原会把已停用渠道静默启用）；④ seed：邮件行补 `to`（**原本是启用中的坏行**）、钉钉行 `secret` → `sign_secret`、企微行 `webhook_url` → `url`；⑤ OpenAPI `config` 由 `object` 改 `string`（对象形态从未工作过：Create 400 / Update 500）；⑥ **跨语言契约单一来源** `frontend/src/pages/__fixtures__/channelConfigSamples.json`（前端 deep-equal、后端读同一文件喂 `NewSender`）。验证：968 backend 测试函数（27 包全绿）+ 170 frontend 测试（27 文件全绿）+ `tsc`/`lint` 干净；**变异 V-9/V-10/V-11 全红在断言上**。**M2（钉钉加签 + 响应 `errcode` 校验）与 M3（企微 sender → G-36）未做**。 **rev3（2026-09-09 深夜，实现后三路只读审计回执，处置见同文档 §7.2）**：安全与正确性两路**独立命中同一个 HIGH**——`Update` 的 fail-closed 建在「精确匹配小写键」上，而 gorm `Updates(map)` 的 `LookUpField` 会把 Go 字段名解析到同一列，`{"Config":12345}` / `{"Type":"dingtalk"}` / `{"id":…}` 全部绕过校验落库（真 PG 18 实测修复前 HTTP 200；新增 T-37）→ 改「键归一化到小写 + 白名单（未知键 400）」并让写入值 == 校验值；② **前端 Modal 串记录（H-2）**：`initialValues` 只在挂载时应用，连续编辑两条渠道会把上一条写进当前记录（后端拦不住，写进去的组合自洽）→ 打开弹窗时 `form.resetFields()`；③ **400 body 回显 Type 的非 URL 形态（安全 M-1）**：裸 token / JWT / percent 编码 / 无 scheme / 多行都能穿过 `redact.Text` → `sender.go` 的未知类型错误改静态文案，400 出口从此无调用方可控内容；④ **契约只钉必填键（M-2）**：表单与样本「一起改名」两侧双绿 → 新增第三条腿「样本键集合 ↔ `channelConfig` json tag」（`sender_contract_test.go`）；⑤ 存量 `wechat` 行改为显示可读标签 + 下线提示（原会按 Webhook 渲染、必填永远过不了）；⑥ OpenAPI 响应 enum 加回 `wechat`、输入 enum 收窄、补 `is_enabled`；手写类型 `config` 改 `string`。验证：**971 backend 测试函数 / 172 frontend 测试全绿**，变异 **V-9..V-16 全红在断言上**（V-11「删 redact.Text」因 400 出口已无可控内容而**失效**，由 V-13「恢复 Type 回显」取代，见文档说明）。 **rev4（2026-09-09 深夜，第三路「测试有效性」审计回执，处置见同文档 §7.3）**：审计自设计 20 条后端 + 9 条前端变异逐条实测，结论是**代码无新缺陷、缺的是测试承重力**，4 处「变异存活」全部闭合——① **H-1** seed 钉钉行 `sign_secret` 无测试钉住（`NewDingTalkSender` 只要求 `webhook_url` 非空，键名改成 `secret` 照样构造成功）→ `cmd/seed` 用例加逐类型**键集合断言**（V-18 红）；② **M-1** 前端 `is_enabled` 保留逻辑无断言（改回硬编码 `true` 全绿 → 编辑已停用渠道会静默启用、真发告警）→ 用例 fixture 改 `is_enabled: false` 并断言 `updateChannel` payload（V-19 红）；③ **L-1/L-2** 兜底样本键名与 wechat 下拉 `disabled` 无测试 → 各补一条（V-21/V-20 红）；④ **L-3** 「只改name不触发校验」子用例此前不承重（种子行合法，恒校验也过）→ 种子改为存量坏行（V-22 红）。**两条存活判为结构性、明确接受**：删 `redact.Text`（400 出口已无任何可造向量）、`Update` 写回 `effType/effConfig`（顺序执行下行为等价，需 `-race` + 可控交错）。另修 L-6（describe 未重置 `apiKeyApi.list` → 测试顺序隐式耦合）。验证：**971 backend 测试函数 / 174 frontend 测试全绿**（+2），5 条新变异**逐条确认红在断言上**。


### v1.0.2 已发布（6/17）
- [x] **README.md**：6 GitHub badges (Release/CI/License/Go/React/Docker) + 状态推进
- [x] **CHANGELOG.md**：v1.0.0 / v1.0.1 / v1.0.2 sections
- [x] **Makefile**：`make deploy` / `deploy-min` / `deploy-status` 3 target
- [x] **GitHub releases**：3 个 (v1.0.0 稳定 / v1.0.1 部署补丁 / v1.0.2 文档)

### v1.0.0 / v1.0.1 累计成果
- [x] **P0-P2 全链路**：P0-1 诊断时间线 + P0-2 抑制规则 + P1-1 拓扑 + P1-2 值班升级 + P2-1 Runbook + P2-2 Zabbix 兜底
- [x] **S 级 3 项**：暗色模式 (ab05d3d) + 误报 ML 训练集 (e502701) + Cmd+K 全局搜索 (f7e98eb)
- [x] **A 级 3 项**：ping/traceroute (6edd9c9) + 复盘 PDF (ea70644) + KPI 仪表盘 (2b893bc)
- [x] **A 级 review 全套修复** (3a667e8)：traceroute dead code + binary 启动验证 + 嵌入中文字体 + io.Writer 流式 + KPI 阈值常量 + KPIs sqlmock 测试
- [x] **测试指标**：backend 22 packages / 603 tests / frontend 19 files / 117 tests / tsc 24=baseline
- [x] **CI**：pre-commit hook (gofmt + swagger validate + .bak 拦截) + `.github/workflows/ci.yml`

### 性能总计
- 估时 14.5h → 实测 6.75h (**2.1x 加速**)

### 已完成 (2026-06-16 更新增量)

- [x] **测试覆盖**：backend 230 → 401 passed (20 packages)，frontend 14 → 53 passed (8 files)，总测试 338 → 454
- [x] **覆盖率**：61.2% (api 85.5% / apierr 88.5% / apikey 100% / config 94.3% / httpx 87.8%)
- [x] **代码审查 Bug 修复 (17)**：
  - handlers 8 bug：FailedLogin race / 弱密码 / 改回旧密码 / rate_limit 越界 / IP 越界 / uuid 静默 / type=garbage / Name UNIQUE
  - httpx 2 bug：half-open race / ctx 取消误触熔断
  - service 17 bug：Severity int / Limit=0 / 重复 First / Bulk 1000 上限 / 死代码 / usedMap 错 / User 分页 / Dashboard 单条 SQL / 假数据
  - cmd + 前端 + 中间件 4 bug：admin role 检测 / testdata schema 漂移 / asset_networks ipv6_address / localStorage 缺失
- [x] **Swagger UI 集成**：`/swagger/index.html` + `swagger-cli validate` CI
- [x] **type-safe API client**：3/13 服务方法已 typed，`npm run gen:api` 自动生成
- [x] **pre-commit hook**：gofmt + swagger-cli validate + .bak 拦截
- [x] **TESTING.md**：4.7K 测试现状报告

### 待完成 (2026-06-17 之后)

## 1. 数据库初始化 (优先级: 高)
- [x] 运行 `go run ./cmd/seed/main.go` 创建初始数据
- [x] 重启后端服务 `go run ./cmd/server`

## 2. 完善认证功能 (优先级: 高)
- [x] 实现 JWT Token 生成和验证中间件
- [x] 完善 Login/Logout 接口
- [x] 添加默认管理员用户 (admin/admin123)
- [x] 添加 API Key 认证支持
- [x] **登录失败锁定** (FailedLogin atomic CAS 修复 race)
- [x] **密码强度校验** (handler 拒绝弱密码)

## 3. 完善后端 API (优先级: 高)
- [x] 资产 CRUD - 完整实现
- [x] 告警确认/解决 - 完善逻辑
  - [x] **Bulk 操作 1000 上限** (ErrTooManyItems)
  - [x] **Severity 数值比较** (string → int)
- [x] 工单管理 - 本地 CRUD 完成 (GLPI 集成待完成)
- [x] **熔断器** (httpx: half-open race + ctx 取消修复)
- [x] **User List 分页** (避免全表扫描)
- [x] **Dashboard 单条 SQL 聚合** (5 次 count → 1 条)

## 4. 第三方集成 (优先级: 中)
- [x] NetBox 集成 - 资产同步
- [x] Zabbix 集成 - 告警获取
- [x] GLPI 集成 - 工单同步
- [x] 集成 API 端点

## 5. 前端完善 (优先级: 中)
- [x] 登录页面
- [x] 实时数据对接
- [x] **vitest 测试** (5 page + 8 hook + 31 store = 53 tests)
- [x] **queryKeys factory** (`useApiQuery.test.ts`)
- [x] **zustand store 单元测试** (`stores/index.test.ts`)
- [x] **type-safe API client** (3/13 服务方法)
- [x] **暗色模式** (zustand persist 'theme-storage' + ConfigProvider `theme.darkAlgorithm` + ThemeSwitcher 按钮 + 13 tests) — commit ab05d3d
- [x] **误报标记 + ML 训练集导出** (alerts 表加 4 字段 + `POST /alerts/:id/mark-fp` + `GET /alerts/false-positives/export` CSV + 8 backend tests + 2 frontend tests)
- [x] **全局搜索 Cmd+K** (Antd Modal + 自写 fuzzy 子序列匹配 + useGlobalHotkey hook + 3 资源跨搜资产/告警/工单 + 18 frontend tests)
- [ ] 机柜可视化增强
- [ ] 前端 coverage 工具 (无 `--coverage` 配置)

## 6. 文档与 CI (优先级: 中) — 2026-06-16 新增
- [x] **TESTING.md** (4.7K)：测试现状 + 21 bug 清单 + 覆盖率表
- [x] **Swagger UI** + CI validate
- [x] **TODO.md / tasks.md / 12-优化建议.md / 开发计划.md** 状态同步 (2026-06-16)
- [ ] **CI 升级**：加 `go test -race` + frontend vitest 步骤
- [ ] **CI 升级**：加 coverage 阈值门禁 (目前仅 generate)
- [ ] **覆盖盲区**：`internal/database` 0% (依赖 PG) / `internal/middleware` 36.5% (跟 api 共享) / `internal/integration` 39.5%
- [ ] **type-safe 推进**：10/13 服务方法仍 `data: any`

## 7. 部署与运维 (优先级: 低)
- [ ] 生产环境 docker-compose
- [ ] Nginx 反向代理
- [ ] HTTPS (Let's Encrypt)
- [ ] 日志收集 (Filebeat)
- [ ] 生产部署 + 冒烟测试

## 6. 优化路线图（详见 [docs/优化路线图.md](docs/优化路线图.md)）

### P0 - 立即做
- [x] **P0-1 诊断时间线** (2-3h) ✅ — `GET /api/v1/diagnostics/assets/:id/timeline` 聚合 alerts/tickets/asset_networks 4 张表 + MTTR 摘要 + 前端 Antd Timeline 渲染。Service 10 test + Handler 7 test + Routes 3 test + 前端 3 test = **23 new tests**。文档：`docs/优化路线图.md`。
- [x] **P0-2 抑制规则引擎** (4-6h) ✅ — `alert_suppressions` 表 + CRUD API + /preview 模拟评估 + in-memory 窗口缓存（sync.RWMutex）。Service 24 test + Handler 15 test = **39 new tests**。前端 `/alert-suppressions` 管理页 + menu。
- [x] **P1-1 网络拓扑** (6-8h) ✅ — `GET /api/v1/topology` 聚合 assets + asset_networks + alerts + 环形自动布局 + 虚拟节点 (connected_to 找不到时)。Service 11 test + Handler 6 test + 前端 5 test = **22 new tests**。前端 `/topology` SVG 渲染（0 依赖）+ 告警 badge + down 边高亮。

### P1 - 本月内
- [x] **P1-2 值班 + 升级** (8-10h) ✅ `720b307` — 4 表 (Schedule/Shift/EscalationPolicy/EscalationLevel) + 接 `now time.Time` 参数 + 排班冲突检测 + 升级链 + CRUD/GetCurrentOncall. Service 19 test + Handler 14 test + 前端 2 test = **35 new tests**. 12 route paths + swagger 10 paths + 5 schemas + type client 18 type.
- [ ] **P1-3 MIB 浏览器** (5-6h) — 网络设备 SNMP MIB 树浏览 → OID → 指标字典
- [ ] **P1-4 历史快照对比** (4-5h) — 2 个时间点资产配置 diff (CDP/LLDP/VLAN 变化)

### P2 - 下个月
- [x] **P2-1 故障 Runbook** (3-4h) ✅ `f79873f` — `runbooks` 表 + CRUD + ListForAssetTypeAndSeverity 推荐 + /runbooks/recommend. Service 16 test + Handler 15 test + 前端 3 test = **34 new tests**. swagger 6 paths + 1 schema + 25 Runbook types.
- [ ] **P2-2 Zabbix 兜底** (4h) — metric_snapshots 落 TimescaleDB

### 覆盖率目标 75%
- [ ] `internal/database` 0% → 60%
- [ ] `internal/middleware` 36.5% → 70%
- [ ] `internal/integration` 39.5% → 60%

### 用户/诊断小改进（穿插做）— 全部完成
- [x] 全局搜索 Cmd+K (3h) ✅ `f7e98eb` — Antd Modal + 自写 fuzzy + 18 tests
- [x] 一键 ping/traceroute (2h) ✅ `6edd9c9` — ICMP 探活 + 14 handler tests
- [x] 暗色模式 (0.5h) ✅ `ab05d3d` — Antd darkAlgorithm + zustand persist + 13 tests
- [x] 标记误报 → ML 训练集 (1h) ✅ `e502701` — 4 字段 + CSV 导出 + 10 tests
- [x] 故障复盘 PDF (4h) ✅ `ea70644` — go-pdf/fpdf + 霞鹜文楷嵌入 (OFL 1.1, 15MB) + io.Writer 流式 + 7 tests
- [x] 工时统计 KPI (3h) ✅ `2b893bc` — MTTR/MTTD/告警密度/SLA + KPI_THRESHOLDS 常量 + 4 sqlmock tests
- [ ] 统一 tags 中间表 (6h) — 搁置
- [ ] 移动端适配 (4h) — 搁置
- [ ] MIB 浏览器 (8h) — 搁置
- [ ] 历史快照对比 (6h) — 搁置

每条任务完成后跑：编写代码 → 代码审查 → 单元测试 → pre-commit → commit。

## 一键部署 (v1.0.1 新增，2026-09-09 按主链/aux 重构)

前置：`cp .env.example .env && chmod 600 .env`，三个 secret 用 `openssl rand -hex 32` 各生成一份
（缺值 compose 直接报错退出）。

```bash
# 生产（推荐）：装依赖 + 起主链 4 服务(postgres/redis/api/web)；迁移由 api 启动时执行
make deploy-min
# 首次 5-10min (拉镜像 + build)，后续 1-2min (缓存)
# 首个管理员：docker compose exec api env FIRST_ADMIN_USERNAME=admin \
#   FIRST_ADMIN_PASSWORD='<强密码>' ./admin-bootstrap

# 演示环境：同上 + seed（⚠️ 写入 admin/admin123 等已知密码账号，勿用于生产）
make deploy

# 健康检查：主链 4 服务 + aux（未启动显示 ⏸️）
make deploy-status

# 辅助系统（netbox/zabbix/glpi/graylog + es/mongo）：默认不启动
make docker-up-aux

# 详细命令
make help
```

详见 [08-部署运维.md](08-部署运维.md) §8.3 与 [README.md](README.md) 快速开始。

## 启动命令（手动模式，无 Docker）

```bash
# 1. 启动 PostgreSQL (如果未运行)
docker run -d --name nmp-postgres \
  -e POSTGRES_USER=nmp \
  -e POSTGRES_PASSWORD=*** \
  -e POSTGRES_DB=network_monitor \
  -p 5432:5432 timescale/timescaledb:latest-pg16

# 2. 初始化数据库数据
cd backend && go run ./cmd/seed/main.go

# 3. 启动后端
cd backend && go run ./cmd/server

# 4. 启动前端 (另一个终端)
cd frontend && npm run dev
```

## 访问地址
- 前端: http://localhost:5173 (vite dev) / http://localhost:3000 (docker)
- 后端 API: http://localhost:8080
- API 文档: http://localhost:8080/swagger/index.html
- 测试报告: [TESTING.md](TESTING.md)
- 一键部署指南: [Makefile](Makefile) + [docker-compose.yml](docker-compose.yml)

- [ ] **G-34 脱敏规则 2/3 的值边界残余：值以分隔符或引号开头时不匹配**（2026-09-09 晚新增，G-28 第二轮正确性审计 P1 残余，未修）— 规则 3 的值类以「空白 / 引号 / `&,;`」为界，值**以**这些字符开头时整条不匹配（`password=&SECRET`、`token="SECRET`），规则 2 同理（`Authorization: Bearer "SECRET`）。RE2 无反向引用 → 做不到「同名引号配对」，当前用「引号可选 + 值类到定界符」近似。修法：拆成「引号形态」与「裸值形态」两条规则（引号形态用 `"([^"\n]*)"` / `'([^'\n]*)'` 两个分支），或改用小型状态机替代正则；改前先补 P1 的四组边界用例。可达性低（需凭据以 `name=value` 形态出现且值首字符是分隔符），但属该规则声称覆盖的形态。
- [ ] **G-35 规则 1 输出被规则 3 二次误伤：主机名命中敏感词时端口被抹掉**（2026-09-09 晚新增，G-28 第二轮正确性审计 P3，未修）— `Text("http://token:8080/x")` → `http://token:***`：规则 1 先把 URL 塌缩成 `http://token:8080`，规则 3 又把 `token:8080` 当键值形态、把端口 `8080` 当值抹掉。**过度脱敏、不泄漏**，但排障时丢端口。修法：规则 3 对「紧跟在 `://` 之后」的片段跳过，或让规则 1 的输出带哨兵字符；需先证明不引入新的绕过面。
- [ ] **G-36 企业微信（`wechat`）渠道端到端**（2026-09-09 新增，G-33 审查发现，未修）— 前端下拉曾提供 `wechat` 但后端 `NewSender` 没有该类型（`unsupported channel type: wechat`）；M1 已把前端选项与 OpenAPI 输入 enum 去掉（存量 `type=wechat` 行不会走到后端：前端下拉保留一条禁用项显示「企业微信（暂不支持）」，编辑时渲染下线提示而**不**按 Webhook 渲染——正确性审计 M-1 更正了此处原先「会被 400 挡住」的说法）。要真正支持需新增 `WeChatSender`：body 为 `{"msgtype":"text","text":{"content":…}}`（当前 `WebhookSender` 发 `{"content":…}`，键名修好也发不出去）、解析 `{"errcode":N,"errmsg":…}` 判失败（与 M2 的钉钉回执校验同批做）、`NewSender` 注册、seed 企微行改 `type: "wechat"`、前端下拉与 OpenAPI enum 加回。属 G-33 的 M3。
- [ ] **G-37 OpenAPI 的通知渠道路径与实际路由不一致**（2026-09-09 新增，G-33 M1 rev3 发现，未修）— `backend/internal/api/openapi.yaml` 写的是 `/notification/channels`、`/notification/channels/{id}`、`/notification/channels/{id}/test`（且 test 是 `post`），而真实路由是 `/notification-channels` 整组（`routes.go:363`）、test 是 `put`（`api.ts:214`）。文档-only 漂移，但会让「OpenAPI 是契约单一事实来源」的说法打折。修法：改 3 处 path + 1 处动词，`npm run gen:api` 重新生成 `api.types.ts`，并加一条「OpenAPI path 集合 == `routes.go` 注册集合」的测试（或反向由路由生成 OpenAPI）。
