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
- [ ] **API Key 路径不检查 `LockedUntil`**（AUTHZ-LEFTOVER G-2）— 登录路径（`auth_handler.go:58`）检查锁定，API Key 路径（`middleware/auth.go:187-191`）只检查 `status`；账号被锁时 Key 仍可用
- [ ] **前端密钥管理打不到后端**（AUTHZ-LEFTOVER G-3）— `frontend/src/services/api.ts` 用 `baseURL=/api` + `/api-keys` → 实际 `/api/api-keys`，后端在 `/api/auth/api-keys`，落到 NoRoute 返 index.html
- [ ] **`/auth/me` 不在 `openapi.yaml`** — `capabilities` 进不了 `gen:api` 生成类型，前端接入前需先补 spec
- [ ] **`cmd/set-role` 并发窗口** — 防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）；该命令是人工运维操作，暂不修
- [ ] **`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名** — 不含 token，属读地板；若需收紧另立任务

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

## 一键部署 (v1.0.1 新增)

```bash
# 全自动：装依赖 + 起 8 服务 + migrate + seed（含演示数据）
make deploy
# 首次 5-10min (拉镜像)，后续 1-2min (缓存)

# 生产：同上但无种子
make deploy-min

# 健康检查：8 服务 UP/DOWN 一表
make deploy-status

# 详细命令
make help    # 列全部 24 个 target
```

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
