---
id: INTENT-M40-jwt-status-check
title: G-5 已签发 JWT 在用户禁用后立即失效（最长 24h → 即时）
status: draft
author: hermes@local (PM)
created: 2026-09-13
outcomes:
  - 管理员把 `users.status` 置为 `inactive` 后，该用户**已签发**的 JWT 在下次请求时立刻失效，不再最长 24h 才生效
  - API Key 路径（已实现）和 JWT 路径（新加）行为一致：禁用用户 = 立即拒绝
  - 性能开销可忽略（in-memory cache 30s TTL，per-user，不是 per-request DB hit）
  - 锁屏/封禁场景下运维不再需要等 JWT 自然过期
acceptance:
  - id: AC-M40-1
    given: 用户 `alice` 登录拿到有效 JWT
    and: 管理员把 `users.status` 改成 `inactive`（绕过 JWT 流程，直接 DB）
    when: alice 用旧 JWT 调任意 protected 路由
    then: 401 Unauthorized（不再等 24h 自然过期）
  - id: AC-M40-2
    given: 用户 `bob` 登录拿到有效 JWT（status=active）
    when: bob 用 JWT 调 `/api/v1/auth/me` 200 次（同一分钟内）
    then: `users` 表只产生 ≤ 1 次 SELECT（30s TTL 命中缓存，**不**是每请求 1 次）
  - id: AC-M40-3
    given: 用户 `carol` 登录拿到有效 JWT，cache TTL 30s 内
    and: 管理员把 carol 改 `inactive` 立即又把 carol 改回 `active`（30s 内）
    when: carol 用 JWT 调 protected 路由
    then: 200 通过（缓存内 status=active，未失效前不重读 DB；30s 后才感知；这是 cache 不可避免的 trade-off）
  - id: AC-M40-4
    given: 缓存条目 TTL 自然过期（> 30s 后）
    and: DB `users.status = inactive`
    when: 用户用旧 JWT 调 protected 路由
    then: 401 Unauthorized（缓存自然过期后命中 fresh DB 读，status=inactive 拒绝）
  - id: AC-M40-5
    given: API Key 路径已实现 status 检查
    when: PM-direct 验证 API Key 行为不变
    then: 既有 `handleAPIKeyAuth` 的「用户 inactive 立即拒绝」语义保持
edges:
  - 缓存 TTL 30s 是 trade-off：缩短到 5s 增加 DB 压力（每用户每分钟 12 次 SELECT），延长到 5min 让运维体验不到立即生效
  - 多副本部署下缓存是进程内的，副本 A 把用户改 inactive 后副本 B 的 cache 仍可能是 active；最长滞后 30s。**这是可接受的**：运维封禁是即时动作（≤30s 全副本生效），不是强一致系统
  - 缓存键是 `user_id`（UUID string），不存 token 内容；token 自身的 24h 过期由 `jwt.RegisteredClaims.ExpiresAt` 独立处理
  - `must_change_password=true` 的用户**不**在本轮处理（FIX-PLAN-AUTHZ-LEFTOVER §8.1 已记 G-1 单独任务）
  - 不引入 Redis / 外部 cache（单仓库进程内 cache；性能与单实例 ITmanager 规模匹配）
not_goals:
  - 不做 token revocation list（黑名单）—— 用户改密码 / 主动 logout 仍是「等 JWT 自然过期」，不在本轮范围
  - 不加 Redis 缓存（单进程内存足够）
  - 不动 `must_change_password` 全局强制（G-1，独立任务）
  - 不重构 `AuthMiddleware` 整体签名
  - 不动 JWT 签发/验证逻辑本身（`GenerateToken`/`VerifyToken`）
evidence:
  - "TODO.md L62 G-5 已签发 JWT 不查库：status=inactive 对 API Key 立即生效（middleware/auth.go:187）但 JWT 最长 24h 才生效"
  - "middleware/auth.go:99-110 AuthMiddleware JWT 分支：仅调 VerifyToken，无 DB 读"
  - "middleware/auth.go:160-180 handleAPIKeyAuth 已含 user.Status 检查（审计 M-5 修复点）"
  - "FIX-PLAN-AUTHZ-LEFTOVER.md §8.1 G-1 follow-up 描述"
