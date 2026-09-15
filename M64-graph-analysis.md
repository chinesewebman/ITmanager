# M64 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-15 CST（M64 代码四笔 + 台账一笔：`2e3ba0f` feat AssetService.Create 写 AssetNetwork /
`ff3a230` feat CreateAsset 嵌套 input + 422 / `e41598a` test 6+6 case / `ae54415` docs openapi + gen:api /
本次 docs CHANGELOG+TODO+双轨+report）
**Scope**: M64 G-Asset-NetworksPersist（backend 6 文件 + 3 测试文件；frontend **0 来源改动**，仅重跑
`gen:api` 的生成物 `src/services/api.types.ts`）
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- **7143 nodes / 14690 edges / 454 communities**（M63 基线 7065 / 14542 / 453 → **+78 / +148 / +1**）
- AST extraction: 123/123 uncached files（100%）
- 社区标签：456 saved labels、454 communities，因新增文档节点，**79–101 个社区按 hub 重命名过**
  （hub 标签会随成员的进出而变，见下「社区归属」的稳定性说明；`graphify label` 未跑）
- **两次运行**：代码落定后跑一次（7122 nodes，用于代码侧归因），台账/报告写完后**再跑一次**
  （7143 nodes，把本轮文档折进图）。下面的数字以**最终（第二次）**为准

**`graphify diagnose multigraph`**: ✓ **0 anomalies**
- `missing_endpoint_edges: 0` / `dangling_endpoint_edges: 0` / `self_loop_edges: 0` / `exact_duplicate_edges: 0`
- `directed_unique_endpoint_pairs: 14690` = `raw_edges: 14690` → **0 条同端点坍缩**
- `directed/undirected_same_endpoint_collapsed_edges: 0` / `same_endpoint_group_count: 0`
- `relation_variant_groups: 0` / `source_file_variant_groups: 0` / `source_location_variant_groups: 0` / `context_variant_groups: 0`
- `unverified_code_nodes: 0`
- `producer_suppression_sites: 12`（`seen_ids`/`seen_keys`/`seen_doc_refs` arity=unknown，
  与 M59–M63 同源，非本轮引入）

### 本轮节点（可归因清单）

新代码节点 **21 个**（全部落在既有社区，**没有新代码社区**）：

| 来源 | 节点 | 社区 |
|---|---|---|
| `backend/internal/api/handlers/asset_handler.go` | `invalidIPAddress()` | handlers 簇（最终图 id **430**，hub 标签 `Unauthorized`） |
| `backend/internal/apierr/apierr.go` | `Unprocessable()` | 32（hub 标签 `apierr_test.go`，apierr + 其测试同簇） |
| `backend/internal/service/asset_service_test.go` | `newAssetSQLiteDB()` / `networksOf()` / `assetRowCount()`（3）+ 6 个 `TestM64_*` | **50** |
| `backend/internal/api/handlers/asset_handler_test.go` | `newAssetSQLiteHandlerDB()` / `postAsset()`（2）+ 6 个 `TestM64_*` | **409** |
| `backend/internal/apierr/apierr_test.go` | 2 个 `TestUnprocessable_*` | 32 |

（另有两个**改名**节点：`TestM63_AssetService_Create_ip_address不进INSERT` →
`TestM63_AssetService_Create_虚拟字段不进INSERT也不建网卡`、`TestM63_CreateAsset_带ip_address_JSON解析OK但不进INSERT`
→ `TestM64_CreateAsset_带ip_address_assets无此列但网卡有行`。M63 的「不落库」语义被本轮替换，
用例名跟着换；图上表现为旧节点消失、新节点出现 —— **不是**新增了一条边，是同一格换了个名字。）

社区规模（M63 基线 → 最终图）：

| 社区 | 标签 | M63 | M64 | Δ |
|---|---|---|---|---|
| 22 | `Asset` | 37 | **38** | +1* |
| 50 | `asset_service_test.go` | 44 | **53** | +9 |
| 409 | `asset_handler_test.go` | 27 | **34** | +7 |
| handlers 簇 | 最终 id **430**（hub 标签 `Unauthorized`） | — （不可比，见下） | **21** | n/a |
| 32 | apierr 簇 | 35 | **35** | ±0（+`Unprocessable()` + 2 个测试，抵掉两条漂走的既有测试） |

