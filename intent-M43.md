# intent-M43: G-23 ticket.Tags 入参规范化 (service 层兜底)

## Context

`TODO.md` G-23:
> `ticket.Tags` 表示一致性 — `default:'[]'` 对 Create 有效, `ticket_service.go:173-175` 另有 `""→"[]"` 归一, 所以插入路径不落 NULL; 属「表示不一致」而非缺陷.

G-23 实际包含两层问题:
1. **表示不一致**: Go `string` vs PG `jsonb` (调用方需自己 marshal/unmarshal)
2. **Update 路径无 service 兜底**: G-21 已 ship `normalizeJSONBFields` 在 handler 守
   `tags`/`custom_fields` 列, 但 service 层仍是裸 `Updates(map)` — 若有第二个
   handler / 内部 caller 绕过 handler, ticket service.Update 写非法 tags 会
   爆 PG `42804` (同 G-21 真 PG 反证口径).

**M42 ship 时**: G-21 加了 handler normalize, 同时把 `tags` 加进了 `jsonbFieldCols`
(`backend/internal/api/handlers/jsonb_normalize.go:21`), 所以 ticket UpdateAsset
(不存在, 但 ticket PATCH) 的 handler 也自动复用.

**但 ticket handler 也走 normalizeJSONBFields?** — 看 ticket handler:

```bash
grep -n 'normalizeJSONBFields\|ShouldBindJSON.*updates\|svc.Update' backend/internal/api/handlers/ticket_handler.go
```

(等下要在 pre-flight 跑; 按现状, ticket handler 也走 G-21 normalize 所以 PATCH 路径
被守门. 但 G-23 scope 是 **service 层兜底** — 让 service.Update 本身能守, 避免
未来第二个 caller 绕过 handler.)

## Goal

`ticket_service.Update` 对 `tags` 列做 service 层入参规范化. 与 G-21 handler
守门形成双层防御:
- **handler 层** (G-21): 拒绝 `null`/`""`/string 标量/非空数组/数值标量 → 400
- **service 层** (G-23): 同样拒绝, 转 `ErrInvalidInput` → handler 转 400

## Non-goals

- 不动 `models.Ticket.Tags` 类型 (string → datatypes.JSON 太重, 跨多文件)
- 不动 Create 路径 (已有 service line 173-175 归一)
- 不动 ticket handler (G-21 已守)
- 不动 G-22 / G-20 / G-21 已 ship 内容

## Approach

### Step 1: ticket service 加 tags 规范化 helper (≤15min)

`backend/internal/service/ticket_service.go` 或新建 `ticket_service_jsonb.go`:

```go
// validateTicketTagsUpdate 校验 PATCH 入参里 tags 的合法性.
// 与 handlers/jsonb_normalize.go 的 checkJSONBValue 同款口径.
// 拒 (返 ErrInvalidInput):
//   - nil / "" / string 标量 / 数值标量
//   - 非空数组 (空数组 [] OK)
// 通过:
//   - 空数组 [] / 任意 map[string]interface{} / 任何合法 jsonb value
func validateTicketTagsUpdate(v interface{}) error { ... }
```

### Step 2: service.Update 接线 (≤15min)

`backend/internal/service/ticket_service.go:491` (Update 方法):
```go
if v, ok := updates["tags"]; ok {
    if err := validateTicketTagsUpdate(v); err != nil {
        return nil, err
    }
}
```

放在 `validateTicketEnumValues(updates)` (line 530) 之后, 跟 enum 校验同等地位.

### Step 3: 单测 (≤15min)

`backend/internal/service/ticket_service_test.go` 或新建:
- 7 个用例 (与 G-21 同款矩阵):
  - tags=null → ErrInvalidInput
  - tags="" → ErrInvalidInput
  - tags="x" → ErrInvalidInput
  - tags=[] → 通过
  - tags=["a","b"] → ErrInvalidInput (PG 42804 风险)
  - tags={"k":"v"} (object) → 通过
  - 非 tags 字段不受影响

### Step 4: 真 PG db_smoke (≤15min)

复用 G-21 `TestDBSmoke_G21_JSONBUpdateReject` 的反证思路, 但测 ticket:
- `TestDBSmoke_G23_TicketTagsUpdateReject`:
  - ticket.Create + svc.Update(map["tags"]:null) → 拒
  - ticket.Create + svc.Update(map["tags"]:["a"]) → 拒 (42804)

### Step 5: docs (≤10min)

- `M43-completion-report.md`
- `CHANGELOG.md` M43 section
- `TODO.md` G-23 标 done

## Acceptance Criteria

AC-M43-1: 7 个 unit test 全 PASS (覆盖 G-23 入参矩阵)
AC-M43-2: 真 PG db_smoke 2 场景 PASS (拒 null / 拒 string array)
AC-M43-3: `go test -race -count=1 ./...` 27 packages 仍 0 FAIL
AC-M43-4: 不动 `models/ticket.go` Tags 类型
AC-M43-5: handler 层 G-21 守门**继续生效** (双层防御, service 兜底与 handler 守门不冲突)

## Trade-offs

- **与 G-21 handler 守门重复**: 没错, 是**有意双层**. handler 拒 vs service 拒:
  - handler 拒 → 400 + 不进 service
  - service 拒 → ErrInvalidInput + handler 转 400
  - 区别: service 拒是兜底, 防未来第二个 handler 绕过
  - 双重拒不会让 user 看到不同错 (最终都是 400)
- **表示不一致** (string vs jsonb): 本 round 不动. 若 user 真正报 ticket.Tags 序列化
  bug 再做, 但当前业务侧无此反馈.

## Validation plan

1. Unit: `go test -count=1 -run TestValidateTicketTagsUpdate ./internal/service/...` → 7 PASS
2. 真 PG: `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` → 47 PASS / 0 FAIL
3. 全包: `go test -race -count=1 ./...` → 27 packages 0 FAIL
4. mutation inversion: `validateTicketTagsUpdate` 永返 nil → null 拒测试 FAIL → revert → PASS

## Commits (planned)

1. `docs(M43): intent-M43.md`
2. `feat(M43): ticket service.Update 加 tags 规范化 (service 层兜底)`
3. `test(M43): 7 个 unit test 覆盖 tags 入参矩阵`
4. `test(M43): 真 PG TestDBSmoke_G23_TicketTagsUpdateReject`
5. `docs(M43): CHANGELOG + completion report + TODO G-23 标 done`

## Status

- Scope: PM-direct (≤1h)
- Author: hermes@local
- Branch: main
- Pre-flight: G-23 in TODO.md, G-21 在 M42 已 ship handler 守门
