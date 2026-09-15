# M63 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-15 CST（M63 代码四笔 + 台账两笔：`33cec6d` feat Asset.IpAddress + pickPrimaryIP /
`215b69c` feat List/Get 投影（含 service 单测）/ `ccf086e` feat UpdateAsset 剥 ip_address + 测试 /
`be5c3da` refactor retireCore 复用 / `b7ab84a` docs CHANGELOG+TODO / `605d2d3` docs trap 编号 T-76）
**Scope**: M63 G-Asset-IpPersistence（backend 5 文件 + 2 测试文件；frontend **0 改动**）
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- **7065 nodes / 14542 edges / 453 communities**（M62 基线 7002 / 14420 / 453 → **+63 / +122 / ±0**）
- AST extraction: 122/122 uncached files（100%）
- 社区标签：458 saved labels、453 communities，本轮 **103 个社区按 hub 重命名**（`graphify label` 可刷新 LLM 名称，本轮未跑）

**`graphify diagnose multigraph`**: ✓ **0 anomalies**
- `missing_endpoint_edges: 0` / `dangling_endpoint_edges: 0` / `self_loop_edges: 0` / `exact_duplicate_edges: 0`
- `directed_unique_endpoint_pairs: 14542` = `raw_edges: 14542` → **0 条同端点坍缩**
- `directed/undirected_same_endpoint_collapsed_edges: 0` / `same_endpoint_group_count: 0`
- `relation_variant_groups: 0` / `context_variant_groups: 0` / `source_file_variant_groups: 0` / `source_location_variant_groups: 0`
- `unverified_code_nodes: 0`
- `producer_suppression_sites: 12`（`seen_ids`/`seen_keys`/`seen_doc_refs` arity=unknown，与 M59/M60/M61/M62 同源，非本轮引入）

### 本轮节点（可归因清单）

新代码节点 **19 个**（全部落在既有社区，**没有新社区**）：

| 来源 | 节点 |
|---|---|
| `backend/internal/service/asset_ip.go`（新文件） | 文件节点 + `pickPrimaryIP()` + `primaryIP()` |
| `backend/internal/service/asset_service_test.go` | `netCard()` + 7 个 `TestM63_*`（含 `PickPrimaryIP_判据` / `PrimaryIP_单值投影` / `List_投影ip_address` / `Get_投影ip_address` / `Get_无网卡时ip_address为nil` / `Create_ip_address不进INSERT` / `Update_map含模型外列时GORM照发SET`） |
| `backend/internal/api/handlers/asset_handler_test.go` | `strPtr()` / `sqlCapture`（+ `Match`/`joined`） / `newSQLCapturingDB()` + 5 个 `TestM63_*`（ListAssets / GetAsset / Update 剥键 / Update 路由级 SQL / Create 不进 INSERT） |

**触及新节点的边共 81 条**；其中**跨社区 25 条**，结构上唯一有意义的一条是：

```
.fetchIP()   [community 8]  --calls-->  primaryIP()   [community 22]
```

—— 复盘报告（社区 8，hub 标签 `github.com/google/uuid.UUID`）与资产（社区 22）之间**新增了一条实边**：
报告头的 IP 与资产列表/详情的主 IP 从此在图上也是同一个节点算出来的。其余 24 条是新测试到
`newMockDB` / `NewAssetService` / `testing.T` / `gorm.DB` / `strPtr`（社区 38）/ `AssetNetwork`
的常规测试边（测试文件各自成社区：`asset_service_test.go` = 50、`asset_handler_test.go` = 409）。

### 社区归属（本轮的关键观察）

| 社区 | hub 标签 | 规模 | 本轮相关节点 |
|---|---|---|---|
| **22** | `Asset` | 37 | `Asset` / `AssetNetwork`（`models/asset.go`）+ `asset_service.go` 全部符号（`AssetService` 接口、`List`/`Get`/`Update`/`Retire`/`listNetworks`/`injectPrimaryIPs`…）+ **`asset_ip.go` 的三个新节点（`asset_ip.go` / `pickPrimaryIP()` / `primaryIP()`）** |
| **8** | `github.com/google/uuid.UUID` | 46 | `.fetchIP()`（复盘报告取 IP）+ `PostmortemService` 一族 |
| 50 | `asset_service_test.go` | — | 7 个 `TestM63_*` + `netCard()` + 既有 asset service 用例 |
| 409 | `asset_handler_test.go` | — | 5 个 `TestM63_*` + `sqlCapture` / `newSQLCapturingDB()` / `strPtr()` / `newTestRouter()` |
| 38 | `strPtr()` 所在社区 | — | 测试用的取址 helper（`user_service_test.go` 原有，本轮 `asset_handler_test.go` 里也落了一个） |

