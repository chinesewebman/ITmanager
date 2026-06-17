# ADR-0002: v2.0 范围与技术决策

- 状态: **proposed** (2026-06-17)
- 日期: 2026-06-17
- 决策者: 主人 (2077 Ling) + 助手
- 关联: [v1.4.0 release](https://github.com/chinesewebman/ITmanager/releases/tag/v1.4.0), [roadmap 13-实施规划](../13-实施规划.md)

## 背景

v1.0.0 → v1.4.0 共 38 commits / 8 tags 落地,核心 CRUD + 监控 + 工单 + 通知 + 限流 + 缓存 + audit 全跑通。

主人 2026-06-17 明确: **目前不需要分布式部署**。单机 LRU 缓存 + 单实例通知 worker 够用。规模 (3 机房/1000+ VM) 不需要拆分。

v2.0 解决的是 v1.4 暴露出来的**性能与解耦**问题,非分布式问题。

## 决策

| 域 | v2.0 决策 | 不做 (deferred) |
|---|---|---|
| 分页 | **cursor 分页** (server-driven) | offset/limit 保留兼容老接口 |
| 通知 | **event bus** (in-process pub/sub) | Kafka / NATS (单机不需要) |
| 内部通信 | **gRPC** (service-to-service) | 外部 REST API 不变 |
| 缓存 | **in-process LRU + TTL** 保留 | Redis 部署 |
| 部署 | **单机 + 进程内调度** | 多实例 + 服务发现 |

## 详细范围

### 1. cursor 分页 (估 6-8h)

**动机**: v1.x 全用 `offset + limit`, 100k 行 alerts 翻到末页要 O(N), P95 > 2s.

**方案**:
- `cursor = base64(timestamp + id)`, 服务端用 `WHERE (created_at, id) < (?, ?) ORDER BY created_at DESC, id DESC LIMIT ?` 二元组比较
- 索引: `(created_at DESC, id DESC)` 联合索引
- 不破坏现有 `page=N&size=M` API — 加 `cursor` 字段, 任一存在即可
- 适用: alerts / audit_logs / tickets / notifications

**不做**:
- ❌ search_after (ES 风格, 复杂)
- ❌ 加密 cursor (无安全需求)

### 2. event bus (估 8-10h)

**动机**: v1.4 通知是 sync call (alert 创建 → send notification), 失败重试耦合在 service 层。

**方案**:
- `internal/eventbus/` 包: in-process `chan Event` + 多个 subscriber
- 事件类型: `alert.created`, `alert.resolved`, `ticket.created`, `ticket.resolved`, `user.locked`
- 通知 worker 改 subscriber, 失败入 dead-letter 队列 (SQLite 表)
- 监控: pending queue size, event/sec, subscriber lag

**不做**:
- ❌ 跨进程 pub/sub (无分布式需求)
- ❌ 持久化 event log (audit_logs 表已记录)

### 3. gRPC (估 12-15h)

**动机**: 内部 handler → service → db 全 HTTP 链路, 加 s2s 通信 1 次 RTT 多 50ms, batch 操作 (BulkResolve) 慢。

**方案**:
- 新增 `internal/grpc/` 包, `proto/*.proto` 定义 service
- handler 不直接调 service, 通过 gRPC client (localhost)
- REST → gRPC 双协议并存, REST 走 gRPC-gateway 反向代理
- 范围: alert / ticket / asset (高频) — 不含 channel / postmortem (低频)

**不做**:
- ❌ 跨机器 gRPC (单机)
- ❌ mTLS (无对外暴露)

### 4. Redis 接口保留 (不动)

v1.4 cache 已有 `Cache` interface 抽象 (`Get/Set/Delete`), in-process LRU 实现。v2.0 不部署 Redis, 但**接口兼容** v3.0 切换零成本 (备 v2.0 → v3.0 path)。

## 影响范围

| 改动 | 文件 | 风险 |
|---|---|---|
| cursor 分页 | `internal/service/{alert,ticket,notification}_service.go` + 4 handler | 低 (兼容老 API) |
| event bus | `internal/eventbus/` 新包 + notification/ + service/ 改发事件 | 中 (改同步链) |
| gRPC | `internal/grpc/` 新包 + proto/ + handler/ 改调 | 高 (协议切换) |

## 兼容性

- v1.x 客户端可继续调 `?page=N&size=M` (默认 cursor=null 走 offset 兼容模式)
- 老 DB schema 不变 (无需 migration)
- 部署方式不变 (单 binary + sqlite/postgres)

## 拒绝的理由 (NOT doing)

| 想做 | 拒绝 | 理由 |
|---|---|---|
| Redis 缓存 | 推迟到 v3.0 | 单机 LRU 命中率 > 90%, 复杂度不值 |
| Kafka/NATS | 永久不做 | 单机 in-process chan 性能 > 100k events/s 足够 |
| 多实例部署 | 推迟到 v3.0 | 当前规模 (单团队) 不需要 |
| 全文搜索 (ES) | 推迟到 v3.0 | LIKE 查 < 10k 行够用 |
| PWA 移动端 | 推迟到 v2.1 | 优先后端能力 |

## 时间线 (估时, 3 段)

| 段 | 内容 | 估时 |
|---|---|---|
| **v2.0.0** | cursor 分页 + event bus | 14-18h |
| **v2.0.1** | gRPC 内部通信 | 12-15h |
| **v2.0.2** | 实测压测 + 优化 + release | 4-6h |
| **总** | | **30-40h** |

实测历史: v1.x 估时 ~76h → 实测 ~28h (2.7x 加速). v2.0 实测估 ~10-15h.

## 关联决策

- ADR-0001 (rate limit 归属) — v1.4 落地 ✅
- ADR-0003 (cursor 分页方案选择) — v2.0 写时补
- ADR-0004 (event bus 持久化策略) — v2.0 写时补

## 待办

- [ ] cursor 分页实施
- [ ] event bus 实施
- [ ] gRPC 实施
- [ ] 压测 + v2.0.0 release
