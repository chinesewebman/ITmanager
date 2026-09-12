# IMPL-R3: vCenter VM 纳管 实施细节

| 项 | 值 |
|---|---|
| 状态 | 📝 docs-only stage 0 |
| 日期 | 2026-09-12 |
| 关联 FIX-PLAN | docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md |
| 关联 ADR | ADR-0008 |
| 关联章节 | 02-资产管理.md §2.8、03-监控采集.md §3.7 |

## §0. 与既有 R1/R2 的变更清单

| 章节 | 变更 | 状态 |
|---|---|---|
| 02-资产管理.md §2.7 (R2 已落) | 复用 asset view 视角，无需变更 | 待 Stage 2 验证 endpoint 协调 |
| 02-资产管理.md §2.8 (R3 新增/扩) | 字段对照表 + 渲染规则 | ✅ done (commit `1fa4c26`) |
| 03-监控采集.md §3.7 (R3 新增) | vCenter 孤儿 VM runbook | ✅ done (commit `be09f84`) |
| docs/adr/0008-r3-vcenter-via-netbox.md | 5 决策 + 副作用 + Round 索引 | ✅ done (commit `83323aa`) |
| docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md | 4 stage + 5 AC | ✅ done (commit `cb6c682`) |
| routes.go | `/api/assets?kind=vm` (Stage 2 round 2) | 📝 待 Stage 2 |
| assets_handler.go | `vm_fields` JSON 字段 (Stage 2 round 2) | 📝 待 Stage 2 |
| frontend AssetView.tsx | VM 行渲染 (Stage 2 round 3) | 📝 待 Stage 2 |
| README.md | version 字段（**不动**，按 R5 范围） | — |

## §1. Stage 0 端点契约（已 spec，本轮 docs-only 无需实施）

### 1.1 `GET /api/assets?kind=vm` （Stage 2 实施后落地）

**输入**：
- query `kind=vm`（必填，与既有 `kind=device/rack/line` 共用过滤参数）
- query `cluster=<cluster_name>`（可选，按 cluster 过滤）
- query `site=<site_slug>`（可选，按 site 过滤）
- query `tenant=<tenant_slug>`（可选，按 tenant 过滤）
- query `status=<active|offline|staged|orphan>`（可选，孤儿 VM 按 `orphan` 过滤 — 02 §2.8.2 第 3 条定义的「NetBox 失联」徽章）
- 通用分页参数：`page`, `page_size`, `sort_by`, `sort_order`

**输出**（参考既有 `GET /api/assets` 响应结构 + `vm_fields`）：
```json
{
  "data": [
    {
      "id": 12345,
      "kind": "vm",
      "name": "vm-web-prod-01",
      "status": "active",
      "site": {
        "id": 1,
        "slug": "shanghai-dc1",
        "name": "Shanghai DC1"
      },
      "tenant": {
        "id": 5,
        "slug": "ecommerce",
        "name": "电商业务"
      },
      "vm_fields": {
        "vcpus": 8,
        "memory_gb": 16.0,
        "disk_gb": 200,
        "cluster": "vsan-prod-cluster-01",
        "tenant_display": "电商业务",
        "tags": ["NetBox-synced", "production"],
        "last_updated_at": "2026-09-12T03:24:18+08:00"
      },
      "last_seen_at": "2026-09-12T03:24:18+08:00",
      "orphan": false
    }
  ],
  "total": 1247,
  "page": 1,
  "page_size": 50
}
```

**孤儿 VM 状态**：
- NetBox 那边 VirtualMachine 记录已删，ITmanager 端暂未刷新
- 响应中 `status: "orphan"`，并 `orphan: true`，前端显示红色徽章（参考 02 §2.8.2 第 3 条）
- 30 天后 ITmanager 端软退役（`assets.retired_at` 字段填充），不再出现在默认列表

### 1.2 `GET /api/assets/{id}` （Stage 2 实施后）

**返回 `vm_fields`**：与 1.1 同结构（增加 `cluster_id`, `site_id`, `tenant_id` 用于前端跳转）

### 1.3 权限

