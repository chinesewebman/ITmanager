# M68 — G-Asset-IpConflictGuard 同 IP 多资产守卫（M66 派生 TODO）

omh-workflow: omh-plan  ←  标注 OMH workflow shape

## Goal
填资产时如果该 IP 已被另一台资产占用, 给操作者明确 409 (Not this asset's network) 而不是
静默成功. `asset_networks.ipv4_address` 当前**只有普通 index, 没有 unique 约束**, 两台
设备可以静默持有同一个地址, 而前端 `AssetTable` 的 Ping/Traceroute 正是拿这个地址去定位
设备 —— 打错一台的代价是「现场找不到设备」（与 M62 加 IP 格式闸同源的动机）。

## Non-goals
- **不**做 IPv6 唯一约束（product 决定先做 v4, v6 留 future; v6 地址空间大冲突概率小）
- **不**做 "退役释放 IP 后能否复用" 的语义变更（当前设计是释放即清空, 不留历史行, 默认允许复用）
- **不**改前端 UI（M68 纯 backend, 只在 server 返 409 时让前端接到正确 status code; 错误文案
  走既有 toaster）

## Assumptions
- 当前 `backend/internal/models/asset.go:106` `IPv4Address string ... index`（普通索引）
- M66 已 ship: `service.updateFirstNetworkIP(tx, assetID, ipAddress)` 在 Create 和 Update
  复用, 走 `tx.First(&network)` + `tx.Create/Update` 网卡表
- 前端表单走 `POST /assets` (Create) 或 `PUT /assets/:id` (Update), 都过 service 写网卡
- 失效资产 (status=retired) 的 IP 释放后是否允许新资产复用 → **policy: 允许**（与 B4 清空逻辑一致）

## Acceptance

### A. service 层
- [ ] `updateFirstNetworkIP` 在 INSERT/UPDATE 网卡**前**做 `SELECT id FROM asset_networks
  WHERE ipv4_address = ? AND asset_id != ?`（自己排除自己）
  - 命中: 返 `ErrIPConflict` (新 sentinel error)
  - 0 命中: 继续走 INSERT/UPDATE
- [ ] 不是物理 unique 约束（不改 migration / schema / unique index）—— 业务规则而非 DB 约束
  （保留退役释放后复用的语义灵活性）
- [ ] v6 暂不做（M68 范围外）

### B. handler 层
- [ ] handler 接到 `ErrIPConflict` → `apierr.Conflict(c, "IP 地址已被其他资产占用")` + 422
  还是 409? 见 Decision 1

### C. 测试
- [ ] service 层 ≥3 case: 同 IP 不同资产 conflict / 同 IP 同资产 (self update) 不 conflict /
  v6 不参与 v4 校验
- [ ] handler 层 ≥1 case: POST 冲突 IP 返 409 + 错误文案
- [ ] mutation inversion 实证 1 处 (bypass 冲突守卫 → 期待 FAIL → 还原 PASS)

### D. 双轨
- [ ] graphify multigraph 0 anomalies
- [ ] codegraph 全量 diff

## Verification
1. `go build ./...` 0 错
2. `go test -count=1 ./...` 全绿
3. mutation inversion 实证 1 处
4. frontend 0 改动（不跑 vitest, 仅 tsc 0 验证）
5. graphify + codegraph

## Risks
- **R1 (中)**: 业务规则 vs DB unique 约束 的选择: 业务规则允许"退役释放复用"语义, DB
  约束做不到这点 → 选业务规则, 但并发场景下两个 Create 同时通过 guard 然后都 INSERT, 需
  要兜底 (SELECT FOR UPDATE 或 tx 内 atomic check) → 缓解: 当前服务单实例 + tx 包
  SELECT-then-INSERT, 足够; 真并发场景留 future 追 unique 约束 (但 product 决定)
- **R2 (低)**: 误伤自己 —— 自己资产 update 自己网卡不算冲突; 已通过 `AND asset_id != ?`
  排除
- **R3 (低)**: 业务语义被绕开 —— 直接走 `db.Updates(map)` 跳过 service 的 guard; 跟 M63
  (T-76) 同源风险, 已有 `assets` 表不带 ip_address 列 + 独立参数位走 service 的双层守卫

## Plan
1. 写 `intent-M68.md` (本文件) + commit
2. 写 brief `/tmp/m68-brief.md`
3. PM-direct 自起 (≤2h backend-only)
4. impl: service `ErrIPConflict` sentinel + guard + 3 service case + 1 handler case
5. mutation inversion 实证
6. completion report + graph analysis + CHANGELOG + TODO
7. Ship

## Decision gate
- **Decision 1 (本轮拍, 推荐 409)**: handler 接到 `ErrIPConflict` 返 409 Conflict + 文案
  "IP 地址已被其他资产占用". 理由: 409 = 状态冲突 (existing state 不允许这次请求), 与
  ErrAlreadyExists (name 唯一冲突) 口径一致. 备选: 422 (语义错误) — 不选, 422 是
  validation_failed (M64 422 已用), 避免冲突语义被两次使用.
- **Decision 2 (本轮拍, 推荐 业务规则)**: 业务规则 guard 而非 DB unique 约束. 理由: 退役
  释放 IP 后必须允许复用 (产品决定), DB unique 约束做不到; 并发场景单实例 + tx 足够.
- **Decision 3 (本轮拍, 推荐 v4 only)**: 本轮只做 v4, v6 留 future. 理由: v6 地址空间大
  冲突概率低, 边际收益 < 边际测试成本.
- **Decision 4 (待 Poison)**: 是否需要"提示哪个资产占了" — 信息泄露风险 (admin 才能看到
  其他资产, viewer 不应该看到); 默认仅返 409 + 文案, 不带占用的资产 ID. **未拍, 默认
  不带**.
