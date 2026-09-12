---
id: INTENT-M36-G55-AND-D6
title: G-55 + D-6/D-7 残余 audit_logs.resource size 100→50 三处对齐 + Settings.tsx 显示新计数键
status: draft
author: hermes@local
created: 2026-09-12
outcomes:
  - backend/internal/models/user.go line 106 `size:100` → `size:50` (G-55 1/3)
  - backend/internal/middleware/audit.go 截断常量 100 → 50 (G-55 2/3)
  - backend/migrations/ 已 VARCHAR(50) (G-55 3/3 已 done, 不动)
  - U7a 反射断言的 9 个常量清单增加 resource=50 (守门新需求)
  - U7b 真 PG 列宽校验清单增加 audit_logs.resource (D-7 残余, 真 PG 守门)
  - 真 PG 冒烟新增 `TestDBSmoke_AuditLogResource_50CharBoundary` (resource=50.5 chars → 截断后落库)
  - frontend/src/pages/Settings.tsx 增加 4 个新字段计数显示 (D-6 残余: zabbix_field_truncations / glpi_field_truncations / netbox_field_truncations / zabbix_metrics_field_truncations)
  - frontend/src/pages/Settings.test.tsx 新增对应测试 (D-6 残余)
  - CHANGELOG M36 段
acceptance:
  - id: AC-G55-1
    given: 迁移列宽 50 已 done (M33), model tag 100 + 截断常量 100 是漂移
    when: 改 user.go:106 + audit.go 截断点 + 反射清单
    then: U7a 反射断言 ✓ (9 个旧 + resource=50 1 个新 = 10 项)
  - id: AC-G55-2
    given: 真 PG 列宽 50 vs 截断常量 50 后, 超 50 字符的路由首个段会被截断
    when: db_smoke 新增边界用例 + U7b 真 PG 清单新增 audit_logs.resource=50
    then: scripts/db_smoke.sh 白名单 +2, 真 PG 跑通新用例
  - id: AC-D6-1
    given: D-6 残余 = 前端 Settings.tsx 不显示 4 个新 *_field_truncations 计数
    when: 加 4 个字段读取 + 显示 ("截断数:netbox×N / zabbix×N / glpi×N / zabbix_metrics×N")
    then: Settings.test.tsx 新增至少 3 个测试 (snapshot + 0/非 0 显式断言 + type 切换隐藏)
edges:
  - G-55 是 G-45 同源缺陷 (常量 > 列宽 → 22001 → 行丢弃); 触发条件是「首个静态段 > 50 字符」, 实际攻击面是「审计行被静默吃掉」, 不是泄漏
  - Settings.tsx 新增读取不能与现有 zabbix_truncated 重命名 (G-41 跨语言字符串), 因为后者是 0/1 标志 (源侧上限), 新增是 >0 计数
  - 真 PG U7b 列宽清单扩充, 必须逐一实测每个常量 = information_schema.columns.character_maximum_length, 不是 reflection-only
  - Settings.test.tsx 已有 174 tests, 新增不能回归 (M33 §11 残余登记 G-57 的 description 仍不动, 仅 Settings.tsx UI)
not_goals:
  - 不修 G-39 (AlertRule.NotifyChannels 写不读, 需「告警匹配规则」语义, 属架构决策)
  - 不修 G-40 (通知渠道凭据静态明文落库, 需求文档先)
  - 不动 openapi.yaml (G-57 残余 description 缺口, 本轮不动契约)
  - 不动 G-50 (导出 CSV 列集拍板, 需产品拍板)
  - 不写 R4 docs stage 0 (R4 优先级低于 G-55 + D-6 残余)
