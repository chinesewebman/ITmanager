# intent-M42: G-21 asset jsonb 入参规范化 (UpdateAsset 路径)

## Context

`docs/FIX-PLAN-ASSET-JSONB.md` §2.3 + R-1 实测口径：

| 写入方式 | 当前行为 | 期望 |
|---------|---------|------|
| `Updates(map["tags"]: "")` | 22P02 → 500 | 拒 400 或归一 `[]` |
| `Updates(map["tags"]: null)` | 写 NULL 破坏回填不变量 | 拒 400 |
| `Updates(map["tags"]: []string{"x"})` | gorm 渲染 `('x')` → 22P02 → 500 | 拒 400 |
| `Updates(map["custom_fields"]: "")` | 22P02 → 500 | 拒 400 或归一 `{}` |
| `Updates(map["custom_fields"]: null)` | 写 NULL | 拒 400 |
| `Updates(map["custom_fields"]: []string{...})` | 22P02 → 500 | 拒 400 |

**CreateAsset** 已被 G-20 (M-e7edfed) 的 `BeforeSave` 钩子覆盖（G-20 fix 已 ship：
`models/asset.go` 把零值归一为 `[]`/`{}`）。

**UpdateAsset** 走 `Updates(map[string]any)` 路径，**没有规范化**：
- `handlers/asset_handler.go:114` 直接 bindJSON → service.Update
- `service/asset_service.go:182` `db.Model(&asset).Updates(updates)` 裸传 map
- gorm 的 `Updates(map)` **不会** 触发 `BeforeSave` 钩子（G-20 修复时实测过）

## Goal

让 `UpdateAsset` 接收 jsonb 列 (`tags`, `custom_fields`) 的非法入参时：

1. **`null` / `""` → 400**（明确拒绝，让客户端知道这是错的）
2. **非对象非数组的 JSON 值（`"x"`, `123`, `true`）→ 400**
3. **数组值 `["a", "b"]` → 400**（tags 应是数组但元素是 string 而非 object，歧义；custom_fields 应是 object 不是 array）

接受：
- `tags: []` （空数组，正常）
- `tags: [{"k":"v"}]` （元素是 object）—— 但**当前实际数据 tags 都是 array of string**，让"空数组"和"有元素数组"都通过会让 client 用错 schema 静默通过；G-21 范围内**只接受空数组**，非空数组若 element 是 string 也拒（G-23 扩展）
- `custom_fields: {}` （空对象）
- `custom_fields: {"k":"v"}` （非空对象）

## Non-goals

- 不动 `BeforeSave` 钩子（G-20 已 ship，scope 守住）
- 不动列默认值 migration 000014（G-20 ship 时已有）
- 不动 service.Get / List / Delete
- 不改 netbox-sync / 一致性问题（属 G-22）
- 不改 ticket.Tags 表示一致性（属 G-23）
- 不动 cmd/seed（已 G-20 修）

## Approach

### Step 1: 加 jsonb 入参 normalize helper (≤30min)

`backend/internal/api/handlers/asset_handler.go` 或新文件 `jsonb_normalize.go`：

```go
// normalizeJSONBMap 修 PATCH 入参的 jsonb 列。返回 (normalized, error)。
// 对 tags / custom_fields 两列做:
//   nil/""    → ErrInvalidJSONBInput (400)
//   非对象非数组 JSON 值 → ErrInvalidJSONBInput
//   非空数组但元素全是 string → ErrInvalidJSONBInput (tags 应该是 array of {tag, ...} 不只是 string; 留 G-23)
//   其他通过, 原样返回 (service 层再用)
```

简化方案（**更聚焦，不扩张**）：
```go
// normalizeJSONBField 对单个 jsonb 入参做规范化:
//   nil/""   → 拒 400 (避免 NULL / 22P02)
//   "x"      → 拒 400 (string 不是合法 JSONB value)
//   [a,b]    → 拒 400 (string array 不符合 tags/custom_fields schema)
//   []/{}    → 通过
//   其他     → 通过 (合法 JSONB value)
```

### Step 2: handler UpdateAsset 用 helper (≤30min)

`handlers/asset_handler.go:114`：
```go
func (h *AssetHandler) UpdateAsset(c *gin.Context) {
    var updates map[string]interface{}
    if err := c.ShouldBindJSON(&updates); err != nil {
        apierr.BadRequest(c, "请求参数错误")
        return
    }
    if err := normalizeJSONBFields(updates); err != nil {
        apierr.BadRequest(c, err.Error())
        return
    }
    ...
}
```

