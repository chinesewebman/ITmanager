# M63-completion-report — G-Asset-IpPersistence 资产 IP 投影 + Update 防 500（M62 派生 TODO）

**Shipped**: 2026-09-15（代码 4 笔 + 台账 2 笔：`33cec6d` feat Asset.IpAddress + `pickPrimaryIP` /
`215b69c` feat `List`/`Get` 投影（含 service 单测）/ `ccf086e` feat `UpdateAsset` 剥 `ip_address`（含测试）/
`be5c3da` refactor `retireCore` 复用 / `b7ab84a` docs CHANGELOG+TODO / `605d2d3` docs trap 编号 T-76；
已 push 到 `origin/main`）
**Scope**: backend 5 文件（1 新建）+ 2 个测试文件；frontend **0 改动**
**摩擦**: M62 给资产表单的 `ip_address` 装上格式闸之后追查「这个值去哪了」，结论是**读取侧根本没有投影**
（列表/IP 列与 Ping/Traceroute 按钮在生产数据上整片空转，测试全绿是因为 mock 里有 IP），
**写入侧还会把请求打成 500**（GORM 对模型里不存在的键照样发 `SET`）
**Time**: ≤2h omp round

## 改动

| 文件 | 改动 |
|---|---|
| `backend/internal/models/asset.go` | `Asset` 加 `IpAddress *string json:"ip_address" gorm:"-"`（虚拟字段，**不是列**；放在末尾并与列空一行） |
| `backend/internal/service/asset_ip.go`（新） | `pickPrimaryIP(networks) (v4, v6 *string)`（v4/v6 各取第一张非空）+ `primaryIP(networks) *string`（v4 优先，否则 v6，无则 `nil`）。**判据只此一份** |
| `backend/internal/service/asset_service.go` | `List`: 一条 IN 查询取回整页网卡 → 按 `asset_id` 分组 → 就地填 `IpAddress`（空页早返）；`Get`: 复用已在手的 networks 投影（不多发查询）；`retireCore`: 删掉第三份同判据循环，改调 `pickPrimaryIP` |
| `backend/internal/service/postmortem_service.go` | `fetchIP` 委托到 `primaryIP`（报告头 IP 与列表 IP 同源）；排序补 `id` 决胜列与 `listNetworks` 对齐 |
| `backend/internal/api/handlers/asset_handler.go` | `UpdateAsset`：`normalizeJSONBFields` 之后 `delete(updates, "ip_address")`（T-76） |
| `backend/internal/service/asset_service_test.go` | 7 个 `TestM63_*`（`pickPrimaryIP` 7 subcase / `primaryIP` 单值 / `List` 投影 3 资产 / `Get` 投影 / `Get` 无网卡 / `Create` 不进 INSERT / `Update` 照发 SET）+ 既有 `TestAssetService_List_带keyword和status过滤` 补上新查询的期望与投影断言 |
| `backend/internal/api/handlers/asset_handler_test.go` | 5 个 `TestM63_*`（ListAssets 投影 / GetAsset 投影 / Update 剥键 / Update 路由级 SQL 无该列 / Create 不进 INSERT）+ 测试基建 `sqlCapture`（sqlmock `QueryMatcher`，记录驱动实际收到的 SQL）+ `newSQLCapturingDB` |

**frontend**: 0 改动（`AssetTable.tsx:13` 的字段类型与 `:105` 的 IP 列、`:149/:159` 的
`disabled={!record.ip_address}` 早就等着这个字段；`AssetFormModal` 不动 —— 写路径留给
`G-Asset-NetworksPersist`）。

## Hard pass

