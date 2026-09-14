# M53 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 20:51 CST (after M53 commit `7ac2855` + docs `5f73b40`)
**Scope**: M53 G-UI-AlertsBulkFP 告警批量标记误报 (2 files / +82/-3 LOC)
**Tools**: `graphify` v0.9.58, `codegraph` v1.6.0

## Graphify

**`graphify update . --force`**: ✓
- **6658 nodes / 13693 edges / 422 communities** (基线 6636 / 13673 / 431)
  注: communities 数从 431 → 422 是因为 M52/M53 把相关 status / FP 节点合并
- M53 引入 `bulkFPMut` 新 const + `runBulk` kind="mark-fp" 新分支

**`graphify diagnose multigraph --json`**: ✓ 边干净
- 0 missing_endpoint_edges / 0 dangling / 0 self_loops / 0 collapses
- 13693 directed unique endpoint pairs

**`graphify path Alerts.tsx ↔ Assets.tsx`**: **0 directed path** ✓
- M53 与 M52 (Assets status) 互不污染 — 兄弟 page, 不直接耦合
- 通过 App.tsx 路由分发, 不互相 import

## CodeGraph

**`codegraph sync`**: ✓
- 2 changed files / 29 nodes / 147ms
- 上次 M52 报 stale 是 sync 缓存逻辑问题, M53 sync 工作正常

**`codegraph callers runBulk`**: 1 caller (Alerts function at line 47) ✓
- 单点复用, 不影响其他组件

**`codegraph callers markFalsePositive`**: 1 caller (Alerts function) ✓
- 跟 bulkFPMut + markFPMut 都从 Alerts.tsx 一处调

## 结论

- **架构健康**: M53 与 M47-M52 互不污染, 0 跨 round 路径
- **影响面小**: runBulk + markFalsePositive 各 1 caller, 改动局部化
- **复用模式**: M53 复用 M51 引入的 `runBulk` 抽象, 这就是**模式红利** — 同 pattern (串行循环) 不同业务 (asset retire vs alert mark-fp), 改动只扩 kind 类型 + 加 mutation
- **后端契约对齐**: `alertApi.markFalsePositive(id, true, "运维批量标记")` 单条调用, 后端 `POST /alerts/:id/mark-fp` 已有, 零改动

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 (secrets / injection / eval) | ✓ M53 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 20/20 Alerts.test.tsx (17 老 + 3 新) |
| 全 frontend vitest | ✓ **42 files / 372 tests PASS** (基线 369 + 3 M53) |
| backend go test | ✓ 27 packages ok (零改动) |
| mutation inversion | ✓ 1/3 FAIL (onConfirm bypass) |
| graphify diagnose | ✓ 0 missing/dangling |
| codegraph callers | ✓ runBulk 1 caller / markFalsePositive 1 caller |
| graphify path | ✓ M53 ↔ M47-M52 0 路径 |
| TODO + CHANGELOG + report | ✓ ship |