evidence:
  - "backend/internal/models/user.go:106 `gorm:\"size:100\"` (目标: size:50)"
  - "backend/migrations/000001_init.up.sql:1097 VARCHAR(50) (已对齐)"
  - "backend/internal/middleware/audit.go 截断点 100 → 50 (现找)"
  - "internal/integration/truncate.go col* 常量清单 (待追加 1 项)"
  - "docs/FIX-PLAN-TRUNCATION.md §8 验证段 TestDBSmoke_AuditFieldTruncation 已存在"
  - "scripts/db_smoke.sh 白名单 (待加 2 条)"
  - "frontend/src/pages/Settings.tsx:264 现有 zabbix_truncated 0/1 标志 (待追加 4 个)"
  - "TODO.md G-55 登记原文 (列宽 100 > 真实 50, M33 D-7 未落地)"
---

# INTENT-M36-G55-AND-D6: G-55 + D-6 残余收口

## Context

`TODO.md` 有两条已识别但未修的缺陷, 都是**文档级 + 真代码双侧**的问题, 不依赖运维前置:

### G-55: `audit_logs.resource` 三处副本漂移

| 副本位置 | 当前值 | 目标值 |
|---|---|---|
| `migrations/000001_init.up.sql:1097` | `VARCHAR(50)` | (已对齐, 不动) |
| `models/user.go:106` | `size:100` | `size:50` |
| `middleware/audit.go` 截断常量 | 100 | 50 |

**后果**: 截到 100 仍可能超 50 → `22001` → 整行 INSERT 被拒 → 审计链少一行。

**触发条件**: 路由首个静态段 > 50 字符 (例如 `/api/integrations/very-long-service-name-...`)。当前生产路由没有 ≥50 字符的 → 潜伏而非活缺陷。

### D-6 残余: M33 加了 4 个 `*_field_truncations` 计数键, 后端透出但前端不显示

M33 加了:
- `netbox_field_truncations`
- `zabbix_field_truncations`
- `glpi_field_truncations`
- `zabbix_metrics_field_truncations`

Settings.tsx:264 仍只读 `res?.data?.data?.synced?.zabbix_truncated`, 新键**不出现在 UI**。运维只看到源侧 0/1 截断标志, 看不到字段级实际次数。

## Outcomes

参见 frontmatter outcomes[] 9 条。

## Acceptance Criteria

参见 frontmatter acceptance[] 3 条。

## Edge cases

1. **G-55 与 G-45 同源**: 都是「三处副本漂移」, 但 G-55 列表已存在的 9 个常量 (NetBox/Zabbix/GLPI 各字段) 是 16:9 缩过表, 而 `resource` 不是截断字段是审计字段 — 需要新增 1 项常量 + 改 `RedactMiddleware` 截断点。
2. **Settings.tsx UI**: 不要破坏现有 `zabbix_truncated` 0/1 标志 (那是源侧是否截断上限的标志), 4 个新键是「字段级截断次数」。
3. **真 PG 边界**: `resource` 取自 `c.FullPath()` 第一个 `/` 前段 (e.g. `/api/integrations/...` → `api`), 实际短名字几乎不可能超 50, 但**守门必须存在** (类似 `TestDBSmoke_AuditFieldTruncation` 已为 `Path` 加)。
4. **测试前端 174 个不能回归**: 新增测试需精确匹配现有 vitest 模式 (jsdom + vi.mock + react testing library)。

## Operational constraints

- Do NOT touch: `routes.go`, `assets_handler.go`, `migrations/000001_init.up.sql:1097`, `openapi.yaml`, schema 任何 DDL, 任何 handler
- Must preserve: `gofmt`, `go vet`, `go test ./... -count=1`, `db_smoke` 全绿
- 9 commits 上限 (每个变更 1 commit + 1 push, 走 small-step cadence)

## E2E verification

按 `verify-e2e`:
- 后端: `cd backend && go test ./...` 全绿, `scripts/db_smoke.sh` 全绿 (40 + 2 = 42 cases), U7a 反射断言 10 项通过, U7b 真 PG 列宽新增 resource 校验通过
- 前端: `cd frontend && npm run typecheck && npm run test` 全绿, Settings.tsx 新增 3 测试通过
- 浏览器: Settings 页同步执行一次同步 → 看到「截断数」4 行

## Completion report

After dispatch: write `M36-G55-D6-completion-report.md` per `task-completion-protocol` skill.
