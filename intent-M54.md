# intent-M54: G-UI-AlertsHostHover 主机名列加 Tooltip (E2 摩擦表)

## Context

T99 PM-direct 摩擦表 E2: AlertTable 主机列 `width: 150` 但 host 字段无长度限制.
长主机名 (e.g. `web-server-01.prod.iad1.example.com` = 38 chars) 在 150px 列宽下:
- 默认行为: 强制换行撑高整行 → 表格行高不一致, 影响扫读
- 或溢出影响操作列布局

## 任务

### `frontend/src/components/AlertTable.tsx`

- 主机列加 `ellipsis: { showTitle: true }` (antd Table 自带 Ellipsis + Tooltip):
  ```ts
  {
    title: "主机",
    dataIndex: "host",
    key: "host",
    width: 150,
    ellipsis: { showTitle: true },  // E2: 长主机名 hover 显示完整
    sorter: (a, b) => a.host.localeCompare(b.host),
  }
  ```
- `showTitle: true` 让 antd Table 自动在 host 文本节点上挂 `title={host}`, 浏览器原生 tooltip + antd Ellipsis 截断

### `frontend/src/components/AlertCard.test.tsx` (mobile card)

- 不用改 (mobile card 用 `getAlertActions` 同源, 不渲染 host 列)

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/components/AlertTable.test.tsx src/pages/Alerts.test.tsx src/components/AlertCard.test.tsx` → 现有 28 PASS 不退化
- 全 frontend `npx vitest run` → ≥372 PASS
- mutation inversion: bypass `ellipsis:` → expect 没 fail (因为不破坏现有断言), 但**视觉效应** 需 antd 渲染层验
- 双轨分析: graphify update + diagnose + codegraph sync

## Commit cadence

2-3 commits:
1. intent-M54.md (本文档)
2. feat(M54): AlertTable 主机列 ellipsis
3. docs(M54): CHANGELOG + TODO + 双轨分析

## Don't

- 不要改 Alerts.tsx (页面层)
- 不要把 width 加大 (150 是设计选择, 不动)
- 不要用自定义 Tooltip (antd Ellipsis 自带)
- 不要改 host sorter 逻辑

## Risk

- **showTitle 兼容性**: antd 5 Table ellipsis 支持 `showTitle: true` 自 v5.0+. 项目用 antd 5.x ✓
- **mobile card (AlertCard.tsx)**: 不渲染 host 列 (操作按钮优先), 不影响
- **getAlertActions 抽出的 actions**: 已 ship M53, 不动

## graph-tools verified (M54 起每 round 必跑)

执行时间: 2026-09-14 21:XX
- graphify update: 6664 nodes / 13698 edges / 417 communities (基线 6658 / 13693 / 422; communities -5 是合并)
- graphify diagnose: 0 missing / 0 dangling / 0 self_loops
- codegraph sync: stale ("Already up to date"), 用 `codegraph index .` 强 re-index (6244 nodes / 15218 edges)
