# Changelog

ITmanager 项目所有重要变更记录。版本遵循 [SemVer](https://semver.org/)。

## [v2.0.2] - 2026-06-27

🛠️ **审计 P1 修复** — 4 项跨模块数据完整性 / 稳定性 / 审计语义

### 修复 (审计 v2.0.1 → v2.0.2 patch)

- **fix(audit-P1)**: `resourceFromPath` 跳过动态段找下一个静态段 — 旧逻辑遇到 `:` 开头段直接 return `""`，导致 `/api/:tenant/users` 等路径的审计 resource 字段丢失。改为 continue 跳过动态段继续找第一个静态段。
  - 新增 4 case 测试：tenant 前缀 / id 中缀 / 纯动态 / 首段动态 + api 段
- **fix(eventbus-P1)**: dispatch panic recover — handler panic 之前会让 worker goroutine 死亡，chan 持续入事件无人消费，最终 ErrBufferFull 风暴。`dispatch` 加 defer recover，panic 后日志 + DLQ 落库，worker 继续服务后续事件。
- **fix(eventbus-P1)**: `Subscribe` 跨 topic 聚合 — 旧版只统计最后一个 Subscribe 的 topic 的 handler 数（A=3 + B=5 应=8 报 5），监控失真。改为聚合所有 topic 的 handler 总和。
- **fix(oncall-P1)**: `DeleteSchedule` 两步 DELETE 包事务 — 旧实现先删 schedule 再删 shifts，若第二步失败（FK/DB）则 shifts 永久孤立。改为 `db.Transaction` 包裹两次 DELETE，失败回滚 schedule。

### 风格

- **style(middleware)**: `DefaultSkipPaths` 4 行 whitespace 重对齐

### 测试

- backend: 818 → **827 passed** (+9 tests)
- 全量 28 包 0 fail
- 审计来源：`/Users/apple/study/ITmanager/docs/audit-v2.0.1.md` (待归档)

### 升级指引

- ✅ 0 DB migration (纯代码修复)
- ✅ 0 API 变更 (审计/事件总线字段不变)
- ✅ 0 行为破坏性变更

## [v2.0.1] - 2026-06-17

🔌 **gRPC 内部通信** — AlertService s2s + proto contract

### 新增

- **gRPC server** (`internal/grpcserver/alert_server.go`) — 内部服务间通信
  - 端口 `:50051` (env `GRPC_PORT` 覆盖)
  - 启动: `cmd/server/grpc.go` (独立 `startGRPCServer` 函数)
- **proto 定义** (`api/proto/alert/v1/alert.proto`)
  - `AlertService` 4 RPC: `ListAlerts` / `GetAlert` / `AckAlert` / `ResolveAlert`
  - 支持 v2.0 cursor 分页 + v1.x `page/size` 兼容
  - 完整 Severity / AlertStatus enum (1-4 / pending/acked/resolved)
- **生成代码**: `alert.pb.go` (771 行) + `alert_grpc.pb.go` (237 行)
- **模型适配**: `TriggerName/Problem/HostID/HostIP/TriggerID/ResolveTime/ResolveUser` → proto
- **10 单元测试** (`alert_server_test.go` + `test_helpers_test.go`)
  - cursor 满页/部分页 + 非法 cursor
  - severity/status 转换 (int↔enum, string↔enum)
  - id 必填校验

### 依赖

- `google.golang.org/grpc@v1.81.1` + `google.golang.org/protobuf@v1.36.11`
- `protoc-gen-go` + `protoc-gen-go-grpc` 安装路径: `~/go/bin/`

### 兼容性

- ✅ 不破坏既有 AlertService interface (gRPC 仅消费)
- ✅ 不破坏 REST API (gRPC 是新增, 不是替代)
- ✅ v2.0 cursor / v1.x page 模式同 RPC 内支持

### 客户端使用

```go
conn, _ := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := alertv1.NewAlertServiceClient(conn)
resp, _ := client.ListAlerts(ctx, &alertv1.ListAlertsRequest{Limit: 50})
```

### 明确不做 (单机 + 内部)

- ❌ gRPC-gateway (REST 已有, 重复)
- ❌ TLS/mTLS (单机内部, 127.0.0.1 only)
- ❌ 反射服务 (生产不暴露)

## [v2.0.0] - 2026-06-17

🚀 **主版本** — 性能与解耦 (cursor 分页 + event bus)

详见 [ADR-0002](docs/adr/0002-v2-scope.md) + [13-实施规划.md §21](13-实施规划.md)。

### 新增

- **cursor 分页** (`internal/cursor/cursor.go`) — 高效翻页 O(log N)
  - base64 + NUL 分隔的紧凑编码, 包含 (timestamp, id) 二元组
  - alert / ticket / audit 服务支持 cursor 入参
  - handler 接受 `?cursor=` + 响应 `next_cursor`
  - schema 加 `(created_at DESC, id DESC)` 联合索引 (alerts / tickets / audit_logs)
  - 老 `?page=N&size=M` 接口完全兼容 (cursor=null 时走 offset 模式)
- **event bus** (`internal/eventbus/bus.go`) — in-process pub/sub
  - 1024 buffer, 4 dispatcher goroutine, 3 retry + 100ms 退避
  - 5 topic: `alert.created` / `alert.resolved` / `ticket.created` / `ticket.resolved` / `user.locked`
  - SQLite DLQ (event_dlq 表), handler 失败 3 次后落死信
  - 优雅关闭 (Close 幂等), Stats 接口 (published / dispatched / dlq / retries / pending)
- **notification worker 接入 bus** (`internal/notification/worker.go`)
  - `SubscribeToBus(bus)` 注册 `alert.created` / `alert.resolved` handler
  - 双轨并行: 5s tick 老 path + event bus 新 path
- **AuditService 新建** (`internal/service/audit_service.go`) — 4 维过滤 + cursor
  - 端点: `GET /api/v1/audit-logs` (admin/debug)
- **AlertService 注入 bus** (`internal/service/alert_service.go`)
  - `WithBus(bus)` 注入
  - `Resolve` 调 `bus.Publish(alert.resolved)` 触发通知

### 数据库迁移

- `000010_v2_eventbus_cursor.up.sql`
  - 新增 `event_dlq` 表 (event_bus_id / topic / payload / last_error / failed_at)
  - `alerts` 加 `(created_at DESC, id DESC)` 联合索引
  - `tickets` 加 `(created_at DESC, id DESC)` 联合索引
  - `audit_logs` 加 `(created_at DESC, id DESC)` 联合索引

### 兼容性

- v1.x 客户端继续用 `?page=N&size=M` (cursor=null 走 offset 兼容) ✓
- DB schema 增量迁移 (新表 + 索引, 不破坏老数据) ✓
- 部署方式不变 (单 binary + sqlite/postgres) ✓

### 推迟到 v2.0.1+ (gRPC 风险评估后)

- ⏳ gRPC 内部通信 (v2.0.1, 估 12-15h, 风险评估中)

### 明确不做 (v2.0 scope 外)

- ❌ Redis 部署 (v1.4 in-process LRU 够, 推迟 v3.0)
- ❌ Kafka/NATS (永久不做, 单机不需要)
- ❌ 多实例部署 (推迟 v3.0)
- ❌ 全文搜索 ES (推迟 v3.0)
- ❌ PWA 移动端 (推迟 v2.1)

## [v1.4.0] - 2026-06-17

🔒 **次版本** — 后端稳定性 + 性能 + 可观测性 (rate limit / 通知 worker / 缓存 / 审计日志)

### 新增 Middleware / Service

- **rate limit middleware** (`internal/middleware/rate_limit.go`) — 滑窗限流
  - per-IP + per-path 维度, 内存 store (留 Redis 后端接口 v2.0)
  - 标准 headers: `X-RateLimit-Limit` / `Remaining` / `Reset` / `Retry-After` (429)
  - 路由级策略: login 5/min, password reset 3/min, protected 100/min
  - 18 routes 在 protected group 接入
- **notification worker** (`internal/notification/sender.go` + `worker.go`) — 异步真发
  - 三种 Sender: DingTalk / Email (SMTP+TLS) / Webhook
  - 5s tick 拉 pending 消息 (IN 查询避免 N+1), 30s per-msg 超时
  - stale-while-error: worker 挂掉不会丢消息, 启动时自动捞 pending
  - `channel_service.Test` 移除 stub, 走 Sender interface 真发
- **cache LRU** (`internal/cache/cache.go`) — 进程内缓存
  - `Cache interface` (Get/Set/Delete/GetOrLoad/Stats/Clear), 30s TTL
  - `GetOrLoad` stale-while-error: load 失败回 fallback, 命中走缓存
  - LRU 淘汰 + 并发安全
  - `NewDashboardServiceWithCache` 注入, dashboard stats 30s TTL
- **audit log middleware** (`internal/middleware/audit_log.go`) — 审计落库
  - 异步落 `models.AuditLog` (user_id / action / resource / status_code / ip / user_agent)
  - `SkipPaths` 跳过探针 (/healthz /metrics), `SkipMethods` 跳过 GET (可配置)
  - 写失败不影响业务响应 (fire-and-forget)
  - gorm `Session{SkipDefaultTransaction: true}` 避免无谓事务

### 测试

- 新增 81 tests:
  - rate limit: 11 (sliding window / headers / KeyFunc / 并发 / GC)
  - notification: 20 (HTTP 集成 / 错误路径 / worker 3 路径)
  - cache: 16 (TTL / LRU 淘汰 / GetOrLoad 5 场景)
  - audit log: 9 (落库 / 跳过 / 失败重试 / 状态码捕获)
  - database: +7 (autoMigrate / Init / 错误路径 / DSN)
  - middleware 辅助: 18 (split path / build entry / custom action)
- backend go test: 621 → **702** PASS (+81)
- vitest: 147 (未动)
- **总**: 768 → **849** tests
- **database 覆盖率**: 21.1% → **55.3%** (+34.2%)
- tsc: 0 errors (维持)

### 估时 vs 实测

| | 估时 | 实测 | 加速 |
|---|---|---|---|
| Batch 1 (3 项) | 13h | ~4.5h | 2.9x |
| Batch 2 (2 项) | 5h | ~1.5h | 3.3x |
| **v1.4 总** | **18h** | **~6h** | **3x** |

## [v1.3.0] - 2026-06-17

🎨 **次版本** — 中优先级 UX 改进 (路由元信息 + 状态展示一致性 + 响应式)

### 新增组件 / Hook

- **`useDocumentTitle`** (`src/hooks/useDocumentTitle.ts`) — 路由级 title 同步
  - 格式: `document.title = 'ITmanager - {page}'`
  - 卸载时还原 base, 避免 SPA 切换残留
  - 12 个 page 接入: 资产管理 / 告警中心 / 工单管理 / 仪表盘 / 故障 Runbook / 值班管理 / 系统设置 / 告警抑制 / 指标快照 / 资产诊断 / 机房机柜 / 网络拓扑
- **`SeverityTag`** (`src/components/SeverityTag.tsx`) — 严重度统一展示
  - P0-P5 六档配色 (gray/blue/gold/orange/red/magenta), 圆角 + 图标
  - AlertTable / Runbook 两处接入, 删除冗余 `SEVERITY_COLOR` map
- **`AppBreadcrumb`** (`src/components/AppBreadcrumb.tsx`) — 自动面包屑
  - 解析 `useLocation().pathname` + 路由表生成面包屑
  - 详情页 `:id` 参数转 `ID: <name>` (读 `useParams` + 资源 cache)
- **`useResponsiveTable`** (`src/hooks/useResponsiveTable.tsx`) — 响应式断点 hook
  - 用 AntD `Grid.useBreakpoint` 检 `{ xs }`, mobile (xs) 时表格 → 卡片列表
  - 配 `MobileCardList` 组件 (Stack + Card, 不引 antd-mobile 15MB 冗余)
  - 资产页接入, mobile 视口避免横向溢出
- **`CommandPaletteTrigger`** — Header 触发按钮 (🔍 搜索 ⌘K)
  - 配合 `useCommandPaletteStore` (zustand) 跨组件共享 open 状态
  - `useGlobalHotkey` 用 `useCommandPaletteStore.getState()` 防 stale closure

### 改进

- **批量操作进度** — Alerts 的批量 ack/resolve 从单次 API 改逐条 await
  - 后端 bulk endpoint 不可见进度, 改前端循环可显示 X/Y
  - 加 `Modal` + `<Progress percent={Math.round(done/total*100)}>` + 失败计数
  - trade-off: 网络请求 N 倍, N<100 可接受
- **键盘可达性** — Progress modal `closable={false}` 避免 Esc 中断后台进程

### 测试

- 新增 15 tests: useDocumentTitle 4 / SeverityTag 6 / AppBreadcrumb 5
- vitest: 128 → **147** PASS (+19)
- tsc: 0 errors (维持 v1.2.0 成果)
- backend go test: 621/621 (未动)

## [v1.2.0] - 2026-06-17

✨ **次版本** — UI/UX 易用性改进

### 新增组件

- **`EmptyState`** (`src/components/EmptyState.tsx`) — 统一空状态组件
  - 5 个 preset: `no-assets` / `no-alerts` / `no-tickets` / `no-racks` / `no-search-result`
  - 标题 + 描述 + 操作按钮三段式布局
  - 紧凑模式 (`compact` prop) 适配表格内嵌
  - 6 个 page 已接入: Alerts / Assets / Tickets / AlertSuppressions / Runbook / Topology
- **`LoadingSkeleton`** (`src/components/LoadingSkeleton.tsx`) — 统一加载占位
  - 5 个 variant: `table` / `kpi-cards` / `detail` / `chart` / `list`
  - 用 AntD `Skeleton` active 动画，首屏不抖
  - 路由级 `Suspense fallback` 改用 skeleton 替"加载中…"文字
- **`StatusPage`** (`src/pages/StatusPage.tsx`) — 通用状态页
  - 3 个导出: `NotFoundPage` (404) / `ForbiddenPage` (403) / `ServerErrorPage` (500)
  - 提供"返回上一页"+"返回首页"两个动作
  - 状态码色块: 4xx 蓝/紫 (用户侧), 5xx 红 (服务器侧)

### 改进

- **暗色模式 Header 配色** — Header 暗色模式用 `colorBgElevated`，与 Sider 形成反差（避免"中间割裂"）
- **Login 页美化** — 大 logo + 三色渐变背景 + 渐变 logo + 圆角阴影
  - 记住用户名 (Checkbox + localStorage)
  - 忘记密码链接 (placeholder)
  - 用 `useNavigate` 跳回 401 重定向目标 (替代 `window.location.href` 丢 state)
- **404 路由** — 不再静默跳首页，渲染 `<NotFoundPage>`

### 顺手修复 — 类型卫生 (24 → 0 tsc error)

6 个 test 文件补 `import '@testing-library/jest-dom'`，消掉 baseline 23 个 `toBeInTheDocument` 类型错。
`Oncall.tsx:142` `levelsJson` 字段加 cast `EscalationPolicy & { levelsJson?: string }`。

### 测试

- frontend: **128** / 128 pass (was 126, +2 Login 新增: 记住用户名 + 恢复)
- backend: 621 / 621 (no change)
- tsc: **0 errors** (was 24 baseline, -24 净消)

### 性能数据

- v1.2 估时 6h → 实测 **~1.5h** (4x 加速, 含 tsc 24→0 顺带改)
- 涉及 12 文件, 净增 3 组件 + Login 改写 + 5 test 加 import

## [v1.1.0] - 2026-06-17

✨ **次版本** — 代码审计 P1+P2 修复

### 修复 (P1 — v1.0.3 已 ship, 在 v1.1 累计)

- **API key `last_used_at` 异步批量写**: 1000 QPS → 1 UPDATE/30s
- **NetBox `SyncDevices` 分页 (100/page)**: 不再漏 50+ 设备
- **`SyncAll` `errors.Join` 合并失败**: 监控告警不再漏报
- **401 CustomEvent + `useNavigate`**: 保留路由 state + 登录跳回

### 改进 (P2)

- **M3-P2-3 错误类型化**: Create 撞 unique 约束 → `service.ErrAlreadyExists` → 409
  - 5 services + 5 handlers: asset/ticket/channel/runbook/alert_suppression
  - `isUniqueViolation()` helper (gorm ErrDuplicatedKey + SQLSTATE 23505)
- **M3-P2-1 分页索引**: 4 张表 `idx_*_created_at_desc` (assets/tickets/runbooks/users)
- **M3-P2-2 Bulk 事务**: `BulkAcknowledge/Resolve/Delete` 包 `gorm.Transaction`
- **M2-P2-1 Zabbix auth TTL + auto-relogin**: 30min TTL + 主动重登 + 检测 -10002 自动重试
- **M3-P2-4 Notification trigger**: Acknowledge/Resolve 落 `notification_logs` (pending)
  - 实际发送 (dingtalk/email) 由 v1.2 worker 消费
- **M4-P2-1 拦截器清理**: `api.ts` 空壳请求拦截器删除 (since C-F5 cookie auth)
- **M1-P2 ADR-001 rate limit 归属**: 4 方案对比，推荐 per-route middleware (v1.2 实现)

### 数据库

- `000008_list_pagination`: assets/tickets/runbooks/users `created_at DESC` 索引
- `000009_notification_logs`: notification_logs 表 + 2 索引

### 测试

- backend: **621** / 621 pass (was 608, **+13**)
- frontend: 126 / 126 pass (no change)
- tsc: 24 = baseline (0 new)

### 文档

- `docs/adr/0001-rate-limit-归属.md`

## [v1.0.2] - 2026-06-17

🐛 **补丁版** — 文档改进

### 改进

- **README.md**: 加 6 个 GitHub badges
  - Release / CI / License / Go / React / Docker
  - 顶部状态从 6/16 推进到 6/17 v1.0.2
- 快速开始章节用 `make deploy` 重写（替代旧 `make run` 路径）

[v1.0.2]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.2

## [v1.0.1] - 2026-06-17

🐛 **补丁版** — 一键部署便利性

### 改进

- **Makefile**: 新增 3 个一键部署 target
  - `make deploy` — install + docker-up + db-migrate + db-seed（含演示数据）
  - `make deploy-min` — 同上但无种子（生产环境首次部署）
  - `make deploy-status` — 8 服务健康检查（PG/Redis/API/Web/NetBox/Zabbix/GLPI/Graylog）
- 私有 `_wait_for_pg` target 轮询 PG 60s（防 migrate 抢跑）
- 链式依赖自动按序，**任一失败立即停下**

[v1.0.1]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.1

## [v1.0.0] - 2026-06-17

🎉 **首个稳定版本** — 10 个 PR 落地，覆盖 P0-P2 全链路

### 重大功能 (P0)

| 模块 | 端点 | 说明 |
|---|---|---|
| 资产诊断 | `GET /diagnostics/assets/{id}/timeline` | 聚合 alerts/tickets/status 历史 |
| 告警抑制 | `POST /suppressions` | 规则引擎（去重/静默/抑制） |

### 重要功能 (P1)

| 模块 | 功能 | 说明 |
|---|---|---|
| 值班升级 (P1-2) | 值班+升级引擎 | oncall 表 + 升级策略 |
| 拓扑图 (P1-1) | 网络拓扑可视化 | 一龙开发 |

### 次要功能 (P2)

| 模块 | 功能 | 说明 |
|---|---|---|
| 故障 Runbook (P2-1) | Runbook 引擎 | 故障自动恢复 |
| 指标快照 (P2-2) | Zabbix 兜底 | 离线数据降级 |

### 小改进 (S 级 / 6/17)

| 改进 | 内容 | Commit |
|---|---|---|
| 暗色模式 (S-1) | Antd darkAlgorithm + zustand persist | `ab05d3d` |
| 误报 ML (S-2) | 标记误报 + CSV 训练集导出 | `e502701` |
| Cmd+K (S-3) | 全局搜索跨资源（资产/告警/工单） | `f7e98eb` |

### 小改进 (A 级 / 6/17)

| 改进 | 内容 | Commit |
|---|---|---|
| A-1 网络探活 | ICMP ping + traceroute + exec.LookPath 验证 | `6edd9c9` |
| A-2 复盘 PDF | 嵌入霞鹜文楷 TC (OFL, 15MB) 中文 PDF + io.Writer 流式 | `ea70644` |
| A-3 KPI 仪表盘 | MTTR/MTTD/告警密度/SLA + 阈值常量 | `2b893bc` |
| A 级 review | traceroute dead code + binary 验证 + 字体 + 流式 + KPI sqlmock | `3a667e8` |

### 测试指标

| 指标 | 数值 |
|---|---|
| 后端 packages | 22 |
| 后端测试 | 603 pass |
| 前端 test files | 19 |
| 前端测试 | 117 pass |
| 覆盖率 | 61.2% |
| tsc baseline | 24 errors (0 new from baseline) |

### 性能数据

| 模块 | 估时 | 实测 | 加速 |
|---|---|---|---|
| S 级 3 项 | 4.5h | 3.5h | 1.3x |
| A 级 3 项 | 9h | 2.75h | 3.3x |
| A 级 review | 1h | 0.5h | 2x |
| **总计** | **14.5h** | **6.75h** | **2.1x** |

### 已知限制

- 中文 PDF 字体 15MB 嵌入二进制（binary 增大约 15MB）— 见 `assets/FONT-LICENSE.txt`
- KPI 阈值硬编码（`KPI_THRESHOLDS`）— v1.1 改为环境变量注入
- tsc 24 errors 为 baseline（testing-library `toBeInTheDocument` 类型 + 1 Oncall levelsJson），非本次引入

### 安装/升级

```bash
# 拉取 v1.0.0
git checkout v1.0.0

# 后端
cd backend && go mod download && go run cmd/server/main.go

# 前端
cd frontend && npm install && npm run dev
```

[v1.0.0]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.0