**反向证据（本轮要验的就是这条）**：新建的 `asset_ip.go` 与它的两个函数**没有分叉出第二个社区** ——
它们和 `Asset` / `AssetNetwork` / `assetService` 同属社区 22。这正是「IP 判据唯一出口」在图上的形态：
不是「资产页一套规则」+「复盘一套规则」两份，而是**同一个簇里的一个节点**被两个调用方消费
（`injectPrimaryIPs` 在簇内、`.fetchIP()` 从簇外连进来）。`retireCore` 里那份被删掉的第三份循环
从来没有自己的节点 —— 它消失后社区 22 的规模变化只来自 `asset_ip.go` 的三节点，没有「孤儿簇」。

- 社区总数 **453 未变**而节点 +63：新增节点全部被既有社区吸收（本轮**没有**新页面/新模块，
  与 M62 的观察口径一致）。
- `+63` 里除上表 19 个代码节点外，其余是文档节点（`intent-M63.md` 的标题级节点、
  `CHANGELOG.md` / `TODO.md` 的本轮段落）—— 文档引用同样进图，于是「这条判据的来历」
  可从 `M63-completion-report.md` / `CHANGELOG.md` 回查。

## CodeGraph

**`codegraph sync`**: watcher 已追上（explore 返回的是磁盘当前版本，`asset_ip.go` 源码原样可见）

`codegraph explore "pickPrimaryIP primaryIP injectPrimaryIPs AssetService.List AssetService.Get fetchIP Asset.IpAddress asset_ip.go asset_service.go asset_handler.go UpdateAsset normalizeJSONBFields"` → 47 symbols / 5 files，返回的**调用链**正是本轮的意图：

```
1. List (asset_service.go:44 接口声明)
   ↓ dynamic: interface → impl @asset_service.go:69
2. List (asset_service.go:69)
   ↓ calls
3. injectPrimaryIPs (asset_service.go:190)
   ↓ calls
4. primaryIP (asset_ip.go:45)
   ↓ calls
5. pickPrimaryIP (asset_ip.go:25)
```

`Get` 走的是**同一终点**（`Get → primaryIP → pickPrimaryIP`），`fetchIP → primaryIP` 亦然 ——
四条入边汇进一个节点，就是「唯一出口」在图上的说法。

| 符号 | 位置 | blast radius |
|---|---|---|
| `pickPrimaryIP` | `asset_ip.go:25` | 2 callers in `asset_ip.go`（`primaryIP` + 文件内）+ 测试边 `asset_service_test.go` |
| `primaryIP` | `asset_ip.go:45` | **4 callers**（`asset_service.go` 的 `Get`/`injectPrimaryIPs` + `postmortem_service.go` 的 `fetchIP`）+ 测试边 |
| `injectPrimaryIPs` | `asset_service.go:190` | 1 caller（`List`）；**no tests found within 3 caller hops**（见下「图上看不见的那条边」） |
| `AssetService` / `NewAssetService` | `asset_service.go:43/65` | **28 callers**（含 `routes.go` 装配、各 service 单测） |
| `UpdateAsset` | `asset_handler.go:117` | 2 callers in `api/routes.go` + 测试边 `asset_handler_test.go` |
| `Asset` / `AssetNetwork` | `models/asset.go:12/100` | 本轮只多一个 `gorm:"-"` 字段，**schema 面不变**（codegraph 不把它当列） |

- **dynamic-dispatch 边**：`List → List`、`Get → Get`（接口 → 实现的接线点在
  `asset_handler_test.go:40/46`，即 mock service 那一侧）。这与 M62 的
  `Assets → AssetFormModal [dynamic: renders <AssetFormModal>]` 同族：**接口/组件的跳转在图上是靠
  wiring site 补出来的**，不是靠静态调用。
- **图上看不见的那条边（如实登记，与 M62 同一条分辨率边界）**：
  `injectPrimaryIPs` 被 report 为「no tests found within 3 caller hops」，而它**确实**被
  `TestM63_AssetService_List_投影ip_address` 打到（用例持 `AssetService` 接口调 `List`）。
  `pickPrimaryIP` 的测试边在 graphify 里也只是 **INFERRED**（不是 EXTRACTED）。
  所以 **「List 真的注入了 IP」这件事不是图给的，是 mutation ③ 与 `TestM63_*` 给的**：
  bypass `injectPrimaryIPs` 的调用、或删掉 `pickPrimaryIP` 的 v4 分支，用例会红在断言上；
  静态图到此为止（接口调用 + `gorm:"-"` 虚拟字段都是图不表达的语义）。
