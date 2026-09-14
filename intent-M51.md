# intent-M51: G-UI-BulkAssets 资产批量操作 (rowSelection + 批量退役/恢复)

## Context

`AssetTable` 已 ship `rowSelection` 接口 (`AssetTable.tsx:55-58`), 但 `Assets.tsx` 父组件**未传**. 资产页没有批量操作能力 — 运维要退役 50 台资产只能一行一行点 [退役] 按钮, 50 次 Popconfirm.

## 任务

### 1. `frontend/src/pages/Assets.tsx`

- 新增 state:
  - `'use client' import { useState } from 'react'` (顶部现有)
  - `const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])`
- `rowSelection` prop 传给 `<AssetTable>`:
  ```tsx
  rowSelection={{
    selectedRowKeys,
    onChange: setSelectedRowKeys,
    preserveSelectedRowKeys: true, // 翻页不丢选中
  }}
  ```
- 在 PageHeader 下方 (asset table 上方) 加批量操作条 (只在 `selectedRowKeys.length > 0` 时显示):
  - 提示文字 "已选 N 项"
  - 按钮 [批量退役] (Popconfirm → 调用现有 handleRetire 循环遍历每行)
  - 按钮 [批量恢复] (仅当至少一项 `status === 'retired'` 时显示, 调用 handleRestore 循环)
  - 按钮 [清空选择]
- 批量 mutation: 用 `useMutation` 包一层, success 后清 selectedRowKeys + invalidate 资产列表
- 后端 zero change — 用现有 `api.post('/assets/:id/retire')` + `restore` 单个调用

### 2. `frontend/src/components/AssetTable.tsx`

- `rowSelection` 类型已 ship, 不动
- 但要确保 `preserveSelectedRowKeys: true` 工作 (antd 已 ship)

### 3. 测试 `frontend/src/pages/Assets.test.tsx`

- 测试选中 1 项时, 批量条出现
- 测试 [批量退役] 点击 → Popconfirm → 二次确认 → 调 `/assets/:id/retire` N 次
- 测试 [批量恢复] 仅当含 retired 项时显示 (mock data)
- 测试 [清空选择] → selectedRowKeys 清空
- 测试 mutation 失败 → toast 错误, 不清 selectedRowKeys

### 4. mutation inversion

临时把 [批量退役] 的 onConfirm 改成 `return;`, expect 至少 1 个测试 FAIL (没发 retire 请求).

## Hard pass criteria

- `npx tsc --noEmit` 0 error.
- `npx vitest run src/pages/Assets.test.tsx` ≥ 4 PASS 新增 + 老 PASS 全过.
- `npx vitest run` 全 frontend ≥ 359 PASS (M50 之后 355 + 新 4), 0 FAIL.
- 后端 `cd ../backend && go test -count=1 -timeout=600s ./...` → 27 packages ok.
- mutation: bypass onConfirm → 1+ FAIL → revert → 全 PASS.

## Commit cadence

5-7 commits, each pushed to origin/main:
1. intent-M51.md
2. feat(Assets): rowSelection + 选中条
3. test: 批量条 + 批量退役/恢复 + 清空
4. docs: CHANGELOG + completion report + TODO

(可选 5. mutation 实证 commit 不需要单独, 实证 inline 即可)

## Don't

- 不要改 AssetTable 接口 (已 ship rowSelection)
- 不要新加 antd 组件依赖 (Space + Button + Popconfirm 已 ship)
- 不要做服务端批量端点 (前端循环 N 次即可, ≤4h 不够)
- 不要给非 admin 加 bulk 操作 (后端 RBAC 已 ship, 前端不必再加 role gate)

## Out of scope (留 round future)

- B3 (AssetFilterBar 字段扩充) — 单独 round
- 批量改标签 / 批量转移 owner — 单独 round
- 资产详情面包屑已 ship (M47)

## Risk

- 批量退役 50 项 N 次调用 — N 大时慢. 短 task 不优化, 留 note.
- preserveSelectedRowKeys 在 antd v5 已 ship, 不需测 antd 内部.
