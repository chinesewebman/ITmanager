# intent-M55: G-UI-AlertsStatsClick 告警统计卡可点击跳转 (C1 摩擦表)

## Context

T99 PM-direct 摩擦表 C1: AlertStatsCards 4 联 (`总告警 / 未处理 / 已确认 / 已解决`) 只显示数字,
不可点击. 用户想"看所有未处理告警"必须手填 status filter.

## 任务

### 1. `frontend/src/components/AlertStatsCards.tsx`

- 加 `onCardClick?: (key: keyof AlertStats) => void` 可选 prop
- Card 加 hover + cursor pointer (onCardClick 存在时):
  ```tsx
  <Card
    loading={loading}
    hoverable={!!onCardClick}  // antd hover 效果
    onClick={onCardClick ? () => onCardClick(c.key) : undefined}
    style={{ cursor: onCardClick ? 'pointer' : 'default' }}
  >
  ```
- 不传 onCardClick 时 Card 行为不变 (向后兼容)

### 2. `frontend/src/pages/Alerts.tsx`

- 加 useNavigate hook (react-router-dom 已 ship)
- 接 onCardClick → navigate('/alerts?status=problem' 等):
  ```ts
  const navigate = useNavigate()
  const handleCardClick = useCallback((key: keyof AlertStats) => {
    if (key === 'total') return  // 总告警不跳 (没意义)
    navigate(`/alerts?status=${key}`)
  }, [navigate])
  ```
- AlertStatsCards 加 `onCardClick={handleCardClick}` prop
- 注: 'problem' / 'acknowledged' / 'resolved' 跟后端契约对齐 (查询参数 status 值域)

### 3. Alerts 页 status filter URL sync (顺手 ship)

- 当前 `useState` 持有 status filter, 跟 URL 无关
- 改为: `const [status, setStatus] = useState(searchParams.get('status') ?? '')`
- 这样 handleCardClick 跳 `/alerts?status=problem` 后, filter 直接生效

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Alerts.test.tsx src/components/AlertCard.test.tsx`: 现有 29 PASS 不退化
- mutation inversion: bypass `onCardClick` 处理 → expect 当前测试不 catch (跟 M54 同, 视觉/交互层)
- 双轨分析 (graphify + codegraph)

## Commit cadence

3 commits:
1. intent-M55.md (本文档)
2. feat(M55): AlertStatsCards onCardClick + Alerts useNavigate + URL sync
3. docs(M55): CHANGELOG + TODO + 双轨

## Don't

- 不要改 CARDS 顺序 (total / problem / acknowledged / resolved 固定)
- 不要给 total 加 onClick (没意义, 跳 '/alerts' 等于无操作)
- 不要重写 useState → useReducer (单 status filter 不必要)
- 不要改后端 (前端路由 + filter 已 ship)

## Risk

- **URL sync useEffect 副作用**: 简单 searchParams.get, 不引入 effect (initial state 已读)
- **测试覆盖**: useNavigate mock 需要 wrap in MemoryRouter (Alerts 页已 ship 测试 wrap 方式)
- **向后兼容**: AlertStatsCards 不传 onCardClick 行为不变 (测试套不复用, 但 Dashboard 暂未用此组件, 安全)

## graph-tools verified (M55 起每 round 必跑)

执行时间: 2026-09-14 23:XX
- graphify update
- graphify diagnose (0 missing/dangling)
- codegraph sync (stale → 强 re-index)
