# M63 — G-Asset-IpPersistence 资产 IP 字段落库 + 列表投影 (M62 派生 TODO)

## Background

M62 final report (T-74 IPv6 regex 锚点 fix) 派生 TODO G-UI-AssetIpPersistence:

1. **POST /assets** 绑进 `models.Asset`（**无 `ip_address` 字段**）→ 键被 `ShouldBindJSON` 静默丢弃
2. **PUT /assets/:id** 走 `db.Updates(map)`，GORM v1.30.0 对模型外键**不丢弃**（`callbacks/update.go:211-232`）→ `SET ip_address=…` → PG `42703` → 500
3. **GET /assets** 不投影 IP → 前端 IP 列与 Ping/Traceroute 按钮（`AssetTable.tsx:149,159`）在生产数据上**空转**，测试全绿是因为 mock 里有 IP

## 范围 (≤4h omp round)

### Backend

1. **`backend/internal/models/asset.go`**: Asset struct 加 virtual field
   ```go
   IpAddress *string `json:"ip_address" gorm:"-"` // virtual, 由 List/Get 注入 (v4 优先, 否则 v6); PUT/POST 不写此列
   ```
2. **`backend/internal/service/asset_service.go`**:
   - 加 private helper `pickPrimaryIP(networks []models.AssetNetwork) (v4 *string, v6 *string)` (复用 `postmortem_service.fetchIP:97-121` 口径: "先非空 v4 否则 v6", 不要写第二份)
   - `List` 改 join AssetNetwork, 每个 asset 加 `IpAddress *string`
   - `Get` 已返回 networks, 同样在 asset 上加 `IpAddress`
3. **`backend/internal/api/handlers/asset_handler.go`**: UpdateAsset 入口剥 `ip_address` key
   - 原因: PUT /assets 走 `db.Updates(map)`, GORM v1.30.0 实证会**发 SQL** → PG `42703` → 500
   - 修法: 在 `normalizeJSONBFields(updates)` 之后加 `delete(updates, "ip_address")` (一行) + 注释 T-75
   - 注: 后续 round 再加写 `asset_networks` 第一张网卡表 (本 round 只防 500)

### Tests

4. `backend/internal/service/asset_service_test.go`:
   - `pickPrimaryIP` 7 case 单测 (空 / 全 v4 / 全 v6 / v4+v6 / v6+v4 顺序无关 / 空字符串 v4 / 空字符串 v6)
5. `backend/internal/api/handlers/asset_handler_test.go`:
   - ListAssets: 3 asset + networks → items[i].ip_address 注入
   - GetAsset: asset + networks → asset.ip_address 注入
   - ListAssets: 资产无 networks → ip_address=null
   - 路由级: PUT /assets/:id 接受 `{ip_address: "1.2.3.4"}` → 不发 SQL 不返 500 (T-75)
6. 现有 PASS 不退化

### Frontend

- 0 改动 (`AssetTable.tsx:13` 已有 `ip_address` 字段类型 + 列)
- `AssetFormModal.tsx` 已 ship `ipRules` (M62), form submit payload 暂未含 `ip_address` 字段 — 本 round **不动 form** (留 `G-Asset-NetworksPersist` round)

## 不要写

- 不要改 AssetNetwork model
- 不要在 AssetFormModal form submit 加 ip_address (留 future round)
- 不要新增 migration (`ip_address` 是 virtual, 不在 DB)
- 不要改 ListAssets 输出结构 (`items` 仍是 `[]Asset`, 仅 item 内多 `ip_address` 字段)

## Hard pass

- backend `go test -count=1 ./...`: 27 packages ok
- frontend tsc 0
- frontend Assets + AssetTable.memo: PASS (无退化, frontend 0 改动)
- mutation inversion 实证: bypass `delete(updates, "ip_address")` → TestUpdateAsset_ipAddress_不被GORM发送_不返500 FAIL; bypass `pickPrimaryIP` v4 优先 → 测试 fail
- 双轨 graphify + codegraph

## Commit cadence (5-6 commit + push)

1. `feat(M63): backend Asset.IpAddress virtual field + pickPrimaryIP helper`
2. `test(M63): backend pickPrimaryIP 单测 (7 case)`
3. `feat(M63): backend AssetService.List/Get 投影 IpAddress`
4. `test(M63): backend ListAssets / GetAsset 测试 (4 case)`
5. `feat(M63): backend UpdateAsset 入口剥 ip_address (T-75 防 500) + 测试`
6. `docs(M63): CHANGELOG + TODO + 双轨分析 + completion report`

## Out of scope (future)

- `G-Asset-NetworksPersist`: form submit 写第一张 AssetNetwork 表
- `G-Asset-BulkIpEdit`: 批量改 IP
- 实时 IP 校验

完成请按 omp 5-section final report 输出. 等下一 prompt 不退 → 我会 SIGKILL.
