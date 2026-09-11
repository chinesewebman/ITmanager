# 可执行细节文档：导出保真（M32）

> 状态: **v2 —— 细节三路审查已跑，发现已合入**（见 §10）
> 日期: 2026-09-12
> 基线: `main` @ 0863584
> 上游: `docs/FIX-PLAN-EXPORT-FIDELITY.md`（需求 **rev2**，已过三路对抗审查）
> 本文件只写「**怎么改**」；「为什么」一律回指需求文档，不重复。
>
> 审查已把本文件 §2/§3/§4/§6.3 的改动**逐字应用到仓库副本**并实跑：
> `go build ./...` + `go vet` 通过，U1/U1b/U1c/U2/U4/U5/U6 全绿，变异 M1–M6 全红在指定断言上（§10）。
> 故本文档的代码块是**已验证可编译可跑**的，不是示意。

---

## 0. 拍板摘要（实现必须遵守的硬约束）

| # | 约束 | 出处 |
|---|---|---|
| **D-1** | 导出走 service **新增的专用全量方法 `ListAll`**；**`List` 与 `asset_service.go:87` 的 500 硬顶一个字都不动** | 需求 §3.3 / §2.1 |
| **D-2** | `X-Total-Count` = `len(items)`（**不是**另发 `COUNT(*)`），CSV 与 JSON 两分支都设 | 需求 D-2 |
| **D-3** | CSV **先写 `bytes.Buffer` → `w.Flush()` → 查 `w.Error()` → 显式设 `Content-Length` → `c.Data` 一次写出** | 需求 D-3 |
| **D-4** | JSON 分支保留 `{code:0, data:[...]}` 形状不变 | 需求 D-4 |
| **D-5** | 导出路由挂 `middleware.RateLimit(middleware.DefaultRateLimitConfig(10))` | 需求 D-5 |
| **D-6** | **不加** capability；**不新增**审计（审计内容变更登记 G-52）；更正 `FIX-PLAN-AUTHZ.md:155` 的**一处**事实错误（见 §9） | 需求 D-6 |
| **D-7** | `/alerts/false-positives/export` 本轮**不碰** | 需求 D-7 |
| **D-8** | **不设行数上限**（显式决策） | 需求 D-8 |
| **D-9** | CSV 列集不变：`ID,Name,Type,Status` | 需求 D-9 |
| **D-10** | **本文件新增**：`ListAll` 的排序加 `id` 兜底 → `created_at DESC, id DESC`（理由见 §2.2） | 本文件 |

> 另有一条不属于决策、但违反即判 INVALID 的硬约束：**T-31** —— `mockAssetService` 缺 `ListAll` 会让
> `handlers` 测试包**编译失败**，届时全部变异判 INVALID（实测：`does not implement service.AssetService`）。

---

## 1. 改动文件清单

| 文件 | 类型 | 内容 |
|---|---|---|
| `backend/internal/service/asset_service.go` | 改 | 新增 `ListAll`（`:60` 附近，紧邻 `List`）；`AssetService` interface（`:39-49`）加一行 |
| `backend/internal/api/handlers/asset_handler.go` | 改 | `ExportAssets`（`:219-256`）重写；**import 加 `bytes`**（`strconv` 已有，`:7`） |
| `backend/internal/api/routes.go` | 改 | `:291` 导出路由加限流中间件 |
| `backend/internal/middleware/cors.go` | 改 | `:36` `Access-Control-Expose-Headers` 追加 `X-Total-Count` |
| **`backend/internal/api/handlers/asset_handler_test.go`** | 改 | **`mockAssetService` 必须补 `ListAll`（否则编译失败，T-31）**；新增 U6 |
| `backend/internal/api/routes_integration_test.go` | 改 | **imports 加 `encoding/csv`、`strconv`、`time`**（`bytes` 已有）；`:772` 理由串；新增 §6 helper 与 U1/U1b/U1c/U2/U4/U5 |
| `backend/internal/api/openapi.yaml` | 改 | `:2161-2169` description 重写；`responses.'200'` 下加 `headers`（**本 spec 首个响应头声明**） |
| `frontend/src/services/api.types.ts` | 改（生成物） | `npm run gen:api` 重生成，一起提交 |
| `TODO.md` / `CHANGELOG.md` / `docs/FIX-PLAN-AUTHZ.md` / `docs/TRAPS.md` | 改 | 见 §9 台账 |
| `docs/IMPL-EXPORT-FIDELITY.md` | 改 | §10 审查记录 + §11 实现记录 |

