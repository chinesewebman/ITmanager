# 修复方案：R2 — 专线归属 NetBox Circuits（v3 改造点二）

> 状态 **draft** · 日期 2026-09-12 · 作者 hermes@local
> 依据：[`docs/v3-架构优化需求.md`](../v3-架构优化需求.md) §3 R2 + §7 自审 #1
> 根因证据：[`docs/v3-架构优化需求.md`](../v3-架构优化需求.md) §1 P-3（`lines`/`line_types`/`line_changes`/`line_monitors` 字段比 NetBox Circuits 更贴运维场景，**但 0 行 Go 代码**，且游离在资产图之外）
> 前置：R5（文档与仓库卫生，commit 已落 `main`）——本轮是 R5 之后的第一个 v3 改造点落地
> 关联：[`docs/v3-架构优化需求.md` §3 R2](../v3-架构优化需求.md)、[`02-资产管理.md` §2.7](../../02-资产管理.md)、[`docs/adr/0006-r2-专线-circuit切分.md`](../adr/0006-r2-专线-circuit切分.md)（如新增）
> 不在本轮：NetBox 部署与建模实施本身（NetBox 部署独立任务；本轮只把 ITmanager 侧的"字段级切分"与"自建表归档"写定）

---

## 1. 问题（What / Why）

### 1.1 P-3：专线表设计得好但没做，且与 NetBox Circuits 重复

v1/v2 时期设计文档（`02-资产管理.md` 旧版、`schema.sql`/`docs/schema-planned.sql`）规划了 `lines` 系列四表，字段比 NetBox Circuits 更贴国内运维场景（运营商故障电话 `fault_phone`、业务归属 `business_unit`、合同期 `contract_start`/`contract_end`、月租 `monthly_fee`）。但 2026-09-09 实测：

|| 事实 | 证据 |
|---|---|---|
| **零 Go 代码引用** | `rg -l "models.Line" backend/` 仅命中迁移/sql，**handler/service/route/test 全无** |
| **游离在资产图之外** | `lines.endpoint_a_device` 是 `VARCHAR(255)` 字符串，与 `assets.id` 无 FK；`line_changes`/`line_monitors` 同理 |
| **与 NetBox Circuits 重复** | NetBox 原生 `Circuit` + `CircuitTermination` 支持端点 → 站点/Provider Network 的拓扑关联，自建版本无法复用 |

v3 §3 R2 决策：**废弃自建 `lines` 系列四表；ITmanager 只读消费 NetBox Circuits**（见 [v3 文档 §3 R2](../v3-架构优化需求.md)）。

### 1.2 自建版本的次生风险：商务元数据无家可归

`lines` 系列四表的字段实际分两类：

|| 类别 | 字段示例 | 归属判断 |
|---|---|---|---|
| **物理真值** | `bandwidth`/`bandwidth_mbps`/`endpoint_a_idc_id`/`endpoint_a_rack_id` | NetBox 已经覆盖且更标准 |
| **商务元数据** | `contract_no`/`contract_start`/`contract_end`/`monthly_fee`/`fault_phone`/`business_unit`/`purpose`/`is_critical` | NetBox Circuits **未覆盖**，需 ITmanager 承载 |

如果只废弃自建表 → 商务元数据丢失，运维视角断片；
如果双写到 NetBox Custom Field + ITmanager 本地表 → 单一写入方原则被破坏，且 NetBox Custom Field 不支持复杂校验 / 变更审计 / 跨表查询。

**根因**：v3 §3 R2 在文档里只画了"NetBox 存什么 / ITmanager 存什么"的思路，**未落到字段级**。v3 §7 自审 #1 自行点出此问题：

> R2 的字段级切分表本文档尚未给出 → 落到 `02-资产管理.md` 时必须补齐。

### 1.3 本轮是 docs-only：schema 已就位（实测）

`docs/schema-planned.sql` 已含 `lines`/`line_types`/`line_changes`/`line_monitors` 四表（共 39 张 planned 表的子集）；`schema.sql`（16 张 live 表）**不**含该四表——R5 阶段已把规划表整体归入 `docs/schema-planned.sql`。实测命令：

```console
$ grep -cE '^CREATE TABLE lines|^CREATE TABLE line_types|^CREATE TABLE line_changes|^CREATE TABLE line_monitors' schema.sql
0
$ grep -cE '^CREATE TABLE lines|^CREATE TABLE line_types|^CREATE TABLE line_changes|^CREATE TABLE line_monitors' docs/schema-planned.sql
4
```

**故本轮无 schema 移动**，仅 doc 落地。

