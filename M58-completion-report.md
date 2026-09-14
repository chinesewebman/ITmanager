# M58-completion-report — G-Asset-BulkRetireEndpoint 批量退役单端点（M51-3 结案）

**Shipped**: 2026-09-15 (commits `f0c3d47` feat backend / `3dfe6d4` test backend / `6a3a3b2` feat frontend / `b2a50dc` test frontend / docs 本次)
**Scope**: backend 4 files（含 1 契约文件）+ frontend 4 files（含 1 生成物）
**摩擦**: M51 ship 的资产批量退役是前端串行循环（100 台 = 100 次 `POST /api/assets/:id/retire`），M51-3 🟡 登记「后端 bulk 端点」留 future round；M53 的告警批量标记误报是同一摩擦（那侧本轮不动）。
**Time**: ≤3h omp round

## 改动

| 文件 | 改动 |
|---|---|
| `backend/internal/service/asset_service.go` | 单条退役体抽成事务内核 `retireCore(ctx, db, …)`（读也进事务）；`Retire` 改为薄包装 `s.db.Transaction(retireCore)`；新增 `BulkRetire`（外层 tx + **每 id 一个 SAVEPOINT**，`succeeded []string` / `failed map[string]string`）；`bulkRetireErrMsg` + `bulkRetireReasonMax=500`；`listNetworks` 改为接收调用方 `*gorm.DB` |
| `backend/internal/api/handlers/asset_handler.go` | `BulkRetireAssets`（200 + `{succeeded, failed}`；空 ids/非法 JSON → 400；>1000 条 → 400；user_id 非 uuid → 401）+ `maxBulkRetireIDs=1000` |
| `backend/internal/api/routes.go` | `assets.POST("/bulk-retire", canWrite, …)` 注册在 `/assets/:id/retire` **之前** |
| `backend/internal/api/openapi.yaml` | 新增 `/assets/bulk-retire` + `AssetBulkRetireResult`（契约门禁是集合相等） |
| `backend/internal/service/asset_service_test.go` | M58 3 条（全成功 / 部分失败 / 非法 uuid）；既有 5 条 `Retire` 用例仅调整 SQL 期望**次序** |
| `backend/internal/api/handlers/asset_handler_test.go` | M58 6 条（全成功 / 部分失败仍 200 / 空 ids / >1000 / 非法 user_id / 路由顺序）；mock 增 `bulkRetireFunc`；test router 增 AuthMiddleware 替身 + `/bulk-retire` 静态段 |
| `backend/internal/api/routes_integration_test.go` | M58 路由顺序 1 条；`gatedRoutes` 补 `CapWrite` 分类 |
| `frontend/src/services/api.ts` | `assetApi.bulkRetire(ids, reason)` |
| `frontend/src/pages/Assets.tsx` | `parseBulkRetireResult(res: unknown)` 收窄；`bulkRetireMut` 改单请求 + 三态分流（success / warning / error） |
| `frontend/src/pages/Assets.test.tsx` | 6 条 M58（单端点 1 次调用 + 单条 spy 不被调 / 全成功 / 部分失败 / 全失败 / 整体失败 onError / 不点确认则不调）；`useApiMutation` mock 补回调传递（`MutationProbe` 替 `as any`）；antd `message` 改 spy |
| `frontend/src/services/api.types.ts` | `npm run gen:api` 重生成（含修 M38-B 起就红的生成物漂移） |

## Hard pass

| 维度 | 结果 |
|---|---|
| backend `go test -count=1 ./...` | **全绿（27 packages ok，0 fail）** ✓ |
| frontend `npx tsc --noEmit` | **0 error** ✓ |
| frontend `npx vitest run` | **42 files / 386 tests PASS** ✓（Assets 26 条含 M58 新 6 条） |
| eslint（3 个改动文件） | 干净 ✓ |
| mutation inversion（前端） | bypass 批量路径 → `assetApi.retire(ids[0])` → **5 failed \| 1 passed** ✓ |
| mutation inversion（后端） | 去掉 per-id SAVEPOINT（`retireCore(ctx, tx, …)` 直调）→ **3 failed**（BulkRetire 全组）✓ |
| 路由顺序 | `POST /assets/bulk-retire` 带合法 body 落 `BulkRetireAssets`（被 `/:id/retire` 吞掉时 `id="bulk-retire"` → 400 且无 `failed`）✓ |
| 双轨分析 | graphify 6771 nodes / 13849 edges / 0 missing / 0 dangling；codegraph 索引已含新符号 ✓ |

## 关键设计决策（含理由）