**不改**：`frontend/src/services/api.ts`（无调用方，需求 F9）、`asset_service.go:87` 硬顶、任何迁移。

> 实现阶段若出现**计划外改动**（M26 的体例：实测暴露的连带修复），回填到 §11 并在本表登记，
> 不要悄悄混进某个 commit。

---

## 2. `service` 层：新增 `ListAll`

### 2.1 interface（`asset_service.go:39-49`）在 `List` 之后加一行

```go
type AssetService interface {
	List(ctx context.Context, f AssetFilter) (items []models.Asset, total int64, err error)
	// ListAll 返回全部资产（导出专用，不分页、不计数）。语义与 List 不同，见实现处注释。
	ListAll(ctx context.Context) (items []models.Asset, err error)
	Get(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
	// ... 其余不动
}
```

### 2.2 实现（紧邻 `List` 之后）

```go
// ListAll 返回全部资产（导出专用，不分页、不计数）。
//
// 为什么不让导出复用 List：List 的语义是「给我第 N 页」，pageSize 有 500 硬顶
// （防止交互式分页被一次拉爆）。导出要的是「给我全部」—— 把 PageSize 调大只会
// 被那个硬顶接管，导出行为一个字不变（docs/FIX-PLAN-EXPORT-FIDELITY.md §2.1）。
//
// 与 List 的等价性契约：本轮两者都没有过滤参数，故「ListAll 的集合 == List 无过滤
// 时逐页拼起来的集合」。将来给 List 加过滤/软删除/scope **必须同步改这里**，
// 否则导出会多返回行，推翻需求 §3.4 的「导出不构成提权」论证。
//
// 排序加 id 兜底：created_at 同刻时（批量导入是常态）单靠 created_at 顺序不稳定，
// 导出产物就没法做 diff —— 对账场景要的就是可 diff（需求 §0）。
func (s *assetService) ListAll(ctx context.Context) ([]models.Asset, error) {
	var items []models.Asset
	if err := s.db.WithContext(ctx).Model(&models.Asset{}).
		Order("created_at DESC, id DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
```

> **D-10 说明**：`Order` 比 `List` 多了 `, id DESC`，这是本文件新增的唯一「计划外」决策。
> `created_at DESC` 这一半写进了 spec（§5.1），由 U1 的顺序断言承重；
> `id DESC` 兜底**没有用例承重**（夹具 created_at 互异），已在 §7 的「抓不到的变异」里如实标注。

---

## 3. `handler` 层：重写 `ExportAssets`

### 3.1 before → after

```diff
 func (h *AssetHandler) ExportAssets(c *gin.Context) {
 	format := c.DefaultQuery("format", "csv")
 
-	// 导出走全量查询（不分页）
-	items, _, err := h.svc.List(c.Request.Context(), service.AssetFilter{Page: 1, PageSize: 500})
+	items, err := h.svc.ListAll(c.Request.Context())
 	if err != nil {
 		apierr.Internal(c, "导出资产失败", err)
 		return
 	}
 
+	// X-Total-Count = 本次导出的数据行数（不含 CSV 表头）。
+	// 取 len(items) 而不是另发一次 COUNT(*)：头值恒等于 body 行数，不存在
+	// COUNT 与 SELECT 之间的竞态窗口。调用方用它自校验完整性（需求 D-2）。
+	c.Header("X-Total-Count", strconv.Itoa(len(items)))
+
 	if format == "csv" {
 		// C-F7: 用 encoding/csv 正确转义（含逗号/换行/双引号）
 		// 并对 = + - @ \t \r 开头字段加前导单引号防止 Excel 公式注入（DDE）
-		c.Header("Content-Type", "text/csv; charset=utf-8")
 		c.Header("Content-Disposition", `attachment; filename=assets.csv`)
 		c.Header("X-Content-Type-Options", "nosniff")
 
-		w := csv.NewWriter(c.Writer)
-		defer w.Flush()
+		// 先写满内存缓冲再一次性发出：要么完整 200、要么 500 无 body。
+		// 直接 csv.NewWriter(c.Writer) 是边写边发，中途失败会产出「半截 CSV + 已 200」，
+		// 且原实现 _ = w.Write / defer w.Flush() 把错误全丢了 —— 那本身就是静默不完整（需求 D-3）。
+		var buf bytes.Buffer
+		w := csv.NewWriter(&buf)
 		_ = w.Write([]string{"ID", "Name", "Type", "Status"})
 		for _, a := range items {
 			row := []string{
 				a.ID.String(),
 				safeCSV(a.Name),
 				safeCSV(a.AssetType),
 				safeCSV(a.Status),
 			}
 			_ = w.Write(row)
 		}
+		w.Flush()
+		if err := w.Error(); err != nil {
+			apierr.Internal(c, "导出资产失败", err)
+			return
+		}
+		// 显式设 CL：body 超过 net/http 的 2KB 缓冲时不会自动带 CL，会退化成 chunked，
+		// 客户端就无法校验「收全了没有」。此处长度与即将写出的字节数恒等（同一 buf）。
+		// 实测：不设 CL 时真实 net/http 回 TransferEncoding=[chunked]、ContentLength=-1。
+		c.Header("Content-Length", strconv.Itoa(buf.Len()))
+		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
 		return
 	}
 
 	c.JSON(http.StatusOK, gin.H{
 		"code": 0,
 		"data": items,
 	})
 }
```

