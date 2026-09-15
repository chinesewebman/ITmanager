# M64 — G-Asset-NetworksPersist form submit 写 AssetNetwork (M63 派生 TODO)

## Background (PM 确认现状)

- Frontend `AssetFormModal.tsx:93` `Form.Item name="ip_address"` + `rules={ipRules}` (M62 ship)
- `AssetFormValues.ip_address` 已 ship (line 22, 48)
- `Assets.tsx:106-115` `createMut / updateMut` 已传 `values` 含 ip_address
- `api.ts:119-120` `assetApi.create(data: any)` POST `/assets` / `update(id, data)` PUT `/assets/:id`
- **Backend Asset model 无 `ip_address` 字段** (只有 name/asset_type/status/rack_id/...)
- IP 真实在 `AssetNetwork.IPv4Address / IPv6Address` (table=`asset_networks`)
- M63 ship: `pickPrimaryIP` helper + `Asset.IpAddress *string` virtual (`gorm:"-"`)
- M63 ship: `UpdateAsset` 入口剥 `ip_address` key 防 500 (T-76) — PUT 仍不写网卡表
- `AssetService.Create` (line 218-228) `db.Create(asset)`, GORM 静默丢模型外键

## 范围 (≤3h omp round)

### Backend

1. **`backend/internal/service/asset_service.go`**: `Create` 改 tx
   ```go
   func (s *assetService) Create(ctx context.Context, asset *models.Asset, ipAddress *string) error {
       if asset == nil || strings.TrimSpace(asset.Name) == "" { return ErrInvalidInput }
       return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
           if err := tx.Create(asset).Error; err != nil {
               if isUniqueViolation(err) { return ErrAlreadyExists }
               return err
           }
           if ipAddress != nil && *ipAddress != "" {
               if ip := net.ParseIP(*ipAddress); ip != nil {
                   network := &models.AssetNetwork{AssetID: asset.ID, InterfaceName: "eth0", InterfaceType: "ethernet", Status: "unknown"}
                   if v4 := ip.To4(); v4 != nil { network.IPv4Address = v4.String() } else { network.IPv6Address = ip.String() }
                   if err := tx.Create(network).Error; err != nil { return err }
               }
           }
           return nil
       })
   }
   ```
2. **`backend/internal/api/handlers/asset_handler.go`** `CreateAsset`: 嵌套 input
   ```go
   var input struct {
       models.Asset
       IpAddress *string `json:"ip_address"`
   }
   c.ShouldBindJSON(&input)
   // 后端兜底校验: net.ParseIP; 失败 → 422
   h.svc.Create(ctx, &input.Asset, input.IpAddress)
   ```
3. `Update`: 不变 (M63 已剥防 500, 真写网卡留 future)
4. **`backend/openapi.yaml`**: Asset + AssetInput schema 加 `ip_address`

### Tests

5. `service/asset_service_test.go` `Create` 4 case:
   - 无 ip_address → 仅 Asset 行
   - ip_address="10.0.0.5" → Asset + 1 AssetNetwork (eth0, IPv4Address)
   - ip_address="" → 同无
   - IPv6 → AssetNetwork.IPv6Address 写入
6. `api/handlers/asset_handler_test.go` `CreateAsset` 3 case:
   - POST 无 ip_address → 201
   - POST ip_address="bad" → 422 (net.ParseIP 兜底)
   - 路由级: POST 含 ip_address → asset_networks 有 1 行
7. 现有 PASS 不退化

### Frontend: 0 改动

## 不要写

- 不要改 UpdateAsset 写网卡表
- 不要加 migration
- 不要给 AssetFormModal 加新字段
- 不要写 AssetNetwork CRUD 独立 API

## Hard pass

- backend `go test -count=1 ./...`: 27 packages ok
- frontend tsc 0
- frontend AssetFormModal + Assets: PASS (无退化)
- mutation inversion 2 处: bypass `tx.Create(network)` → FAIL; bypass `net.ParseIP` v4 分流 → IPv6 测试 FAIL
- 双轨 graphify + codegraph

## Commit cadence (5 commit + push)

1. `feat(M64): backend AssetService.Create 写 AssetNetwork (tx + IPv4/IPv6 分流)`
2. `feat(M64): backend CreateAsset handler 嵌套 input + 调新签名`
3. `test(M64): backend AssetService.Create 4 case + handler 3 case`
4. `docs(M64): openapi.yaml Asset+AssetInput 加 ip_address`
5. `docs(M64): CHANGELOG + TODO + 双轨分析 + completion report`

## Out of scope (future)

- `G-Asset-UpdateIpPersist`: PUT 写网卡
- `G-Asset-MultiNetwork`: 多网卡支持
- `G-Asset-NetworksCRUD`: 独立 AssetNetwork 增删改 API

完成请按 omp 5-section final report 输出. 等下一 prompt 不退 → 我会 SIGKILL.
