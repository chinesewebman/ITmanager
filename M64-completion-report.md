# M64 — G-Asset-NetworksPersist 资产 IP 写入路径（form submit → 第一张 `asset_networks`）

**日期**: 2026-09-15 CST · **Author**: hermes@local · **基线**: `262b33a`（M64 intent）·
**交付**: `2e3ba0f` → `ff3a230` → `e41598a` → `ae54415` → 本次台账（全部已 push 到 `origin/main`）
**来源**: M63 final report 派生 TODO `G-Asset-NetworksPersist`（`intent-M64.md` = PM 写的 brief）

---

## 1. 做了什么（结论先行）

**表单里的 `ip_address` 从此真的落库 —— 落到 `asset_networks`（第一张网卡），而不是被静默丢掉。**

| 层 | 前 | 后 |
|---|---|---|
| `POST /assets` 带 `ip_address` | 201 + **忽略**（绑进 `gorm:"-"` 虚拟字段 → GORM 静默丢） | 201 + **同一事务**建资产行 + 第一张网卡行（v4→`ipv4_address` / v6→`ipv6_address`） |
| `POST /assets` 带非法 `ip_address` | 201 + 忽略（前端正则挡不住的形态照收） | **422 + `validation_failed`**（新增 `apierr.Unprocessable`） |
| `POST /assets` 不带 / 空串 `ip_address` | 201 | 201，**不建网卡**（可选字段，不是错误） |
| `PUT /assets/:id` 带 `ip_address` | 200 + 忽略（M63 剥键，防 42703→500） | **不变**（写网卡是 `G-Asset-UpdateIpPersist`） |
| `GET /assets` 的 `ip_address` | M63 已投影（但生产上恒空，因为没人写得进去） | 现在**有数据**了（M63 读 + M64 写接上） |
| `openapi.yaml` | `Asset.ip_address` 不可空、`AssetInput.ip_address` **必填**且是无效写 | `Asset.ip_address` `nullable`（投影可为 null）+ `AssetInput.ip_address` 从 `required` 摘掉（可选 = 不建网卡） |

**动到的文件**（backend 6 + 生成物 1；**frontend 0 来源改动**）：

```
backend/internal/service/asset_service.go          Create 签名 + tx + v4/v6 分流
backend/internal/api/handlers/asset_handler.go     CreateAsset 嵌套 input + invalidIPAddress + 422
backend/internal/apierr/apierr.go                  新增 Unprocessable(422)
backend/internal/service/asset_service_test.go     +6 用例（真 sqlite）
backend/internal/api/handlers/asset_handler_test.go +6 用例（真 sqlite 端到端 + sqlmock）
backend/internal/apierr/apierr_test.go             +2 用例
backend/internal/api/openapi.yaml                  Asset/AssetInput ip_address
frontend/src/services/api.types.ts                 gen:api 重生成（生成物，非手改）
```

---

## 2. 关键实现决定（含被否掉的方案）

1. **IP 走显式入参，不走 `models.Asset` 上的字段**。`Asset.IpAddress` 是 M63 立的 `gorm:"-"` 只读投影
   —— 绑进它 = 被 GORM 在 schema 阶段排除 = 本轮要修的 bug 本身。handler 用匿名嵌套结构
   `struct { models.Asset; IpAddress *string }`：JSON 展平时**顶层字段胜出**，所以 `ip_address`
   只落 `input.IpAddress`，嵌进去的 `Asset.IpAddress` 恒为 nil（有专门用例钉这条，见 §4）。
   *否掉*：「给 `assets` 加一列」—— 两份存储（`assets.ip_address` 与 `asset_networks.ipv4_address`）
   必然漂移（B4 退役只改网卡），M63 已否过一次，本轮沿用。
2. **包事务**。资产行与网卡行必须同生：分两次写时第二条失败会留下「资产在、IP 没了」，而调用方拿到
   500 会当整条失败去重试 → 撞 `name` 唯一约束变 409 —— 用户看到「资产已存在」，而它确实存在、只是没 IP。
   守卫是 mutation ①（bypass `tx.Create(network)`）里那条**回滚用例**（真 sqlite：删表制造真 DB 错误）。