\* 社区 22 的 +1 不是本轮代码：漂进来的是 `alert_suppression_service.go` 的 `.Create()`（**同名方法**，
非 `assetService.Create`），同时漂走一个既有节点。**同名方法的社区归属会漂，hub 标签也会漂**
（handlers 簇两次运行的 id 从 115 变成 430、标签从 `NotFound` 变成 `Unauthorized`）。
更直接的证据：最终图里 handlers 簇（21 节点）只收了 `asset_handler.go` 的 6 个方法，
而 `.ListAssets()` / `.GetAsset()` / `.UpdateAsset()` / `.DeleteAsset()` 被分到了别的簇
（hub 是 `auth_handler.go` 的 `Unauthorized()`）——**同一文件的方法不保证同簇**。
所以社区这一层只能回答「新节点落在**哪一簇**」，**不能**用来做规模增减的论据
（这也是 M63 报告里 `asset_ip.go` 三节点「没有分叉出第二个社区」那条观察之所以成立的原因：
它说的是**反例不存在**，而不是在比大小）。本轮真正可靠的归因是上表那 21 个节点 id + 下面的边。

### 本轮触及新节点的边

新节点入边 **76 条**（含新增测试节点；两次运行后此数不变），其中**跨社区 17 条**，
结构上有意义的只有 1 条：

```
.CreateAsset()   [handlers 簇]  --calls-->  Unprocessable()   [apierr 簇]
```

—— 这是 M64 新造的那条路径在图上唯一的跨簇实边：**handler 的入参校验**（handlers 簇）第一次把
**422 出口**（apierr 簇）接上。其余 16 条是新测试到 `testing.T`（12）、测试文件互调
`NewAssetService`（3，`asset_handler_test.go → asset_service_test.go`）与 apierr 测试到 `gin.Context`
（1）的常规测试边。

**反向证据（本轮要验的）**：`invalidIPAddress()` 与 `AssetHandler` / `.CreateAsset()` **同簇**
（handlers 簇），且 `.CreateAsset() --calls--> invalidIPAddress()` 是**簇内边** —— 判据与它的唯一消费者
住在一起，不是「handler 一套判据、service 另一套」。service 侧的兜底判据（`net.ParseIP`）在
`assetService.Create` 内部，不上图（见下「图上看不见的边」）。

### 文档节点

净增 `+78` 的构成（两次运行合计，按 id 逐个对账）：

| 类别 | 第一次运行（代码落定后） | 第二次运行（台账写完后） |
|---|---|---|
| 新代码节点 | 21（见上表） | 0 |
| 改名节点（旧 id 消失、新 id 出现） | 2（两条被本轮替换语义的 `TestM63_*`） | 0 |
| 文档节点 | **49**：`M63-completion-report.md` 16、`intent-M64.md` 11、`M63-graph-analysis.md` 9、`CHANGELOG.md` 各段落 13（含 `[未发布]`/`[vX.Y.Z]` 段重锚） | **33**：`CHANGELOG.md` M64 段 13、`M64-graph-analysis.md`（本文）10、`M64-completion-report.md` 9、`docs/TRAPS.md` T-77 条目 1 |
| 漂移（既有节点换了 id：行号变动、聚类边界移动） | 12 移除 / 部分新增 | 12 移除 |

即 `净增 78 = (新增 104) − (移除 26)`；`104` 里代码只占 21，其余是文档与重锚。
**文档进图**这件事本身是有用的：图上能查到「T-77 这条判据的来历」指向 `docs/TRAPS.md` 的条目、
`M64-completion-report.md` 的 §2/§7 与 `CHANGELOG.md` 的 M64 段。

## CodeGraph

**`codegraph sync`**: watcher 已追上（explore 返回磁盘当前版本，`invalidIPAddress` / `Unprocessable` 源码原样可见）

`codegraph explore "AssetService.Create assetService.Create CreateAsset invalidIPAddress Unprocessable AssetNetwork models.Asset.IpAddress"` → 81 symbols / 5 files，blast radius：

| 符号 | 位置 | blast radius |
|---|---|---|
| `CreateAsset`（handler） | `asset_handler.go:115` | 挂在 `AssetHandler` 上；路由装配在 `routes.go`；本轮 5 条新测试边 |
| `invalidIPAddress` | `asset_handler.go:98` | **1 caller**（`CreateAsset`）—— 与 graphify 的簇内边一致 |
| `Unprocessable` | `apierr.go:103` | 1 caller（`CreateAsset`）+ 2 测试边（`apierr_test.go`） |
| `AssetService`（接口） | `asset_service.go:44` | 3 callers（`asset_handler.go` 的构造 + 接口→实现接线）+ 测试边 |
| `NewAssetService` | `asset_service.go:68` | **34 callers**（`routes.go` 装配 + 各 service/handler 单测） |
| `AssetNetwork` | `models/asset.go:100` | **12 callers**（`asset_ip.go` + `asset_service.go`）+ 2 个测试文件 |
| `assetService.Create` | `asset_service.go:234` | 接口方法（1 实现）+ 本轮 6 条 service 测试边 |

