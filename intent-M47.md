# intent-M47: G-UI-Breadcrumb 详情页面包屑改显示资产名 / 工单标题

## Context

用户视角 P2 (`12-优化建议`-型 ticket, PM 自查):
- 当前所有详情页 (`/assets/:id`, `/tickets/:id` 等) 面包屑显示
  `ID: a1b2c3d4...`, 运维**不知道是哪台机器/哪张票** →
  操作前要回到列表再搜; 跑现场手快的人看 8 位 hex 不友好.

→ **详情页加 display name 解析**: assets→`asset.name`,
tickets→`ticket.title`. fallback 是现在的 `ID: xxx...`.

## Scope

- **改**: `frontend/src/components/AppBreadcrumb.tsx`
- **不动**:
  - `frontend/src/services/api.ts` (assetApi.get + ticketApi.get 已 ship)
  - 后端 (无)
  - 其他路由 (`alert-suppressions/:id`, `metric-snapshots/:id` 等保留 ID 截断)

## Why so narrow

- AppBreadcrumb 是顶层 render-每次 nav 的组件, 用 useEffect 调
  detail fetch 是最简形态; 不引入 cache (保持组件职责唯一)
- fallback 路径就是当前的 `ID: ...`, 老代码 100% 兼容.
- 仅 assets/tickets 改是因为它们的详情页是用户最常盯的两个: 资产页 (现场维护)
  和 工单页 (日常运维). 其他详情页频次低, 优化点不显.
- 后端 zero change 是关键 — 不引入 schema 迁移 / API 文档更新 / OpenAPI 改.

## Hard constraint

- **不要**在面包屑 spinner 让 breadcrumb 项抖动; 显示前用前一个 label (cached
  上一个 detail) 或 fallback, 然后异步 swap. 首屏 detail 不阻塞 breadcrumb render.
- **不要**给 alert-suppressions / metric-snapshots / assets/:id/diagnostics 等
  加 detail fetch — scope 限定 assets/tickets.
- 命名前缀: 详情 breadcrumb 应该**带括号区分图标/颜色**, 而不是只 raw 字符串 —
  但本轮不引, 留 v2.

## Plan

1. **Step 1 (this commit)**: intent only.
2. **Step 2 (this round, fix+push)**: 改 AppBreadcrumb — `useApiQuery` 拉取
   asset/ticket detail, displayLabel 字段替换 hardcoded `ID: ...`.
   严格保持"加载完成前"用 fallback 行为 (react-query `keepPreviousData` 跟
   `placeholderData` 同 effect, 选 useEffect 简单形态 + cache 走 component state).
3. **Step 3 (test)**: vitest 扩 — mock `assetApi.get` + `ticketApi.get`,
   验证 "首页 / 资产管理 / name" + "首页 / 工单管理 / title"; 再加一个
   "fetch 失败回退 ID: xxx" 反向测试.
4. **Step 4 (verify)**: `npm run vitest` + `npm run build` (lint+tsc).
5. **Step 5 (docs)**: CHANGELOG + M47-completion-report.md + TODO.

## Estimate

≤1h PM-direct. Single file change. No backend.

## Process retro commitment

- 改 useEffect 拉 API 不能引循环 (因为 useApiQuery 的 key 含 pathname + id)
- 单测必须验证 fetch 前的 fallback 行为, 不能等待 fetch 完成
- 不要 mock 整个 `api` 而只 mock `assetApi.get` / `ticketApi.get` — 全局 mock
  会让 fetch 路径全返回 undefined 把 react-query 误判 loading=true