### Step 3: 单测覆盖 (≤30min)

`handlers/asset_handler_test.go`（已有，扩展）：
- 7 个用例：
  - tags=null → 400
  - tags="" → 400
  - tags="x" → 400
  - tags=[] → 200（空数组 OK）
  - tags=[a,b] (string array) → 400
  - custom_fields=null → 400
  - custom_fields="" → 400
  - custom_fields={} → 200
  - custom_fields={"k":"v"} → 200

### Step 4: docs (≤15min)

- `M42-completion-report.md` (5 sections)
- `CHANGELOG.md` M42 section
- `TODO.md` G-21 标 done

### Step 5: 真 PG db_smoke (≤15min)

`backend/tests/db_smoke_test.go`：
- `TestDBSmoke_G21_JSONBUpdateReject`：5 场景 (上述 7 类的子集，能跑真 PG 的)

## Acceptance Criteria

AC-M42-1: 7 个 unit test 全 PASS（覆盖 G-21 §Step 3 列的输入）
AC-M42-2: 真 PG db_smoke 5 场景 PASS（`null`/`""`/string-array 拒 400；合法通过 200）
AC-M42-3: `go test -race -count=1 ./...` 27 packages 仍全绿
AC-M42-4: `go vet ./...` clean
AC-M42-5: 不动 `BeforeSave` 钩子（`git diff origin/main...HEAD backend/internal/models/asset.go` 仅注释/不变动逻辑）
AC-M42-6: 不动 migration 000014 (列默认值已 ship)

## Trade-offs

- **拒 vs 归一**：spec §6 写「`null` → 写 NULL 破坏回填」需要拒 400。但 `""` 既可以视为「清空」也可以视为「非法」。本 round 选**统一拒**（`null`/`""` 都 400），理由：(a) 与 §2.3 表格「无人保证」的对齐——明确告诉 client 这是错的；(b) 归一会让 BeforeSave 钩子的「零值归一 `[]`」失效（UpdateAsset 不走钩子），一致性差；(c) 用户可见的"清空 tags" 应该用 `DELETE` endpoint 或显式 `[]`，不是 `null`/`""`。
- **string array `["a","b"]`**：当前数据模型 `tags` 是 array of `{tag, ...}`，纯 string array 不符合 schema。**G-21 拒**；G-23 处理 ticket.Tags 表示一致性问题。
- **不在 service 层做**：service.Update 接 `map[string]any` 是通用接口，做规范化会让 service 层引入 HTTP 语义（400 vs 500）。handler 层拒更干净，service 层保持纯数据语义。

## Risks

- **R-1 守门扩展**：本来仅 docs 列"G-21 包含 `Updates(map)` 语义"，本 round 落地。如有 client 依赖 `null`/`""` 静默通过，会拿到 400 → 需 client 改（good — 早暴露）
- **service 层可能仍被绕过**：handler 拒了非法输入，但 service.Update 仍直接 `Updates(map)`。如果未来有第二个 handler 走 service.Update，会绕过规范化。**R-mitigation**：本 round 留 interface-level 注释，注明"非法 jsonb 入参需 caller 守门"；下 round 加 service 层兜底（不在 M42 scope）

## Validation plan

1. Unit: `go test -count=1 -timeout=60s -run TestAssetHandler_UpdateAsset_JSONB ./internal/api/handlers/...` → 7 PASS
2. 真 PG: `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` → 44+5=49 PASS / 0 FAIL
3. 全包: `go test -race -count=1 ./...` → 27 packages 0 FAIL
4. mutation inversion: 注释 normalize → tags=null 测试 FAIL → revert → PASS
5. mutation inversion: 把 `nil/"" → 拒` 改成 `nil/"" → 放行` → 测试 FAIL → revert → PASS

## Commits (planned, per commit + push)

1. `docs(M42): intent-M42.md`
2. `feat(M42): UpdateAsset jsonb 入参规范化 helper + handler 接线`
3. `test(M42): 7 个 unit test 覆盖 G-21 入参矩阵`
4. `test(M42): 真 PG db_smoke TestDBSmoke_G21_JSONBUpdateReject`
5. `docs(M42): CHANGELOG + M42-completion-report.md + TODO G-21 标 done`

## Status

- Scope: PM-direct (≤2h)
- Author: hermes@local
- Branch: main
- Pre-flight: G-21 在 docs/FIX-PLAN-ASSET-JSONB.md §2.3/R-1/§6 已详细登记
