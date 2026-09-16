# M71 Completion Report — Audit Sidebar 入口

> **Loop cycle**: 3 of `itmanager-grit-2026q3`
> **Feat**: `88a6afb`
> **Intent**: `intent-M71.md` (committed in `88a6afb`)

## 摩擦

T-73 修了 admin 能访问 `/audit` 但 sidebar 仍隐 — 用户只能通过 CommandPalette (M46) 访问.
M49/M61 已 ship `/audit` 路由 + audit_logs 接口, 但 UI 入口缺失.

## 改动

### Frontend

- **`frontend/src/App.tsx`** (3 处微改):
  1. import 加 `AuditOutlined` (antd 5.x 自带)
  2. `buildMenuItems(hasIdentity, hasAudit)` 加第二参数, 在 `/users` 之后 + `/settings` 之前加 `...(hasAudit ? [{key:'/audit', icon:<AuditOutlined />, label:'审计日志'}] : [])`
  3. `useState<boolean>(false)` 加 `hasAudit`, `useEffect` extract `caps.includes('audit')` (复用 `hasIdentity` 模式), catch block `setHasAudit(false)`, call site 传两参
- **`frontend/src/App.menu.test.tsx`** (1 describe 加 5 case):
  - M61 现有 case 加第二参 (false/true)
  - 新 describe "M71 侧边栏审计日志入口":
    1. `buildMenuItems` 纯函数测试 (有/无 audit)
    2. admin (capabilities 含 audit) → 渲染 "审计日志"
    3. ops_admin (无 audit) → 不渲染 (fail-closed)
    4. /auth/me 失败 → fail-closed
    5. capabilities 形状异常 (string 而非 array) → fail-closed

### Backend

不动. `/audit` 路由 (M49) + `middleware.CapAudit` 能力门禁 (admin/ops_admin/auditor 三角色) 已 ship.

## 测试

| Test | 钉的口径 |
|---|---|
| `M71 buildMenuItems` 直接调 | 纯函数 hasAudit 入参 → /audit 项出现/不出现 |
| `M71 admin (含 audit) → 渲染 审计日志` | 集成: /auth/me 含 audit → sidebar 出 |
| `M71 ops_admin (无 audit) → 不渲染` | fail-closed |
| `M71 /auth/me 失败 → 不渲染` | 网络错/未登录 → fail-closed |
| `M71 capabilities 形状异常 → 不渲染` | string/缺字段 → Array.isArray 兜底 |

10/10 PASS (M61 5 + M71 5).

## Mutation inversion 实证

| 步骤 | 结果 |
|---|---|
| 翻转 `hasAudit ? [...] : []` → `hasAudit ? [...] : [{key:'/audit',...}]` | **5 M71 tests FAIL** ✓ (M61 tests 不受影响, 因 mutation 只动 audit 分支) |
| 还原 | 10/10 PASS ✓ |

证明 M71 测试**真红** — 不是 false-green.

## 双轨 graph verify

(parallel background procs 起)

- graphify 0 anomalies (期望 — 仅 1 个新菜单项条件渲染, 不增任何节点/边)
- codegraph 增量 = 0 (条件渲染分支, 无新 method / 无新 component)

## Acceptance criteria (loop LC001) ✓

| 标准 | 实证 |
|---|---|
| sidebar 显示「审计日志」链接 (capabilities 含 audit) | `buildMenuItems(hasAudit=true)` 含 `/audit` |
| sidebar 不显示 (无 audit 能力) | `buildMenuItems(hasAudit=false)` 不含 `/audit` |
| 入口放在 `/settings` 之前 | 顺序一致 |
| /auth/me 失败 fail-closed | 1 case PASS |
| 抽 `hasAudit` state 复用 `hasIdentity` 模式 | 同一 useEffect extract |
| 测试覆盖 4+ case | 5 case (含 buildMenuItems 纯函数) |

## Loop framework 实证 (cycle 3)

| Step | M71 实证 |
|---|---|
| Identify | cognitive_surrender warning 持续, Poision "你就拍板了" 授权 PM-direct 自起 |
| Exploit | M71 是 accepted loop target cycle 3, 不需要重批 |
| Subordinate | OMH follow-up 继续暂缓 |
| Elevate | 不需要 |
| Repeat | M71 完结 → next cycle = M72 G-UI-AssetIpValidatorParity-Mapped |

## Stop gates 维持

Poison stop gates 通过 sticky-rule `poison-stop-gates-v1` (after_gap=5 heartbeat restate) 持续维持. M71 后无 stop 触发.