3. **v4/v6 分列**。两列并存正是为区分地址族：把 v6 塞进 `ipv4_address` 会让 M63 的
   `pickPrimaryIP`（先第一个非空 v4，否则第一个非空 v6）**读出错误结果**（列表把 v6 当 v4 显示）。
   地址用 `ParseIP().String()` 归一（`2001:0DB8::0001` → `2001:db8::1`），否则同一地址的两种写法在
   按字符串比对的检索/去重里各算一条。4-in-6 映射（`::ffff:1.2.3.4`）经 `To4()` 归到 v4 —— 它本来就是 v4。
4. **空串 = 未提供**（不是「写错了」）。`AssetFormValues.ip_address` 是 `string` 而非可选，用户清空后
   确实送 `""`。判据在 handler 与 service **逐字对齐**（nil / trim 后空串都算没提供），
   否则会出现「清空 IP 后创建被拒」这种无法解释的行为。
5. **422 而不是 400，且挡在 handler 入口**。JSON 合法、字段名也对，只有取值不对 → 前端要按字段高亮。
   只靠 service 兜底不够好：`ErrInvalidInput` 已被映射成「资产名称不能为空」，把 IP 错误报成
   「名称不能为空」是**指错字段**。service 侧保留同判据兜底，给**直接调用方**
   （`tests/db_smoke_test.go`、将来的导入器）——宁可报错，也不把脏 IP 写进网卡表。
6. **`apierr.Unprocessable` 是 `CodeValidationFailed` 的第一个后端生产者**。该 code 常量与前端
   `ApiErrorCode.ValidationFailed` 早已声明却一直没有出口；2 条用例把「状态码 + code 字符串」一起钉住
   （code 是跨语言契约，写成 `bad_request` 前端会按错的方向分支）。
7. **第一张网卡 `interface_name = "eth0"`** —— `AssetNetwork.InterfaceName` 是 `not null`，建卡必须给值。
   真实接口名（`ens18` / `GigabitEthernet0/1`）与多网卡一起设计（`G-Asset-MultiNetwork`）。这是本轮
   唯一一处「产品决定由我拍」，已写进 TODO 与 CHANGELOG。
8. **`Update`（PUT）不动**。改 IP 是改第一张卡还是新建一张卡 —— 那是产品决定，不该顺带做
   （brief 也明确划在 `G-Asset-UpdateIpPersist`）。

---

## 3. 验证（本机实测，非推断）

| 项 | 命令 | 结果 |
|---|---|---|
| backend 全量 | `cd backend && go test -count=1 ./...` | ✓ **27 packages ok** |
| backend service | `go test -count=1 ./internal/service/... -run Asset` | ✓ ok（含 6 个 M64 用例） |
| backend handler | `go test -count=1 ./internal/api/handlers/... -run Asset` | ✓ ok（含 6 个 M64 用例） |
| backend apierr | `go test -count=1 ./internal/apierr/...` | ✓ ok（含 2 个 `TestUnprocessable_*`） |
| backend 格式 | `gofmt -l`（7 个改动文件） | ✓ 6 个干净；`tests/db_smoke_test.go` 是 HEAD 上就不干净的既存文件（本轮只改一行签名） |
| frontend 类型 | `npx tsc --noEmit` | ✓ **0 error** |
| frontend 定点 | `npx vitest run src/components/AssetFormModal.test.tsx src/pages/Assets.test.tsx` | ✓ **2 files / 30 tests PASS** |
| frontend 全量 | `npx vitest run` | ⚠ 47 files / 489 tests：**488 passed / 1 failed**（`Settings.test.tsx` 一条 antd 校验弹窗断言超时；**单独复跑 58/58 PASS**）—— 已知 flake，见 §5 |
| openapi | `npx swagger-cli validate ../backend/internal/api/openapi.yaml` | ✓ valid |
| 生成物 | `npm run gen:api` | ✓ diff 只有 `Asset.ip_address` → `string \| null`、`AssetInput.ip_address` 必填→可选 |
| 双轨 | graphify `update --force` + `diagnose multigraph` | ✓ **7143 nodes / 14690 edges / 454 communities**（M63 基线 7065 / 14542 / 453），**0 anomalies** |
| 双轨 | codegraph explore | ✓ `invalidIPAddress` 1 caller、`Unprocessable` 1 caller + 2 测试边、`assetService.Create` 接口接线 |

**mutation inversion（4 处，全部红在断言上，全部还原后复跑全绿）**：

