# M66 — G-Asset-UpdateIpPersist PUT /assets/:id 写 AssetNetwork (M63/M64 派生 TODO)

## Goal
让 PUT `/api/assets/:id` 真改第一张 `asset_networks` 的 IP — 与 POST (M64) 对齐, 不再静默丢 `ip_address` 键.

## Non-goals (out of scope)
- 不修多网卡场景 (`G-Asset-MultiNetwork` 留 future)
- 不动 frontend form (PUT path 已经发 `ip_address`, frontend 0 改动)
- 不重写 `pickPrimaryIP` 判据 (M63/M64 已 ship)
- 不动 Retire/Restore IP 流程 (B4 已有)

## Assumptions (诚实登记)
1. **接口语义延续 PUT partial update 语义**: 只发 `ip_address` 不影响其他字段
2. **第一张网卡 = `created_at ASC, id ASC` 排序的第一行** (与 `listNetworks` 的 M45 T-45 一致)
3. **空字符串 = 不改**: PUT 不带 `ip_address` / `ip_address: ""` → 不动网卡 IP
4. **无网卡的资产**: PUT `ip_address` 仍要**先建第一张网卡** (与 POST 一致: `interface_name="eth0"`, `interface_type="ethernet"`)
5. **校验**: backend `net.ParseIP` 兜底, 失败 → 422 `validation_failed` (与 POST 一致)

## Acceptance criteria
- [ ] `PUT /assets/:id` body `{ip_address: "10.0.0.5"}` → asset_networks 第一张网卡 IPv4Address = "10.0.0.5"
- [ ] `PUT /assets/:id` body `{name: "new-name", ip_address: "10.0.0.5"}` → 仅改 name + ip_address, 不动其他字段
- [ ] `PUT /assets/:id` body `{ip_address: ""}` → 不动网卡 IP (空串 = 不改)
- [ ] `PUT /assets/:id` body `{ip_address: "bad"}` → 422 + `validation_failed` (不写库)
- [ ] `PUT /assets/:id` body `{ip_address: "2001:db8::1"}` → 第一张网卡 IPv6Address = "2001:db8::1"
- [ ] `PUT /assets/:id` 无网卡 → 先建第一张网卡 (eth0) 再设 IP
- [ ] `PUT /assets/:id` 不发 ip_address → 行为与 M63 ship 态完全一致 (剥 ip_address, 200)
- [ ] 路由级集成测试覆盖以上 7 case

## Verification shape
- backend `go test -count=1 ./...` → 27 packages ok
- service 层单测 `TestUpdateAssetWithIP` 覆盖 7 case
- handler 层集成测 `TestPutAssets_WithIP_*` 覆盖 4 case (成功/v4/v6/422/空)
- mutation inversion: bypass `tx` → FAIL; bypass `net.ParseIP` 422 兜底 → FAIL; bypass `updateFirstNetworkIP` 逻辑 → FAIL
- 双轨 graphify + codegraph (T-77 仍有效, 写入通道 + 投影双侧)

## Risks
- **T-77 复发**: 投影字段 (`gorm:"-"`) 当写入通道 → 写入失败. PUT 必须走 `tx` 而非 `db.Updates(map)`
- **并发写**: 两个 PUT 同时改 IP → 加 `FOR UPDATE` 持锁读旧值 (M61 用户管理同族)
- **审计行**: PUT 现在是 audit `Action=PUT` 单行, IP 字段进 resource 含 ID 列表摘要 (与 M58 bulkRetire 同族)
- **真 PG 往返**: 本机无 PG, 实证仅 sqlite + sqlmock (与 M63/M64 同)

## Plan (≤2h omp round)

### 1. `backend/internal/service/asset_service.go`
- `Update(ctx, id, updates)` 拆出 `updates["ip_address"]` (类似 M63 的 `delete(updates, "ip_address")`)
- 调 `s.updateFirstNetworkIP(ctx, assetID, ipAddress *string)` 在**同一事务**
- helper 内部: `net.ParseIP` → v4 进 `IPv4Address` / v6 进 `IPv6Address`; 失败 → `ErrInvalidInput`
- 第一张网卡: `listNetworks` 同款排序; 不存在 → 新建 eth0 (与 POST Create 对齐)
- 整批包 tx (asset 行 update + 网卡行 update/insert)

### 2. `backend/internal/api/handlers/asset_handler.go`
- `UpdateAsset` 入口剥 `ip_address` (M63 已 ship) → 走 service `Update(ctx, id, updates)` 时**不**传 ip_address, 改传新签名 `(ctx, id, updates, ipAddress *string)`
- 422 兜底 (与 POST 同: net.ParseIP 失败 → `apierr.Unprocessable`)

### 3. Tests
- `service/asset_service_test.go` `Update` 7 case (与 acceptance criteria 一一对应)
- `api/handlers/asset_handler_test.go` `UpdateAsset` 5 case 路由级

### 4. OpenAPI (M64 follow-up: G-Asset-IpPersistence-Contract 待收部分)
- `PUT /assets/{id}` 入口声明 422 response

### 5. Commit cadence (5 commit + push)
1. `feat(M66): backend updateFirstNetworkIP helper (tx + v4/v6 分流 + net.ParseIP 422)`
2. `feat(M66): backend Update 拆 ip_address + 走 updateFirstNetworkIP`
3. `test(M66): backend service 7 case + handler 5 case`
4. `docs(M66): openapi.yaml PUT /assets/{id} 加 422 response`
5. `docs(M66): CHANGELOG + TODO (G-Asset-UpdateIpPersist 结案) + 双轨分析 + completion report`

## Decision gate
- **是否起 round**: 本周 14:00 周二 omp 窗口内可 dispatch, ≤2h backend-only = omp dispatch 可省 → **PM-direct 自起 ≤2h** 也可接受 (Poison 时段规则 ≤2h 不浪费 omp dispatch)
- **拍板**: **PM-direct 自起** (≤2h round, 不走 omp dispatch 起步成本 ~30min)
- **不打扰 Poison**: 时段规则允许 (周二 14:00-18:00 PM-direct 时段)
- **fallback**: 若 PM-direct 写超 2h 仍有问题, mid-round 起 omp subagent 收尾

## Verification commands
```bash
cd /home/webman/Projects/ITmanager/backend
go test -count=1 ./internal/service/... -run Asset
go test -count=1 ./internal/api/handlers/... -run Asset
go test -count=1 ./...

cd /home/webman/Projects/ITmanager/frontend
npx tsc --noEmit
npx vitest run src/components/AssetFormModal.test.tsx src/pages/Assets.test.tsx
```

## Out of scope (future)
- `G-Asset-MultiNetwork` 多网卡 CRUD
- `G-Asset-UpdateIpPersist-Atomic` 跨资产原子改 IP (如换机房批量改 IP 段)
- IP 写冲突检测 (`G-Asset-IpConflictGuard` 派生)

完成请按 omp 5-section final report 输出 (PM-direct 等价).