| 维度 | 结果 |
|---|---|
| backend `go test -count=1 ./...` | **27 packages ok** ✓ |
| backend `go test -count=1 ./internal/service/... -run Asset` | ok（含 M63 单测）✓ |
| backend `go test -count=1 ./internal/api/handlers/... -run Asset` | ok（含 M63 handler 用例）✓ |
| M63 用例计数（`-v`） | **12 顶层 + 7 subcase = 19 条断言路径全 PASS** ✓ |
| backend `gofmt -l`（本轮 7 文件） | 干净（全仓另 3 个既存文件不干净，M61 已登记、非本轮）✓ |
| backend `go vet`（service/handlers/models） | 无新告警 ✓ |
| frontend `npx tsc --noEmit` | **0 error** ✓（0 改动） |
| frontend `npx vitest run src/components/AssetTable.memo.test.tsx src/pages/Assets.test.tsx` | **2 files / 27 tests PASS**（无退化）✓ |
| mutation ①（bypass `delete(updates,"ip_address")`） | `UpdateAsset_剥掉ip_address键` + `UpdateAsset_带ip_address不产生该列的SQL_返200` **2 failed**；失败信息含**驱动实际 SQL** `UPDATE "assets" SET "ip_address"=$1,…` ✓ |
| mutation ②（`primaryIP` 反转 v4/v6 优先级） | `PrimaryIP_单值投影` + `List_投影` + `Get_投影` **3 failed** ✓ |
| mutation ③（删 `pickPrimaryIP` 的 v4 分支） | **8 failed**（Retire 的 last_known / 复盘 ReportData / fetchIP / List / Get 投影 / 既有 keyword 用例）✓ |
| 还原后复跑 | 两包全绿，`git status` 干净 ✓ |
| 双轨分析 | graphify **7065 / 14542 / 453**（M62 7002 / 14420 / 453）+ **0 anomalies**；codegraph `primaryIP` **4 callers** 跨两个 service 文件、新节点全落社区 22（`Asset`）不分叉 —— 见 `M63-graph-analysis.md` ✓ |

## 关键设计决策（含理由）

### 1. 虚拟字段而不是给 `assets` 加列
IP 属于**网卡**（一个资产 N 张卡）不属于资产。加列 = 两份存储必然漂移：B4 退役改的是
`asset_networks`（清空它的 IP、把地址存进 `asset.last_known_ip*`），资产表再放一份「当前 IP」
就会在退役后与网卡表矛盾，而且需要迁移 + 回填 + 两处写入方对齐。`gorm:"-"` 让字段只活在
序列化层：**读侧注入、写侧不碰**。

代价必须记住：绑定层（`map[string]interface{}` / 结构体）**会**收下它、什么也不说，只有 DB 会爆 ——
这就是 T-76。

### 2. 投影用「一条 IN 查询 + 内存分组」，不用 `Joins`
`assets` 与 `asset_networks` 是 1:N。join 会把有 N 张网卡的资产**复制成 N 行**：
「第 N 页」的页大小、`total` 的含义当场改变（同一条资产在表格里出现多次、分页数与总数对不上）。
多一条 `WHERE asset_id IN (…)`（一页 ≤500 个 id）换来行数语义不变，代价与一页同阶；
逐条查是 N+1，也不选。空页**早返**（不发 `IN ()`）。

`Get` 不重复这条查询：`listNetworks` 已经把网卡拿在手里了，直接 `primaryIP(networks)`。

### 3. 判据单独成文件，并把**已有的两份**一起收编
brief 只要求「`fetchIP` 的口径复用，不要写第二份」。实际清点时这条判据在仓里有**三份**：
`fetchIP`（报告头）、`pickPrimaryIP` 要新增的、以及 `retireCore` 取 `last_known_ip4/6` 的那段循环。
只收编前者，`asset_ip.go` 的注释里那句「唯一出口」就是假的 —— 而「退役存下的地址」与
「列表显示的地址」正是最容易被运维拿去 ping 的两个值。故三处合一，`retireCore` 少 15 行循环。

守卫是既有用例（断言 `UPDATE` 的 **args**，不是「发过 UPDATE」）：
mutation ③ 一动手，`TestAssetService_Retire_成功_IP转移到last_known` 与复盘的两条用例同时红。

### 4. `delete(updates, "ip_address")` 而不是「顺手把 IP 写进 `asset_networks`」
本轮 `ip_address` 是**只读投影字段**（brief 明确）。写路径要么与「表单里没有网卡概念」一起设计，
要么就会做出「每次 PUT 都静默覆盖第一张卡的 IP」这种隐式副作用 —— 运维改个机房位置，
设备的 IP 被表单里那个残留值覆盖。一行 `delete` 把 500 变成「忽略该键返 200」，
语义诚实：本轮**没做**写入，而不是**假装做了**。

### 5. trap 编号：`T-76`，不是 brief 说的 `T-75`
`T-75` 在本仓已被**两个不同含义**占用：M61 的全站白屏（`M61-completion-report.md:209`）、
M62 的 IPv6 正则锚点（`CHANGELOG.md:473`）。照 brief 写会得到第三个含义，编号就再也回查不到东西。
代码注释里保留了「brief 写作 T-75」的映射（`asset_handler.go:130`）。

## 验证链

