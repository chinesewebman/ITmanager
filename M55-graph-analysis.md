# M55 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 23:30 CST (after M55 commit `79ad6fd` + docs pending)
**Scope**: M55 G-UI-AlertsStatsClick 告警统计卡可点击 (3 files / +63/-3 LOC)
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- 6676 nodes / 13703 edges / 422 communities (基线 6664 / 13698 / 417; communities -5 合并)
- 0 missing / 0 dangling / 0 self_loops / 0 collapses

## CodeGraph

**`codegraph sync`**: 同样 stale 自报 "Already up to date" (M53/M54/M55 后新 const arrow 没索引)
- **修**: `codegraph index .` 强 re-index

## M55 影响面

- AlertStatsCards.tsx 加 1 prop + 4 行 JSX 改 (Card hoverable/onClick/cursor)
- Alerts.tsx 加 useNavigate + handleCardClick + 1 行 prop 接线
- Alerts.test.tsx 加 vi.mock useNavigate + 2 新测试
- 全部 3 files, **0 跨文件 import 新增**, 改动局部化

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 | ✓ 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 31/31 (Alerts 22 + AlertCard 9) |
| mutation inversion | ✓ 1/2 FAIL (handleCardClick body bypass 被 catch) |
| graphify diagnose | ✓ 0 anomalies |
| codegraph index | ✓ 强 re-index 后 |
| TODO + CHANGELOG | ✓ ship |
