# M47 Completion Report — G-UI-Breadcrumb 详情面包屑加资产名 / 工单标题

## Delivered

PM-direct 自查 (`12-优化建议` 心态, 8 位 hex ID 看着猜不出是谁):

**改:**
- `frontend/src/components/AppBreadcrumb.tsx`:
  - `useDetailLabel()` — `useApiQuery` 拉 `assetApi.get(id)` 或 `ticketApi.get(id)`, 命中后显示 `asset.name` / `ticket.title`. fetch 失败 / loading 中 fallback 到原来的 `ID: a1b2c3d4...`.
  - `fallbackIdLabel()` — 老行为封装成一个函数, 等同 M47 ship 前的 `slice(0, 8)`.
  - `isDetailTop()` — 限定 `/assets` / `/tickets` 才 fetch. 其他详情页 (alert-suppressions 等) 仍保留 ID 截断, 不扩散 scope.
  - `topMatch` 提到 `useMemo` 顶层, 避免同时触发 `topMatch` 计算 + `useDetailLabel` 的反复 re-render.

**测试** (`frontend/src/components/AppBreadcrumb.test.tsx`, 9/9 PASS):
- 老 5 个全保: 首页不显示 / 二级 / 三级 fallback / 告警中心 / 404.
- M47 新 4 个:
  - 资产详情 fetch 命中 `switch-core-01` 后, `ID: a1b2c3d4...` 不再出现.
  - 工单详情 fetch 命中 `交换机端口告警` 后, `ID: t1-id` 不再出现.
  - fetch 失败 fallback (assetApi.get 抛错 → 仍 `ID: abcd1234`).
  - 非 detail 范围: `/alert-suppressions/some-id` 仍走 fallback (范围不扩散).

## Changed files

- `intent-M47.md` (新)
- `frontend/src/components/AppBreadcrumb.tsx` (97 → 145 行, +48)
- `frontend/src/components/AppBreadcrumb.test.tsx` (50 → 116 行, +66, 5 → 9 测试)
- `TODO.md` (G-UI-Breadcrumb `[ ]` → `[x]`)
- `CHANGELOG.md` (M47 section inserted before M46)

## Validation

### 单测
- `vitest run src/components/AppBreadcrumb.test.tsx` → **9/9 PASS**
- `vitest run` 全 frontend → **39 文件 + 338 测试全 PASS**

### tsc
- `tsc --noEmit` (用项目 tsconfig, 非默认) → **0 错误**

### Mutation inversion
- 把 `detailLabel = useDetailLabel(...)` 替换为 `detailLabel = undefined` (mock fetch 拿到 data 但 detailLabel 永远 undefined)
- 实测: 2 用例 FAIL (`资产详情 fetch 命中` + `工单详情 fetch 命中`)
- 剩下 7 PASS (老 5 + fetch 失败 fallback × 2 — 因为失败场景本来就该 fallback, 不需要 detailLabel 实际值)
- revert → 9/9 PASS

## Process retro

### 教训 1: vitest setup 不包 QueryClientProvider 时, 新加 useQuery 静默炸全部老测试

`useApiQuery` 包 `useQuery` (react-query), 没 `QueryClientProvider` 就 `No QueryClient set`. 老 AppBreadcrumb 测试不带 provider, 加 detail fetch 后 5 个老测试全 FAIL (红了看似我代码改错的"回归").

**正确修法**: 每个测试 file 自带 QueryClient wrapper (跟 CommandPalette 一致). **不应**改 setup.ts 全局包 — 会让所有组件测试都被 QueryClient 影响, 而现有不少组件不依赖 react-query, 加 provider 是 no-op (silent ok) 但会让某些用了 store 的组件 cross-talk.

### 教训 2: 详情 fetch mock 写错会让 react-query 静默 loading 永久

第一版 mock 写 `vi.spyOn(assetApi, 'get').mockResolvedValueOnce({ data: { name: '...' } })` 没包装 `.data.data` 双层. react-query 拿到的 `data` 是 `{ data: { name: '...' } }` —— 用 `r.data.data.name` 取出来是 undefined. UI 显示 fallback ID. 测试看起来 PASS (因为 `queryByText(/ID: .../)` 还在, 不需要 name).

**正确 mock** 包齐两层: `{ data: { data: { id: 'a1', name: 'switch-core-01' } } }`. 然后 `queryByText(/ID: /)` 断言**不再出现**, 才能 catch fetch 失败 fallback.

### 教训 3: useMemo 复用 topMatch 避免双重 re-render

useMemo (topMatch) + useApiQuery (useDetailLabel) 如果都在 render 里, 每次 render 都跑一次匹配. 提到 useMemo 后二者互相依赖少, 但 React 还是会重跑 `useDetailLabel` 的 key 计算 (因为 `params.id` 变了). 不会无限循环, 但值得一记.

## Risk & 残余

### 已 ship
- assets / tickets 详情页显示 name / title (走 react-query cache, 60s staleTime)
- 失败 / loading fallback 到 ID 截 8 位 (老行为, 100% 兼容)
- 非 /assets /tickets 详情页保持 ID 截断 (范围不扩散)
- 9/9 测试 + 338 测试 + tsc 全 PASS + mutation 实证

### 未 ship (明确不在 scope, 留 backlog)
- **CI 实际跑 frontend vitest**: `package.json` 没 `test` script, 加 CI step 另立 round. 项目现靠手跑 `npx vitest run` + `npx tsc --noEmit`. M47 ship 时用本地跑过, 不算 automerge 闸.
- **面包屑过长截断**: name 过长 (中文 50+ 字) 会让面包屑一行溢出. 加 ellipsis 是 ≤30min 改动, 但本轮 ship 不包.
- **name 显示加 icon/color**: 例如资产名前加 `<ServerOutlined />`, 工单号前加 `<ToolOutlined />`. UI 美化是 PM-direct 自查时计划 v2 才做.
- **alert-suppressions / metric-snapshots / racks 详情也 fetch**: 范围扩大 (~每种 +1 个 API +1 测 +1 docs), 单 backend change + 1 frontend file, ≤2h PM-direct. 留到 P1-2 backlog.

## Status

- Scope: PM-direct ≤1h, 实证
- Author: hermes@local
- Branch: main
- 3 commits: `b35cf17` / `e0de2ce` / (待 docs)
- All pushed ✓
- **Poison "开动马力" 后自主起 M47 = G-UI-Breadcrumb** (queue 顶 G-UI-Breadcrumb, 1h PM-direct)

## 下一步

PM-direct queue 仍剩:
- **G-UI-TopoClick** (≤2h): 修拓扑节点 `cursor: pointer` 但无 onClick 的视觉欺骗
- **G-UI-Audit** (≤4h): audit 日志页 (backend 已 ship M22, 但前端无 ui)
- **G-UI-Tickets** (≤4h): TicketDetailModal 加操作按钮 + TicketTable 行内快捷 + updated_at 列

按"先小后大"原则, **M47 收尾后下 round = G-UI-TopoClick**.
