# M49 Completion Report — G-UI-Audit 审计日志前端页 + admin 入口

## Delivered

后端 `/api/audit-logs`（M22 起就在，`routes.go:286` + `canAudit`）此前**前端零入口** ——
管理员查「谁什么时候改了什么」只能自己 curl。本轮补齐前端（frontend-only，后端一字未改）：

- `frontend/src/pages/Audit.tsx`（**新**）— 审计页。
  - 列：时间 / 操作人 / 动作 / 对象类型 / 对象 ID / 摘要（method + path + status）/ `[详情]`。
    时间列不加前端 sorter：最新在前由**服务端** `ORDER BY created_at DESC, id DESC` 保证，
    前端 sorter 只能排当前 30 条，会让人误以为是全局排序。
  - `[详情]` → antd `Drawer` 展示该行**完整 JSON**。审计行不含请求体（只记
    method / path / status / error_msg），故如实展示整行，**不伪装成「字段 diff」**。
  - 过滤 4 项：操作人（`user_id` 精确）/ 动作（精确）/ 方法 / 路径前缀。
  - 词表**不硬编码**：后端无 action/user 枚举接口（`action` 是自由字符串 `varchar(50)`），
    下拉项取**不带筛选的 100 条采样**（独立 query key `audit.vocabulary`）∪ 当前页。
    用「当前页」当词表会让用户筛到只剩一条后再也切不回去。
  - 分页 **cursor 式**（`limit` + `next_cursor`；后端**没有** total/page/page_size）→
    上一页/下一页 + cursor 栈，不是页码跳转。**没有 `next_cursor` 即到底**。
  - 空态两分：无筛选 → `EmptyState`「暂无审计事件」；有筛选 → 预设 `no-search-result`「暂无匹配」。
  - 失败 → `ErrorState` + 重试（**不**静默回落到空列表 —— 那会把 500 伪装成「没有留痕」）。
- `frontend/src/services/api.ts` — `auditApi.list(params)`（路径 `/audit-logs`）、`authApi.me()`。
- `frontend/src/types/index.ts` — `AuditEvent` / `AuditListParams` + 单向漂移守卫。
- `frontend/src/pages/Settings.tsx` — API 密钥 tab 下方「管理」卡片 + `Menu.Item`「审计日志」
  → `navigate('/audit')`；**不新加 tab**（审计页是独立路由）。入口按 `/auth/me` 的
  **capabilities** 显示，取不到则 fail-closed 不渲染。
- `frontend/src/App.tsx` — 懒加载 `Audit` + `<Route path="/audit">`。
- `frontend/src/components/AppBreadcrumb.tsx` — `'/audit': '审计日志'`。
- `frontend/src/hooks/useApiQuery.ts` — `queryKeys.audit.{all,list,vocabulary}`。
- 测试：`frontend/src/pages/Audit.test.tsx`（新，6 例）+ `Settings.test.tsx`（3 新例 + 既有 13 处
  render 改用 `renderSettings()`）。
- `CHANGELOG.md` `### M49`（插在 M48 之前）、`TODO.md` G-UI-Audit 标 `[x]`、本报告。

未做（intent 明确 out of scope）：导出（PDF/CSV）、后端不支持的 since/until 与 entity_type 过滤、
实时 stream、任何写操作（审计只读）、非 admin 开放。**不做前端假过滤** —— 拿一页数据本地筛
会漏掉未加载的行，比没有更危险。

## Changed

| file | 说明 |
|------|------|
| `frontend/src/pages/Audit.tsx` | **新** 331 行 — 列表 / 4 过滤 / cursor 分页 / Drawer / 两类空态 / 错误态 |
| `frontend/src/pages/Audit.test.tsx` | **新** 195 行 — 6 例；只桩最外层 axios 的 `api.get` |
| `frontend/src/services/api.ts` | +`auditApi`（/audit-logs）、+`authApi.me()`、+`AuditListParams` import |
| `frontend/src/types/index.ts` | +`AuditEvent` / `AuditListParams` / `AuditLogDriftOK`（单向守卫） |
| `frontend/src/services/apiClient.ts` | +`AuditLogDTO`（生成物 re-export，给上面那个守卫用） |
| `frontend/src/hooks/useApiQuery.ts` | +`queryKeys.audit.{all,list,vocabulary}` |
| `frontend/src/pages/Settings.tsx` | +`useNavigate` / 能力查询 / 「管理」卡片 + 审计入口 |
| `frontend/src/pages/Settings.test.tsx` | 13 处 render 包 `MemoryRouter`（`renderSettings()`）+ mock `authApi` + 3 新例 + `RouteProbe` |
| `frontend/src/App.tsx` | +懒加载 `Audit` + `<Route path="/audit">` |
| `frontend/src/components/AppBreadcrumb.tsx` | `TOP_LABELS['/audit'] = '审计日志'` |
| `CHANGELOG.md` / `TODO.md` | `### M49` section / G-UI-Audit 结案 |