- **dynamic-dispatch 边**：`AssetService [interface] → assetService.Create [impl]` 的接线点在
  `asset_handler_test.go` 的 mock（`mockAssetService.Create`）与 `routes.go:168` 的
  `service.NewAssetService(db)`。这与 M63 的 `List → List` / `Get → Get` 同族：
  **接口跳转靠 wiring site 补出来**，静态调用图里没有这条边。
- **图上看不见的边（如实登记，与 M62/M63 同一条分辨率边界）**：
  1. `assetService.Create` 的 SQL 面（`INSERT INTO "assets"` + `INSERT INTO "asset_networks"`）
     **完全不进图** —— `tx.Create(asset)` / `tx.Create(network)` 是本地变量的方法调用，
     既不是包级符号也没有静态类型边。graphify 里 `.Create()` 的出边只有
     `--references--> Asset` / `context.Context` / `--calls--> isUniqueViolation()`：
     **「网卡行到底写没写、写进哪一列」不是图给的**，是 mutation ① 与真 sqlite 用例给的。
  2. `models.Asset.IpAddress`（`gorm:"-"`）在图上只是个 struct field 节点，
     **无法表达「它不进 INSERT」**；这条契约由 `sqlCapture`（记录驱动实际收到的 SQL）与
     `TestM64_CreateAsset_ip_address走独立入参不变虚拟字段` 守。
  3. `AssetHandler` 被 codegraph 报为「no tests found within 3 caller hops」，而它**确实**被
     `asset_handler_test.go` 的 15 条用例打到（经 `newTestRouter` 装配的 `*gin.Engine`）。
     与 M63 的 `injectPrimaryIPs` 同一条边界：**wiring 在测试里做，图上看不到**。

## M64 影响面（调用链）

```mermaid
flowchart TD
  FE["frontend AssetFormModal.tsx:93<br/>Form.Item name=ip_address + ipRules (M62)"]
  AS["frontend Assets.tsx:106-115<br/>createMut → assetApi.create(values)"]
  API["POST /assets (routes.go)"]
  H["AssetHandler.CreateAsset<br/>嵌套 input{models.Asset; IpAddress *string}"]
  V["invalidIPAddress(ip)<br/>nil/空串 = 未提供, 否则 net.ParseIP"]
  U["apierr.Unprocessable<br/>422 + validation_failed（本轮新增出口）"]
  S["assetService.Create(ctx, asset, ipAddress)<br/>tx: INSERT assets → INSERT asset_networks"]
  AN["models.AssetNetwork<br/>v4→ipv4_address / v6→ipv6_address（列）"]
  M["models.Asset.IpAddress<br/>gorm:\"-\" 虚拟字段（非列, 只读投影）"]
  GET["GET /assets · GET /assets/:id<br/>M63 投影: pickPrimaryIP → ip_address"]
  T1["asset_service_test.go 6 用例<br/>无IP/空串/v4/v6/非法/回滚（真 sqlite）"]
  T2["asset_handler_test.go 6 用例<br/>端到端落行 · 负控 · 422 · 入参不变虚拟字段 · SQL 原文"]
  FE --> AS --> API --> H
  H --> V
  V -->|非法| U
  V -->|合法/未提供| S
  S --> AN
  M -.只读投影, 本轮再次确认它不是写入通道.-> S
  AN --> GET
  S -.真库断言.-> T1
  H -.入口+SQL 断言.-> T2
```

- **M63 缺的那条边补上了**：M63 的图上「`AssetInput` → `Asset`」这条链路**没有**任何指向
  `AssetNetwork` 的边（表单里的 IP 到此为止）。M64 补的就是 `S → AN`：
  `POST /assets` 的 `ip_address` 与网卡表之间第一次有了实边。
- **`M -.-> S` 是虚线且写「不是写入通道」**：`models.Asset.IpAddress` 仍然不参与任何 SQL
  （`gorm:"-"`），本轮只是把「值从哪来」改成显式入参。这条虚线的存在本身就是 T-77 的形状。