**三个必须守住的次序约束**：
1. `ListAll` 失败时**任何 `c.Header` 都还没设** → 500 + JSON 错误体、**不带 CSV 头**（U6 钉住）。
2. 所有 `c.Header(...)` 必须在 `c.Data` / `c.JSON` **之前**。
3. `Content-Type` 交给 `c.Data` 设（`render.Data` 只在未设时才写，实测 `render/render.go:36-41`），
   不再手写，避免两处重复。

**imports**：加 `bytes`。改完后 `service` 包仍被 `AssetFilter`（`ListAssets`）使用，**不要**顺手删 import。

---

## 4. 路由限流 + CORS

### 4.1 `routes.go:291`

```diff
-			assets.GET("/export", assetH.ExportAssets)
+			// D-5: 导出是一次全表读（去掉 500 上限后单请求成本 O(N)）。组级 100/min
+			// 允许 1 秒内突发 100 个全表导出 → 可打满连接池（MaxOpenConns=100），
+			// 故单独收紧到 10/min。夜间脚本导一次不受影响。
+			// 注意：本行排在组级 AuditLog **之后**，被 429 拒掉的请求仍会写审计行 ——
+			// 对批量数据端点这是想要的（留下滥用记录），与 :222-224 登录那处
+			// 「限流必须排在审计前」的教训不冲突（那里的问题是**未认证**即可无限写库）。
+			assets.GET("/export",
+				middleware.RateLimit(middleware.DefaultRateLimitConfig(10)),
+				assetH.ExportAssets)
```

- `DefaultRateLimitConfig(10)` 的缓存键 = `1m0s|10|请求过于频繁，请稍后再试`（`rate_limit.go:111-113`），
  与组级 `100`、login 的 `5`、改密的 `3` **互不冲突** → 独立的桶（实测：同 IP 下 `X-RateLimit-Limit="10"`，
  第 11 次 429，而组级 100 的桶同时仍有余量）。
- **追加中间件不改 `r.Routes()` 报告的 (method,path)**，也不改 `ungatedRoutes` 的键（实测）。
- gin 路由表不会因为 `/export` 多带一个中间件而与 `/:id` 冲突 panic（实测两种注册顺序均不 panic）。

### 4.2 `cors.go:36`

```diff
-			c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Disposition")
+			// X-Total-Count 是 M32 新增的导出完整性声明；不 expose 的话，
+			// 契约里声明了、跨域前端却读不到（需求 D-2 的连带项）。
+			c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Disposition, X-Total-Count")
```

---

## 5. 契约：`openapi.yaml` + 生成物

### 5.1 description 重写（`:2161-2169`）

```yaml
      description: |
        `format=csv`（默认）→ 返回 `text/csv` 附件，表头固定 `ID,Name,Type,Status`，
        字段经 `encoding/csv` 转义；以 `= + - @` 或制表符 / CR 开头的字段会加**前导单引号**，
        防 Excel 公式注入（DDE）。
        其他取值 → 返回 JSON（`data` 为资产数组）。

        导出**全部资产**（本端点无过滤参数；无行数上限、不分页），按 `created_at` 倒序，
        同刻按 `id` 倒序 —— 保证产物可 diff。响应头 `X-Total-Count` = 数据行数（不含表头），
        `Content-Length` = 完整字节数：两者都供调用方自校验「收到的内容是完整的」。
        CSV 分支先写满内存缓冲再一次性发出：取数失败返回 500 且**不带** CSV 头，不会产出半截文件。
        注意：本端点有独立限流（10 次/分钟 per IP），比其余端点更紧。
```

