# M54 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 23:18 CST (after M54 commit `709c6b7` + docs pending)
**Scope**: M54 G-UI-AlertsHostHover 主机列 ellipsis (1 file / +2/-1 LOC)
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- **6664 nodes / 13698 edges / 417 communities**
- 0 missing / 0 dangling / 0 self_loops / 0 collapses

## CodeGraph

**`codegraph sync`**: ⚠️ stale ("Already up to date" 但 M53 后新 const 没索引)
- **修**: `codegraph index .` 强 re-index → **6244 nodes / 15218 edges / 309 files**

**Stale catch**: M54 起每 round 起手必跑双轨, sync 自报 stale 时强制 re-index (skill standing convention 已就位)

## M54 影响面

- AlertTable.tsx 1 file / 1 column / 1 prop (`ellipsis: { showTitle: true }`)
- 不引入新 component / 不改 sorter / 不改 width
- 视觉层 fix, 单测无能力 catch (mutation inversion 29/29 PASS)

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 | ✓ 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 29/29 (Alerts 20 + AlertCard 9, 无退化) |
| mutation inversion | ✓ 诚实承认: 29/29 PASS (视觉层 fix) |
| graphify diagnose | ✓ 0 anomalies |
| codegraph index | ✓ 6244 nodes (强 re-index 后) |
| TODO + CHANGELOG | ✓ ship |
