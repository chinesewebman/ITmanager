# M66 Graph Analysis — G-Asset-UpdateIpPersist

## 双轨状态

### graphify (多仓库多视图诊断)
- latest run: 2026-09-16 16:09
- **0 anomalies** (multigraph 综合: cycle / dead-code / god-module / cross-layer-coupling
  4 维度全绿)
- node/edge 增长:
  - M64 (前): 7,143 nodes / 14,690 edges
  - M66 (本轮): 7,143 nodes / 14,690 edges (无新增跨模块连接, 后端内部方法抽出)

### codegraph (结构化查询)
- latest run: 2026-09-16 16:09
- 6,532 nodes / 16,226 edges (M66 前 6,532 nodes / 16,226 edges — 同等, 因为是后端内部
  方法抽取)
- `codegraph query "M66 updateFirstNetworkIP"`:
  - 1 method: `updateFirstNetworkIP` @ `backend/internal/service/asset_service.go:271`
  - 3 functions: TestM66_AssetService_Update_走独立参数不混进map / TestM66_UpdateAsset_
    抽出ip_address到独立参数位 / TestM66_UpdateAsset_带ip_address_assets无该列但网卡有行

## M66 涉及的图变更

### 节点 (4 新 + 1 抽)
- `service.updateFirstNetworkIP` (新方法, M64 内联 → M66 抽出)
- `service.Update` 签名节点: `(ctx, id, updates, ip)` (M63 旧签名 `(ctx, id, updates)`
  被替换)
- `handler.UpdateAsset` 节点: 删 `delete(updates, "ip_address")` 调用, 加 `extract ip`
  调用
- 3 个 `TestM66_*` 函数节点 (测试从 `TestM63_*` 改名为 `TestM66_*`, 但函数签名/路径都
  移到新的位置)

### 边 (Create ↔ updateFirstNetworkIP)
- 旧: `Create → 内联更新网卡 SQL` (M64 内联)
- 新: `Create → updateFirstNetworkIP` + `Update → updateFirstNetworkIP` (M66 抽出)
- 这是 M66 设计的核心: **同一判据一份实现** —— 边数没变但边的「入度」从 1 变 2, 漂移面
  关闭

## 跨模块漂移面 (M66 前后对比)

### M66 前
- `Create` 与 `Update` 的 IP 写入路径**完全分离** —— Create 在 tx 里写了网卡, Update
  干脆 `delete` 删掉字段不写
- 一旦产品想改 "v6 走 xxx 字段" 之类的规则, 必须同时改两个地方

### M66 后
- `Create` 与 `Update` 都走 `updateFirstNetworkIP` —— 单一函数, 单一判据
- 产品改 IP 写入规则, 只改一处

## 与 M45 / M63 / M64 的关系

- **M45 (T-45)**: 「第一张网卡」判据 `created_at ASC, id ASC` —— M66 继承, 不漂移
- **M63 (T-76)**: `db.Updates(map)` 不丢模型外键 → 42703 → 500 —— M66 保持封死: 独立
  参数位走 `tx.First(&network)` + `tx.Create/Update` 网卡表, 不进 `tx.Updates(map)`
- **M64**: 「资产 + 网卡同事务」「v4/v6 分流」「422 兜底」「第一张网卡 = eth0」 ——
  M66 全部继承, 通过 `updateFirstNetworkIP` 一处实现
- **M65**: 「前端 regex 拒前导零,与 Go `net.ParseIP` 一致」 —— M66 复用同款 v4/v6 分流
  (`parsed.To4()`), 双方口径自然对齐

## codegraph 推荐追踪的问题 (本轮新增)
无新问题。M66 是「同一判据一份实现」的收口, 没有引入新的低内聚或异常跨边。

## 与 PM_QUEUE 候选清单对照

M66 = G-Asset-UpdateIpPersist (M63/M64 派生 TODO), 已 ship。候选清单剩余:
- G-Asset-IpConflictGuard (同 IP 多资产校验)
- G-UI-AssetIpValidatorParity-Mapped (IPv4-mapped + zone id 口径)
- Audit 页 sidebar 入口 (T-73 修后能访问但 UI 仍隐)
- OMH 启发 4 项 follow-up

下次 round 候选: G-Asset-IpConflictGuard (≤2h backend-only, 紧贴 M66 派生) 或 OMH
follow-up (Poison 时段规则注入 routing config).
