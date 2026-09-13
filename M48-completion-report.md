# M48 Completion Report — G-UI-TopoClick 拓扑节点 cursor:pointer 死按钮 → 真按钮

## Delivered

节点真按钮化（frontend-only，后端零改动）：

- `frontend/src/pages/Topology.tsx` — `<g>` 节点组加 `onClick` / `role="button"` /
  `tabIndex={0}` / `aria-label={n.name}` / `onKeyDown`(Enter·Space) / SVG `<title>` 原生
  tooltip；`cursor: pointer` 从 inner `<circle>` **上移**到 `<g>`（点击热区覆盖圆 + 名称 +
  类型 label）。普通节点 → `navigate('/assets/<id>/diagnostics')`；虚拟节点 → antd `Popover`
  显示 `name` / `asset_type` / `open_alerts`，不 navigate（虚拟节点无 assets 记录，跳详情必 404）。
- `frontend/src/pages/Topology.test.tsx` — 老 11 用例全保 + 新 5 用例（click navigate /
  虚拟节点 popover / Enter navigate / Space popover / a11y + 无 cursor 双重化）。
- `CHANGELOG.md` — `### M48` section（插在 M47 之前）。
- `TODO.md` — 末尾 G-UI-TopoClick 标 `[x]`。
- `M48-completion-report.md`（本文件，新）。

未做（PM 明确 out of scope）：边（edge）click、节点拖动/编辑、TopologySettings、
`useApiQuery` key/staleTime/refetch（一字未动）、后端。

## Changed

| file | lines before → after | 说明 |
|------|---------------------|------|
| `frontend/src/pages/Topology.tsx` | 207 → 270 | `<g>` onClick/键盘/role/aria-label/`<title>`；cursor 上移；`handleNodeClick`（navigate 或 `setVirtualNode`）；`Popover` 包虚拟节点；`VirtualNodeMeta` 元数据内容 |
| `frontend/src/pages/Topology.test.tsx` | 145 → 199（11 → 16 tests） | `navigateMock`（`importActual` + 覆盖 `useNavigate`）；新 5 用例；老「渲染所有节点名」加 `ignore: 'title'`（`<title>` 里同名否则双命中）；`beforeEach` 里 `navigateMock.mockClear()` |
| `CHANGELOG.md` | 1058 → 1093 | `### M48` section |
| `TODO.md` | 343 → 345 | G-UI-TopoClick 结案条目 |
| `M48-completion-report.md` | (新) → 本文件 | — |

关键实现片段：

```tsx
const handleNodeClick = (n: TopologyNode) => {
  if (n.is_virtual) { setVirtualNode(n); return }
  navigate(`/assets/${n.id}/diagnostics`)
}
```

```tsx
<g role="button" tabIndex={0} aria-label={n.name}
   onClick={() => handleNodeClick(n)}
   onKeyDown={(e) => {
     if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleNodeClick(n) }
   }}
   style={{ cursor: 'pointer' }}>
  <title>{n.name}</title>
  …
</g>
```

虚拟节点用 **controlled** `Popover`（`open={virtualNode?.id === n.id}` +
`onOpenChange`），所以「点了有反应」这件事由页面 state 决定，不依赖 rc-trigger 的内部
`onClick` 覆盖行为；rc-trigger 确实会 chain 原 child 的 `onClick`（读过
`node_modules/@rc-component/trigger/es/index.js:349-363`），controlled 模式下 `triggerOpen`
不再影响可见性，行为确定。

## Validation

### tsc
```
$ cd /home/webman/Projects/ITmanager/frontend && npx tsc --noEmit
TSC_EXIT=0            # 0 error
```

### eslint（changed files）
```
$ npx eslint src/pages/Topology.tsx src/pages/Topology.test.tsx
ESLINT_EXIT=0
```

### 单文件
```
$ npx vitest run src/pages/Topology.test.tsx
 ✓ src/pages/Topology.test.tsx  (16 tests) 2597ms
 Test Files  1 passed (1)
      Tests  16 passed (16)
```

### 全 frontend
```
$ npx vitest run
 Test Files  39 passed (39)
      Tests  343 passed (343)     # 338 基线 + 5 新增
   Duration  271.43s
```