---

# M40 — G-5 JWT 用户禁用即时生效

## Context

`middleware/auth.go` 两条认证路径行为不一致：

- **API Key 路径** (`handleAPIKeyAuth:160-180`)：`database.DB.First(&user, ...)` 后查 `user.Status == "inactive"` → 禁用立即生效。
- **JWT 路径** (`AuthMiddleware:99-110`)：仅 `VerifyToken(tokenString)`，验签 + 过期检查，**不查 DB** → 禁用最长 24h 才生效（`Auth.JWT.Expire` 默认 86400）。

运维封禁场景下，禁用 admin 后被禁用者的 JWT 仍能用 24h → 严重安全 gap。审计 M-5 已修复 API Key 侧，JWT 侧留作本轮收口。

M40 在 JWT 分支加用户状态查库 + 进程内 30s TTL cache（per-user），与 API Key 路径行为对齐。性能影响：每用户每 30s ≤ 1 次 SELECT，远低于现状 JWT 验签 1 次 DB 写（如果有的话）。

## Outcomes (4)

详见 frontmatter `outcomes[]`。

## Acceptance Criteria (5)

详见 frontmatter `acceptance[]`，AC-M40-1 到 AC-M40-5。

## Edge cases (5)

详见 frontmatter `edges[]`：

- 缓存 TTL 是 trade-off，不是强一致
- 多副本部署下 cache 是 per-process，30s 内可滞后（可接受）
- 不动 `must_change_password`
- 不引入 Redis
- 不存 token 内容，只存 user_id

## Operational constraints

- **必动文件**：`backend/internal/middleware/auth.go`（加 status cache + lookup）、`backend/internal/middleware/auth_test.go`（新增 4-5 条用例）、`backend/tests/db_smoke_test.go`（真 PG 端到端用例）、`scripts/db_smoke.sh`（白名单加新 test）、`TODO.md`（G-5 结案）、`CHANGELOG.md`（M40 条目）、`M40-completion-report.md`（5-section 报告）
- **不动**：`GenerateToken`/`VerifyToken`/`handleAPIKeyAuth`/Redis/外部 cache/`must_change_password` 处理
- **不引入新依赖**

## Evidence

详见 frontmatter `evidence[]`，4 条锚点全部已验证（含审计 M-5 修复点）。

## Round plan (≤2h, PM-direct)

按 task-completion-protocol 拆 5 小步，每步独立可验证：

1. `feat(M40): middleware authStatusCache (sync.RWMutex + map + 30s TTL)`
2. `feat(M40): AuthMiddleware JWT 分支接 cache lookup + status=inactive 拒绝`
3. `test(M40): auth_test.go 4 条用例 (立即拒绝 / cache 命中 / 30s 过期 / cache TTL 内 status 翻转不感知)`
4. `test(M40): 真 PG TestDBSmoke_M40_JWTDisableTakesEffect (端到端：admin 改 inactive → JWT 下次请求 401)`
5. `docs(M40): TODO.md G-5 结案 + CHANGELOG M40 条目 + 报告`

每步 gate：
- `cd backend && go build ./...` (exit 0)
- `cd backend && go vet ./...` (exit 0)
- `/home/webman/.local/share/mise/installs/go/1.25.14/bin/gofmt -l` on changed files (empty)
- `cd backend && go test -count=1 ./internal/middleware/... ./tests/...` (green for touched packages)
- `git -c user.email=hermes@local -c user.name=hermes commit` + `git push origin main`

**Mutation inversion required**：
- PASS 初始（AC-M40-1 401）→ 改 cache TTL 到 0（恒不过期）→ AC-M40-4 旧 JWT 永不过期（FAIL）→ revert → PASS

最终 gate：
- `cd backend && go test -count=1 ./...` (27 packages green)
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` (44 cases green, 1 new M40 test)
- Append M40 section to CHANGELOG.md
- Write `/home/webman/Projects/ITmanager/M40-completion-report.md` (5-section)
