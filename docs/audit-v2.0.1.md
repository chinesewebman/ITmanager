# ITmanager v2.0.1 分模块代码审计报告

**审计耗时**: ~50min | **审计范围**: 4 模块 / 23 源文件 / ~8400 LOC | **基准**: HEAD `3dd38d3`（v2.0.1）

## 🎯 评分总览

| # | 模块 | 评级 | 主要 P1 缺口 |
|---|---|---|---|
| 1 | `internal/middleware/` | **B+** | resourceFromPath 路径解析 bug |
| 2 | `internal/cursor/` | **A** | — |
| 3 | `internal/eventbus/` | **B-** | handler panic 不 recover |
| 4 | `internal/service/` | **B** | oncall DeleteSchedule 非事务 |
| **整体** | | **B+** | 跨模块 4 P1，0 安全 P1 |

## 🔴 P1 必修（4 项）

| # | 模块 | 问题 | 估时 | 风险类型 |
|---|---|---|---|---|
| 1 | middleware | `resourceFromPath` 动态段路径解析 bug | 15min | 审计元数据 |
| 2 | eventbus | handler panic 不 recover，worker 死亡 | 20min | 稳定性 |
| 3 | eventbus | Stats.Subscribers 监控失真 | 10min | 可观测性 |
| 4 | service | oncall DeleteSchedule 非事务 | 15min | 数据完整性 |

**P1 总估时**: ~60min

## 🟡 P2 改进（13 项）

| 模块 | 数量 | 主题 |
|---|---|---|
| M1 middleware | 4 | rate limit 单例 / UUID fallback / logger 一致性 / tracker 可重入 |
| M2 cursor | 1 | benchmark test |
| M3 eventbus | 3 | 指数退避 / payload 大小限制 / stats 命名 |
| M4 service | 4 | logger 一致性 / publish err 日志 / List stats opt-in / 事务审计 |

**P2 总估时**: ~2.5h

## 🟢 已做对（跨模块亮点）

| 类别 | 实现 |
|---|---|
| 错误处理 | `ErrNotFound`/`ErrInvalidInput`/`ErrTooManyItems` 类型化统一 |
| 性能 | Bulk SQL / FILTER+SUM / cursor 二元组 / 异步批写 |
| 安全 | JWT HS256 + 常量时间 compare + CORS 白名单 + rate limit |
| 可观测 | Prometheus metrics + slog 结构化日志 + audit log |
| 架构 | Interface + struct + DI 模式统一；可选 Bus 注入（`WithBus`） |
| 测试 | 17/17 service 全配对、sqlmock 覆盖 gorm query、100 并发压测 |

## 📋 修复路线图

| 阶段 | 范围 | 状态 |
|---|---|---|
| **v2.0.2 patch** | P1 4 项 (M1-1, M3-1, M3-2, M4-1) | ✅ 已完成（commit dc6fe58 / bee3b17 / db75568） |
| v2.1.0 minor | P2 13 项，分批实施 | 待办 |
| v2.2.0 minor | service 多步写入事务全面审计 (M4-5) | 待办 |
| v3.0 major | 分布式 event bus (NATS/Kafka) + 跨实例 session | 长期 |

## M1: `internal/middleware/` (B+)

**P1**: `audit.go:138-149` `resourceFromPath` 对 `/api/:id/sub` 这类动态段在前路径返回 `""`（审计语义丢失）— ✅ 已修
**P2 (4)**: rate_limit 单例 / UUID fallback / logger 一致性 / tracker 可重入
**亮点**: JWT HS256 + 常量时间 API key compare / API key last_used_at 异步批量写 / 滑动窗口 rate limit + Retry-After

## M2: `internal/cursor/` (A) — 🏆 标杆模块

64 LOC，11 test cases，100% 行覆盖。NUL 分隔符避免 RFC3339Nano 的 `:` 冲突（专门防退化测试）；纳秒精度保留；时区归一 UTC；ErrInvalid 错误包装 + 客户端降级路径明确。

## M3: `internal/eventbus/` (B-)

**P1 (2)**: handler panic 不 recover / Stats.Subscribers 监控失真 — ✅ 已修
**P2 (3)**: 指数退避 / payload 大小限制 / stats 命名
**亮点**: 架构图注释清晰 / DLQ 落 SQLite / 100 并发 Publish 测试 / Close 幂等 + drain

## M4: `internal/service/` (B) — 业务核心

**P1**: `oncall_service.go:64-77` `DeleteSchedule` 两步 DELETE 无事务 — ✅ 已修
**P2 (4)**: logger 一致性 / publish err 日志 / List stats opt-in / 事务审计
**亮点**: 17 文件全检 ctx 透传 0 漏 / 错误类型化统一 / Bulk SQL 无 N+1 / FILTER+SUM 兼容 / cursor 二元组 / 17/17 测试文件全配对

## 后续建议

- v2.1.0 minor: P2 13 项分批实施
- v2.2.0 minor: 全 service 多步写入事务审计（M4-5）
- v3.0 major: 分布式 event bus 评估（NATS/Kafka）
