# M56 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 23:35 CST (after M56 commit `449d935` + docs pending)
**Scope**: M56 G-UI-SearchAutofocus 搜索 Input autoFocus (2 files / +4/-0 LOC)
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- 6702 nodes / 13733 edges / 434 communities (基线 6696 / 13703 / 422)
- 0 missing / 0 dangling / 0 self_loops / 0 collapses

## CodeGraph

**`codegraph sync`**: stale 自报, 强 re-index.

## M56 影响面

- AssetFilterBar.tsx 1 prop (autoFocus)
- Audit.tsx 1 prop (autoFocus)
- 视觉/UX 层 fix, 不引入新 component / 不改 wiring / 不改 API

## 审查发现 (PM 自起, 8 项 friction → 4 项 ship + 4 项 future)

| Friction | 来源 | 真/伪 | 决策 |
|---|---|---|---|
| F-1 Settings 0 validator | 静态扫描 | 真 | 留 future (跨 backend, M60) |
| F-2 Modal Esc | 静态扫描 | **伪** | 撤回: antd 5 Modal 默认 keyboard=true |
| F-3 搜索 autoFocus | 静态扫描 | 真 | **M56 ship ✓** |
| F-4 Tickets 无空态 | 静态扫描 | 真 | 留 M57 |
| F-5 Topology 无清空筛选 | 静态扫描 | 真 | 留 M58 |
| F-6 Settings Token 留空语义 | 占位符扫描 | 真 | 留 M60 |
| F-7 Settings 集成"测试连接" | 静态扫描 | 真 | 跨 backend, omp |
| F-8 Tab URL sync | 静态扫描 | 真 | 留 M61 |
| F-9 保存按钮 loading | 静态扫描 | 真 | 留 M62 |

**学到**: 静态扫描要快速 verify (F-2 一查就撤回, 避免无效 ship)

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 | ✓ 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 29/29 (Assets 22 + Audit 6 + memo 1) |
| graphify diagnose | ✓ 0 anomalies |
| codegraph index | ✓ 强 re-index 后 |
| TODO + CHANGELOG | ✓ ship |