> ⚠️ **T-59**：该块是 `|` 块标量，可以反引号开头；**不要**把它改成 plain scalar。
> 改完必须 `cd frontend && npm run validate:api`。

### 5.2 新增响应头声明（本 spec 首个，无先例可抄）

在 `responses.'200'` 下、`content:` **之前**插入（缩进已按实际文件核对，可直贴）：

```yaml
        '200':
          description: |
            `format=csv` 时是 `text/csv` 附件（非 JSON）；否则为 JSON 信封。
          headers:
            X-Total-Count:
              description: 本次导出的数据行数（不含 CSV 表头）。CSV 与 JSON 两个分支都会返回。
              schema: { type: integer }
          content:
            # ...以下不动
```

- 实测 `swagger-cli validate` → `valid`；`openapi-typescript` → exit 0。
- OpenAPI 3.0.3 的 Header Object = Parameter 去掉 `name`/`in`，保留 `schema` → 写法合法。
- 与 M31 D-4 一致：`schema: integer` 来自 handler 实读（`strconv.Itoa(len(items))`）；`example` 可选，不写不违规。
- `Content-Length` 是 HTTP 标准头，不在 spec 里重复声明。

### 5.3 生成物

```bash
cd frontend
npm run validate:api          # 本地加严（CI 没有这一步，ci.yml:192-198 只有下面两条）
npm run gen:api
git add src/services/api.types.ts                      # 先 add：判据是「重生成后无差异」
git diff --exit-code -- src/services/api.types.ts      # 判据是退出码，不是输出为空（T-58）
```

**预期 diff 有两处**（都属预期，一起提交）：
1. `description` → JSDoc 注释（改文案必然产生）；
2. **新增的 `headers` 声明** → `operations["exportAssets"].responses[200]` 里多出
   `"X-Total-Count"?: number`。实测 `openapi-typescript 7.13.0` 只加这 2 行，不破坏既有
   `headers: { [name: string]: unknown }` 形状，`tsc` 不受影响（该 operation 全仓无调用方）。

> 实测：对**未改**的 spec 重生成，产物与仓库里的 `api.types.ts` **逐字节相同** → 生成器确定性没问题，
> 出现 diff 一定是本轮改动造成的。

---

## 6. 守门用例

### 6.1 测试基座与硬约束

基座：`setupTestRouter(t)`（`package api_test`）= 真 `SetupRouter` + 真 service + **sqlite 兼容测试 schema**
（`internal/api/testdata/migrations/`，**不是**生产迁移）。已实测可用：`created_at`/`updated_at` 列存在、
`id TEXT PRIMARY KEY` 无默认值、`asset_tag TEXT UNIQUE` 可 NULL、中文测试名的 dsn 正常。

```go
// seedAssets 裸 SQL 播种 n 条资产（导出夹具）。
//
// 为什么不用 db.Create(&models.Asset{...})：测试 schema 的 assets 表列集与 models.Asset
// **不一致**（缺 brand/vendor/warranty_end/retired_*/business_unit/source），GORM 全列
// INSERT 会 `no such column: brand`（同坑现成证据：routes_integration_test.go:1301）。
//
// id 必须显式给（表无默认值；gen_random_uuid() 只在驱动层注册为函数，不是列默认）。
// asset_tag 留 NULL（UNIQUE 列，sqlite 允许多行 NULL）。
//
// created_at 递减：让「按 created_at 倒序」可判定 —— asset-0000 最新，应排第一行。
// 前三条的 name 刻意含逗号 / 换行 / 公式前缀，覆盖 csv.Writer 转义与 safeCSV 两个分支
// （否则这两条分支零覆盖，把缓冲实现换成字符串拼接也不会有用例变红）。
func seedAssets(t *testing.T, n int) {
	t.Helper()
	db := database.GetDB() // setupTestRouter 已 SetDBForTest
	require.NotNil(t, db)
	base := time.Now().UTC()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("asset-%04d", i)
		switch i {
		case 0:
			name = "zw,comma"      // csv.Writer 必须加引号
		case 1:
			name = "zw\nnewline"   // 跨行记录：数 \n 会数错，必须用 csv.Reader 解析
		case 2:
			name = "=cmd()"        // safeCSV 必须加前导单引号（DDE 防护）
		}
		require.NoError(t, tx.Exec(
			`INSERT INTO assets (id, name, asset_type, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), name, "server", "active",
			base.Add(-time.Duration(i)*time.Second), base).Error)
	}
	require.NoError(t, tx.Commit().Error)
}
```

**⚠️ 限流桶（本步实测发现，登记 T-60）**：`rlCache` 是**包级**的（`rate_limit.go:88`），
桶键 = `ClientIP|FullPath`（`:36-38`）。`package api_test` 里所有测试**共用**同一个
`/api/assets/export` 的 RL(10) 桶，而 `resetRateLimiterCache()`（`:132`）**未导出**、无逃生门。
实测后果：`go test -count=1` 绿，`-count=2` 恰好压线（10 次），**`-count=3` 全线红**。

**解法 = 复用既有模式**：每个用例设独立源 IP，`req.RemoteAddr = probeIP()`
（`routes_integration_test.go:54-62` 就是为此存在的，注释原文：「让每次运行（含 `go test -count=N`）
拿到不同的限流桶」）。**不要**为此导出 reset 钩子，也**不要**放宽 Max（那会带走 D-5 的保护）。

每个用例另带前提钉子（T-57 式），把 429 与真正的逻辑失败区分开：

```go
require.NotEqual(t, http.StatusTooManyRequests, w.Code,
	"撞上导出端点 RL(10) 的桶 —— 不是导出逻辑坏了。本用例必须 req.RemoteAddr = probeIP()")