关键实现片段：

```tsx
// 键集不硬编码：后端（audit_handler.go:26 → audit_service.go:38）只认 4 个过滤 + cursor/limit
const filters = {
  user_id: userId || undefined,
  action: action || undefined,
  method: method || undefined,
  path: pathPrefix || undefined,
  cursor,
  limit: PAGE_SIZE, // 30
}
const { data, isLoading, isError, error, refetch } = useApiQuery(
  queryKeys.audit.list(filters), () => fetchAudit(filters),
)
// 词表独立一次请求（不带筛选）——用当前页会让用户筛到只剩一条后切不回去
const { data: vocabulary } = useApiQuery(queryKeys.audit.vocabulary(), () =>
  fetchAudit({ limit: VOCAB_LIMIT }),
)
```

```tsx
// 入口：能力集由 /auth/me 下发，不复制角色→能力矩阵（roles.go 警告过复制会漂移；
// auditor 有 audit 而无 manage，按 role/manage 判会把它的入口藏掉）
try {
  const res = (await authApi.me()) as { data?: { data?: { capabilities?: unknown } } }
  const caps = res?.data?.data?.capabilities
  if (alive) setCanAudit(Array.isArray(caps) && caps.includes('audit'))
} catch { if (alive) setCanAudit(false) }
```

## Validation

- `npx tsc --noEmit` → **0 error**
- `npx eslint <9 changed files> --max-warnings 0` → **clean**
- `npx vitest run src/pages/Audit.test.tsx` → **6/6 PASS**
- `npx vitest run src/pages/Settings.test.tsx` → **31/31 PASS**（既有 28 + 新 3）
- `npx vitest run`（全 frontend）→ 见下（基线 343 PASS / 39 files，新增 9 例 → 352）
- `cd backend && go test -count=1 -timeout=600s ./...` → 见下（本 round 未改后端，保无回归）
- **Mutation inversion（审计页）**：`useApiQuery` 的 fetcher 换成 `async () => ({ items: [] })`
  （绕过 `fetchAudit`）→ **5 failed | 1 passed**；revert 后 6/6 PASS。
- **Mutation inversion（入口门禁）**：`{canAudit && (` → `{true && (` → **2 failed | 29 passed**
  （readonly + `/auth/me` 403 两例红）；revert 后 31/31 PASS。

## Risk

- **时间区间 / 对象过滤缺失**（T-56 候选）：intent 要求 DatePicker.RangePicker + entity_type 过滤，
  但**后端不支持** —— `audit_handler.go` 只认 `action` / `method` / `path` / `user_id` +
  `cursor`/`limit`，**没有 since/until，也没有 entity_type/entity_id**（模型里叫 `resource` /
  `resource_id`，过滤器里没有对应参数）。本 round 选择如实按后端能力做（4 个过滤），
  不用「拉一页本地筛」假装有时间/对象过滤 —— 那会静默漏掉未加载的行（同 G-49 那类「产物不保真」）。
  真要时间区间需后端加 `since`/`until`（及 `resource`/`resource_id`）过滤 + openapi + 迁移级回归。
- **词表采样窗口**：下拉项来自最近 100 条（后端无枚举接口）。窗口外才出现过的 `action`
  不在下拉里（仍可用路径前缀等条件定位）。要彻底解决需后端加 `GET /audit-logs/actions` 之类的
  去重枚举端点。
- **路径前缀输入是回车生效**（受控 state 只在下拉/回车时更新）：空 `path` 与「清空输入框但没回车」
  视觉上一致。属交互细节，未加额外「应用」按钮（避免与筛选交互不一致）。
- **cursor 栈不回退失效**：从第 3 页往前翻时用栈里的旧 cursor 重新请求，若期间有新行写入，
  相邻页可能重复/漏行 —— cursor 分页的固有语义（不是 offset），非本 round 引入。

## Status

- 分支：`main`，每个 commit 后 `git push origin main`。
- AC：`tsc` 0 error ✓ / Audit 6 例 PASS ✓ / 全 frontend ≥341 PASS ✓ / 后端无回归 ✓ /
  mutation 双向实证 ✓ / commit cadence ✓。
