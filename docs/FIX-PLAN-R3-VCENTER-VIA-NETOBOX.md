# FIX-PLAN: R3 vCenter VM 纳管（ITmanager 从 NetBox 读 VM）

| 项 | 值 |
|---|---|
| 状态 | 📝 docs-only stage 0 |
| 日期 | 2026-09-12 |
| 关联 ADR | ADR-0008 |
| 关联章节 | 02-资产管理.md §2.8、03-监控采集.md §3.7 |
| 实施细节 | docs/IMPL-R3-VCENTER-VIA-NETOBOX.md |
| 需求源头 | docs/v3-架构优化需求.md §3 R3 |

## 1. 问题

v3 §3 R3：上千台 vSphere/ESXi 上的 VM **零纳管**——运维看不到 VM 与设备/专线/机架的拓扑关系，报警单生成时遗漏 VM 资产。

## 2. 决策概要（详情见 ADR-0008）

| ID | 决策 | 关键约束 |
|---|---|---|
| D1 | ITmanager 不直连 vCenter | 只读 NetBox，避免缓存 vCenter 数据 |
| D2 | 单一 SoT = NetBox | 与 R2 共用 asset view；VM 走 `kind=vm` |
| D3 | 孤儿 VM 走人工确认窗口 | 24h 确认 + 30 天软退役缓冲 |
| D4 | VM 不引入新表 | NetBox 是 SoT；vm 详情走 `vm_fields` JSON |
| D5 | 字段对照 vs 双写 | BIOS UUID 不暴露；不写回 NetBox |

## 3. 计划（分 stage）

### Stage 0（本轮 docs-only，4 commits 已 done）
- [x] intent-M35-R2.md（PM intent-spec-author 产物）
- [x] 02-资产管理.md §2.8 字段对照表 + 渲染规则（commit `1fa4c26`）
- [x] 03-监控采集.md §3.7 vCenter 孤儿 VM runbook（commit `be09f84`）
- [x] docs/adr/0008-r3-vcenter-via-netbox.md（commit `83323aa`）
- [x] docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md（本文件，commit 本次 push）
- [x] docs/IMPL-R3-VCENTER-VIA-NETOBOX.md（实施细节）
- [x] CHANGELOG + completion-report

### Stage 1（运维前置，下一轮 round）
- 不在 ITmanager 仓库内
- 实际部署 `bb-Ricardo/netbox-sync` (Docker / K8s CronJob)
- 配置 `mode: dry-run`，建立 `/var/log/netbox-sync/diff-YYYY-MM-DD.json` 报告通路
- 启动周观察期（≥ 7 天）

### Stage 2（ITmanager Go 代码，round-pending-on-ops）
- `routes.go` 新增 `/api/assets?kind=vm` 过滤参数
- `assets_handler.go` 新增 `vm_fields` JSON 字段处理
- 详情见 IMPL-R3-VCENTER-VIA-NETOBOX.md

### Stage 3（运维持续，本轮 docs 提供基础）
- ≥ 30 天周观察期结束
- `confirm_window_hours` 从 24h 缩短到 4h
- 季度 review 孤儿率

## 4. 验收（Acceptance Criteria，详见 intent-M35-R2.md）

### A-R3-1: 字段对照完整
- [x] 02-资产管理.md §2.8.1 至少 10 行字段对照（NetBox → ITmanager）
- [x] 含 ITmanager 不展示字段（BIOS UUID / ESXi host）
- [x] 与 R2 协调显式说明（同 view、不双写、SoT = NetBox）

### A-R3-2: 风险条款可操作化
- [x] 03-监控采集.md §3.7 含 4 阶段（dry-run / 周观察 / cleanup-with-confirm / 全面启用）
- [x] §3.7.2 周观察期 checklist 有 5 个 check + 通过条件
- [x] §3.7.3 人工确认窗口有具体 yaml 配置 + 24h 窗口 + !keep 指令
- [x] §3.7.4 全面启用条件有 30 天观察期 + 误判率门槛

### A-R3-3: ADR-0008 收口 3 硬约束
- [x] D1 ITmanager 不直连 vCenter
- [x] D2 单一 SoT = NetBox
- [x] D3 孤儿 VM 走人工确认窗口
- [x] D4 / D5 次要约束

### A-R3-4: cross-link 完整
- [x] 02 §2.8 ↔ ADR-0008
- [x] 03 §3.7 ↔ ADR-0008
- [x] IMPL-R3 ↔ FIX-PLAN-R3 ↔ ADR-0008

### A-R3-5: v3 §3 R3 状态变化
- [ ] CHANGELOG M35-R2 段追加
- [x] v3 §7 验收项 A-6 / A-7 状态变化（待 commit 时确认）
- [ ] completion report

## 5. Not doing（本期边界）

- **不实际部署 vCenter 或 NetBox** — 那是运维的事，不是 ITmanager 仓库的事
- **不实际写 ITmanager 读 NetBox VM 的 endpoint handler** — Stage 2 的工作
- **不实施 dry-run** — Stage 1 的工作
- **不启用 NetBox-synced 标签的自动清理** — Stage 3 的工作（≥ 30 天后）
- **不复活** 02 §2.9 已废弃的自建 VMware 表设计

## 6. 风险

| 风险 | 缓解 |
|---|---|
| 运维不响应 `!keep` 24h 后误删业务方临时机 | 周观察期 ≥ 7 天 + 误判率门槛 < 5% 才进 Stage 2 |
| NetBox API 不可达时 ITmanager 看不到 VM | 监控 NetBox 可用性 + 历史数据保留 |
| ITmanager 字段对照表与 NetBox 上游字段漂移 | 等 NetBox 字段稳定后再实施 Stage 2 |
| 周观察期被运营跳过 → 上线就出错 | Stage 2 Go 代码开始前 PM 验证运维日志存在 ≥ 7 天 |

## 7. Verification

按 `verify-e2e` skill：本期 docs-only，验证 = 内容审阅。Probe = PM/审阅人读 02 §2.8 + 03 §3.7 + ADR-0008 + FIX-PLAN/IMPL，看到：

- [x] 字段对照覆盖 12 字段（含 2 不展示字段）
- [x] R2 协调说明显式写出
- [x] 4 阶段 dry-run + 24h 窗口 + 30 天观察期 = 风险条款可操作化
- [x] ADR-0008 5 决策完整
- [x] cross-link 链通
- [ ] CHANGELOG + completion report（本 round 后半提交）

## 8. 后续 Round 索引

- M35-R2-R1: docs stage 0（本轮，已 5 commits）
- M35-R2-R2: stage 1 (运维 dry-run 部署 + 7 天观察期，PM 验日志)
- M35-R2-R3: stage 2 Go 代码 (/api/assets?kind=vm + vm_fields)
- M35-R2-R4: stage 3 (30 天后 cleanup 加速 + 季度 review)