---

## 2. 方案（How）

### 2.1 总体形态

```text
NetBox (Circuit + CircuitTermination + Provider + ProviderNetwork)
   │  /api/circuits/  （ITmanager 通过 internal/integration/netbox.go 只读消费）
   ▼
ITmanager 专线页 ──── 只读渲染（A/Z 端点、带宽、电路类型、运营商）
   │ circuit_id (FK 关联，无双写)
   ▼
ITmanager `line_meta` 只读视图（合同期/月租/故障电话/业务归属/关键性）—— 本轮不建表，仅在 02-资产管理.md 描述承接形态
```

**单一写入方 = NetBox**。ITmanager 侧所有"专线"数据要么是 NetBox 镜像缓存（短期、最终一致），要么是商务元数据（以 `circuit_id` 为关联键）。**不做双写**。

### 2.2 字段级切分表（v3 §7 自审 #1 落地）

下表覆盖 `lines` / `line_types` / `line_changes` / `line_monitors` **全部字段**（来源：`docs/schema-planned.sql:39-125`）：

#### 2.2.1 物理真值 → NetBox Circuits

| 原 `lines` 字段 | NetBox 落点 | 说明 |
|---|---|---|
| `id` (UUID) | NetBox 自动生成 | ITmanager 以 NetBox `circuit.id` (int) 为外键关联键 |
| `line_no` (`VARCHAR(50) UNIQUE`) | `Circuit.cid` (`VARCHAR(100)`) | NetBox 唯一标识；保留"业务编号"语义 |
| `line_type_id` | `Circuit.type` (FK → `CircuitType`) | NetBox CircuitType 已含 MPLS/VPN/Internet 等等 |
| `carrier` | `Circuit.provider` (FK → `Provider`) | NetBox Provider 实体 |
| `bandwidth` (`VARCHAR(30)`) | `CircuitTermination.port_speed` (int, Kbps) | NetBox 用整数 Kbps，更标准 |
| `bandwidth_mbps` (`INTEGER`) | 同上（合并表达） | |
| `endpoint_a_idc_id` | `CircuitTermination.site` (FK → Site) | NetBox Site |
| `endpoint_a_rack_id` | `CircuitTermination.termination_[abc]_rack` (FK → Rack) | NetBox Rack |
| `endpoint_a_device` | 通过 Cable 关联 Device | NetBox Cable 拓扑更精确 |
| `endpoint_a_interface` | `CircuitTermination.termination_[abc]_device` + Cable → Interface | |
| `endpoint_a_vlan` | `Interface.untagged_vlan` / `tagged_vlans` | |
| `endpoint_b_*` | 同 endpoint_a_* 的 Z 端 | NetBox Circuit **恰好两个端点**（A/Z） |
| `status` (`active`/`disabled`/`decommissioning`) | `Circuit.status` (枚举：`active`/`planned`/`provisioning`/`offline`/`decommissioning`) | 字段对齐度高 |
| `install_date` | `Circuit.install_date` (DATE) | |

> **特别说明**：`endpoint_a_device` / `endpoint_a_interface` 在 NetBox 里不是 Circuit 的直接字段，而是通过 Cable 关联到 Device.Interface。NetBox 自动化发现时由 Cable 反映"这条物理专线接在哪台设备的哪个端口"，比自建字符串列强。

#### 2.2.2 商务元数据 → ITmanager 视图（NetBox 不覆盖）

| 原 `lines` 字段 | ITmanager 落点 | 为什么不进 NetBox |
|---|---|---|
| `contract_no` | `line_meta.contract_no` | 合同号是法务/财务字段，与资产真值无关 |
| `contract_start` / `contract_end` | `line_meta.contract_period` | NetBox 不管理合同生命周期 |
| `monthly_fee` (`DECIMAL(10,2)`) | `line_meta.monthly_fee` | 财务字段，不属于 CMDB |
| `contact_person` / `contact_phone` | `line_meta.carrier_contact` | 客户经理是商务关系，非拓扑 |
| `fault_phone` (`VARCHAR(20)`) | `line_meta.fault_hotline` | 运营商故障电话是运维速查项，需独立查询 |
| `purpose` (`VARCHAR(200)`) | `line_meta.business_purpose` | 业务用途为运维视角，与 NetBox 拓扑语义不同 |
| `business_unit` (`VARCHAR(50)`) | `line_meta.business_unit` | 业务归属按公司部门，非资产真值 |
| `is_critical` (`BOOLEAN`) | `line_meta.criticality` | 关键性是 SLO 视角，由 ITmanager 评 |

