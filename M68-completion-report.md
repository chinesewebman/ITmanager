# M68 Completion Report — G-Asset-IpConflictGuard 同 IP 多资产守卫

## 摩擦
M64/M66 把 IP 写入路径打通了, 但**没有任何守卫**禁止两台资产填同一个 IP:
- `asset_networks.ipv4_address` 只有普通 index (非 unique 约束)
- 退役释放 IP 后能否复用是产品决定 (允许), 所以 DB unique 约束做不到这点
- 前端 `AssetTable` 的 Ping/Traceroute 正是拿这个 IP 去定位设备 —— 打错一台的代价是
  "现场找不到设备" (与 M62 加 IP 格式闸同源的动机)

## 决策 (PM-direct 拍)
- **Decision 1** (handler 返 409): 与 ErrAlreadyExists 口径一致, 都是"状态冲突"。两者
  文案不同 ("资产已存在" vs "IP 地址已被其他资产占用"), 调用方按 message 区分。
- **Decision 2** (业务规则 vs DB unique): 选业务规则。退役释放 IP 后必须允许复用, DB
  unique 约束做不到; 并发场景单实例 + tx 足够。
- **Decision 3** (v4 only): v6 地址空间大冲突概率低, 边际收益 < 边际测试成本。
- **Decision 4** (不返占用资产 ID): 信息泄露风险 (viewer 不应该看到其他资产 ID)。
  默认仅返 409 + 文案, 不带占用的资产 ID。future 追 G-Asset-IpConflictAudit。

## 关键设计变化

### service 层
- 新 sentinel `ErrIPConflict = errors.New("ip address already in use by another asset")`,
  与 `ErrAlreadyExists` 区分 (asset.name vs asset_networks.ipv4_address 两层都用 409 但
  语义不同)。
- `updateFirstNetworkIP` 在 SELECT 网卡**之前**先做 IP 占用检查:
  ```sql
  SELECT id FROM asset_networks
  WHERE ipv4_address = $1 AND asset_id <> $2 AND ipv4_address <> ''
  LIMIT 1
  ```
  - 自排除必须用 `asset_id <> ?` 而非 `network.ID` —— INSERT 分支 (isNew=true) 还没网络行
  - `ipv4_address <> ''` 排除空字符串 (PG 上 `'' = NULL`, 但 mysql/sqlite 不同; 显式排除稳)
  - v6 跳过 (`parsed.To4() == nil` 直接跳过整个 if 块)
- 守卫位置: SELECT 网卡**之前**, 不是之后; 失败早返 (不浪费 SELECT 网卡 + INSERT/UPDATE)
- 守卫 SQL 走 `tx.Raw().Scan(&taken)` 而非 `tx.Where(...).First(&taken)` —— 选 Raw 是因为
  `First` 默认会 panic 当 id 字段被当作 struct 路径时; Raw + Scan + LIMIT 1 是安全的"全表查"
  模式

### handler 层
- `CreateAsset` 错误映射加 `errors.Is(err, service.ErrIPConflict)` → 409 + "IP 地址已被
  其他资产占用" (在 `ErrAlreadyExists` 之前先判, 因为更具体)
- `UpdateAsset` 错误映射同样加 ErrIPConflict → 409

## 验证 (实证)

### backend
- `go build ./...`: 0 错
- `go test -count=1 ./...`: **27 packages ok**
- 新增 4 个 test (3 service + 1 handler):
  - TestM68_AssetService_ip被其他资产占用_返ErrIPConflict (service mock)
  - TestM68_AssetService_自己资产持同一IP_不冲突 (self-exclude 钉子)
  - TestM68_AssetService_v6_不参与v4校验 (v6 跳过守卫)
  - TestM68_CreateAsset_IP已被占用_返409 (handler→service→HTTP 状态码)

### mutation inversion (1 处)
- bypass `updateFirstNetworkIP` 内的 IP guard (if v4 块整段注释)
  - TestM68_AssetService_ip被其他资产占用_返ErrIPConflict → **FAIL**
  - 还原 → 全绿
  - 实证: 测试钉子真红 (不是 stale 绿)