- `asset_ip.go` 的 `gorm:"-"` 字段 `IpAddress` **不出现在任何列清单里**：codegraph/graphify 都只把它
  当一个 struct field 节点，**无法**表达「它不在 INSERT/SET 里」—— 那条契约由
  `sqlCapture`（记录驱动实际收到的 SQL）的两条用例守，见「验证链」。

## M63 影响面（调用链）

```mermaid
flowchart TD
  subgraph svc [backend/internal/service]
    AIP["asset_ip.go（新）<br/>pickPrimaryIP: v4/v6 各取第一张非空<br/>primaryIP: v4 优先否则 v6, 无则 nil"]
    L["assetService.List<br/>一条 IN 查询 → injectPrimaryIPs"]
    G["assetService.Get<br/>复用已在手的 networks"]
    R["retireCore<br/>last_known_ip4/6（改调 pickPrimaryIP）"]
    F["PostmortemService.fetchIP<br/>报告头 IP（改调 primaryIP）"]
  end
  AN["models.AssetNetwork<br/>ipv4_address / ipv6_address（IP 真身, 列）"]
  M["models.Asset<br/>IpAddress *string gorm:\"-\"（虚拟字段, 非列）"]
  H["AssetHandler.UpdateAsset<br/>delete(updates, \"ip_address\") T-76"]
  API["GET /assets · GET /assets/:id<br/>items[i].ip_address · data.asset.ip_address"]
  PUT["PUT /assets/:id<br/>带 ip_address 不再 42703/500"]
  PDF["复盘 PDF 报告头"]
  FE["frontend AssetTable.tsx:105 IP 列<br/>:149/:159 Ping/Traceroute disabled 判据"]
  T1["asset_service_test.go 7 用例<br/>判据 7 subcase · 投影 · Create 不进 INSERT · Update 照发 SET"]
  T2["asset_handler_test.go 5 用例<br/>响应字段名 · 剥键 · 路由级 SQL 无该列"]
  L --> AIP
  G --> AIP
  R --> AIP
  F --> AIP
  AIP --> AN
  L --> M
  G --> M
  M --> API
  H --> PUT
  API --> FE
  F --> PDF
  AIP -.边界断言.-> T1
  H -.入口+SQL 断言.-> T2
```

- **四入边一终点**：`List`/`Get`/`retireCore`/`fetchIP` 全部走 `pickPrimaryIP`/`primaryIP`。
  其中 `retireCore` 与 `fetchIP` 是**本轮顺带收编**的既有实现（brief 只点名 `fetchIP`）——
  不收编的话 `asset_ip.go` 注释里那句「唯一出口」就是假的，而这正是 M62 报告里
  「规则唯一出口」的同一诉求。
- **写路径本轮不动**：`POST /assets` 与 `PUT /assets/:id` 的 `ip_address` 一律被忽略
  （虚拟字段不落库 / handler 剥键）。图上表现为「`AssetInput` → `Asset`」这条链路**没有**
  任何指向 `AssetNetwork` 的边 —— 也就是 `G-Asset-NetworksPersist` 要补的那条边。

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| backend `go test -count=1 ./...` | ✓ **27 packages ok** |
| backend `go test -count=1 ./internal/service/... -run Asset` | ✓ ok（含 M63 单测） |
| backend `go test -count=1 ./internal/api/handlers/... -run Asset` | ✓ ok（含 M63 handler 用例） |
| backend M63 用例计数（`-v`） | ✓ **12 个顶层用例 + 7 个 subcase = 19 条断言路径全 PASS** |
| backend `gofmt -l`（本轮 7 个改动文件） | ✓ 干净（全仓另有 3 个 M63 未触碰的既存文件不干净，M61 已登记） |
| backend `go vet ./internal/service ./internal/api/handlers ./internal/models` | ✓ 无新告警（仅 sqlite3 cgo 的 `-Wdiscarded-qualifiers` 编译噪声，与代码无关） |
| frontend `npx tsc --noEmit` | ✓ 0 error（本轮 frontend **0 改动**） |
| frontend `npx vitest run src/components/AssetTable.memo.test.tsx src/pages/Assets.test.tsx` | ✓ **2 files / 27 tests PASS**（无退化） |
| mutation inversion ①（bypass `delete(updates, "ip_address")`） | ✓ `TestM63_UpdateAsset_剥掉ip_address键` + `TestM63_UpdateAsset_带ip_address不产生该列的SQL_返200` **2 failed**，失败信息含驱动实际 SQL：`UPDATE "assets" SET "ip_address"=$1,"name"=$2,"updated_at"=$3 …` |
| mutation inversion ②（`primaryIP` 反转 v4/v6 优先级） | ✓ `TestM63_PrimaryIP_单值投影` + `List_投影ip_address` + `Get_投影ip_address` **3 failed** |
| mutation inversion ③（删 `pickPrimaryIP` 的 v4 分支） | ✓ **8 failed**：`TestAssetService_Retire_成功_IP转移到last_known`、`TestFetchIP_IPv4优先`、`TestGenerateReport_有IP_填入ReportData`、`TestM63_PickPrimaryIP_判据`、`TestM63_PrimaryIP_单值投影`、`List_投影ip_address`、`Get_投影ip_address`、既有 `TestAssetService_List_带keyword和status过滤` |
| mutation reversal（三处全部还原） | ✓ 两包全绿，`git status` 干净 |
| 与 T-76 的关系（GORM 对模型外列照发 SET） | ✓ **由 mutation ① 从「源码推理」升级为「渲染语句级证据」**（M62 只到源码引用） |
| 真 PG 往返（42703 实测） | ✗ **未做**：本机无 PG 服务端（仅 `postgresql-libs` 客户端；docker API 无权限）——见 completion report 残余 |
| graphify diagnose | ✓ 0 anomalies（0 missing / 0 dangling / 0 self_loops / 0 collapses / `unverified_code_nodes: 0`） |
| codegraph index | ✓ 新符号已入图（`primaryIP` 4 callers 跨两个 service 文件；`pickPrimaryIP` 2 callers + 测试边；`injectPrimaryIPs` 挂在 `List` 下） |
| TODO + CHANGELOG | ✓ ship（M63 段在 M62 之前；`G-UI-AssetIpPersistence` 结案 + 派生 2 条） |