```

### 6.2 用例表（U3/U7 已并入 U1 的断言）

| # | 夹具 | 断言 |
|---|---|---|
| **U1** | `seedAssets(t, 1001)` → `GET /api/assets/export` | ① `csv.NewReader` 记录数 == **1002**（1001 数据 + 1 表头；旧实现固定 501）；② `X-Total-Count` == `"1001"`；③ `Content-Length` == `len(body)`；④ **顺序**：首条数据行 == `zw,comma`（即 asset-0000，created_at 最大）；⑤ **转义**：第 2/3 条数据行分别解析回 `zw\nnewline` 与 `'=cmd()`（跨行记录被正确引号包裹、公式前缀被 safeCSV 加引号） |
| **U1b** | `seedAssets(t, 501)` | 记录数 == **502**。501 是旧代码出错的**最小判别点**（旧行为静默少 1 行） |
| **U1c** | `seedAssets(t, 500)` | 记录数 == **501**。新老行为应当一致的对照点 |
| **U2** | `seedAssets(t, 1001)` → `?format=json` | `data` 数组长度 == **1001**；`X-Total-Count` == `"1001"`。**必须与 U1 同基座（真 service）** —— 用 `mockAssetService` 断行数是空转（其 `List` 忽略 `AssetFilter`），且 M4 将不可能变红 |
| **U4** | 空表（不播种） | 记录数 == **1**（仅表头）、200；`X-Total-Count` == `"0"`（区分「空」与「错」） |
| **U5** | `seedAssets(t, 1001)` → `GET /api/assets?page_size=600` | `data.items` 长度 == **500**（证明 500 硬顶**保留**、未被 R4 误删）；并断言 `data.size` == **600** —— 既有语义是「`size` 回显原始 query、不承诺被钳制」（`asset_handler.go:62`），钉住它免得将来被「顺手修正」。**走 `/api/assets` 路径，不占用导出桶** |
| **U6** | `asset_handler_test.go` 的 mock 基座，注入 `listAllFunc` 返回 error | 500 且响应体**不含** CSV 头（`Content-Type` 非 `text/csv`、无 `Content-Disposition`）；**且** `require.Contains(t, w.Body.String(), "internal_error")` |

**U1 的断言必须按这个顺序写**（实测踩过的坑）：

```go
body := w.Body.Bytes()                                        // ① 先抓 body
recs, err := csv.NewReader(bytes.NewReader(body)).ReadAll()   // ② 再解析
require.NoError(t, err)
require.Len(t, recs, 1002)
require.Equal(t, "1001", w.Header().Get("X-Total-Count"))
require.Equal(t, strconv.Itoa(len(body)), w.Header().Get("Content-Length"))
```

> **假红陷阱**：`csv.NewReader(w.Body)` 会把 `httptest` 的 body buffer **读空**，
> 之后再取 `w.Body.Len()` 恒为 0 → 第 ③ 条断言必然失败，且与 D-3 无关。
> 实测输出：`after ReadAll: w.Body.Len()=0 (CL header=640)`。

