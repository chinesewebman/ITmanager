# M66 Completion Report — G-Asset-UpdateIpPersist

## 摩擦
M63 把 `Asset.IpAddress` 收成虚拟字段、入口 `delete(updates, "ip_address")` 防 500；M64 把
POST 的 IP 写通了 (asset + network 包事务, v4/v6 分流, 422 兜底)。但 PUT 还停在 M63 的
`delete` 兜底: 表单编辑态改 IP, 提交后字段静默丢, 卡片 IP 仍是旧值, UI 上的输入跟 GET
回来的 IP 对不上。

## 决策
PM-direct 自起 (≤2h backend-only). 走 omh-plan 的骨架 (Goal/Non-goals/Assumptions/Acceptance/
Verification/Risks/Plan/Decision gate) 写 intent-M66.md + brief.

**关键设计变化**: 把 `Create` 的「写网卡」判据抽出为 `service.updateFirstNetworkIP(tx,
assetID, ipAddress)` 给 `Update` 复用 —— POST/PUT 同一入口避免判据漂移; `service.Update`
签名加 `ipAddress *string` 独立参数位 (nil/空串 = 不动网卡; 非空 = 复用 M64 的 v4/v6 分流
与 SELECT-first-再-write 判据); handler `UpdateAsset` 从 `updates map` 抽出 ip_address
(**不是 `delete`, 是 `extract**) → 走独立参数位; tx 包 SELECT 网卡 + UPDATE assets +
UPDATE/INSERT 网卡 整批; 失败任意一步回滚整批 (同 M64).

**M63 (T-76) 风险保持封死**: `assets` 表的 UPDATE SQL **仍不带** `ip_address` 列 ——
独立参数位走 `tx.First(&network)` + `tx.Create/Update` 网卡表, 不是 `tx.Updates(map)`.

## 验证

### backend
- `go build ./...`: 0 错
- `go test -count=1 ./...`: **27 packages ok** (service / handlers / api / apierr / apikey /
  cache / config / cmd/* 全绿)
- 6 backend tests touched:
  - 4 旧 caller 改 4 参签名 (TestAssetService_Update_成功 / _空updates只Get不写DB /
    _不存在返回ErrNotFound / _唯一冲突返ErrAlreadyExists)
  - 1 旧 M63 测试改名 TestM66_AssetService_Update_ipAddress_走独立参数不混进map
  - 1 新 service 层钉子: ip_address 进 ip 参数位 + 不在 updates map + SELECT 网卡 +
    INSERT/UPDATE 网卡
  - 2 handler 层钉子改名 + 加 INSERT asset_networks 期望

### mutation inversion (1 处)
- bypass `updateFirstNetworkIP` 调用 (把 if err != nil 块改成注释)
  - TestM66_UpdateAsset_带ip_address_assets无该列但网卡有行 → **FAIL** (expected 200, got 500)
  - 还原 → 测试通过
  - 实证: 测试钉子真红 (不是 stale 绿)

### frontend
- 无改动 (本轮纯 backend)
- `tsc --noEmit`: 0 错 (空跑)
- vitest 全量 489+ tests 应仍 PASS (未重新跑, 因为代码无 frontend 变更)

### 双轨分析
- **graphify multigraph 0 anomalies** (latest run 16:09)
- **codegraph** 6,532 nodes / 16,226 edges (latest run 16:09)
- **codegraph query "M66 updateFirstNetworkIP"** → 命中 1 method + 3 function (含钉子测试)

## commit
1 个 backend commit (`a63a7d`), 4 files / +206 / -89:

- `backend/internal/service/asset_service.go` — 抽出 `updateFirstNetworkIP` 给 Create/Update
  复用; `Update` 加 `ipAddress *string` 参数位; tx 包整批 (含网卡 SELECT/UPDATE); 删
  重复注释
- `backend/internal/api/handlers/asset_handler.go` — `UpdateAsset` 从 updates map 抽
  ip_address (删 M63 `delete` 兜底) → 走独立参数位
- `backend/internal/service/asset_service_test.go` — 5 caller 改 4 参签名; `TestM63` →
  `TestM66_AssetService_Update_ipAddress_走独立参数不混进map` (改 SELECT asset_networks
  期望 + INSERT asset_networks 期望 + 用 `WillReturnError(gorm.ErrRecordNotFound)` 触发
  isNew=true 分支)
- `backend/internal/api/handlers/asset_handler_test.go` — `TestM63_*` 两条 → `TestM66_*`:
  mock 路径断言 map 不含 ip_address 但独立参数位收到值; 路由级钉子断言 UPDATE assets 不带
  ip_address + INSERT asset_networks 多一条

## 派生 TODO (留 future)
- **G-Asset-IpConflictGuard**: 同 IP 多资产校验 (M63 `pickPrimaryIP` 不去重; M66 写网卡
  不查重 —— 两条网卡都拿同一 IP 不会报错, 要靠这条 future 兜)
- **G-UI-AssetIpValidatorParity-Mapped** (M65 派生保留): IPv4-mapped `::ffff:1.2.3.4` +
  zone id `fe80::1%eth0` 口径 (M66 复用 M64 的 v4/v6 分流覆盖同款 `To4()` 行为)

## 注意事项
1. **M66 mutation inversion 必须走 TestM66_UpdateAsset_带ip_address_assets无该列但网卡有行**:
   这是路由级钉子, 绕 `updateFirstNetworkIP` 直接 500 → 真红
2. **`WillReturnError(gorm.ErrRecordNotFound)` 是 sqlmock 的关键**: 空 rows 返回会让
   GORM `First()` 走"找到"路径而不是 `ErrRecordNotFound`, 触发 `isNew=false` 然后 UPDATE
   到 uuid.Nil (这是 M66 调通前的最大坑)
3. **service 层 `tx.Create` 后 `asset.ID` 没回写** (sqlmock 的 RETURNING 不会回写到
   结构体): 用 `assetID := uuid.New()` + `WillReturnRows(...AddRow(assetID))` + `WithArgs(assetID)`
   让 SELECT 期望与 INSERT 返回的 ID 对得上
4. **M63 的 `TestM63_*` 测试要重命名而不是删除**: 它们钉的核心行为 (UPDATE assets 不带
   ip_address 列) 仍是 M66 的正确行为, 只是观测点从 "delete" 改成 "extract + independent
   parameter"
