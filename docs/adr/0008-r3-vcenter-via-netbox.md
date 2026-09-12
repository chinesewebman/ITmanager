# ADR-0008: R3 vCenter VM 纳管 — 单一 SoT(NetBox) + 人工确认窗口

| 项 | 值 |
|---|---|
| 状态 | ✅ Accepted |
| 日期 | 2026-09-12 |
| 涉及章节 | 02-资产管理.md §2.7 / §2.8、03-监控采集.md §3.7、09-AI辅助.md、docs/IMPL-R3-VCENTER-VIA-NETOBOX.md |
| 决策者 | hermes@local (PM) |
| 关联 ADR | ADR-0006 (R2 专线归 NetBox)、ADR-0007 (R1 HolmesGPT toolset) |

## 上下文

v3 §3 R3 需求：上千台 vSphere/ESXi 上的 VM **零纳管**（没有在 ITmanager 出现）。需把 VM 数据纳入 ITmanager 资产视图，让运维人员能在 ITmanager 看全所有资产。

## 决策

### D1. ITmanager **不直连** vCenter

- ITmanager 是**只读读者**，只读 NetBox。
- vCenter → NetBox 的同步走社区版 `bb-Ricardo/netbox-sync`（项目独立，不在 ITmanager 仓库）。
- 副效应 1：NetBox 是 vCenter 数据的**单一 SoT**——任何 vCenter 数据走 NetBox 才进 ITmanager，避免 ITmanager 缓存 vCenter 数据导致数据不一致。
- 副效应 2：监控（Zabbix VMware 模板）单独走，不经 NetBox，与 R3 同步路径正交。

### D2. 单一 SoT = NetBox（vCenter + 专线 + 设备统一）

- 已有资产管理（device / rack / line）已落 NetBox（ADR-0006 R2）。
- 新增 VM 资产视图，本质是把 NetBox VirtualMachine 类型拉进 ITmanager asset view 的同一种渲染规则。
- `kind=vm` 与既有 `kind=device/rack/line` 共用同一张 ITmanager `assets` 表，只增加 `vm_fields: {vcpus, memory_gb, disk_gb, cluster, ...}` JSON 字段（不引入新表）。
- 副效应：02-资产管理.md §2.8 资产视图同时呈现 line + VM + device + rack，运维人员切换不同 view 的认知负担消失。

### D3. 孤儿 VM 删除走人工确认窗口（v3 §3 R3 风险条款）

- vCenter VM 频繁增删（快照 / 克隆 / 临时机）产生大量孤儿对象（NetBox 有记录、vCenter 已无对应 VM）。
- 若 `bb-Ricardo/netbox-sync` 直接自动清理 NetBox 记录，等于把 vCenter 的「临时机」当成真正要管的资产做删除——**风险不可接受**。
- 因此 `bb-Ricardo/netbox-sync` 配置从 `mode: dry-run` → `mode: cleanup-with-confirm`，**24 小时人工确认窗口**才执行实际删除。
- 业务方回复 `!keep` 阻止删除；不回复视为同意，24h 后自动清理。
- ITmanager 端**不**主动删除资产行（即使 NetBox 失联），保留 30 天作为「软退役」缓冲（与 `assets.retired_at` 字段兼容）。

### D4. VM 不引入新表（与既有资产表共存）

- ITmanager 不为 VM 落地新表——NetBox 是 SoT，ITmanager 只读。
- 资产视图按 `kind` 区分，vm 详情走 `vm_fields` JSON 列。
- 副效应：与 R2 专线复用同一张 asset view，未来审计 / 报表自然统一。

### D5. 字段对照 vs 字段双写（信息流收敛）

- ITmanager 端为 vCenter VM 字段（vcpus / memory / disk / cluster / site / tenant）做**只读展示**，**不**写回 NetBox。
- NetBox-synced 标签是 bb-Ricardo/netbox-sync 项目的约定，ITmanager **不重新定义**，只引用。
- BIOS UUID / ESXi host 等敏感字段 ITmanager **不暴露**（即使 NetBox 存了）。

## 决策带来的后果

### 收益

- **统一视图**：运维在 ITmanager 一处看全 line + VM + device + rack。
- **零本地状态**：VM 数据不在 ITmanager 落表，等于 vCenter 重装/迁移都不影响 ITmanager。
- **误删保护**：人工确认窗口 + 30 天软退役缓冲 + 标签双重保险 = 三层防误删。
- **可扩展**：未来加 NSX-T / Hyper-V / OpenStack 只需复用 R3 决策，加 `kind=hypervisor` 即可。

### 代价

- **依赖 NetBox**：NetBox 不可达时 ITmanager 看不到 VM（但 historical 数据仍在，只是 stale）。
- **人工确认需要运维响应**：要求运维 24h 内回复 `!keep` 或不回复。
- **周观察期**：≥ 7 天 dry-run + ≥ 30 天 cleanup-with-confirm 观察期 = 至少 37 天才能全面启用（运维资源投入）。

## 副作用与迁移路径

- **R2 已有影响**：02 §2.7 专线字段对照表 (by `f568099`) 与 R3 §2.8 VM 字段对照表共用同一种 `asset view` 视角——这是 D2「单一 SoT = NetBox」的落地。
- **R1 兼容性**：R1 HolmesGPT toolset 的 `/api/assets?kind=vm` 端点必须复用既有 `kind` 过滤（不引入新 endpoint）。
- **不再回头**：之前的「自建 vCenter sync / 自建 VMware 表」设计已被 02 §2.9 标记「已废弃」——本 ADR 不复活 02 §2.9 的内容。

## 确认

- [x] 02-资产管理.md §2.8 已扩字段对照表 + 渲染规则（commit `1fa4c26`）
- [x] 03-监控采集.md §3.7 vCenter 孤儿 VM runbook 已落（commit `be09f84`）
- [x] NetBox 是单一 SoT 的硬约束已显式写在 02-资产管理.md §2.8.1
- [x] 24 小时人工确认窗口已显式写在 03-监控采集.md §3.7.3
- [x] 30 天软退役缓冲与 `assets.retired_at` 字段的接口已写在 03-监控采集.md §3.7.6

## 后续 round

| 阶段 | 工作量 | 触发条件 |
|---|---|---|
| Stage 1 | 实际部署 `bb-Ricardo/netbox-sync` dry-run 模式（运维出马，非 ITmanager 代码） | 需要运维环境准备就绪 |
| Stage 2 | ITmanager 端实现 `/api/assets?kind=vm` + `vm_fields` 字段（实际 Go 代码） | R3 docs 全部 ship + 运维环境就绪 |
| Stage 3 | 周观察期（≥ 30 天）+ `confirm_window_hours` 从 24h 缩短到 4h | Stage 2 稳定运行 + 监控孤儿率 < 5% |