**关联形式**：`line_meta.circuit_id` (int, FK 概念) → NetBox `Circuit.id`。NetBox 端以 Webhook/周期同步将 circuit 列表拉到 ITmanager 侧 `circuit_cache`（只读），`line_meta` 与 `circuit_cache` 通过 `circuit_id` 关联。**不向 NetBox Custom Field 写**。

#### 2.2.3 审计与监控 → 拆分

| 原表 | 原字段 | 处置 |
|---|---|---|
| `line_changes` | `change_type`/`change_field`/`old_value`/`new_value`/`reason`/`operator_id`/`changed_at` | **物理真值变更**由 NetBox changelog 保留（不可篡改）；**运维视角变更**（如"业务归属从 A 部门调到 B 部门"）写入 ITmanager `audit_logs`，schema 已存在，无需新增表 |
| `line_monitors` | `monitor_type`/`target`/`interval`/`is_enabled` | **不建表**。专线监控通过 Zabbix host template 表达（与设备监控同链路），配置写 Zabbix，ITmanager 通过 `/api/v1/monitoring/hosts` 只读查询。已存在监控链路无新增表需求 |
| `line_types` | `code`/`name`/`description` | **不建表**。NetBox `CircuitType` 实体已覆盖 |

#### 2.2.4 join key 总表

| 关联关系 | 键 | 方向 |
|---|---|---|
| NetBox Circuit ↔ ITmanager `circuit_cache` | `circuit.id` (int) | NetBox → ITmanager（周期/Webhook 拉取） |
| ITmanager `circuit_cache` ↔ `line_meta` | `circuit_id` | 同库 join |
| ITmanager `audit_logs` ↔ NetBox Circuit | `circuit_id` + 时间窗 | ITmanager → NetBox（运维视角变更时反查原始变更） |

---

## 3. 验收标准

|| 编号 | 验收项 | 判定方法 |
|---|---|---|---|
| **A-NETBOX-1** | `02-资产管理.md` §2.7 完全重写为 v3 视角，**含完整字段级切分表**（物理真值 → NetBox；商务元数据 → ITmanager；审计 → ITmanager `audit_logs`；监控 → Zabbix） | 逐字段对照 `docs/schema-planned.sql:39-125` 的原 `lines`/`line_types`/`line_changes`/`line_monitors` 字段，无遗漏 |
| **A-NETBOX-2** | `schema.sql`（live）**不含** `lines`/`line_types`/`line_changes`/`line_monitors` 四表 | `grep -E '^CREATE TABLE lines\|^CREATE TABLE line_types\|^CREATE TABLE line_changes\|^CREATE TABLE line_monitors' schema.sql` → 0 行 |
| **A-NETBOX-3** | `docs/schema-planned.sql` **含**上述四表（已就位，本轮不移动） | `grep -cE '...lines\|line_types\|line_changes\|line_monitors' docs/schema-planned.sql` ≥ 1 |
| **A-NETBOX-4** | `docs/v3-架构优化需求.md` §3 R2 段或本 fix-plan 引用 ADR-0006 链接（如新建）或内联切分表 | 文本搜索 `ADR-0006` 或 `字段级切分` |
| **A-NETBOX-5** | `gofmt -l .` 输出为空（无代码改动，文档不应触发 gofmt） | 命令直跑 |
| **A-NETBOX-6** | `go test ./... -count=1` exit 0（sanity，文档改动不影响） | 命令直跑 |
| **A-NETBOX-7** | `git diff --stat` 仅含本轮 in-scope 文件：`docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md`（new）、`02-资产管理.md`（modified）、可选 `docs/adr/0006-r2-专线-circuit切分.md`（new） | 直跑 |

---

## 4. 明确不做（NOT doing）

按 v3 §5 已声明清单 + R2 专属不划：

| 不做 | 理由 |
|---|---|
| 自建 `lines` CRUD（含 handler/service/route/test） | 与 NetBox Circuits 重复（v3 §5 R2 行） |
| 双写到 NetBox Custom Field | 单一写入方原则；Custom Field 不支持复杂校验 |
| 实施 NetBox 部署本身 | NetBox 部署独立任务，本轮只定边界 |
| 实现 ITmanager 专线只读视图（前端/API/service/handler） | 本轮 docs-only；端到端实施随 NetBox 部署任务 |
| 把 `line_meta` 落到实际 DDL | 商务元数据**字段描述**写进 `02-资产管理.md`，具体表/视图的 DDL 推迟到 NetBox 部署任务 |
| 用 NetBox Webhook 实现实时同步 | Webhook 复杂度（去重、丢包补偿、回放）超出 R2 范围；先 polling |
| `line_monitors` 的 ITmanager 自建版本 | 监控走 Zabbix host template，与设备监控统一链路 |