| # | 变异 | 结果 |
|---|---|---|
| ① | bypass `tx.Create(network)`（`return nil`） | **5 FAIL**：service `IPv4落ipv4列` / `IPv6落ipv6列` / `网卡写失败时资产行一并回滚` + handler `带ip_address_落成网卡行` / `带ip_address_assets无此列但网卡有行` |
| ② | 去掉 v4/v6 分流（`v4 != nil \|\| true`，一律写 `ipv4_address`） | **1 FAIL**：`TestM64_AssetService_Create_IPv6落ipv6列`（在 `ipv4_address` 里读到值） |
| ③ | 关掉 handler 入口校验（`invalidIPAddress` 恒 false） | **1 FAIL**：`TestM64_CreateAsset_非法ip_address_返回422` |
| ④ | 关掉 service 兜底（`if false && …`） | **1 FAIL**：`TestM64_AssetService_Create_非法IP不落库` |

> ② 只有一条用例红是**预期**的：handler 层的 IPv4 端到端用例在「一律写 v4」时仍绿（它用的是 v4）。
> 检测「地址族分流」的观测点在 service 层，那里有 v6 的断言。

---

## 4. 测试的选取（为什么这样测，而不是更多/更少）

- **service 层用真 sqlite（不是 sqlmock）**：被测行为是「网卡表里到底有没有那一行、值落在哪一列」。
  sqlmock 只能断言「某条 SQL 被发过」，行内容得由测试作者手写进期望 —— 而「写是写了、写错列/行」
  正是这类改动最容易出的缺陷，恰恰是手写期望盖不住的地方。
- **回滚用例用「删掉表」制造真 DB 错误**，而不是 mock 编排一个假错误：要证的正是**真驱动上**
  的事务边界行为。
- **handler 层有一条端到端（真 sqlite + 真 service）**：证明 `POST /assets` 的 JSON 里的值一路
  变成 `asset_networks` 的一行，且 `AssetID` 等于响应里那张资产（不是孤儿行）。
- **handler 层有一条负控（不带 IP → 网卡表为空）**：没有它，「恰一行」在实现改成「总是建一行」
  时仍会绿。
- **`TestM64_CreateAsset_ip_address走独立入参不变虚拟字段`**：直接钉 T-77 的机制 ——
  service 收到的 `Asset.IpAddress` 必须为 nil。这是「值走显式入参」这条设计的回归网，
  也是 M63 那个 bug 的机制本身。
- **M63 的 `Create ip_address 不进 INSERT` 用例改名而非删除**：它断言的那件事（`assets` 列清单里
  没有 `ip_address`）**仍然成立且仍然重要**（T-76 那类 42703 的另一半），只是补上了本轮新增的
  「`asset_networks` 有一行 INSERT」。M63 的「不落库」语义被替换，用例名跟着换。
- **没写的测试**：没给 `InvalidIPAddress` 单独写表驱动（它的取值边界由 2 条 handler 用例覆盖：
  非法 → 422、空串 → 201；`net.ParseIP` 本身不是我们的代码）；没给「`eth0` 这个名字」单独断言
  之外的接口名语义（多网卡另立轮）。

---

## 5. 残余 / 未覆盖（如实登记）

1. **未在真 PG 上实测**。本机无 PG 服务端（`postgresql-libs` 只有客户端，docker 不可用）。
   证据是：真 sqlite（事务、列落点、回滚）+ sqlmock 捕获的**SQL 原文**
   （`assets` 列清单里无 `ip_address`，且确有 `INSERT INTO "asset_networks"`）。
   **不是**真库往返 —— 与 M63 同一处缺口。
2. **前端全量套件一条 flake**：`Settings.test.tsx` 的 antd 校验弹窗断言在**同机并发跑全量
   frontend + 全量 backend** 时超时（488/489），单独复跑 58/58 PASS。该文件与 M64 改动无交集，
   现象与 M61 retro 记录的同源。**登记为已知 flake，不计入「全绿」**。
3. **前后端 IP 判据不等价**（M64 让它第一次有用户可见后果）：前端 `OCTET = "[01]?\d\d?"`
   接受**前导零**（`010.1.1.1` 过表单），`net.ParseIP` 自 Go 1.17 拒绝 → **过表单 → 吃 422**，
   且文案是通用「创建失败」；反方向：前端不接受 IPv4-mapped / zone id，后端接受前者。
   → 派生 TODO `G-UI-AssetIpValidatorParity`（T-52 家族，需先定产品口径）。
