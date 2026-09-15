# M67 + M68 Graph Analysis — OMH Workflow Adoption + IP Conflict Guard

## 双轨状态 (M67 + M68 合并分析)

### graphify (多仓库多视图诊断)
- M66 → M67 (OMH install + workflow shape 试作用): 0 anomalies (OMH 不进 ITmanager 仓)
- M67 → M68 (G-Asset-IpConflictGuard): **0 anomalies**
- M67 是元工作 (改 ~/.omh / ~/.hermes 不进 ITmanager 仓), graphify 视角等同于无变化

### codegraph (结构化查询)
- M67: 0 增量 (没改 ITmanager 仓)
- M68: 6,532+ nodes / 16,226+ edges (本轮新方法/case 若干)
- `codegraph query "M68 ErrIPConflict"`:
  - 1 method: `updateFirstNetworkIP` @ `backend/internal/service/asset_service.go:284`
  - 2 functions: TestM68_AssetService_ip被其他资产占用_返ErrIPConflict / TestM68_CreateAsset_IP已被占用_返409

## M67 涉及的变化 (OMH install + workflow shape)

### OMH install 部分
- 二进制: `~/.local/bin/omh` (v2.0.3)
- 123 skills: `~/.omh/skills/`
- config.yaml 4 节追加: `skills.external_dirs` / `plugins.enabled` / `display.skin` /
  `interface`
- backup: `/tmp/hermes-pre-omh-backup-20260916-0606/` (37 文件)

### Poison 时段规则安置 (M67 关键修正)
**结论**: OMH 不接 routing config, 不能"注入 Poison 时段"进 setup-profile.json。改走
三处冗余:

1. **`~/.omh/project-rules.md`** (新): OMH project-local decision, advisory not enforced
2. **fact_store fact_id=6** (新): Poison 时段 + OMH-aware PM-direct
3. **MEMORY.md** (更新 1 处, 把 keyring section 收 200 字 + 加 OMH-aware 180 字)

不动:
- `~/.omh/setup-profile.json` (保留 `--default-executor hermes --skip-apply`)
- `display.skin: omh` / `interface: tui` (Poison 没要求, 待决策)
- `~/.hermes/config.yaml` (除 OMH install 时加的 4 节外, 不二次扰动)

### OMH workflow shape 起 round (M66 / M67 / M68 实证)
| Round | OMH skill 触发 | 收益 |
|---|---|---|
| M66 intent | omh-plan 8 节骨架 | +Non-goals, +Decision gate 两节之前会漏 |
| M67 intent | omh-plan 8 节骨架 | 同上, plus "不动 OMH config" decision 显式化 |
| M64/M66 tests → M68 tests | task-completion-protocol | mutation inversion 实证 1 处真红 → 还原绿 |
| M68 dispatch | ulw-work (brief) | 显式区分 prepared vs observed |

OMH 没自动接管 PM-direct 流程, 但骨架让 PM-direct 写得更结构化。这是"更好地使用 omh"
的实际收益 — 不是"OMH 接管 PM-direct", 而是"OMH 提供骨架让 PM-direct 更稳"。

## M68 涉及的图变更

### 节点 (5 新)
- `ErrIPConflict` (sentinel error)
- 守卫 SQL 块 (在 `updateFirstNetworkIP` 内)
- `CreateAsset` 错误映射 + 1 个分支 (ErrIPConflict → 409)
- `UpdateAsset` 错误映射 + 1 个分支 (ErrIPConflict → 409)
- 3 个 TestM68_* 函数 + 1 个 TestM68_CreateAsset handler 函数

### 边 (新)
- `Create` → `ErrIPConflict` (守卫失败时返)
- `Update` → `ErrIPConflict` (守卫失败时返)
- `ErrIPConflict` → 409 (handler 映射)
- `CreateAsset` → `ErrIPConflict` branch
- `UpdateAsset` → `ErrIPConflict` branch

## 跨模块漂移面 (M66 → M68 对比)

### M66 前
- `Create` 与 `Update` 都写网卡 IP, 但**没有任何守卫**检查重复 IP
- 两台资产填同一 IP 静默成功, 等待后台巡检发现

### M68 后
- `Create` 与 `Update` 共享 `updateFirstNetworkIP` (M66), 现在都走守卫
- 同 IP 不同资产 → 409 + "IP 地址已被其他资产占用"
- 自己的资产更新自己的网卡 IP → 不冲突 (self-exclude)
- v6 不参与守卫 (v6 留 future)

## 与 PM_QUEUE 候选清单对照

M67 + M68 都 ship。候选清单剩余:
- **M69 = G-Asset-IpConflictGuard-v6** (M68 派生, 留 future)
- **M70 = G-Asset-IpConflictAudit** (M68 派生, 留 future)
- **Audit 页 sidebar 入口** (T-73 修后能访问但 UI 仍隐, 真摩擦)
- **OMH follow-up 4 项** (Poison 时段已注入 / model calibration / capability impact 节 /
  skills 骨架升级)

下次 round 候选:
- OMH 启发 #1: model calibration paragraph (≤1h PM-direct, 给 minimax + deepseek 各写一份
  model 表现规格, 钉进 skill 防漂)
- OMH 启发 #2: 升级 intent-spec-author / omp-task-brief skill 骨架 (≤4h, 把 M66/M67/M68
  intent 沉淀的 omh-plan 8 节骨架搬到现有 skill)
- G-Asset-IpConflictAudit (M70, ≤2h backend, 历史数据巡检)
