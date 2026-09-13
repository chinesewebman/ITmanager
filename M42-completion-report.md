# M42 Completion Report — G-21 UpdateAsset jsonb 入参规范化

## Delivered

1. **`normalizeJSONBFields` helper** (`backend/internal/api/handlers/jsonb_normalize.go`) — 校验 PATCH 入参中 `tags` / `custom_fields` 列的合法性
2. **`NormalizeJSONBFieldsForTest` export wrapper** — 跨包测试用
3. **`UpdateAsset` 接线** (`backend/internal/api/handlers/asset_handler.go`) — `ShouldBindJSON` 之后立即调 `normalizeJSONBFields`，非法入参转 `apierr.BadRequest` (400)
4. **10 个 unit test** (`backend/internal/api/handlers/jsonb_normalize_test.go`) — 覆盖 G-21 入参矩阵
5. **真 PG db_smoke** (`backend/tests/db_smoke_test.go` `TestDBSmoke_G21_JSONBUpdateReject`) — 反证 service 层兜底确实有 42804 风险
6. **`scripts/db_smoke.sh` 白名单** — 新增测试名加进 fresh 路径 `-run`
7. **`TODO.md` G-21 标 done** + 详细结案说明

## Changed

```
backend/internal/api/handlers/jsonb_normalize.go       |  76 +++++++
backend/internal/api/handlers/jsonb_normalize_test.go  | 122 ++++++++
backend/internal/api/handlers/asset_handler.go         |   7 +
backend/tests/db_smoke_test.go                         |  57 ++++
scripts/db_smoke.sh                                    |   2 +-
TODO.md                                                |   3 +-
6 files changed
```

## Validation

### AC-M42-1: 10 个 unit test 全 PASS
- `TestNormalizeJSONBFields_NilJSONBValue` — tags=null 拒 400 ✓
- `TestNormalizeJSONBFields_EmptyStringJSONBValue` — custom_fields="" 拒 ✓
- `TestNormalizeJSONBFields_StringScalarRejected` — tags="x" 拒 ✓
- `TestNormalizeJSONBFields_NonEmptyArrayRejected` — tags=["a","b"] 拒 ✓
- `TestNormalizeJSONBFields_ScalarNumericRejected` — custom_fields=123 拒 ✓
- `TestNormalizeJSONBFields_EmptyArrayAccepted` — tags=[] 过 ✓
- `TestNormalizeJSONBFields_EmptyObjectAccepted` — custom_fields={} 过 ✓
- `TestNormalizeJSONBFields_NonEmptyObjectAccepted` — custom_fields={"k":"v"} 过 ✓
- `TestNormalizeJSONBFields_NonJSONBColIgnored` — name/status 不拦 ✓
- `TestNormalizeJSONBFields_MixedPassAndFail` — name+tags=null 时 fail-fast ✓

### AC-M42-2: 真 PG db_smoke 5 场景 PASS
- ① service.Create 走钩子（合法 jsonb 自动归一）✓
- ② service.Update 写 custom_fields={} 成功 ✓
- ③ service.Update 写 tags=[] 成功 ✓
- ④ service.Update 写 tags=["a","b"] → 42804 类型拒收（实证 handler 守门价值）✓
- ⑤ 反证：若未来第二个 handler 绕开 normalizeJSONBFields，service 层会爆 42804 ✓

### AC-M42-3: `go test -race -count=1 ./...` 27 packages 0 FAIL
- 实测 1m 5s，含 race detector 开销

### AC-M42-4: `go vet ./...` clean
- 除 sqlite3 C warning `zTail = strrchr(zName, '_')` (pre-existing) ✓

### AC-M42-5: 不动 BeforeSave 钩子
- `git diff da68dc5...HEAD backend/internal/models/asset.go` — 0 changes ✓

### AC-M42-6: 不动 migration 000014
- `git diff da68dc5...HEAD backend/migrations/` — 0 changes ✓

### Mutation inversion (defensive)

