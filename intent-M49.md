# intent-M49: G-UI-Audit 审计日志前端页面 (admin "谁改了啥")

## Context

后端 `/api/audit/logs` 已 ship (M22, `audit_handler.go:26 ListAuditLogs`),
但**前端没 ui** — 管理员想"谁什么时候改了某资产/某工单/某用户"只能
直接 curl API. 同 user PM 自查笔记 (G-UI-Audit P1).

`grep` 验证: `App.tsx` 路由清单 11 项 (assets / alerts / tickets / ...),
**无 `/audit`**.

→ **加 Audit 页** + Settings/Admin 区域导航 + 调用 `/api/audit/logs`.

## Scope

- **新增** `frontend/src/pages/Audit.tsx` (列表 + filter by user/action/entity)
- **新增** `frontend/src/services/api.ts` 的 `auditApi.list(params)` 桥 (已 ship
  service, 仅 client export 没加 — PM 直查)
- **新增** `frontend/src/types/index.ts` 的 `AuditEvent` 类型
- **新增**路由 `/audit` in `App.tsx` + `AppBreadcrumb.TOP_LABELS` 加 '审计日志'
- **新增** Settings 页 admin 菜单入口 (Settings.tsx 有 30+ tabs 区, 加 "审计日志"
  link, 或独立 sidebar)
- **新增** `frontend/src/pages/Audit.test.tsx` (list 调用 / filter / 分页 测 ≥3)
- **改**: `CHANGELOG.md` `### M49` section (before M48)
- **改**: `TODO.md` 末尾加 G-UI-Audit done

## Hard constraints

- **不要**给 audit 数据加导出 PDF (本 round 留 backlog) — 仅 CSV (admin 习惯)
- **不要**给 audit 数据加 delete/edit (只读) — audit 是不可变历史
- **不要**前端缓存 audit 多页 (默认 30+ page deep cache, 不设)
- **不要**给普通用户开放 audit 页 (admin only) — backend 已 ship RBAC,
  前端按 settings 同款菜单隐藏 (Settings 的 admin tabs)
- **不要**硬编码 action/entity 词表 — 从 event.action 实际数据动态列

## Acceptance

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Audit.test.tsx` ≥3 测试 PASS
- `npx vitest run` 全 frontend ≥338 + 3 = ≥341 PASS
- 后端 27 packages + 47 db_smoke 全绿 (本 round 不动 backend, 保无回归)
- mutation inversion: bypass auditApi.list 调用 → 测试 FAIL
- commit cadence (4-5 commits per round, intent + feat + test + docs)

## Out of scope

- audit 数据导出 PDF
- audit 高级搜索 (regex / 时间段 / 多 action 组合) — 现只支持 4 简单 filter
- audit 实时 stream (SSE/WebSocket) — 一次性 GET 已满足 admin 日常

## Estimate

≤4h PM-direct — **但 dispatch omp**

## Plan (subagent 不写; PM 不 idle)

- intent 文本先 ship intent-M49.md (commit docs)
- 写 brief
- dispatch omp background (PM 用 `secret-tool lookup provider deepseek`)
- omp 完成后 PM verify 全套 + commit any remaining docs

## Backend 已有 (不需要改)

- `audit_handler.go:26 ListAuditLogs` — GET /api/audit/logs 支持
  `user_id` / `action` / `entity_type` / `entity_id` / `since` / `until` /
  `page` / `page_size`
- 返回 `{data: {items: AuditEvent[], total, page, page_size}}`
- AuditEvent 字段 (M22 ship 时定义): `id, user_id, user_name, action,
  entity_type, entity_id, payload, ip, user_agent, created_at`

(实际字段 dispatch 时 PM 补 grep 验证)

## 不要 spawn subagent

M49 is single-agent dispatch (≤4h frontend change). 不要 omp task 加 subagent.

## Risk

- audit 数据量大时分页性能 (前端 30/页 默认, 不引 virtual scroll, 不要
  假装分页问题)
- 时间显示用本地时间 vs UTC — Settings 已 ship 全局配置, 复用 utils/time
- 用户体验: 给 admin 看 raw JSON payload 不友好 — list 第一列只显示
  summary (e.g. "修改资产 X 的 Y 字段") 然后点开 drawer 显示完整 payload