4. **422 的 body 是通用文案**（「IP 地址格式不合法」），而 `Assets.tsx` 的
   `onError: () => message.error('创建失败')` 把它显示成 topline 提示，**没有字段级高亮**
   （`ApiErrorCode.ValidationFailed` 已被前端声明但还没有消费点）。属 frontend 改动，本轮 0 改动原则。
5. **同一 IP 可挂在多台资产上**（`asset_networks.ipv4_address` 是普通索引）。M64 之前表单里的 IP
   根本写不进库，「重复 IP」不是可发生状态；现在它是常规写入源了。这是产品决定（拦在写入 / 巡检报）
   → 派生 TODO `G-Asset-IpConflictGuard`。
6. **`interface_name = "eth0"` 是硬编码**（见 §2.7）。
7. **`docs/TRAPS.md` 的 trap 索引停在 T-63**：T-64…T-76 只存在于各轮报告/CHANGELOG 里（M50→M63
   的既有做法）。本轮 T-77 按 M63 的先例登记在报告 + CHANGELOG，并**同时**补进了
   `docs/TRAPS.md`（§三 + 索引行）—— 索引因此从 T-63 直接跳到 T-77，中间 13 个号的回填是既存
   drift，未在本轮处理（避免把无关的历史回填混进 M64 的 diff）。

---

## 6. 台账 / 派生

- **结案**：`G-Asset-NetworksPersist`（M63 派生）→ 标 `[x]` + 结案说明。
- **部分修复登记**：`G-Asset-IpPersistence-Contract`（M63 派生）→ 标注「M64 修掉主体」
  （`Asset.ip_address` nullable、`AssetInput.ip_address` 从 required 摘掉、`gen:api` 重跑），
  **仍待收**：`POST /assets` 的 responses 只声明 201（400/409/422 未进 spec）、
  `components.schemas.Error.code` 是 `integer` 而实际是字符串 code、
  `frontend/src/types/index.ts` 的手写 `Asset.ip_address: string` 仍不可空、openapi 全量 sync。
- **派生 2 条**（brief 里写「派生 1 条」—— 我登记了 2 条并在此披露）：
  `G-UI-AssetIpValidatorParity`（前后端判据不等价，见 §5.3）、
  `G-Asset-IpConflictGuard`（重复 IP，见 §5.5）。两条都是本轮**新写路径**直接带来的可发生状态，
  不是顺手扩范围。
- **Trap**：新增 **T-77**（投影字段当写入通道：绑定层收下、schema 层排除 → 201 而库里什么都没有），
  已进 `docs/TRAPS.md` §三 + 索引，并在 `M64-graph-analysis.md` 里给了检测线索与解法。
  T-52（判据两侧共享语义）与 T-45（「第一张卡」的顺序定义）在本轮各有一次新实例，**未新开编号**。
- **Out of scope（留 future round）**：`G-Asset-UpdateIpPersist`（PUT 写网卡）、
  `G-Asset-MultiNetwork`（真实接口名 / mac / 多网卡）、`G-Asset-NetworksCRUD`（独立网卡 API）、
  openapi 全量 sync。

---

## 7. Retro（本轮值得记住的）

- **「读取侧修完了」会伪装成「功能修好了」**。M63 让列表能显示 IP，看起来这件事结束了；
  实际生产上那列永远是空的 —— 因为没有写入方。**读路径修完时必须立刻问「谁写这个字段」**，
  两者缺一，用户看到的是同一个空值。
- **静默丢弃是最贵的一类「成功」**。`gorm:"-"` + `ShouldBindJSON` 的组合下，三层各自都没错，
  接口返 201，日志干净 —— 没有任何信号提示数据没落。破法是**断言目标表**（真库读回），
  以及用 `sqlCapture` 把驱动实际收到的 SQL 抄下来看（T-58 的同一精神：检查命令自己空转时，
  它长得和「通过」一模一样）。
- **brief 的代码骨架是意图不是文本**。brief 给的 `Create` 骨架里 `net.ParseIP` 失败时**直接跳过**
  （不建网卡、不报错）—— 那会让「填错 IP」重新变成静默丢弃。我改成早返 `ErrInvalidInput`
  + handler 入口 422，并把这条差异写进 commit 与报告。
- **同一判据的两份实现迟早会分叉**（T-52）：前端正则与 `net.ParseIP` 在前导零上不等价，
  在 M64 之前无害，在 M64 之后立刻变成用户可见的 422。判据落地时**顺手核对两侧边界**
  比事后修便宜得多。