---

## 5. 实施

### 5.1 docs 改动（in-scope files）

| 文件 | 动作 | 内容要点 |
|---|---|---|
| `docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md` | **新建** | 本文件 |
| `02-资产管理.md` §2.7 | **重写** | 替换原 §2.7（"自建 vs NetBox 对比表" + 简单字段切分表）为：完整字段级切分表（§2.2 全表复制）；新增 §2.7.5"join key 与审计"、§2.7.6"明示不做" |
| `docs/adr/0006-r2-专线-circuit切分.md` | **新建（可选）** | ~30 行 ADR，记录"为什么不双写到 NetBox Custom Field"决策与"join key = circuit_id"硬约束 |

### 5.2 schema.sql / schema-planned.sql 改动

**本轮无改动**——已实测 4 张 `lines*` 表全部在 `docs/schema-planned.sql`，`schema.sql` 不含（§1.3）。若后续 R3/R5 续轮再做"live 表清点"复查，本任务不受影响。

### 5.3 02-资产管理.md §2.7 重写要点

新版本结构（替换原 §2.7 全文 + 原 §2.7.4 待确认注释）：

```
### 2.7 专线建模（v3 R2）

**结论**：专线以 NetBox `Circuit` + `CircuitTermination` 建模，ITmanager 只读渲染；
自建 `lines` 系列四表已归档（`docs/schema-planned.sql:39-125`），不实施。

#### 2.7.1 数据流
[ASCII 图：NetBox Circuits → /api/circuits/ → ITmanager circuit_cache → ITmanager 只读页]
[商务元数据 → ITmanager line_meta 视图，以 circuit_id 关联，不做双写]

#### 2.7.2 字段级切分
[物理真值 → NetBox：circuit_id / A-Z 端点 / 带宽 / 电路类型 / 运营商 / status / install_date]
[商务元数据 → ITmanager：contract_no / contract_period / monthly_fee / fault_hotline /
 business_purpose / business_unit / criticality]
[审计 → ITmanager audit_logs；监控 → Zabbix host template（与设备监控统一链路）]

#### 2.7.3 join key
`circuit.id` (int) 为唯一关联键。NetBox 周期/Webhook 拉到 ITmanager circuit_cache，
line_meta 以 circuit_id = circuit.id 与 cache 关联。

#### 2.7.4 MPLS VPN 建模硬限制
NetBox 只建物理电路；MPLS 每条腿一条 Circuit 终止在 Provider Network，
VPN 逻辑拓扑用 Tag / Custom Field / 外部文档表达（与原 §2.7.3 一致）。

#### 2.7.5 明确不做
- 不自建 lines CRUD（与 NetBox 重复）
- 不双写到 NetBox Custom Field（单一写入方）
- 不建 line_monitors（监控走 Zabbix）
- 不实施 NetBox 部署本身（独立任务）
```

### 5.4 ADR-0006 可选要点（如选择创建）

```markdown
# ADR-0006: R2 专线字段级切分与单一写入方

**状态**: ✅ 已采纳 (2026-09-12)
**日期**: 2026-09-12
**关联**: ADR-0003（三层定位）、v3 §3 R2、02-资产管理.md §2.7

## 背景
[v3 R2 段 + P-3 摘录]

## 决策
1. 物理真值归 NetBox Circuits（单一写入方）
2. 商务元数据归 ITmanager line_meta，以 circuit_id (int) 关联，**不做双写**
3. 审计归 ITmanager audit_logs（运维视角），原始变更归 NetBox changelog
4. 监控走 Zabbix host template，不建 line_monitors

## 被本 ADR 修订
- 02-资产管理.md §2.7（含原 §2.7.4 待确认注释 → 已确认）

## Risk
[双写风险：NetBox Custom Field 不支持复杂校验 / 跨表查询 → 单一写入方原则]
```

---

## 6. 自审 Risk

