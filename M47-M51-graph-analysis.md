# M47-M51 Codegraph + Graphify 双轨分析报告

**Generated**: 2026-09-14 (after Poison 提醒补做)
**Scope**: 17 commits / 34 files / ~3010 LOC
**Tools**: `codegraph` v1.6.0, `graphify` v0.9.58

## 1. Graphify (semantic / structural graph)

### Index state (after `graphify update . --force`)

- **6590 nodes · 13631 edges · 422 communities**
- Built from commit: `372936a`
- Extraction: 92% EXTRACTED · 8% INFERRED · 0% AMBIGUOUS
- INFERRED: 1061 edges (avg confidence 0.85)

### `graphify diagnose multigraph --json` — 边干净

```
node_count: 6590
missing_endpoint_edges: 0
dangling_endpoint_edges: 0
self_loop_edges: 0
exact_duplicate_edges: 0
directed_same_endpoint_collapsed_edges: 0
valid_candidate_edges: 13631
```

**结论**: 0 missing / 0 dangling / 0 self_loops / 0 collapses — M47-M51 没污染图.

### M47-M51 涉及文件 community hubs (per GRAPH_REPORT.md)

| File | Nodes (matched) | Community Hub? |
|---|---|---|
| `frontend/src/pages/Assets.tsx` | 33 | ✓ |
| `frontend/src/pages/Audit.tsx` | 34 | ✓ |
| `frontend/src/pages/Topology.tsx` | 14 | ✓ |
| `frontend/src/components/AppBreadcrumb.tsx` | 15 | ✓ |
| `frontend/src/components/TicketDetailModal.tsx` | 4 | ✓ |
| `frontend/src/components/TicketTable.tsx` | 4 | ✓ |

全部进入 community hubs.

### Cross-module coupling (via `graphify path`)

```
graphify path Assets.tsx Audit.tsx          → No directed path ✓
graphify path TicketDetailModal.tsx Assets.tsx → No directed path ✓
```

**结论**: 5 个 G-UI-* round 互不污染, 架构健康.

## 2. CodeGraph (call graph / symbols)

### Index state (after `codegraph index .` 强 re-index)

- **309 files · 6243 nodes · 15207 edges in 1.2s**
- 注: `codegraph sync` 自报 "Already up to date" 但实际新增 symbols 找不到, 需要强 `codegraph index .`

### Cross-component coupling (via `codegraph callers`)

**`useApiMutation` callers** (8 total, 跨 4 components):
- `TicketDetailModal.tsx:97`
- `Alerts.tsx:46`
- `Assets.tsx:26`
- `Tickets.tsx:47`

→ M51 fix 只改 `Assets.tsx` 内部 button disable 逻辑, **零外部 caller 影响** ✓

**`useApiQuery` callers** (20 total, 跨 9+ components):
- `useDetailLabel` (AppBreadcrumb, M47)
- `CommandPalette`, `TicketDetailModal`, `TicketHistoryTimeline`
- `AlertSuppressions`, `Alerts`, `AssetTimeline`, `Assets`, `Audit`
- (plus tests)

→ M47/M49/M50/M51 都用同一 hook, **无耦合破坏** ✓

### Tool 限制 (新发现)

**CodeGraph v1.6.0 不索引 const arrow functions**, 例如 `const bulkRetireMut = useApiMutation(...)`. Query `codegraph query bulkRetireMut` 找不到, 但 `codegraph node Assets.tsx` 显示 line numbers OK (line 243).

老 bulk 函数 (function 声明式 `function bulkDelete`) 能 query 到.

这不是 M47-M51 的问题, 是 tool 设计. **修法**: query 时用 file 名 + grep, 或重命名 const 为 function declaration.

## 3. T-64 实证 (M51 mutation inversion 真证时发现的活缺陷)

`useApiMutation` mock `() => ({ mutate: vi.fn(), mutateAsync: vi.fn() })` — `mutate` 是 noop, 真 mutation 路径不触发. 这是 M51 ship 时**未验出**的 trap.

**M51-2 fix (commit `a74e081`) 已 ship**: 让 `useApiMutation` mock 走真 mutator:

```ts
useApiMutation: <TVars, TResult>(
  mutator: (vars: TVars) => Promise<TResult>,
) => {
  return {
    mutate: (vars: TVars) => { void mutator(vars) },
    mutateAsync: (vars: TVars) => mutator(vars),
    // ...
  } as any
}
```

实证: bypass `onConfirm={() => { /*MUT*/} }` → 真 mutation 测试 FAIL (1/19), revert → 19/19 PASS ✓

## 4. 修正 (M52 起执行)

**每 round 必跑双轨 (5 步, 总耗时 ~15s)**:

1. `graphify update . --force` (10s)
2. `graphify diagnose multigraph --json` 验 0 missing/dangling (1s)
3. `codegraph sync` (3-5s); 如报 stale → `codegraph index .` 强 re-index (1.2s)
4. `codegraph callers <new_func_or_hook>` 验影响面
5. Round intent.md 末尾加 "graph-tools verified" 一行 (含 timestamp + node count)

## 学到

PM-direct 不代表 "PM 自己写就不需要 graph tools" — graph 是 PM-direct 的眼睛:

- 验新 wiring 是否真连上 (`graphify path`)
- 验 caller 影响面 (`codegraph callers`)
- 验边干净 (`graphify diagnose`)
- 验活缺陷 (M51 mutation inversion 实证靠 graph context 定位真 mock 漏洞)

**无 graph 实证 = 等同"写完代码没跑测试"** — 是 standing convention, 不是 nice-to-have.

## Retro 笔记

`~/.hermes/cache/notes/codegraph-graphify-retro-M47-M51.md` (2647 bytes)
PM_QUEUE history entry `M47-M51-RETRO` 已加.