**U6 的假绿陷阱**：mock 忘设 `listAllFunc`（nil）会 **panic**，`gin.Recovery()` 返回的
500 恰好也「无 CSV 头」→ U6 会因错误的原因通过。所以加 ② 那条形状断言（只有
service-error 路径才打出 `{"code":"internal_error",...}`；panic 路径是空 body）。
另在 mock 构造处加 `require.NotNil(t, svc.listAllFunc, "U6 前提：必须注入 listAllFunc")`。

CSV 记录数一律用 `csv.Reader` 解析后 `len(records)`，**不要**数 `\n`（U1 的 `zw\nnewline` 夹具就是为此而设）。

### 6.3 mock 补偿（T-31 前置）

`asset_handler_test.go` 的 `mockAssetService`：
```go
type mockAssetService struct {
	listFunc    func(ctx context.Context, f service.AssetFilter) ([]models.Asset, int64, error)
	listAllFunc func(ctx context.Context) ([]models.Asset, error)   // 新增
	// ... 其余不动
}

func (m *mockAssetService) ListAll(ctx context.Context) ([]models.Asset, error) {
	return m.listAllFunc(ctx)
}
```
**漏了这一步 → `handlers` 测试包编译失败 → 按 T-31 所有变异判 INVALID。**
实测确认：全仓**只有** `mockAssetService` 一个类型实现 `AssetService`，改它是唯一的编译面。

---

## 7. 变异表

审查已把 M1–M6 **逐条实跑**（先 `go build` 确认可编译，再确认红在预期断言）：

| # | 变异 | 实测红在 | 证明 |
|---|---|---|---|
| **M1** | handler 改回 `List(ctx, AssetFilter{Page:1, PageSize:500})` | U1 ① | 用例真在测行数 |
| **M2** | 删 `c.Header("X-Total-Count", ...)` | U1 ② / U2 | 头被真的断言 |
| **M3** | `ListAll` 里加 `.Limit(1000)`（**有限上限**而非去掉） | U1 ①（1002 vs 1001）+ ②（"1001" vs "1000"） | 夹具选 1001 而非 600 的价值 —— 600 抓不到这条 |
| **M4** | JSON 分支注入 `items = items[:500]`（**人工注入式**，两分支共用同一次取数，非自然可改） | U2 | **准确含义是「U1 管 CSV、U2 管 JSON」**，单看 U1 不会红 |
| **M5** | 删 `asset_service.go:87` 的 500 硬顶 | U5（items 变 600） | 反向破坏被钉住 |
| **M6** | 改回 `csv.NewWriter(c.Writer)` 直接写（不缓冲、不设 CL） | U1 ③（`"62082"` vs `""`） | D-3 的原子写被断言 |
| **M7** | 把 `X-Total-Count` 挪到 CSV 分支**之内、`c.Data` 之后** | U1 ②/U2 | 头必须对两个分支都生效、且在写出前设 |
| — | `X-Total-Count` 改成独立 `COUNT(*)` | **不会红** | **诚实标注**：抓不到，所以 D-2 的理由是**消除竞态**而非「测试能抓」 |
| — | 删掉 `ListAll` 的 `, id DESC` 兜底 | **不会红** | 夹具 `created_at` 互异，`id` 兜底无用例承重（见 §2.2） |
| — | `w.Error()` 检查去掉 | **不会红** | 无夹具能触发对 `bytes.Buffer` 的写错误；保留纯属防御 |

---

## 8. 门禁序列

```
cd backend && gofmt -l ./internal ./cmd ./tests   # 必须空
go vet ./... && go test ./... -count=1 && go build ./...
cd .. && ./scripts/db_smoke.sh
cd frontend && npx tsc --noEmit && npx eslint src --ext .ts,.tsx && npx vitest run
```

- **`db_smoke.sh` 白名单不需要改**：本轮不新增 `TestDBSmoke_*` 用例；新用例在 `internal/api/`，由 `go test ./...` 覆盖（T-42 不适用）。
- 改 spec 的步骤**额外**跑 §5.3 的三条命令。
- `go test ./internal/api/ -count=3 -run TestU` 应保持绿 —— 这是 §6.1 的 `probeIP()` 是否到位的验收（不设独立 IP 时此命令必红）。

---

## 9. 台账动作（步骤 5）