- 与既有 R1/R2 权限模型共用（见 ADR-0007 R1 鉴权）
- 普通用户：只读 `kind=vm` 资产
- 资产管理员：可标「保留」孤儿 VM（防止 24h 内被自动清理）

### 1.4 与 NetBox 的同步状态接口（**本轮不实施，预留 endpoint**）

`GET /api/assets?kind=vm&_meta=sync_status` — 响应头加 `X-NetBox-Last-Sync-At`：

- 运维面板可见 NetBox 上次同步时间
- 距上次同步 > 24h 发警告

## §2. 阶段 0 docs-only 实施（详细）

### 2.1 commit `1fa4c26` — 02-资产管理.md §2.8 扩展

变更：
- 原 §2.8 只有 18 行结论 + 风险（v3 R3 决策概要）
- 新增 §2.8.1 字段对照表（12 行 NetBox → ITmanager 映射 + 2 行不展示字段）
- 新增 §2.8.2 渲染规则（4 条行为规范）

关键设计点：
- 字段对照表 12 行覆盖 NetBox VirtualMachine 关键字段：`name/vcpus/memory/disk/status/cluster/site/tenant/tags/custom_fields/last_updated`
- 不展示字段：`config.hardware.uuid` (BIOS UUID)、`runtime.host` (ESXi host)
- 与 §2.7 R2 显式协调（同一 asset view、单一 SoT = NetBox、不双写）

### 2.2 commit `be09f84` — 03-监控采集.md §3.7 新增

变更：
- §3.7 引言解释 v3 §3 R3 风险条款为什么需要「人工确认窗口」
- §3.7.1 阶段 0 dry-run 启用（运维前置，yaml 示例 + 3 步骤）
- §3.7.2 周观察期 checklist（≥ 7 天，5 项每日 review）
- §3.7.3 cleanup-with-confirm + 24h 人工确认窗口（yaml + 指令）
- §3.7.4 全面启用（≥ 30 天，confirm 24h → 4h 加速）
- §3.7.5 异常处理（4 种异常 + 处理）
- §3.7.6 与 ITmanager 资产视图的接口（cleanup_queue 不落地、NetBox 失联徽章、30 天软退役）

### 2.3 commit `83323aa` — ADR-0008

5 决策落地：D1 不直连 / D2 单一 SoT / D3 人工确认 / D4 不引入新表 / D5 字段对照 vs 双写。
副作用段写明：与 R2/R1 兼容 + 02 §2.9 已废弃设计不再复活。
Round 索引：M35-R2-R1 (docs) → R2 (Stage 1 dry-run) → R3 (Stage 2 Go 代码) → R4 (Stage 3 加速)。

### 2.4 commit `cb6c682` — FIX-PLAN-R3

4 stage 计划 + 5 AC + 5 Not doing + 6 风险 + 7 Verification。
M35-R2-R1 完成 Stage 0；后续 round 由运维推进 Stage 1/2/3。

### 2.5 本 commit — IMPL-R3 (本文件)

详细实施细节，跨 stage 索引：
- §0 变更清单（含待后续 stage 落地的端点契约）
- §1 Stage 0 端点契约（已 spec，本轮不实施）
- §2 阶段 0 docs 实际 commits 落地说明
- §3 待 Stage 2 实施的 Go 接口（route + handler + 前端）
- §4 待 Stage 1 运维部署的配置 + checklist
- §5 待 Stage 3 加速的清理策略

## §3. Stage 2 实施前置（Go 代码，待后续 round）

### 3.1 backend Go 改动范围

| 文件 | 变更 |
|---|---|
| `backend/internal/api/routes.go` | 新增 `/api/assets?kind=vm` 过滤分支，复用既有 handler |
| `backend/internal/api/assets_handler.go` | 新增 `vm_fields` JSON 字段填充逻辑 |
| `backend/internal/models/asset.go` | 增加 `vm_fields` GORM type + `MemoryGB()` / `DiskGB()` / `Vcpus()` 访问器 |
| `backend/internal/integration/netbox.go` | 增加 `GetVirtualMachine(id)` / `ListVirtualMachines(...)` NetBox API client 函数（不实施，重新讨论） |
| `backend/migrations/000029_vm_fields.up.sql` | `ALTER TABLE assets ADD COLUMN vm_fields JSONB` + 索引 (待 Stage 2 实测) |

