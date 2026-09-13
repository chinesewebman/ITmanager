# intent-M50: G-UI-Tickets 工单工作流产品化 (3 sub-task)

## Context

工单 (运维日常最常用页) 现状的功能入口缺失:

1. **TicketDetailModal 只读** (`frontend/src/components/TicketDetailModal.tsx:34`)—
   footer 只有 [关闭] 按钮, 不能 "改派 / 改优先级 / 加评论 / 关单"
2. **TicketTable 无行内操作** — 只有 [查看] 按钮, 想改工单得回到详情再点关闭
3. **TicketTable 列只有 created_at** — 没显示 `updated_at`, 运维看不出
   "老问题" 与 "活跃工单"

→ **加** 1) Modal 内表单操作, 2) 表格行内下拉操作, 3) updated_at 列

## Scope (in)

- `frontend/src/components/TicketDetailModal.tsx` — 加 in-modal 操作区:
  改派 (select 用户) / 改优先级 (select) / 加评论 (textarea + 提交) /
  改状态 (按状态合法性 gating) / 关单 (popconfirm 二次确认)
- `frontend/src/components/TicketTable.tsx` — 加 updated_at 列 + 行内 dropdown
  ([更多操作] 含 "改派 / 改优先级 / 关单", 与 Modal 内 same 复用 mutation)
- (本轮) `frontend/src/services/api.ts` — 检查 `ticketApi.update(id, data)` 是
  PATCH? PUT? 不明确. 看 comment (api.ts:177-181). 用 `update`.
- (本轮) `frontend/src/pages/Tickets.tsx` — 包装 modal + table + 共享
  `onAfterAction` refresh.
- (本轮) `frontend/src/components/TicketHistoryTimeline.tsx` — 已 ship, 复用.
  M50 加评论时调用 `ticketApi.history` invalid 后台.

## Scope (out, 留 M51+)

- Settings admin 入口 (admin 工单页面 link) — 留 M51.
- 工单模板 / 批量派单 — 留 backlog.
- 工单 SLA 自动提醒 — 独立 round.
- 工单导 PDF / CSV — 独立 round.

## Hard constraints

- **不要**改后端 API (M50 是 frontend-only)
- **不要**改 Ticket model schema
- **不要**给工单加 delete 按钮 (不可逆, audit 反对)
- **不要**让 "改派" 接受任意用户 — 用 `userApi.list()` filter role=operator
- **不要**让 "加评论" 允许空提交 (frontend 验证)
- **不要**改 mutation 行为: 用 `useMutation` + `onSuccess invalidate`. 已有
  `useApiQuery` 的 `queryKeys.tickets` hook 已 ship, 复用.
- **不要**让 [关单] 立即生效 — 用 `Popconfirm` 二次确认. 已有其他处范例
  (Settings.tsx:503 类)
- **不要**让 [改派] 在 Operation 后忘记 invalidate 列表 — 用同 query key.
- **不要**把 modal 的 footer 加 [保存草稿] 之类用户未要求的按钮

## Acceptance

- 5 文件改动 + 1 test 文件
- `npx tsc --noEmit` 0 error
- `npx vitest run src/components/TicketDetailModal.test.tsx` ≥3 PASS (新建)
- `npx vitest run src/components/TicketTable.test.tsx` ≥2 PASS (现有 + 新加列)
- `npx vitest run` 全 frontend ≥343+3+2 = ≥348 PASS
- 后端 go test 27 packages ok (no backend change)
- mutation inversion 实证 (bypass 操作 onClick → 至少 1 测试 FAIL)
- commit cadence (5-7 commits: feat modal + test modal + feat table column +
  test table + feat row ops + docs)
- 所有 push to origin/main

## Estimate

≤4h PM-direct — **但 dispatch oom**

## Backend already has (no change)

- `PATCH /api/tickets/:id` — update (handler exact path TBD, grep 时确认)
- `POST /api/tickets/:id/comments` — 加评论
- `ticketApi.update(id, data)` in `frontend/src/services/api.ts:177-181`
  (PUT 后端是 PUT, 不是 PATCH — 看清楚, 别误用 PATCH 写)

## 不要 spawn subagent

M50 is single-agent dispatch (≤4h). 不要 omp task 加 subagent.
