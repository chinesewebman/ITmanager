# M71 — Audit sidebar 入口（OMH ulw-loop 第 3 cycle）

> **Loop cycle**: 3 of `itmanager-grit-2026q3`

## Goal

admin/ops_admin/auditor 三种 audit 能力的用户登录后, 侧边栏直接看到「审计日志」入口.
目前 `/audit` 路由可达, 但 sidebar 入口缺失, 用户只能通过 CommandPalette 访问 (T-73 派生真摩擦).

## Non-goals

- 不动 `/audit` 路由本身 (M49 已 ship)
- 不动 backend `middleware.CapAudit` 能力矩阵 (admin/ops_admin/auditor 三角色)
- 不复制 role 字面量 (M49/M61 standing rule: 用 /auth/me 下发的 capabilities)
- 不动其他 sidebar 项

## Assumptions

- T-73 修了 admin 能访问 /audit 但 sidebar 仍隐 → 复制 M61 "用户管理" 入口模式
- Audit 走 `middleware.CapAudit` 能力 (admin/ops_admin/auditor 三种)
- 业务: ops_admin/auditor 也需要看审计日志, 不能只给 admin

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| sidebar 显示「审计日志」链接 (capabilities 含 audit) | `buildMenuItems(hasAudit=true)` 含 `/audit` |
| sidebar 不显示 (无 audit 能力) | `buildMenuItems(hasAudit=false)` 不含 `/audit` |
| 入口放在 `/settings` 之前 (与 `/users` 同位, audit 也是能力门禁类) | 顺序一致 |
| /auth/me 失败 fail-closed | 不显示 |
| 抽 `hasAudit` state 复用 `hasIdentity` 模式 | `useEffect` extract `caps.includes('audit')` |
| 测试覆盖: 4 case (有/无/失败/形状异常) | 复用 `App.menu.test.tsx` 模式 |

## Verification

- `tsc --noEmit` 0 err
- `vitest run src/App.menu.test.tsx` 全 PASS
- **mutation inversion**: bypass `hasAudit` 检查 → Audit link 出现 / 不出现 翻转 → 测试真红

## Risks

- **顺序**: Audit 与 Users 都是能力门禁入口. M61 把 Users 放在 settings 之前, audit 同样.
- **图标**: 用 `AuditOutlined` 或 `SafetyOutlined` 或 `FileSearchOutlined` (antd). 需检查 antd 版本.
- **fail-closed**: 与 M61 一致, /auth/me 失败 = 默认无 audit 能力 = 不显示入口.

## Plan

1. `App.tsx`:
   - `buildMenuItems(hasIdentity, hasAudit)` 加第二参数
   - 加 `hasAudit` state + useEffect 提取
   - `buildMenuItems` 返回值加 `...(hasAudit ? [{key:'/audit',...}] : [])`
2. `App.menu.test.tsx`:
   - 加 `buildMenuItems(hasIdentity, hasAudit)` 测试 (4 case)
3. Mutation inversion: `hasAudit && false` → 测试 fail
4. CHANGELOG + TODO + completion + graph analysis

## Decision gate

- **D1**: Audit 与 Users 同 sidebar pattern (能力门禁 / 顺序在 settings 之前) ✓
- **D2**: 复用 M61 的 capabilities 提取模式, 不复制 role 矩阵 ✓
- **D3**: 图标用 `AuditOutlined` (antd 5.x 默认有) ✓