### mutation inversion（真做了一次）
把 `handleNodeClick` 的函数体替换成 `return null`（`navigate` / `setVirtualNode` 都不再被调）：
```
$ npx vitest run src/pages/Topology.test.tsx
 Test Files  1 failed (1)
      Tests  4 failed | 12 passed (16)
```
红的 4 条正是 M48 的行为用例（click navigate / 虚拟节点 popover / Enter / Space）；
第 5 条 a11y 用例（role/aria/title/cursor 结构）不依赖 handler 行为，故仍绿。
`cp` 回原文件后重跑 16/16 PASS（`git diff` 确认 revert 干净，`navigate(\`/assets/${n.id}/diagnostics\`)` 在 `Topology.tsx:133`）。

### 后端（本轮未改，跑冒烟保无回归）
```
$ cd /home/webman/Projects/ITmanager/backend && go test -count=1 -timeout=600s ./...
ok  …/internal/api 19.077s   ok …/internal/integration 14.681s   …
# 27 packages ok（其余 ?    … [no test files]）
```

### 真实浏览器
未做（诚实标注）。本机后端需要 Postgres 才能返回非空 `/topology`，未起 full stack；
组件层面的交互由 jsdom 里**真实 React 渲染 + 真实 DOM 事件**覆盖（`fireEvent.click` 命中
`getByRole('button')` 的 `<g>`）。

## Risk

残余：

- **R-1 虚拟节点 Popover 无「跳详情」链接**：intent 里写的是「资产名 + 状态 + 当前告警数 +
  跳详情链接」，而虚拟节点**没有** assets 记录，链接无处可跳，故只渲染元数据 + 一行说明
  「虚拟节点无资产详情页」。这是刻意的取舍，不是漏做。
- **R-2 `<title>` tooltip 在虚拟节点 Popover 打开时仍在**（浏览器原生行为，无冲突）。
- **R-3 未做真实浏览器 e2e**（见上）；jsdom 不覆盖 CSS `cursor` 的实际手感与 Popover 定位。
- **R-4 `role="button"` + `tabIndex` 在 SVG 上的 AT 支持度**：Chrome/Firefox + NVDA/VoiceOver
  现代版本正常，老 AT 可能不朗读——与「不给节点套 `<button>`（HTML button 不能包 SVG group）」
  这个约束共同决定的，非本轮可修。

backlog（本轮显式不做，已写进 TODO）：

- **T-M48-1 拓扑边（edge）click** —— 运维主操作是节点，边点击非 P0。
- **T-M48-2 TopologySettings（节点位置 / 视图模式）** —— 拓扑当前是只读视图，属用户偏好功能。

## Status

final commit SHAs（本 round 全部）：

```
aa52508  docs(M48): intent-M48.md - G-UI-TopoClick 修拓扑节点 cursor:pointer 死按钮   (PM 的 intent commit)
516f702  feat(M48): Topology 节点 onClick → navigate / popover (a11y + 键盘)
9ed2421  test(M48): Topology 节点点击 + 键盘 + 虚拟节点 popover 5 测试
```

docs commit（CHANGELOG + TODO + 本报告）在本文件写出后立即 commit；SHA 见 push 后的
`git log`（本 round 共 4 个 M48 commit：intent / feat / test / docs，符合 4-5 cadence）。

push status: **all to origin/main**。

- 注：推送初段撞上环境 TLS 故障（`HTTP(S)_PROXY=http://127.0.0.1:2080` → `TLS connect error:
  unexpected eof while reading`，curl 直连 github 200 / 走代理超时）。确认为代理侧故障后，
  以 `env -u *_proxy git push` 直连推送成功；**代码/测试本身与网络无关**。

acceptance criteria all-met check：

- `npx tsc --noEmit` 0 错误 —— ✅（TSC_EXIT=0）
- `npx vitest run` ≥338 + 新增 ≥3 拓扑 click 测试全 PASS —— ✅（343 = 338 + 5，39 files，M48 新 5 条）
- 节点可点跳转 / 打开 popover；视觉不再「点了无反应」 —— ✅（普通节点 `navigate('/assets/<id>/diagnostics')`；虚拟节点 Popover 三字段；`cursor: pointer` 与真实点击区域同在 `<g>`）
- mutation inversion 实证（bypass onClick → 测试 FAIL） —— ✅（`return null` → 4 failed | 12 passed；revert 后 16/16 PASS）
- 27 backend packages 全绿（后端未动，跑过冒烟） —— ✅（`go test -count=1 -timeout=600s ./...` 27 packages ok）；**47 真 PG `db_smoke.sh` 未跑**（本轮后端零改动，且需 `sudo -n docker`；如需补证据可单跑，见 intent 的补充路径）
- 4-5 commits per cadence —— ✅（intent / feat / test / docs = 4）
- All pushed to origin/main —— ✅（`7e85026..9ed2421` 已推；docs commit 随后推）
