# ADR-001: Rate Limit 归属

**状态**: ✅ 已落地 (v1.4.0, commit `0f65854`)
**日期**: 2026-06-17
**关联审计**: M1-P2 (代码审计 v1.0.3, 2026-06-17)

## 背景

M1 internal/middleware 审计发现一个待决策项: **Rate Limit 应该放在哪里**。
当前实现: **无 rate limit** (P0 风险 — API 完全没限流)。

## 候选方案

### A) Middleware (全局)
- ✅ 单一位置，统一所有 endpoint 限流
- ✅ 复用现有 middleware 链
- ❌ 难以做"不同 endpoint 不同阈值" (e.g. /api/auth/login 应更严)
- ❌ 业务 handler 拿不到限流状态 (headers 可见但语义不可读)

### B) Handler 内部
- ✅ 每个 handler 自己控制阈值 (login: 5/min, list: 100/min, etc.)
- ✅ 可结合业务状态 (e.g. 同一 IP 失败次数限流)
- ❌ 重复代码 — 18 个 handler 都要写
- ❌ 容易遗漏

### C) Per-route Middleware (Gin IRoutes.Use)
- ✅ Middleware 复用 + Handler 级别阈值
- ✅ 业务层用 `c.Get("rate_limit")` 拿状态
- ❌ 路由定义变长 (每个 route 加 Use)

### D) 第三方 (e.g. github.com/ulule/limiter)
- ✅ 算法成熟 (token bucket / sliding window)
- ✅ Redis 后端天然分布式
- ❌ 引入新依赖
- ❌ 配置文件增加 1 个 section

## 推荐

**选 C (per-route middleware) + 未来 D 升级路径**:
- v1.1: 实现 C，单一 token bucket 在内存 (per-IP, 简单)
- v2.0: 切 D (Redis 后端，多实例共享限流)

## 决定推迟

本 ADR 留作参考。Rate limit 实现本身估时 ~2h (含测试)，
**不在 v1.1 范围内** — 等 v1.1 tag 后单独排期。

## 落地结果 (v1.4.0)

- ✅ 选 C (per-route middleware via `IRoutes.Use`), 实现于 `internal/middleware/rate_limit.go`
- ✅ 18 routes 在 protected group 接入 (`backend/internal/router/routes.go`)
- ✅ 滑窗算法 + 内存 store, per-IP + per-path
- ✅ 路由级策略: login 5/min, password reset 3/min, protected 100/min
- ✅ 标准 headers: `X-RateLimit-Limit` / `Remaining` / `Reset` / `Retry-After`
- ✅ 11 tests PASS (sliding window / 超限 429 / 不同 IP 独立 / 窗口过期 / KeyFunc / 并发 / GC)
- ✅ D 升级路径保留: 未引 `ulule/limiter` / `go-redis`, 内存 store 可换 Redis 后端 (v2.0)

### 决策复盘

- 估时 3h (审计报告) → 实测 ~1.5h (2x 加速)
- 与 ADR 推荐完全一致, 无意外
- 中间件抽象足够灵活, v2.0 换 Redis 后端无需改业务代码

## 关联

- `internal/middleware/` 现有: AuthMiddleware / CORS / RequestID
- 待加: RateLimitMiddleware (v1.2+)
- 文档参考: 优化路线图.md §M1 P2
