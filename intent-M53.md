# intent-M53: G-UI-AlertsBulkFP 告警页加批量标记误报 (C2 摩擦表)

## Context

T99 PM-direct 摩擦表 C2: Alerts 页 header 只有 `[批量确认] [批量解决]` (有 selection 时), 没有 `[批量标记误报]`.
运维收到一条 webhook 风暴 (50+ 假阳告警) 时只能一行一行点 `[标记误报]` × 50 次.

跟 M51 G-UI-BulkAssets 同样的"批量操作缺位"问题. Backend 已有 `POST /alerts/:id/mark-fp` (单条),
**无 bulk endpoint** (跟 M51 bulk_retire 同样的"前端串行循环"模式).

## 任务

### 1. `frontend/src/pages/Alerts.tsx`

- 加 `bulkFPMut` (跟 `bulkAckMut` / `bulkResolveMut` 同模式, 复用 `runBulk`):
  ```ts
  const bulkFPMut = useApiMutation(
    (ids: string[]) => runBulk(ids, "mark-fp", (id) =>
      alertApi.markFalsePositive(id, true, "运维批量标记")),
    {
      onError: () => {
        setBulkProgress(null)
        message.error("批量标记误报失败")
      },
    },
  )
  ```
- header `<Space>` 里加 `[批量标记误报]` 按钮 (在 `[批量解决]` 之后), 条件 `hasSelection`:
  ```tsx
  <Popconfirm
    title={`批量标记已选的 ${selectedIds.length} 条为误报？`}
    okText="标记误报"
    cancelText="取消"
    okButtonProps={{ danger: true }}
    onConfirm={() => bulkFPMut.mutate(selectedIds)}
  >
    <Button
      icon={<StopOutlined />}
      loading={bulkFPMut.isPending}
      disabled={bulkAckMut.isPending || bulkResolveMut.isPending || bulkFPMut.isPending}
    >
      批量标记误报
    </Button>
  </Popconfirm>
  ```
- 注: `StopOutlined` 可能未 import, 看现有 antd icons 已有再决定. 备选 `CloseCircleOutlined` / `WarningOutlined`.

### 2. `frontend/src/pages/Alerts.test.tsx`

新加 3 测试 (跟 M51 pattern 同, 但测试 Alerts 自己的 onMarkFP 接线):

1. **M53 渲染**: 选中 N 项 → `[批量标记误报]` button 出现
2. **M53 触发**: 点 `[批量标记误报]` → Popconfirm → `alertApi.markFalsePositive` 被调 N 次 (mock spy), reason="运维批量标记"
3. **M53 race**: 任何其他 bulk 操作 in-flight 时 `[批量标记误报]` 按钮 disabled (防 race condition)

## Hard pass criteria

- `npx tsc --noEmit` 0 error
- `npx vitest run src/pages/Alerts.test.tsx` → ≥20 PASS (17 老 + 3 新)
- 全 frontend `npx vitest run` → ≥372 PASS (基线 369 + 3 新), 0 FAIL
- backend `cd ../backend && go test -count=1 -timeout=600s ./...` → 27 packages ok (零改动)
- mutation inversion: 临时把 onConfirm 改成空箭头 → expect 至少 1 FAIL
- 双轨分析:
  - `graphify update . --force` 增量 re-index OK
  - `graphify diagnose multigraph --json` 0 missing/dangling/self_loops
  - `codegraph sync` (or `codegraph index .` 强 re-index 如 stale)
  - `codegraph callers bulkFPMut` 或 `markFalsePositive` 验影响面
  - `graphify path Alerts.tsx AssetTable.tsx` 验 M53 互不污染

## Commit cadence

5 commits, each pushed:
1. intent-M53.md (本文档)
2. feat(M53): bulkFPMut + `[批量标记误报]` 按钮
3. test(M53): Alerts 3 用例 (渲染 + 触发 + race)
4. docs(M53): CHANGELOG + report + TODO + 双轨分析

## Don't

- 不要改后端 (M53 frontend-only)
- 不要新加 antd dependency (Button / Popconfirm / Space / icons 已 ship)
- 不要把 bulkFPMut 的 note 写成动态 (统一"运维批量标记", 跟 M51 "批量退役" 同模式 — 不弹填 modal)
- 不要同时跑多个 bulk mut (防 race condition 已在 button disable 处理)

## Out of scope (留 future round)

- 批量"取消误报" — 同接口反向参数, 但运营场景 99% 是"标 FP", 留 future
- 后端 bulk_fp 端点 — N=100+ 慢, 同 M51-3 留 future backend round
- 自动化 FP (基于历史 ack 时间阈值) — 完全是新功能, 跟 M53 无关

## Risk

- **markFalsePositive note 写死"运维批量标记"**: 跟 M51 "批量退役" 同 trade-off (不要弹 modal, 简单可接受). 留 future: 如果合规要求"每条 note 必须填原因", 需后端 bulk endpoint 支持 note 数组
- **bulkFPMut onError 覆盖 setBulkProgress(null)**: 跟 bulkAck/Resolve 同模式, 失败时强制关 modal. 单条失败计入 failed 数组不 throw → 不触发 onError
- **mock 测试**: 用 Assets.test.tsx 同样的 vi.hoisted spy + useApiMutation mock (M51-2 修过真路径); 验真路径 via mutation inversion