| 文件 | 动作 |
|---|---|
| `TODO.md:330` | G-49 改 `[x]`，并在**正文头部**加重写摘要（原正文仍以「固定前 500 条」为现存缺陷描述，直接勾选会留下过期叙述）；新增 **G-50**（CSV 列不保真 + 新列必须过 `safeCSV`）、**G-51**（false-positives 无界导出 + 导出资源策略/触发条件）、**G-52**（导出能力门禁 + 审计内容） |
| `CHANGELOG.md` | 新增 M32 条目；**不改 `:166`**（M31 的历史登记）。格式照既有体例 `- ⚠️ **行为突变告知（M32，运维需知）**：…` —— `/assets/export` 从「前 500 条」变为**全量**，并新增 10 次/分钟独立限流 |
| `docs/FIX-PLAN-AUTHZ.md:155` | **只更正一处**：「导出限 500 行」随本次失效。**`sanitizeFilename` 那半句不动** —— 该句把三个端点作为一个组描述，`sanitizeFilename` 对应组内的 `/postmortem/assets/:id/report`（`postmortem_handler.go:85` 调用、`:104-105` 定义），是**真陈述**；asset/false-positives 导出用静态字面量、AUTHZ 也从未声称它们做清洗 |
| `routes_integration_test.go:772` | `"资产导出（限 500 行 + safeCSV）"` → `"资产导出（全量 + X-Total-Count + safeCSV）"`（该串只被断言非空，**没有任何测试会提醒它过期**） |
| `docs/TRAPS.md` | 新增 **T-60**：限流桶跨测试共享（包级 `rlCache` + `IP\|FullPath`，reset 钩子未导出）→ 集成测试里同一端点的累计调用会静默逼近上限，`-count=N` 时最先爆。体例照 `:866`（T-59）：`### T-60. <标题>` + `**状态**: ACTIVE | **类别**: <…>` + 现象/根因/解法 + `**来源**: M32(2026-09-12, …)`；**并在 §五索引表尾（`:970-972` 之后）追加 `\| — (M32 轮) \| T-60 \| ACTIVE \|`** |
| `docs/IMPL-EXPORT-FIDELITY.md` | §10 审查记录（已填）+ §11 实现记录（逐项 before/after、变异实跑输出、门禁输出） |

---

## 10. 审查记录

**v1 → v2 三路对抗审查**（正确性 / 边界与测试 / 一致性与契约）。
审查方法：把 §2/§3/§4/§6.3 的改动**逐字应用到仓库副本**（`/tmp/be`、`/tmp/itmb`），实跑
`go build` / `go vet` / `go test`，并逐条施加变异 M1–M6 验证。**仓库零改动。**

### 已被采纳并改写正文的发现

| # | 发现 | 严重度 | 并入处 |
|---|---|---|---|
| B1 | **U1 断言顺序假红**：`csv.NewReader(w.Body)` 掏空 buffer，之后 `w.Body.Len()` 恒 0（实测 `after ReadAll: Len=0`） | 重要 | §6.2 的断言骨架（先抓 `body := w.Body.Bytes()`） |
| B2 | **测试文件 import 未列**：`routes_integration_test.go` 缺 `encoding/csv`/`strconv`/`time` → 首次编译失败 | 重要 | §1 表格 |
| B3 | **U6 假绿**：nil `listAllFunc` → panic → `gin.Recovery()` 的 500 同样「无 CSV 头」 | 重要 | §6.2 增 `internal_error` 形状断言 + `require.NotNil` 钉子 |
| B4 | **CSV 转义/safeCSV 分支零覆盖**（夹具名无逗号/换行/公式前缀）→ 把缓冲换成字符串拼接也无用例变红 | 重要 | `seedAssets` 前三条特殊 name + U1 ⑤ |
| B5 | **限流桶共享会让 `-count=3` 全线红**（实测 `-count=1` 绿 / `-count=2` 压线 / `-count=3` 红） | 重要 | §6.1 改用既有 `probeIP()`（每个用例独立源 IP），并新增 §8 的 `-count=3` 验收 |
| B6 | **`FIX-PLAN-AUTHZ.md:155` 的「第二处更正」是误判** —— `sanitizeFilename` 那半句对 report 端点为真 | 重要 | §0 D-6 / §9 台账（改为**一处**） |
| B7 | 「sqlite 变量上限 999，6006 会超」**实测证伪**（真实上限 32766，6006 参数成功） | 次要 | `seedAssets` 注释删掉该理由 |
| B8 | §5.3 命令在提交前**恒非 0 退出**（`gen:api` 改了工作区，diff 必然非空） | 次要 | §5.3 加 `git add` 再 `diff --exit-code` |
| B9 | `validate:api` **不在 CI**（`ci.yml:192-198` 只有 `gen:api` + diff） | 次要 | §5.3 注明「本地加严」 |
| B10 | 新增 `headers` 声明**也会改生成物结构**（`"X-Total-Count"?: number`），不止 description | 次要 | §5.3 列出两处预期 diff |
| B11 | U5 不断言 `size` 回显（实测回显 600，非钳制后的 500） | 次要 | U5 增断言 |
| B12 | spec 承诺了排序但**零用例承重** | 次要 | U1 ④ 顺序断言（夹具改 `created_at` 递减） |
| B13 | §5.1「导出**匹配的**全部资产」暗示有筛选（实际无过滤参数） | 次要 | 去掉「匹配的」 |
| B14 | TRAPS 只加了条目、漏了**§五索引表一行**与条目字段体例 | 次要 | §9 台账 |
| B15 | TODO G-49 直接勾选会留下**过期正文** | 次要 | §9 台账（重写正文头部） |
| B16 | 缺独立的「门禁序列」节（与 `IMPL-SYNC-FIDELITY.md:610` 体例不一致） | 次要 | 新增 §8 |
| B17 | §0 D 表混入 T-31 行、D-6 措辞与需求漂移 | 吹毛求疵 | §0 改为脚注 + 对齐需求措辞 |
| B18 | §1 缺「计划外改动回填」指引（M26 体例） | 吹毛求疵 | §1 表末 |
| B19 | M4 的「两个分支都覆盖」表述不精确 | 吹毛求疵 | §7 M4 行改写 |
| B20 | 补一条变异：`X-Total-Count` 挪到 CSV 分支内 | 次要 | §7 M7 |

