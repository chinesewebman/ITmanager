# intent-M56: G-UI-SearchAutofocus 搜索 Input autoFocus (F-3 摩擦)

## Context

审查发现: 全站搜索 Input 不 autoFocus, 用户进页面想搜必须先鼠标点 input 才能键盘打字. 这是键盘流摩擦.

实际只有 2 处真搜索 Input (其他页 Select 筛选无 Input):
- `AssetFilterBar.tsx` — "搜索名称 / IP"
- `Audit.tsx` — "路径前缀, 如 /api/assets"

## 任务

### 1. `frontend/src/components/AssetFilterBar.tsx`

- 搜索 Input 加 `autoFocus`:
  ```tsx
  <Input
    allowClear
    autoFocus  // M56: 键盘流不用先点 input
    prefix={<SearchOutlined />}
    placeholder="搜索名称 / IP"
    ...
  />
  ```

### 2. `frontend/src/pages/Audit.tsx`

- 路径前缀 Input 加 `autoFocus`:
  ```tsx
  <Input
    placeholder="路径前缀，如 /api/assets（回车）"
    allowClear
    autoFocus  // M56
    defaultValue={pathPrefix}
    ...
  />
  ```

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Assets.test.tsx src/components/AssetFilterBar.test.* src/pages/Audit.test.tsx`: 现有 PASS 不退化
- mutation inversion: bypass `autoFocus` → expect 当前测试不 catch (jsdom 不渲染 input focus 状态) — 视觉/UX 层, 接受
- 双轨分析 (graphify + codegraph)

## Commit cadence

2-3 commits:
1. intent-M56.md
2. feat(M56): AssetFilterBar + Audit 加 autoFocus
3. docs(M56): CHANGELOG + TODO + 双轨

## Don't

- 不要给 Select 筛选加 autoFocus (Select 不支持)
- 不要给 Modal 内表单 Input 加 autoFocus (模态聚焦习惯: 用户先看 modal 标题)
- 不要改其他 input 行为 (e.g. 创建资产时 input 不 autoFocus 是对的)

## Risk

- **anti-pattern**: autoFocus 在某些场景 (modal 弹出 / 弹窗打开) 是 anti-pattern. 这里我们是**页面初始 mount** 时 autoFocus 搜索框, 不是 modal 内
- **可访问性**: 屏幕阅读器用户可能不喜欢 autoFocus, 但搜索框是用户进页面的常见第一动作, 接受

## graph-tools verified (M56 起每 round 必跑)

执行时间: 2026-09-14 23:XX
- graphify update
- graphify diagnose (0 missing/dangling)
- codegraph sync (stale → 强 re-index)