### 1. 为什么必须 SAVEPOINT，而不是「外层一个 tx 循环调 retireCore」
PG 里一条语句报错会把**整个事务**置为 aborted，后续每条语句都以 `25P02` 失败并回滚全批 ——
于是「其中一条 id 不存在」会静默升级成「整批失败」，正好和 `failed` 字段承诺的部分成功语义相反。
`tx.Transaction(...)` 的嵌套调用在 gorm 里就是 SAVEPOINT（`db.SavePoint/ROLLBACK TO`），
所以「一个外层 tx + 每 id 一个 savepoint」既是单个 DB 事务、又能逐条隔离失败。
单元测试把这条钉在 SQL 上：部分失败用例必须出现 `ROLLBACK TO SAVEPOINT sp…` 且外层 `COMMIT`。

### 2. 事务边界从「只包写」扩到「读+写」（行为变更，已在 CHANGELOG 记录）
原来 `Retire` 先读资产/网卡（事务外）再开事务写。本轮把读也纳入事务：网卡 IP 快照与随后的清空 IP
必须在同一快照里，否则并发改网卡会丢 `last_known_ip*`（快照读到旧值、清空作用在新行上）。
副作用是两条路径必须先统一次序（`retireCore` 只有一份实现），既有 5 条 sqlmock 用例的 SQL **次序**随之调整 ——
语义断言（快照值 / 拒绝重复退役 / `ErrNotFound` / 无网卡时空快照）一字未动。

### 3. 部分成功用 200 + 两字段，而不是 4xx
用 4xx 表达「其中一条不存在」会让调用方丢掉成功的那部分（axios 拦截器还会顺手 toast 一个错误）。
前端因此把三种结局都放在 `onSuccess` 分流：全成功 `success` / 部分成功 `warning` / 全失败 `error`；
`onError` 只留给 4xx/5xx（网络错、超限、无权限）。旧实现用 `throw` 表达部分失败 ——
那会把已退役成功的那批说成「批量退役失败」，用户重试时又对已退役的资产报 400。

### 4. 审计一行，不拆 N 行
批量端点的审计价值恰在「一次批量动作 = 一行」：AuditLog 中间件按请求落
`path=/api/assets/bulk-retire`，`failed` 的明细在响应体里给调用方，不写库（写了就无法区分一次批量与 N 次单条）。
id 列表不落 `audit_logs`（列宽 `resource varchar(50)` 装不下，且审计行**不含请求体**是既有约定）。

### 5. 契约与生成物
`TestRoutes_OpenAPI契约集合相等` 是**集合相等**（无 allowlist），新路由必须同时进 openapi.yaml；
`api.types.ts` 由 CI 的「生成物漂移检查」守护（`gen:api` 后 `git diff --exit-code`）。
重生成时发现该文件自 M32 后未再生成，M38-B 的 `/alert-rules/{id}/triggers` 一直缺失 —— CI 那一步自 M38-B 起就是红的，本轮顺带修掉。

## 学到 / Retro

- **sqlmock 的 savepoint 名不可预测**：gorm 用 `maphash` 生成 `sp<随机>`，期望必须写 `SAVEPOINT sp[0-9]+`；
  第一版按 `sp0` 写，三条用例全红在「命令不匹配」而不是业务断言上。
- **路由顺序测试要落在调用面上**：`/assets/bulk-retire` 与 `/assets/:id/retire` 对空 body 都返 400，
  只看状态码分不出来；必须给合法 body 并断言是**哪个** service 方法被调（`bulkCalled true / retireCalled false`）。
- **测试 mock 的回调传递是真断言的前提**：`useApiMutation` 的 mock 原先丢掉 `options`，
  于是「部分成功走哪个后缀的消息」在单测里根本不可观测。补上 onSuccess/onError 传递后，
  三态分流才有回归网（否则测试只覆盖「mutate 被调」）。
- **`any` 清扫**：本仓存量大量 `res?.data?.data` 风格 + `(e: any)`；本轮新增代码按仓库规则改用
  `unknown` + `in`/`typeof` 收窄与 `instanceof Error`，并把 `MutationProbe` 类型替掉测试 mock 的 `as any`。

## Follow-up（留 future round）

- **bulk restore 端点**（`POST /assets/bulk-restore`）：同形，但恢复用得少，优先度低。
- **bulk update metadata**：不在本轮 scope。
- **告警侧 `bulk_fp` 端点**：M53 留的同族摩擦（`runBulk` 串行循环），照本轮的 service 内核 + savepoint 模式可复制。
- **`failed` 明细落审计**：当前只在响应体。若要做「谁退役了哪些 id」的取证查询，需要新表（audit 行不含请求体）。
- **`Retire`/`Restore` 的 last_known IP 归属**（T-* 已登记）：IP 原属 NIC[1] 时恢复会落到 NIC[0]，要精确还原需给 assets 加来源列。
