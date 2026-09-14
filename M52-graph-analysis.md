# M52 双轨分析 (CodeGraph + Graphify)

**Generated**: 2026-09-14 16:14 CST (after M52 commit `997adf2` + docs `aee3ee6`)
**Scope**: M52 G-UI-AssetFilter AssetFilterBar 加 status 下拉 (3 files / +102/-5 LOC)
**Tools**: `graphify` v0.9.58 (semantic / structural), `codegraph` v1.6.0 (call graph / symbols)

## Graphify

**`graphify update . --force`**: ✓
- **6630 nodes / 13668 edges / 424 communities** (基线 6590 / 13631 / 422 + M52 增量)
- AST extraction: 108/108 uncached files (100%) [2 workers]
- 增量: M52 引入 5 个新 const (STATUS_OPTIONS + assetApi.list status line 等)

**`graphify diagnose multigraph --json`**: ✓ 边干净
- 0 missing_endpoint_edges / 0 dangling / 0 self_loops / 0 collapses
- 13668 directed unique endpoint pairs

**`graphify path AssetFilterBar ↔ AssetTable`**: **0 directed path** ✓
- 它们都是 Assets.tsx 的 sibling 子组件, 不直接耦合
- 通过 Assets.tsx 中介 (Bar 渲染在头部, Table 渲染在主体)

## CodeGraph

**`codegraph sync`**: ✓ (这次没报 stale — 3 changed files / 34 nodes / 175ms)
- 上次 M47-M51 报"Already up to date"是 sync 没识别新增 const arrow; 现在 sync 工作正常

**`codegraph node AssetFilterBar.tsx`**: ✓
- 57 lines / 3 symbols / used by **1 file** (Assets.tsx)
- 接口契约: `statusOptions?: { value, label }[]` 可选 prop (通用组件)

**`codegraph callers AssetFilterBar`**: 1 caller
- `frontend/src/pages/Assets.tsx:1` — 唯一父消费者

**`codegraph node Assets.tsx`**: ✓
- STATUS_OPTIONS 找到 (虽然它是 const arrow, 但 codegraph 这次能索引到 — 可能因为我加了详细注释或 placement)

## 结论

- **架构健康**: M52 与 M47-M51 互不污染, 0 跨 round 路径
- **影响面小**: AssetFilterBar 1 caller, 改动局部化
- **后端契约对齐**: status 字段直接下沉到 assetApi.list, 走 queryKey, 后端 `service.AssetFilter.Status` 已 ship
- **学到的 traps (落档 TODO.md + report)**:
  - T-65: antd 5 Select placeholder 不是 input attribute, 需 `.ant-select-selection-placeholder` 限定
  - T-66: 测 queryKey ≠ 测 api.list 调用 (受 mock 限制)

## 验证链完整性

| 验证维度 | 结果 |
|---|---|
| 静态扫描 (secrets / injection / eval) | ✓ M52 无新增 |
| tsc | ✓ 0 error |
| 单测覆盖 | ✓ 23/23 Assets + memo (19 老 + 3 新 + 1 memo) |
| 全 frontend vitest | ✓ 42 files / 369 tests PASS |
| backend go test | ✓ 27 packages ok (零改动) |
| mutation inversion | ✓ 2/3 FAIL (status bypass in queryKey) |
| graphify diagnose | ✓ 0 missing/dangling |
| codegraph callers | ✓ AssetFilterBar 1 caller |
| graphify path | ✓ M52 ↔ M47-M51 0 路径 |
| TODO + CHANGELOG + report | ✓ ship |
