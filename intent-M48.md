# OMP Brief — M48: G-UI-TopoClick 修拓扑节点 cursor:pointer 死按钮

## Task

修 `frontend/src/pages/Topology.tsx` 拓扑节点 `cursor: pointer` 但无 `onClick`
的视觉欺骗。点节点 → 跳资产详情或显示 popover (资产名 + 状态 + 当前告警数 + 跳详情链接).

## Outcome

- 拓扑节点变成真按钮 (click → navigate 或 popover)
- 所有现有 Topology.test.tsx 测试 PASS
- 新加测试: 节点 click → 触发 navigate 或 popover open
- mutation inversion 实证 (bypass onClick → 测试 FAIL)
- 27 packages 全绿 + 338 frontend 测试 + 47 真 PG (后端本轮不动, 跑冒烟保无回归)
- 5 commits per cadence (intent / fix+test / docs)

## Scope (in)

- `frontend/src/pages/Topology.tsx` — 加 onClick 到 `<g>` (节点组). 不要拆出 SVG
  `circle` 单独 click — group 点击区域包含整个节点 + 名称 label, 触控体验更好.
- `frontend/src/pages/Topology.test.tsx` — 加 click 测试 (mock api + 验证
  navigate/popover 行为)
- `intent-M48.md` (新, repo root)
- `M48-completion-report.md` (新, repo root)
- `CHANGELOG.md` (`### M48` section, before M47)
- `TODO.md` 末尾加 G-UI-TopoClick done

## Out of scope (不在本 round)

- 后端 zero change (M48 是 frontend-only)
- Topology 边 (edge) click — 不是 P0 (运维主要操作节点). 留 backlog.
- 节点拖动 / 编辑 — 拓扑是只读视图, 不要改这块.
- TopologySettings (节点位置/视图模式 — 用户偏好) — 留 backlog.

## Hard constraints

- 不要在 onClick handler 里加 console.log 或测试痕迹 — 上生产时不要.
- 不要改 `useApiQuery` 的 key/staleTime/staleTime handling — 接 frontend cache.
- 不要给节点加 `<button>` 嵌套 SVG (HTML/SVG 不允许 button 包 SVG group); 用
  `role="button"` + `aria-label={name}` + `tabIndex={0}` + 键盘 onKeyDown handler
  (Enter / Space) 保 a11y.
- 节点 hover 加 `<title>` SVG element 显示节点名 (浏览器原生 tooltip) 满足可观察性
  — 不引外部 tooltip library.
- 不要 mix 视觉: cursor 已经是 pointer, 修了之后**不要**再额外加 outline (避免双
  视觉).

## Plan

1. Step 1 (intent) — write intent-M48.md, commit + push.
2. Step 2 (fix) — 改 Topology.tsx + Topology.test.tsx. Files:
   - `g key={n.id}` 加 `style={{ cursor: 'pointer' }}` (原本在 inner circle 的移到 g 上,
     因为 click 触发在 g).
   - `g key={n.id}` 加 `onClick` + `role="button"`, `tabIndex={0}`, `aria-label={n.name}`,
     `<title>{n.name}</title>` 子节点.
   - `onClick` 行为: 跳 `/assets/${n.id}/diagnostics` (诊断页), 因为运维点击拓扑节点
     大概率是要 "ping/traceroute" (这是 Topology 视觉核心价值 — 故障节点一查就诊断).
     - 注意: 虚拟节点 (`n.is_virtual === true`) 没有 asset, 跳诊断会 404 →
       虚拟节点 onclick 改显示 popover (用 antd Popover / Dropdown / Modal), 不 navigate.
3. Step 3 (test) — 写 Topology.test.tsx (如果不存在先建). 测:
   - onClick navigate (mock useNavigate) 命中 `/assets/<id>/diagnostics`
   - 虚拟节点 onClick 打开 modal/popover, 不 navigate
   - 键盘 Enter / Space 也触发 navigate
   - mutation: bypass onClick 行为 → 测试 FAIL
4. Step 4 (verify) — `npm run tsc --noEmit` + `npm run vitest run` 全 frontend.
5. Step 5 (docs) — CHANGELOG + M48-completion-report.md + TODO. commit + push.

## Working dir

`/home/webman/Projects/ITmanager/frontend` — omp `--cwd` 设此目录.

## Author

`hermes@local`. git identity 在 omp 里默认 **user.email='hermes@local' user.name='hermes'`.
若 omp 没有自动 set, 在 dispatch 前手动 `git config user.email 'hermes@local'` + 
`git config user.name 'hermes'` (项目级, global 不动).

## Acceptance criteria

- `npx tsc --noEmit` 0 错误
- `npx vitest run` ≥338 (老) + 新加 ≥3 个拓扑 click 测试 → 全 PASS
- 节点可点跳转 / 打开 popover; 视觉不再 "点了无反应"
- mutation inversion 实证 (bypass onClick → 测试 FAIL)
- 27 backend packages + 47 真 PG db_smoke 全绿 (后端未动, 跑过冒烟)
- 4-5 commits per cadence
- All pushed to origin/main

## Failure policy

- tsc 报错 / vitest FAIL → fix before push (spec 阶段钉死: 改不引入新类型 + 测试
  全部 PASS 才算 ship)
- mutation inversion FAIL (修改形为绕过 onClick 后测试 PASS 而不是 FAIL) →
  说明测试不 robust, 重写测试
- 若 omp 跑出 dispatch 时间 > 30min → 退出 + escalate (项目经理 PM 会 rescue)

## 不要 spawn subagent

M48 是 single-agent dispatch (≤2h frontend change). 不要用 omp 的 task 工具再加
subagent (round 内的 dispatch 边界: 一层). PM-direct rescue 另立.

## 输出格式 (final report)

5 sections per task-completion-protocol:

1. **Delivered** — bullet 改了哪些 files
2. **Changed** — table (file: lines before → after, plus 1 行说明)
3. **Validation** — actual command output (tsc, vitest run, mutation inversion)
4. **Risk** — residual + T-N backlog
5. **Status** — final commit SHAs + push status + acceptance criteria all-met check

## Reference

- `intent-M48.md` (此 round 规范) - 命名前缀 G-UI-TopoClick, 范围 ≤2h 单 frontend file + test file
- 旧 Topology 页详情: `Topology.tsx:176` `style={{ cursor: 'pointer' }}`
- 既有 `TopologyNode` interface 已含 `id`, `name`, `open_alerts`, `is_virtual` 字段,
  不用新加
- 路由 `/assets/:id/diagnostics` 已 ship (App.tsx:241), 后端 diagnostic handler 也 ship
- antd 已 ship: Modal / Popover / Dropdown 都可. 优先 Popover (轻量, 不阻塞 UI)