### 双轨分析
- **graphify multigraph 0 anomalies** (latest run)
- **codegraph** 6,532+ nodes / 16,226+ edges (M66 baseline + M68 minor increments)
- **codegraph query "M68 ErrIPConflict"**: 命中 1 method (`updateFirstNetworkIP`) +
  2 functions (TestM68_AssetService_ip被其他资产占用_返ErrIPConflict +
  TestM68_CreateAsset_IP已被占用_返409)

### frontend
- 0 改动 (本轮纯 backend)
- `tsc --noEmit`: 0 错 (待 verify 实证)

## commit
1 个 backend commit (`cc7a46d`), 4 files / +172:

- `backend/internal/service/asset_service.go` — 加 `ErrIPConflict` sentinel + 守卫 Raw SQL
  嵌入 `updateFirstNetworkIP`
- `backend/internal/service/asset_service_test.go` — 3 新 case (占用冲突 / self-exclude /
  v6 跳过); 已有 M66 case 加 guard 期望
- `backend/internal/api/handlers/asset_handler.go` — CreateAsset + UpdateAsset 错误映射
  加 ErrIPConflict → 409
- `backend/internal/api/handlers/asset_handler_test.go` — 1 新 case (handler→service→HTTP
  链路); 已有 M64/M66 case 加 guard 期望

## 派生 TODO (留 future)
- **M69 = G-Asset-IpConflictGuard-v6** (v6 也做冲突守卫, 待 product 决定; v6 地址空间大
  冲突概率低, 边际收益 < 边际测试成本)
- **M70 = G-Asset-IpConflictAudit** (后台巡检视图报现有重复 IP, 历史数据无法用 guard 拦住)

## OMH workflow shape 实证 (M67 派生)

M68 intent-M68.md 用了 omh-plan 8 节骨架, brief 也带 `ulw-work` / `omh-decide` 描述。
实证:

1. **Non-goals 节** 写出来时, 发现"v6 留 future"和"UI 改不改"已经清晰; 不写出来容易无脑做
2. **Decision gate 节** 写出来时, 钉死 4 个决策 (handler 409 / 业务规则 vs DB / v4 only /
   不返占用资产 ID), 中途没漂
3. **Verification 节** 写"mutation inversion 实证 1 处" → 实际跑了, 真红 → 还原绿;
   不是空话

OMH 没自动接管我的 PM-direct 流程, 但 omh-plan 骨架帮我在写 intent 阶段就钉死漂移面。
这是"更好地使用 omh"的实际收益 — 不是"OMH 接管 PM-direct", 而是"OMH 提供骨架让我写得
更结构化"。

## 注意事项
1. **sqlmock QueryMatcherRegexp 与 PG 占位符**: GORM 在 PG 走 `$1`/`$2` 占位符, sqlmock
   默认 matcher 把 expected 当 regex 处理; `?` 是 regex meta (0 或 1), `$` 是 regex anchor。
   必须用 `\\$1` / `\\$2` 转义。`ipv4_address <> ''` 在 backtick string 里就是 `''` (两个
   单引号) — 不要写 `\\'\\'`。
2. **`tx.Raw().Scan(&taken)` 在 0 行时返 `gorm.ErrRecordNotFound`**, 不是 nil。这是 GORM
   的设计; 我们用 `errors.Is(err, gorm.ErrRecordNotFound)` 把它当"没占用"处理。
3. **守卫位置要在 SELECT 网卡之前**: 失败早返, 不浪费后续 SQL; INSERT 分支 (isNew=true)
   还没 network.ID, 必须用 assetID 排除。
4. **handler 错误映射顺序**: ErrIPConflict 必须在 ErrAlreadyExists **之前**判 —— 两者都
   映 409, 但文案不同; 先判更具体的 (IP 冲突) 比先判更宽泛的 (资产已存在) 更准。