- **两个写入源的顺序**：`AN` 同时被「M64 的 Create」与「B4 的 Retire/Restore」写，
  但两者口径不同 —— Create 建第一张卡（`created_at ASC, id ASC` 语义上的第一张），
  Retire/Restore 只碰既有网卡。图上它们在同一簇，靠 `listNetworks` 的排序契约区分（T-45）。

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| backend `go test -count=1 ./...` | ✓ **27 packages ok**（含本轮 4 处 mutation 还原后的复跑） |
| backend `go test -count=1 ./internal/service/... -run Asset` | ✓ ok（含 6 个 M64 service 用例） |
| backend `go test -count=1 ./internal/api/handlers/... -run Asset` | ✓ ok（含 6 个 M64 handler 用例） |
| backend `go test -count=1 ./internal/apierr/...` | ✓ ok（含 2 个 `TestUnprocessable_*`） |
| backend M64 用例计数（`-v`） | ✓ **service 6 + handler 6 顶层用例全 PASS**（另含 3 个 subcase：空串/空白、IPv6 全写归一） |
| backend `gofmt -l`（本轮 7 个改动文件） | ✓ 6 个干净；`tests/db_smoke_test.go` **在 HEAD 上就不干净**（Go 1.19+ 注释重排规则，本轮只改了一行调用签名，未碰注释）——全仓这类既存不干净文件共 4 个，非本轮引入 |
| backend `go vet ./internal/service ./internal/api/handlers ./internal/apierr ./tests` | ✓ 无新告警（仅 sqlite3 cgo 编译噪声） |
| frontend `npx tsc --noEmit` | ✓ 0 error（`gen:api` 重生成后） |
| frontend `npx vitest run`（全量） | ⚠ **47 files / 489 tests，488 passed / 1 failed** —— `src/pages/Settings.test.tsx` 一条 antd 校验弹窗断言（`findByText("请输入SMTP服务器")`）超时；**单独复跑 58/58 PASS**。该文件与本轮改动无交集（backend + openapi + 生成物），且失败发生在同机并发跑「全量 frontend + 全量 backend」时 —— 与 M61 retro 记录的同一现象（并发下 antd 时序断言偶发红）。**如实登记为已知 flake，不当作绿** |
| frontend `npx swagger-cli validate backend/.../openapi.yaml` | ✓ valid |
| frontend `npm run gen:api` | ✓ 生成物 diff 只有 `Asset.ip_address: string \| null` 与 `AssetInput.ip_address` 必填→可选 |
| mutation ①（bypass `tx.Create(network)`） | ✓ **5 failed**：service `IPv4落ipv4列` / `IPv6落ipv6列` / `网卡写失败时资产行一并回滚` + handler `带ip_address_落成网卡行` / `带ip_address_assets无此列但网卡有行` |
| mutation ②（去掉 v4/v6 分流：`v4 != nil \|\| true`） | ✓ `TestM64_AssetService_Create_IPv6落ipv6列` **1 failed**（断言在 `ipv4_address` 里读到值） |
| mutation ③（关掉 handler 入口校验） | ✓ `TestM64_CreateAsset_非法ip_address_返回422` **1 failed** |
| mutation ④（关掉 service 兜底 `if false && …`） | ✓ `TestM64_AssetService_Create_非法IP不落库` **1 failed** |
| mutation reversal（四处全部还原） | ✓ 两包全绿，`git status` 干净（`grep -c MUTATION` = 0） |
| 真 PG 往返 | ✗ **未做**：本机无 PG 服务端（`postgresql-libs` 只有客户端，docker 不可用）—— 见 completion report 残余 |
| graphify diagnose | ✓ 0 anomalies（0 missing / 0 dangling / 0 self_loops / 0 collapses / `unverified_code_nodes: 0`） |
| codegraph index | ✓ 新符号已入图（`invalidIPAddress` 1 caller、`Unprocessable` 1 caller + 2 测试边、`assetService.Create` 接口接线） |
| TODO + CHANGELOG | ✓ ship（`G-Asset-NetworksPersist` 结案；派生 2 条；`G-Asset-IpPersistence-Contract` 标注「M64 修掉主体」） |

> 全量 frontend 用例数是本轮**顺带**跑的（brief 只要求 `AssetFormModal` + `Assets` 两个文件）：
> `gen:api` 改的是生成物，而生成物被 11 个源文件引用 —— 用全量跑替代「应该没事」的推断。

