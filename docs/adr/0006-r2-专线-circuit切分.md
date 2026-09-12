# ADR-0006: R2 专线字段级切分与单一写入方

**状态**: ✅ 已采纳 (2026-09-12)
**日期**: 2026-09-12
**决策者**: 主人 (燕如) + 助手
**关联**: [ADR-0003](0003-grpc-作废与三层定位.md)（三层定位基线）、[v3 §3 R2](../v3-架构优化需求.md)、[02-资产管理.md §2.7](../../02-资产管理.md)、[`docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md`](../FIX-PLAN-R2-NETBOX-CIRCUITS.md)

---

## 背景

v1/v2 时期设计了 `lines` / `line_types` / `line_changes` / `line_monitors` 四表（`docs/schema-planned.sql:39-125`），字段比 NetBox Circuits 更贴国内运维场景（运营商故障电话 `fault_phone`、业务归属 `business_unit`、合同期/月租等）。但 **0 行 Go 代码**，且字段实际分两类：

|| 类别 | 字段示例 |
|---|---|
| **物理真值** | `bandwidth` / 端点 A/B（机房/机柜/设备/接口/VLAN） |
| **商务元数据** | `contract_no` / `contract_start` / `contract_end` / `monthly_fee` / `fault_phone` / `business_unit` / `purpose` / `is_critical` |

v3 §3 R2 决策：废弃自建四表，ITmanager 只读消费 NetBox Circuits。但 v3 §7 自审 #1 显式指出：**字段级切分表本文档尚未给出 → 落到 `02-资产管理.md` 时必须补齐**。否则实施时容易出现"双写到 NetBox Custom Field"反模式（违反 ADR-0003 单一写入方原则）。

## 决策

1. **物理真值归 NetBox Circuits（单一写入方）**：NetBox `Circuit` + `CircuitTermination` 表达电路、A/Z 端点、带宽、Provider、status、install_date。ITmanager 通过 `internal/integration/netbox.go` 只读 `/api/circuits/`，缓存到本地 `circuit_cache` 表。
2. **商务元数据归 ITmanager `line_meta` 视图**：合同号 / 合同期 / 月租 / 故障电话 / 业务归属 / 关键性 — **不向 NetBox Custom Field 写**，原因是 Custom Field 不支持复杂校验 / 跨表查询 / 变更审计。
3. **关联键硬约束 = `circuit.id` (int)**：`line_meta.circuit_id` → NetBox `Circuit.id`。**不做双写**——任何"两边都更新"的设计都违反本 ADR。
4. **审计分轨**：物理真值变更由 NetBox changelog 保留（不可篡改）；运维视角变更（如业务归属调整）写入 ITmanager `audit_logs`，不复用 `line_changes` 表。
5. **监控复用 Zabbix**：专线监控通过 Zabbix host template 表达，与设备监控统一链路；不建 `line_monitors` 表。

完整字段级切分表见 [`02-资产管理.md` §2.7.2](../../02-资产管理.md)。

## 被本 ADR 修订

| 来源 | 内容 | 处置 |
|---|---|---|
| `02-资产管理.md` 原 §2.7 | 「自建 lines vs NetBox Circuits」对比表 + 简陋字段切分表 | **作废**，由 v3 R2 重写版取代（含完整 A/B/C 三组字段级切分 + join key 总表） |
| `02-资产管理.md` 原 §2.7.4 注 | 「合同字段若能由 NetBox Custom Field 表达则统一进 NetBox」 | **作废**：本 ADR 第 2 条明令不写 NetBox Custom Field（理由见 Risk-1） |
| `docs/schema-planned.sql:39-125` | `lines` / `line_types` / `line_changes` / `line_monitors` | **保留作历史**（按 v3 §5「只归档不删除」原则），不实施 |

## 影响

- `02-资产管理.md` §2.7 完全重写为 v3 R2 视角，含字段级切分表 + 单一写入方约束。
- `line_meta` 字段描述在 `02-资产管理.md §2.7.2 B 组` 给出；具体表/视图 DDL 推迟到 NetBox 部署任务（**本 ADR 不约束 DDL 形态**）。
- NetBox 侧建模与端到端实施（ITmanager `circuit_cache` / 只读视图 / 前端页）独立任务，本 ADR 只定边界。

## Risk

|| # | 失败模式 | 缓解 |
|---|---|---|---|
| 1 | **双写反模式**：后续有人把 `contract_no` / `monthly_fee` 写到 NetBox Custom Field，单一写入方被破坏，且 Custom Field 不支持复杂校验 | 本 ADR 第 2/3 条硬约束；`02-资产管理.md §2.7.1` 再次声明；code review 必查 |
| 2 | **`circuit_id` 类型漂移**：原 `lines.id` 是 UUID，NetBox `circuit.id` 是 int，关联键需转换 | `02-资产管理.md §2.7.3` 明确写 `int`；实施时用 `circuit_cache.netbox_id INT UNIQUE` 收口 |
| 3 | **NetBox 部署延迟 → 专线功能卡住** | R2 是 docs-only；端到端实施随 NetBox 部署任务，**不阻塞** v3 R2 文档落地 |
| 4 | **商务元数据缺失导致运维视角断片**（如查不到运营商故障电话） | `02-资产管理.md §2.7.2 B 组` 显式列出全部字段；验收 A-NETBOX-1 必查 |