### 3.2 frontend 改动范围

| 文件 | 变更 |
|---|---|
| `frontend/src/views/AssetView.tsx` | VM 行渲染（vcpus / memory_gb / disk_gb / cluster / NetBox 失联徽章） |
| `frontend/src/types/asset.ts` | 增加 `kind: 'vm' \| 'device' \| 'rack' \| 'line'` 判别字段 |
| `frontend/src/components/AssetCard.tsx` | VM 特定字段卡片 |
| `frontend/src/__tests__/AssetView.test.tsx` | VM 行渲染测试 + 孤儿徽章测试 |

### 3.3 测试要求

- Go: 新增 `TestAPI_Assets_VM_Filter*` (pg + memory)，验证 `/api/assets?kind=vm` 过滤
- Go: 新增 `TestAPI_Assets_VM_OrphanField`，验证孤儿 VM 字段
- frontend: VM 行 snapshot 测试 + 孤儿徽章交互测试
- verify-e2e skill 驱动: 网页打开 `#/assets?kind=vm` 看到 ≥ 1 个 VM 行

## §4. Stage 1 运维前置（待后续 round + 运维就绪）

### 4.1 运维预备条件

- 已经在生产环境部署 NetBox（版本 ≥ 3.0 支持 VirtualMachine type）
- 已经在生产环境部署 vCenter（任何版本）
- ITmanager 团队有权限访问 vCenter & NetBox API
- 网络可达 vCenter → NetBox → ITmanager

### 4.2 bb-Ricardo/netbox-sync 部署

```bash
# 假设用 Docker
docker run -d \
  --name netbox-sync \
  -e NETBOX_URL=https://netbox.yourdomain.local \
  -e NETBOX_TOKEN=... \
  -e VCENTER_URL=https://vcenter.yourdomain.local \
  -e VCENTER_USER=... \
  -e VCENTER_PASSWORD=... \
  -v /var/log/netbox-sync:/var/log/netbox-sync \
  ghcr.io/bb-ricardo/netbox-sync:latest
```

### 4.3 周观察期触发器

- PM 看到 `/var/log/netbox-sync/diff-YYYY-MM-DD.json` 文件存在 ≥ 7 天 → Stage 2 启动
- PM 看到 `orphans.length > 100/d` 持续 3 天 → 与运维一起 review 是否调整

## §5. Stage 3 加速（待运维观察 ≥ 30 天后）

### 5.1 触发条件

- [ ] Stage 2 稳定运行 ≥ 30 天
- [ ] `orphans.length` 平均 ≤ 50/d
- [ ] `误判率` ≤ 5%
- [ ] 业务方无强制 `!keep` 反对

### 5.2 加速动作

- `confirm_window_hours: 24` → `4`
- `confirm_notification.slack_channel` → 保留审计通路
- 季度 review `orphans.json` 与误判率

## §6. 后续 Round 索引

| Round | 工作量 | 触发条件 |
|---|---|---|
| M35-R2-R1（本轮 docs Stage 0） | ~30min (4 commits) | R3 spec ready |
| M35-R2-R2 | 依赖运维就绪 | 部署 dry-run + ≥ 7 天观察期 |
| M35-R2-R3 | ~6-8h 真 Go + 前端代码 | 运维 dry-run 报告 ≥ 7 天 |
| M35-R2-R4 | 配置调整 + 季度 review | ≥ 30 天后 |

## §7. 相关文件索引

- `intent-M35-R2.md` — intent-spec 起点
- `docs/v3-架构优化需求.md` §3 — R3 需求源头
- `02-资产管理.md` §2.8 — 字段对照 + 渲染规则
- `02-资产管理.md` §2.9 — 已废弃自建 VMware 表（**不复活**）
- `03-监控采集.md` §3.7 — vCenter 孤儿 runbook
- `docs/adr/0008-r3-vcenter-via-netbox.md` — 5 决策
- `docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md` — 计划
- `docs/IMPL-R3-VCENTER-VIA-NETOBOX.md` — 本文