见 `M63-graph-analysis.md` 的「验证链完整性」表（27 包 / 两个定向 `-run` / 19 条 M63 断言路径 /
gofmt / vet / tsc / Assets+AssetTable / 3 处 mutation + 还原 / graphify 0 anomalies / codegraph 入图）。

**最强的那条证据**（本轮把 M62 的推理变成实测）：mutation ① 捕获到的语句原文是

```
UPDATE "assets" SET "ip_address"=$1,"name"=$2,"updated_at"=$3 WHERE "id" = $4
```

—— 由真 `AssetService` + 真 GORM 语句生成 + sqlmock 驱动、经 `gin` 路由走完 `PUT /assets/:id`
得到。`sqlCapture`（实现 sqlmock 的 `QueryMatcher`）存在的理由：sqlmock 的期望只能断言
「某条 SQL **发过**」，无法断言「某列**没被写**」，而本轮要证明的恰恰是后者。

## 行为变更（运维可见）

1. **`GET /assets` 的每一项现在带 `ip_address`**：取「第一张有非空 IPv4 的网卡」的 v4；
   没有 v4 则取第一张非空 v6；都没有 → `null`（无网卡的资产）。前端 IP 列与
   Ping / Traceroute 按钮**从此有数据**（此前生产库上恒空、按钮恒灰）。
2. **`GET /assets/:id`** 的 `data.asset.ip_address` 同规则注入（详情页头部与弹窗预填）。
3. **`PUT /assets/:id` 带 `ip_address` 不再 500**：该键被忽略，其余字段正常更新（200）。
4. **`POST /assets` 带 `ip_address` 仍不落库**（201 + 忽略）—— 与 M63 之前**同行为**，
   写入路径是 `G-Asset-NetworksPersist` 的事。
5. **复盘 PDF 报告头的 IP 与资产列表显示的是同一个地址**（此前两处各判一遍；对多网卡资产，
   两处选到不同网卡时会出现「列表 A、报告头 B」）。

## 残余 / 未覆盖（如实登记，不假装完整）

- **未在真 PG 上实测 42703**：本机无 PG 服务端（`postgresql-libs` 只有客户端，docker API 无权限，
  `pg_isready` 无响应），故「PUT 带 `ip_address` → 42703 → 500」的完整证据是
  **GORM 渲染出的语句原文**（mutation ①）+ PG 错误码语义。相对 M62 已从「源码级核实」
  推进到「语句级证据」，但**不是**真库往返。补齐方式：在有 PG 的环境跑一次
  `PUT /api/assets/:id` 带该键（应 200 且库无变化）。
- **`ip_address` 当前是只读字段**：前端 `AssetFormModal` 仍会把它放进 payload（PUT 被剥、POST 被丢）。
  真正落库需等 `G-Asset-NetworksPersist`（写第一张网卡：v4 → `ipv4_address`、v6 → `ipv6_address`）。
- **`openapi.yaml` 与实现口径不一致**（登记为 `G-Asset-IpPersistence-Contract`）：
  `Asset.ip_address` 声明 `type: string`（不可空）而实际可为 `null`；
  `AssetInput.required` 含 `ip_address` 而 POST 根本不落库。本轮不动 spec 的理由：
  改它要重跑 `npm run gen:api`，会牵动 `frontend/src/types/index.ts` 的 `Asset` 类型与若干 `_Assert`，
  超出「frontend 0 改动」的本轮边界。
- **列表多一次查询**：每页 1 条 `asset_networks WHERE asset_id IN (…)`。500 条一页时 `IN` 列表
  500 项，PG 参数上限（65535）内。若将来要并列显示 v4/v6，`pickPrimaryIP` 已把两者都返回。
- **`assets` 无网卡的存量数据**：投影为 `null`，前端按「无 IP 地址」禁用探活（符合预期）；
  但这也意味着**列表页不会告诉运维「这个资产没配网卡」与「配了但没填 IP」的区别**。
  两者都渲染成空列，需要区分的话要给前端加文案（未做）。
- **`Assets.tsx:372/528`** 的副标题与探活弹窗文案读同一字段，本轮不动（行为随投影一并生效）。

## 学到 / Retro

- **「mock 里有 = 手上有」是本轮缺陷的共同遮罩**：M62 修的是**表单闸**，M63 发现**值从来没到过后端**；
  而列表这一侧之所以三轮都没人发现 IP 列是空的，是因为 `Assets.test.tsx` 的 mock 数据自带 IP ——
  **测试数据比生产数据「更完整」**，测试就越绿越骗人。修完之后值得记住：
  投影类改动应当问「这条断言用的是真数据形状还是我编的形状」。