## 本轮 trap 记录

> **编号冲突（本轮新发现，已如实登记并绕开）**：`T-75` 在本仓**已经有两个不同含义** ——
> ① `M61-completion-report.md:209` 与 `7dface4` 的 commit message：全站白屏
> （`<CommandPalette />` 挂在 `<BrowserRouter>` 外调 `useNavigate()`）；
> ② `CHANGELOG.md:473`（M62 段）：拼接正则时被外层兜底掩盖的锚点缺失。
> brief 与 `intent-M63.md` 都要求本轮这条记作 `T-75`，若照办则编号既回查不到
> `docs/TRAPS.md` 的条目、也回查不到本轮。故本轮用 **`T-76`**（全仓未使用，已核对
> `CHANGELOG.md` / `docs/TRAPS.md` / 各 completion report 与 graph analysis 里的 `T-*` 全集），
> 并在代码注释里保留「brief 写作 T-75」的映射说明（`asset_handler.go:130`）。

**T-76（`db.Updates(map)` 不丢弃「模型里不存在」的键 —— 绑定层静默收下、DB 层 42703 爆）**：
`AssetHandler.UpdateAsset` 把整张 JSON map 交给 `service.Update` → `db.Model(&asset).Updates(map)`。
GORM v1.30.0 的 `callbacks/update.go` 在 `LookUpField` 未命中（模型无此字段）时**不跳过**该键：
`selectColumns` 为空且 `restricted=false`，于是照样 `append(clause.Assignment{Column:{Name:k}})`。
`assets` 表没有 `ip_address` 列 → `SET "ip_address"=$n` → PG **42703** → `apierr.Internal` → **500**。
本轮实测到的语句原文（mutation ① 捕获，`sqlCapture` 把驱动收到的 SQL 抄下来）：

```
UPDATE "assets" SET "ip_address"=$1,"name"=$2,"updated_at"=$3 WHERE "id" = $4
```

危险点在**两层各自都「没错」**：JSON 绑定层（`map[string]interface{}` 什么都收）不会报错，
GORM 也不会报错 —— 只有数据库会，而它报的是列名，不是「你少加了字段」。
同族：T-46（`Updates(map)` 里的 `clause.Expr` 不回写 struct 字段）、T-43（状态机只编码在某一层）。
**修法**：handler 入口显式 `delete(updates, "ip_address")`（本轮），根本解是让该字段有真正的落点
（`G-Asset-NetworksPersist` 写 `asset_networks`）。

**T-45 的延续（本轮再次踩到同一块地基，未新开编号）**：`pickPrimaryIP` 的前置条件写进了函数注释 ——
入参必须是**已排序**的网卡列表（`ORDER BY created_at ASC, id ASC`）。`List` 新增的那条 IN 查询
因此**必须**带同一个 `ORDER BY`：漏掉它，「第一张网卡」在 PG 上没有定义，同一批数据可能
「列表显示 eth1 的 IP、退役快照存 eth0 的 IP」。用例钉的是「v4 在第二张卡上时仍选它」，
排序本身由既有 `listNetworks` 的契约注释与 sqlmock 的 `ORDER BY` 匹配守住。
