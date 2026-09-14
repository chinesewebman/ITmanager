# M57 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 23:55 CST (after M57 feat `a7f51f6` + docs pending)
**Scope**: M57 G-UI-TabUrlSync (4 files / +139/-5 LOC)
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- 6720 nodes / 13768 edges / 440 communities (基线 6702 / 13733 / 434)
- 0 missing / 0 dangling / 0 self_loops / 0 collapses

## CodeGraph

**`codegraph sync`**: stale 自报, 强 re-index.

## M57 影响面 (useSearchParams 受控 wiring)

| 组件 | 受控 prop | URL key | default |
|---|---|---|---|
| Settings `<Tabs>` | activeKey | `tab` | `integrations` |
| Oncall `<Tabs>` | activeKey | `tab` | `current` |

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 | ✓ 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 59/59 (Settings 35 + Oncall 24) |
| mutation inversion | ✓ 实证 (bypass setSearchParams → 39 failed) |
| graphify diagnose | ✓ 0 anomalies |
| codegraph index | ✓ 强 re-index 后 |
| TODO + CHANGELOG | ✓ ship |

## 关键测试 (RouteProbe / ProbeRoute)

`useLocation()` 在 `setSearchParams` 触发时会 re-render, 把 `pathname + search` 暴露成 `data-testid="current-path"` / `data-testid="oncall-path"`, 让 setSearchParams 渲染时机可断言.

## 8 项 friction 终态

| Friction | 终态 | 来源 |
|---|---|---|
| F-1 Settings 0 validator | 留 future (跨 backend) | static scan |
| F-2 Modal Esc | **撤回 (antd 5 keyboard=true)** | static scan |
| F-3 搜索 autoFocus | **M56 ship** | static scan |
| F-4 Tickets 无空态 | **撤回 (已有 EmptyState)** | static scan |
| F-5 Topology 无清空筛选 | **撤回 (只有 Switch)** | static scan |
| F-6 Settings Token 留空语义 | 留 future | static scan |
| F-7 Settings "测试连接" | 留 future (跨 backend, omp) | static scan |
| F-8 Tab URL sync | **M57 ship** | static scan |
| F-9 保存按钮 loading | 留 future | static scan |