- **缺陷可以只有一层「没错」**：JSON 绑定层（`map[string]any` 全收）不报错、GORM 也不报错，
  只有数据库会报，而它报的是**列名**。所以「绑定成功 + 返回 500」这种组合出现时，
  下一个该看的地方是「是不是有个字段没有落点」。
- **一行 `delete` 也要有证据**：这行代码的正当性完全依赖「GORM 会对模型外的键发 SET」这个反直觉事实，
  所以本轮没有停在「源码里读到 `LookUpField` 为 nil 时仍 append」，而是让测试**打印驱动收到的 SQL**。
  经验：**当修复的理由是「某库/某框架的反直觉行为」时，把那个行为本身写成断言**（哪怕它看起来
  像在断言 bug）—— `TestM63_AssetService_Update_map含模型外列时GORM照发SET` 就是这条，
  将来 GORM 改了行为它会红，那时 `delete` 就能删。
- **「唯一出口」这种说法必须清点全部调用点才算数**：brief 只点了 `fetchIP`，实际清点出第三份
  （`retireCore`）。写注释说「唯一」之前先 grep 判据的**形状**（`!= ""` / 循环），别只 grep 函数名。
- **编号体系需要唯一性守卫**：`T-75` 三义（M61 白屏 / M62 锚点 / brief 本轮）说明
  「轮次内编号 + 全局 TRAPS.md 编号」并行时，没有任何机制拦住重复。本轮绕开（改用 T-76）并登记；
  根治要么给 trap 号加轮次前缀（`M63-T1`），要么加一条 CI 检查（全仓 `T-\d+` 引用与
  `docs/TRAPS.md` 的条目对齐）—— 留作独立小轮。

## 偏离 brief 的三处（都有理由，均已在 CHANGELOG 记录）

1. **trap 编号 `T-75` → `T-76`**（理由见上；注释里保留 brief 编号的映射）。
2. **判据放独立文件 `asset_ip.go`，并把 `retireCore` 里的第三份实现一起收编**：brief 只要求
   `fetchIP` 复用；只做到那一步，「唯一出口」的名与实不符（三条消费路径里两条各判一遍）。
   顺带删掉 15 行重复循环，守卫是既有 Retire/Restore 用例。
3. **commit 粒度按文件而非 brief 的 6 条 hunk 级**：brief 的 6 步里，`asset_service_test.go`
   同时承载「`pickPrimaryIP` 单测」与「`List`/`Get` 投影测试」，且其中**既有用例
   `TestAssetService_List_带keyword和status过滤` 的实现改动必须与新查询同笔提交**（否则该步的树是红的）。
   故合并为 4 笔代码提交，每笔的树都**独立绿** —— 逐笔用 `git worktree add --detach <sha>`
   复跑 `go test ./internal/service/... ./internal/api/handlers/...`（`33cec6d` / `215b69c` /
   `ccf086e` / `be5c3da` 四笔全 ok），而不是产出「中间态编译过但测试红」的提交。
   代价：brief 的 feat/test 分离在这两笔里没体现。

## Follow-up（留 future round）

- **`G-Asset-NetworksPersist`**（本轮登记）：`POST`/`PUT /assets` 的 `ip_address` 真正落到第一张
  `asset_networks`（v4 → `ipv4_address`、v6 → `ipv6_address`），事务内建行/更新、
  「第一张」的语义与 `listNetworks` 的排序对齐；后端仍要做格式兜底（前端 `ipRules` 只覆盖表单路径，T-56）。
- **`G-Asset-IpPersistence-Contract`**（本轮登记）：`openapi.yaml` 的 `Asset.ip_address` 加
  `nullable: true`、`AssetInput.required` 摘掉 `ip_address`，并重跑 `gen:api`（牵动前端 `Asset` 类型）。
- **`G-Asset-BulkIpEdit`**：批量改 IP（brief 列出的 out of scope）。
- **trap 编号唯一性守卫**（本轮 Retro 派生）：CI 里校验全仓 `T-\d+` 引用，或改为轮次前缀编号。
- 其他页接入 `utils/validators`（M60 起未动）、CIDR / FQDN（需与后端写路径同轮）、
  `ws://`/`wss://`（M60 起未动）、SMTP user 强 email 的 SASL 误伤（M59 起未解）。