| Step | Mutation | Result |
|------|----------|--------|
| Unit | `checkJSONBValue` 永返 nil | 6 测试 FAIL（拒类全挂）→ revert → 10 PASS ✓ |
| 真 PG | (无 mutation；反证场景已内置) | ⑤ 直接断言 handler 不守门 = service 42804 ✓ |

## Risk

### 残余 1: service.Update 仍是裸 `Updates(map)`，handler 是唯一守门

- 若未来有第二个 handler 绕过 normalizeJSONBFields，service 层会爆 42804
- 真 PG 反证用例已经钉住这一风险
- 下 round 可在 service 层加 `Updates(map)` 入参校验（不在 M42 scope）

### 残余 2: 结构体 `Updates` 零值被 gorm 静默跳过

- `db.Model(&a).Updates(Asset{Tags: ""})` — gorm 行为，零值字段不写入
- 这不算 ITmanager 缺陷，是 gorm 设计
- 若 client 想"清空 tags" 必须传 `tags: []` 或 `tags: null`（后者被 normalize 拒，前者过）

### 残余 3: array elements 没细查

- 当前规则：非空 `[]interface{}` 一律拒（避免 gorm 渲染 PG record 类型 42804）
- 但「非空数组元素都是 object」理论上 PG jsonb 接受
- 比如 `tags: [{"k":"v"}]` —— 这种合法但当前被拒
- **本 round 拒**：保守策略；下 round 若 telemetry 显示 client 真要传 object array，可放宽

## Status

- **AC-M42-1 ~ AC-M42-6**: 全过
- **CI**: workflow 已含 `-race`（M41）+ 本 round 代码不需 CI 改动
- **Commits** (5 per round cadence):
  1. `650d618` docs(M42): intent-M42.md
  2. `a856dcb` feat(M42): UpdateAsset jsonb 入参规范化 helper + handler 接线
  3. `5741ae7` test(M42): 10 个 normalizeJSONBFields 单测
  4. `fc0767d` test(M42): 真 PG TestDBSmoke_G21_JSONBUpdateReject
  5. (本 commit) docs(M42): CHANGELOG + completion report + TODO.md G-21 标 done

## Process retro

### 教训 1: 真 PG 报 SQLSTATE 跟 sqlite 不一样

- FIX-PLAN 写 "22P02 invalid input syntax"
- 真 PG 实际报 "42804 column ... is of type jsonb but expression is of type record"
- 起因：sqlite 把 `('{a,b}')` 当 invalid syntax；PG 把 `('{a,b}')` 当 record 类型 vs jsonb 类型不匹配
- 修法：测试断言用 `42804 OR 22P02 OR type OR invalid` 任一关键词命中即可；不必钉死一个 SQLSTATE

### 教训 2: 自己定的 intent scope 也要权衡

- intent step 5 写了 "真 PG db_smoke 测 service.Update 写空数组"——其实 handler 层已 ship 的规范化 + service.Update 已能写 jsonb 列，这条 db_smoke 价值不高
- 但 step 5 另一条 "service.Update 写非空数组 → 22P02" 价值**极高**——它实证了 handler 守门的必要性 + 钉住了未来 service 层兜底缺失的风险
- 下次 intent 拆分更细：正向测试 vs 反证测试分开列

### 教训 3: lint cache 误报 vs go build source of truth

- patch 时 lint 报 "undefined: normalizeJSONBFields" (pre-existing go.mod not found)
- 实际 `go build` OK（lint cache 还没 rebuild）
- 规则：**lint 报 "undefined" 时，跑 `go build` 验证**（不要相信 lint cache）

## Next round candidates

按 PM_QUEUE.json 当前 6 个 pending：
- **G-23** (≤1h) — ticket.Tags 表示一致性
- **G-30** (≤1h) — smoke 脚本密码泄漏
- **G-32** (≤1h) — gorm/pgx %#v
- **G-31** (≤2h) — 同类错误回显点收口
- **G-4** (2-3h) — 账号处置无产品化入口
- **P1-3-MIB** (5-6h subagent) — MIB 浏览器