## 本轮 trap 记录

**T-77（投影字段当写入通道：绑定层收下、schema 层排除 —— 接口返 201，库里什么都没有）**：

`models.Asset.IpAddress` 是 `gorm:"-"` 的**只读投影字段**（M63 为「IP 属于网卡」而这样设计）。
把表单里的 IP 绑进它时，**三层各自都「没错」**：

1. `c.ShouldBindJSON(&asset)`：字段在，JSON 合法 → 绑定成功，进内存 struct；
2. `db.Create(asset)`：GORM 在 schema 解析阶段把这个字段排除在 `Fields` **之外**（`gorm:"-"`），
   不报错、不警告，生成的 INSERT 里**根本没有**这一列；
3. handler 看到 `err == nil` → **201**。

于是出现「填了 IP 点创建 → 成功提示 → 列表里那台设备 IP 为空」。**危险点在它长得像成功**：
没有错误、没有告警、日志干净，只有用户会在几天后问「为什么这台设备 ping 不了」——
而前端 `AssetTable` 的 Ping / Traceroute 恰是以 `!record.ip_address` 禁用的（M63 已确认）。

**检测线索**：

1. 某个 JSON 字段在响应里读得到、在**任何** SQL 里都找不到（用 `sqlCapture` 把驱动实际收到的语句抄下来，
   而不是靠「SQL 里应该没有吧」）；
2. 该字段在模型上的 tag 是 `gorm:"-"` / `json:"-"` / 带 `->` 的只读关系 —— 这类字段**天然**是
   「绑定层收、持久层丢」的形状；
3. 「改了内容但对象没变」的前后对比缺失：只看接口返回的 201，不看**目标表**里那一行。

**解法**（本轮）：把值从**虚拟字段**里拿出来，走**显式入参** —— handler 用匿名嵌套结构
`struct { models.Asset; IpAddress *string }`，`service.Create(ctx, asset, ipAddress)` 拿它建网卡行；
用例钉两件事：① 值真的到了目标表（真 sqlite 读回 `ipv4_address`）；② `service` 收到的
`Asset.IpAddress` 必须为 `nil`（虚拟字段不是写入通道，这条断言就是 T-77 的回归网）。
**不要**用「给模型加一列」来绕过它 —— 那会把两份存储（`assets.ip_address` 与
`asset_networks.ipv4_address`）同时留下，退役/恢复只改后者，漂移立刻发生（M63 的取舍，本轮沿用）。

**同族**：T-76（`db.Updates(map)` 不丢弃模型外的键 → 绑定层静默收下、DB 层 42703 爆）。
两者是**同一个设计的两侧**：`gorm:"-"` 字段在 **Create** 路径被静默丢弃（本轮修），
在 **Update(map)** 路径被照单全收并炸在 SQL 上（M63 修）。M64 之后两条路径都指向
「虚拟字段不是写入通道，写入必须有显式落点」。

**T-52 的一个新实例（不新开编号）**：前端 `validators.ts` 的 `IPV4_PATTERN` 与后端
`net.ParseIP` 是**同一个问题的两份实现**，且判据不等价 —— `OCTET = "[01]?\d\d?"` 接受
**前导零八位组**（`010.1.1.1` 过表单），而 `net.ParseIP` 自 Go 1.17 起拒绝它。
M64 之前这个差异无害（后端不看这个值），M64 之后它有了用户可见后果：**过表单 → 吃 422**。
反方向也存在（前端不收 IPv4-mapped `::ffff:1.2.3.4`，后端接受并归一成 `1.2.3.4`）。
按 T-52 的口径登记为派生 TODO `G-UI-AssetIpValidatorParity`（需先定产品口径：前导零算不算写错）。
**本轮不擅自改前端**（0 前端来源改动原则），改动方向与代价写进 TODO。

**T-45 的延续（再次踩到同一块地基，未新开编号）**：新网卡行的 `created_at` 与「第一张卡」的语义
绑在一起 —— `listNetworks` 的 `ORDER BY created_at ASC, id ASC` 是「第一张」的定义处（M63 已论证
PG 不保证无 ORDER BY 的行序）。M64 建的这张卡**恰好是每个资产的第一张**（也是唯一一张），
所以本轮不引入新的顺序假设；将来 `G-Asset-MultiNetwork` 加第二张卡时，这条排序就是「谁是第一张」
的唯一仲裁者，不能被绕过。
