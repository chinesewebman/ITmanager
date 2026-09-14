# intent-M52: G-UI-AssetFilter AssetFilterBar 加 status 下拉 (B3 摩擦表)

## Context

T99 PM-direct 摩擦表 B3: `AssetFilterBar` 当前只有 **keyword + assetType** 两字段. 后端 `AssetFilter` 已支持 `status` (`active / offline / maintenance / retired`),
但前端无 status 筛选入口 — 用户想"看哪些资产 offline"必须滚页肉眼找.

Backend 实证 (verified):
- `service.AssetFilter.Status` (`backend/internal/service/asset_service.go:33`)
- handler `c.Query("status")` (`asset_handler.go:47`)
- 后端 Status 值域 `active / offline / maintenance / retired` (`models/asset.go:35` + `frontend/src/components/StatusTag.tsx:36-39`)

## 任务

### 1. `frontend/src/components/AssetFilterBar.tsx`

- `AssetFilterValues` 接口加 `status: string`:
  ```ts
  export interface AssetFilterValues {
    keyword: string
    assetType: string
    status: string  // 新加
  }
  ```
- 新增 `statusOptions` (4 项, 用 `StatusTag` 同样的中文 label):
  ```ts
  const STATUS_OPTIONS = [
    { value: 'active',       label: '在线' },
    { value: 'offline',      label: '离线' },
    { value: 'maintenance',  label: '维护' },
    { value: 'retired',      label: '已退役' },
  ]
  ```
  注: AssetFilterBar 是受控通用组件, statusOptions 在父组件传 (跟 typeOptions 同模式), 不在 Bar 内 hardcode.
- 渲染加 `<Select allowClear placeholder="状态" ... />` (跟 type 那个 Select 同形态)
- 自清理: keyword/type/status 任一 clear 时 `onChange({ ...value, [field]: '' })` (已 ship 模式)

### 2. `frontend/src/pages/Assets.tsx`

- `filter` state 加 `status: ''`:
  ```ts
  const [filter, setFilter] = useState({ keyword: '', assetType: '', status: '' })
  ```
- `assetApi.list({ keyword, type, status })` 调用传 status
- `<AssetFilterBar>` 加 `statusOptions={STATUS_OPTIONS}` prop (定义在本页内, 跟 TYPE_OPTIONS 同位置)

### 3. `frontend/src/pages/Assets.test.tsx`

- 新加 3 测试:
  1. AssetFilterBar 渲染包含"状态"placeholder
  2. 选 status='active' → `assetApi.list` 的 query params 含 status='active'
  3. 状态变更后副标题/列表刷新触发 queryKey 更新 (跟 type 字段同模式, 翻页重置回 1)

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Assets.test.tsx src/components/AssetTable.memo.test.tsx` → ≥22 PASS (19 旧 + 3 新), 0 FAIL
- 全 frontend `npx vitest run` → ≥362 PASS (基线 359 + 3 新), 0 FAIL
- backend `cd ../backend && go test -count=1 -timeout=600s ./...` → 27 packages ok (零改动)
- mutation inversion: 临时把 onChange 路径改成只 `return`, expect 新增 3 测试中至少 1 FAIL
- 双轨分析:
  - `graphify update . --force` 增量 re-index OK (新 graph 节点包含 status 字段, status label const)
  - `graphify diagnose multigraph --json` 0 missing/dangling/self_loops
  - `codegraph sync` (or `codegraph index .` 强 re-index 如 stale)
  - `codegraph callers AssetFilterBar` 验影响面 (预期仅 Assets.tsx + Assets.test.tsx + Assets.mobile.test.tsx)
  - `graphify path AssetFilterBar AssetTable` 验 wiring 真连上

## Commit cadence

5 commits, each pushed to origin/main:
1. intent-M52.md (本文档)
2. feat(M52): AssetFilterBar + status 下拉 + AssetFilterValues 加字段
3. test(M52): Assets 3 用例 (状态渲染 + status 入参 + queryKey)
4. docs(M52): CHANGELOG + report + TODO G-UI-AssetFilter done

(可选 5. mutation 实证 commit — 如果发现活缺陷, 跟 fix 一起; 否则 inline 即可)

## Don't

- 不要改后端 (M52 frontend-only)
- 不要在 AssetFilterBar hardcode statusOptions (AssetFilterBar 是通用组件, 应接受父传)
- 不要动 status 字段在后端的值域 (4 个值是后端契约, 不要扩/缩)
- 不要给 status 加模糊匹配 (后端 `c.Query("status")` 严格等值, 跟 type 字段同模式)
- 不要新加 antd 依赖 (Select 已 ship)

## Out of scope (留 future round)

- 机房 (site_id) 筛选 — 后端 `AssetFilter` 无此字段, 加需先 backend round
- 机柜 (rack_name) 筛选 — 同上
- 时间范围 (created_at range) — 后端无
- 保存筛选为视图 — 留 round future

## Risk

- **search box 太长**: 加 status 后 AssetFilterBar 3 Select + 1 Input, 移动端 (xs) 会拥挤. 验证: `useResponsiveTable` 已 ship, 移动端走 MobileCardList, AssetFilterBar 在移动端不渲染 (`isMobile ? MobileCardList : ...`). 不阻塞.
- **后端 status 实际接受什么字符串**: 已 grep 实证 active/offline/maintenance/retired. 但 `service.AssetFilter.Status` 直接透传, 拼到 SQL WHERE 子句. 不规范值 (e.g. 'unknown') → 返回空结果 (后端不报 400). 可接受.

## graph-tools verified (M52 起每 round 必跑)

执行时间: 2026-09-14 14:XX
- graphify update: 6605 nodes / 13645 edges / 432 communities
- graphify diagnose: 0 missing / 0 dangling / 0 self_loops
- codegraph sync: up to date (status: 309 files / 6243 nodes)
