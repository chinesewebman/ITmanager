# Changelog

ITmanager 项目所有重要变更记录。版本遵循 [SemVer](https://semver.org/)。

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
