# 网络运维监控平台 - 开发待办

## 当前状态 (2026-06-16)

> 本次更新：补 2026-02-15 ~ 2026-06-16 期间实际完成的工作（21 bug 修复 + 116 new tests + Swagger UI 集成 + type-safe API client）。
> 优化方向见 [docs/优化路线图.md](docs/优化路线图.md)。

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

### 待完成 (2026-06-16 之后)

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

### 用户/诊断小改进（穿插做）
- [ ] 全局搜索 Cmd+K (3h)
- [ ] 一键 ping/traceroute (2h)
- [ ] 暗色模式 (0.5h)
- [ ] 标记误报 → ML 训练集 (1h)
- [ ] 统一 tags 中间表 (6h)
- [ ] 移动端适配 (4h)
- [ ] MIB 浏览器 (8h)
- [ ] 历史快照对比 (6h)
- [ ] 故障复盘 PDF (4h)
- [ ] 工时统计 KPI (3h)

每条任务完成后跑：编写代码 → 代码审查 → 单元测试 → pre-commit → commit。

## 启动命令

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
- 前端: http://localhost:5173
- 后端 API: http://localhost:8080
- API 文档: http://localhost:8080/swagger/index.html
- 测试报告: [TESTING.md](TESTING.md)