### 审查者实测确认**无问题**的项（不重复验证，直接采信）

- §2/§3/§4/§6.3 的改动原文套用后 `go build ./...` + `go vet` 通过；U1/U1b/U1c/U2/U4/U5/U6 实跑全绿
  （U1：`bodyLen=62082 CL="62082" records=1002`）。
- **gin 路由不 panic**：`/export`（带额外中间件）与 `/:id` 两种注册顺序均不 panic（`/tmp/ginroutetest`）。
- **追加中间件不改 `r.Routes()`**，`ungatedRoutes` 键不用动；三处契约用例均不发请求 → 不影响。
- **无凭据请求不消耗导出桶**（`AuthMiddleware` 在链上先于路由级中间件并 `Abort`）→
  `TestRoutes_OpenAPI公开端点集合相等` 的全路由遍历不占预算。
- **包内对 export 的带凭据调用**：既有 1（`:297` CF4）+ 新增 5 = 6（引入 `probeIP()` 后此约束自动解除）。
- `Content-Length` 在 `httptest.ResponseRecorder` 上可读；不设时真实 net/http 回 `chunked` + `ContentLength=-1`。
- `openapi.yaml` 缩进基准逐字核对一致、`swagger-cli validate` 对加 headers 的变体报 `valid`。
- 对未改的 spec 重生成，产物与仓库 `api.types.ts` **逐字节相同**（生成器确定性 OK）。
- 空表 JSON 返回 `[]` 而非 `null`。
- 全仓 grep 「限 500 行 / 前 500 条」共 6 处，**全部在台账内**，无台账外遗漏。
- G-50/G-51/G-52 与 T-60 编号均未占用；D-1..D-9 与需求文档逐一对应无错位。

### 审查者声明**无法证实或证伪**的项（继承到下一轮，不得当作已证）

1. **真 PostgreSQL 路径**：全程只跑 sqlite 测试基座；`ListAll` 在 PG 上的行为（含与 `List` 的集合等价性）未实测。
2. **仓库外是否有脚本依赖「前 500 条」截断** —— 需求 R5 已承认「无证据 ≠ 不存在」。
3. **生产 assets 真实行数** —— 决定 R3/R6 的实际严重度；仓库内无数据。
4. `openapi-typescript` **未来版本**对响应头的生成行为（当前 7.13.0 已被 `package-lock.json` 钉死）。
5. **`w.Error()` 写失败路径与并发两次导出**：无夹具，未构造。

### 审查副产品（可在实现时顺手确认）

`c.Data` 签名、`csv.Writer.Error()`（`encoding/csv/writer.go:131`）、`render.Data` 只在未设时写 Content-Type
（`render/render.go:36-41`）均已核对无误，实现阶段不需要再验。

---

## 11. 实现记录

待填（逐项 before/after、变异实跑输出、门禁输出、计划外改动回填）