|| # | 失败模式 | 缓解 |
|---|---|---|
| **R-1** | 字段级切分表漏字段 → 实施时发现某字段无家可归 | §2.2 显式对照 `docs/schema-planned.sql:39-125` 全部字段；A-NETBOX-1 验收项要求"逐字段对照，无遗漏" |
| **R-2** | 商务元数据缺失导致运维视角断片（如查不到运营商故障电话） | `line_meta` 字段列表在 §2.2.2 显式给出；A-NETBOX-1 验收时同步核对 |
| **R-3** | 后续有人向 NetBox Custom Field 双写商务字段 → 单一写入方被破坏 | ADR-0006（如建）显式记录；v3 §3 R2 风险段已点出"Custom Field 承载不了复杂合同语义" |
| **R-4** | NetBox 部署延迟 → 专线功能卡在 NetBox 依赖上 | R2 是 docs-only，**不等** NetBox；实施随 NetBox 部署任务一起做 |
| **R-5** | MPLS 物理建模与运维视角不匹配（运维记的是 VPN 拓扑，不是物理腿） | §2.7.4 显式说明 VPN 拓扑用 Tag / Custom Field / 外部文档表达；与 v3 §3 R2 R-段一致 |
| **R-6** | `line_meta` 字段被当成"待落 DDL 的表"提前实现 | §4 明确不做"line_meta DDL 落地"；推迟到 NetBox 部署任务，避免本轮越界 |
| **R-7** | docs 改动意外影响 gofmt / 测试 | A-NETBOX-5/6 显式验收；本轮纯文本，无代码 |
| **R-8** | 误改 schema.sql（哪怕一行也算越界） | §5.2 显式"无改动"；A-NETBOX-2 显式验收"schema.sql 不含 lines*"；如发现 schema.sql 含 lines* → 拒绝自决，halt 报告 |

---

## 7. Where（含文件 diff 预期）

| 文件 | 动作 | 预期行数 |
|---|---|---|
| `docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md` | 新建 | ~180 行（本文件） |
| `02-资产管理.md` §2.7 | 重写 | §2.7 当前约 50 行 → 新版约 90 行（字段级切分表占空间） |
| `docs/adr/0006-r2-专线-circuit切分.md` | 新建（可选） | ~30 行 |
| `schema.sql` | **无改动** | — |
| `docs/schema-planned.sql` | **无改动** | — |

**diff 范围承诺**：`git diff --stat HEAD` 仅含上述文件；若出现任何其他路径的 diff（如 backend/、frontend/、migrations/、openapi.yaml），视为越界，立即 halt 报告。

---

## 8. 验证清单

|| # | 验证 | 方式 |
|---|---|---|---|
| V-1 | `schema.sql` 不含 `lines*` CREATE TABLE | `grep -cE '^CREATE TABLE lines\|^CREATE TABLE line_types\|^CREATE TABLE line_changes\|^CREATE TABLE line_monitors' schema.sql` == 0 |
| V-2 | `docs/schema-planned.sql` 含 `lines*` 四表 | `grep -cE '...lines\|line_types\|line_changes\|line_monitors' docs/schema-planned.sql` ≥ 1 |
| V-3 | `02-资产管理.md` §2.7 含完整字段级切分表 | 文本搜索 `circuit_id`、`monthly_fee`、`fault_hotline` 同时存在 |
| V-4 | §2.7 含 join key 说明 | 文本搜索 `circuit.id` 或 `circuit_id` |
| V-5 | §2.7 含明确不做清单 | 文本搜索 `明确不做` 或 `NOT doing` |
| V-6 | `gofmt -l .` 空 | 命令直跑 |
| V-7 | `go test ./... -count=1` exit 0 | 命令直跑 |
| V-8 | `git diff --stat` 仅 in-scope | 直跑 + §7 表对照 |
| V-9 | ADR-0006（如建）含"单一写入方"硬约束 | 文本搜索 `单一写入` |
| V-10 | 文档无破坏性改动（章节顺序、标题层级未乱） | 人眼 review §2.7 上下衔接 |

---

## 9. 关联文档

- [`docs/v3-架构优化需求.md`](../v3-架构优化需求.md) §3 R2（决策）、§5 不做清单、§7 自审 #1
- [`docs/v3-架构优化需求.md`](../v3-架构优化需求.md) §1 P-3（根因证据）
- [`02-资产管理.md` §2.7](../../02-资产管理.md)（本轮落地位置）
- [`docs/adr/0002-v2-scope.md`](../adr/0002-v2-scope.md)（v2 时期 ADR，无 R2 内容；R2 由 ADR-0006 或本 fix-plan 记录）
- [`docs/adr/0003-grpc-作废与三层定位.md`](../adr/0003-grpc-作废与三层定位.md)（三层定位基线）
- [`docs/adr/0004-工单SoT决策.md`](../adr/0004-工单SoT决策.md)（同一原则的工单侧落地，可参考 ADR 风格）
- [`docs/schema-planned.sql`](../schema-planned.sql):39-125（被归档的 `lines`/`line_types`/`line_changes`/`line_monitors` 四表）