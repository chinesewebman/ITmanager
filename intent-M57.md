# intent-M57: G-UI-TabUrlSync Tab 切换 URL 同步 (F-8 摩擦)

## Context

审查发现: Settings 3 tabs (integrations / notifications / api) 和 Oncall 3 tabs (当前值班 / 值班组 / 升级策略) 都没 URL sync, 用户:
- 刷新页面 → 丢失 tab 状态, 回到默认 (integrations / 当前值班)
- 分享 URL → 同事打开看到默认 tab, 不是他/她想看的
- 浏览器后退 → 跳走整个 Settings 页面, 而不是回到上一 tab

## 任务

### 1. `frontend/src/pages/Settings.tsx`

- 引入 `useSearchParams` from `react-router-dom`
- 引入 `useMemo` (如未引入)
- 把 `<Tabs items={tabItems} />` 改成受控, activeKey 来自 URL:
  ```tsx
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = searchParams.get('tab') || 'integrations'
  const handleTabChange = (key: string) => {
    setSearchParams({ tab: key })
  }
  <Tabs activeKey={activeTab} onChange={handleTabChange} items={tabItems} />
  ```

### 2. `frontend/src/pages/Oncall.tsx`

- 引入 `useSearchParams`
- Tabs 改成受控:
  ```tsx
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = searchParams.get('tab') || 'current'
  const handleTabChange = (key: string) => {
    setSearchParams({ tab: key })
  }
  <Tabs activeKey={activeTab} onChange={handleTabChange} items={[...]} />
  ```

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Settings.test.tsx src/pages/Oncall.test.tsx`: 现有 PASS 不退化
- mutation inversion: bypass `setSearchParams({ tab: key })` → expect setSearchParams catch 不到 (jsdom URL 行为), 接受 mutation inversion 诚实承认走 e2e (人工点击 tab 看 URL 变化)
- 双轨分析 (graphify + codegraph)

## Commit cadence

3 commits:
1. intent-M57.md
2. feat(M57): Settings + Oncall Tab URL sync
3. docs(M57): CHANGELOG + TODO + 双轨

## Don't

- 不要改其他 tabs (Audit 没 Tabs, Tickets 没 Tabs, Runbook 没 Tabs)
- 不要给 Oncall 嵌套 CurrentTab / SchedulesTab / PoliciesTab 加 URL sync (它们是 content 不是 tab)
- 不要写多余 router state (e.g. nested routes for tabs), 用 query string 最简

## Risk

- `useSearchParams` 在嵌套 Router 下行为不同. 检查 MemoryRouter 是否包 Oncall / Settings 测试 (M55 T-69 vi.mock useNavigate 已 ship, 类似)
- `setSearchParams` 在首次 render 时 setState 会触发 re-render, 测试可能警告 act(). 用 vi.mock useSearchParams 即可
