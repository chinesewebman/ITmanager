# intent-M61: G-User-AdminManagement 用户管理 (admin 端启用/禁用/改角色)

## Context

**G-4 摩擦** (multi-angle 审查发现):

- Backend `users` group 路由只有 GET (List + Get), 无 PUT/PATCH/DELETE
- Backend UserService 只有 List + Get
- Frontend userApi 只有 list + get
- Frontend pages 无用户管理 page

Admin 现在无法:
- 禁用离职员工账号
- 修改用户角色 (晋升/降级)
- 强制用户下次登录改密

## 任务

### Backend

1. **`backend/internal/service/user_service.go`**:
   - `Update(ctx, id, UpdateUserInput)` method
   - `UpdateStatus(ctx, id, status)` method
   - `UpdateRole(ctx, id, role)` method
   - **守卫**: 禁止 self-disable last admin (S-1a 同族病灶)

2. **`backend/internal/api/handlers/user_handler.go`**:
   - `UpdateUser` (PUT /users/:id)
   - `UpdateUserStatus` (PATCH /users/:id/status)
   - `UpdateUserRole` (PATCH /users/:id/role)
   - role 走 CanonicalRole 归一

3. **`backend/internal/api/routes.go`**:
   - 3 新路由挂 `canIdentity` (admin only)

4. **Backend tests**:
   - Unit: Update 正常 / 部分字段 nil / 词表外 / self-disable 守卫
   - Handler: 正常 / 403 (self) / 400 (词表)

### Frontend

5. **`frontend/src/services/api.ts`**:
   - `userApi.update(id, updates)`
   - `userApi.updateStatus(id, status)`
   - `userApi.updateRole(id, role)`

6. **新建 `frontend/src/pages/Users.tsx`**:
   - PageHeader + Table (用户名 / 昵称 / 邮箱 / 角色 / 状态 / 最后登录 / 操作)
   - 角色 Select 5 词表
   - 状态 Switch + Popconfirm
   - 操作: 重置密码 (placeholder)
   - 失败回滚 (乐观更新)

7. **`frontend/src/App.tsx`**:
   - 加 `/users` route + 条件 sidebar (admin only)

8. **`frontend/src/pages/Users.test.tsx`** (新建):
   - mock list → 3 users
   - 切状态 → 弹 Popconfirm → 调 updateStatus → 列表刷新
   - 改角色 → 弹确认 → 调 updateRole
   - admin self-disable → mock 403 → Switch 回滚 + toast

## Hard pass

- backend `go test -count=1 ./...`: 27 packages ok
- frontend tsc 0
- frontend Users.test.tsx: PASS
- 全量 frontend: 无退化
- mutation inversion 实证 (2 处)
- 双轨 graphify + codegraph

## graph-tools verified

执行时间: 2026-09-15 01:XX
- graphify update + diagnose
- codegraph sync (stale → 强 re-index)
