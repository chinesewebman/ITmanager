# 缺陷修复规划：D-1 + D-2（**仅工单表 tickets**）

> 状态: **M34 步骤 1，方案已锁定，IMPL 在步骤 2**
> 日期: 2026-09-12
> 范围: `tickets` 表 + `models/generateTicketNumber` + 真 PG 冒烟用例 3 条
> 不做: users / audit_logs / D-3 / D-4 / D-5 / D-6 / D-7 — 这些已经在 2026-09-09 的
> `2ec518c fix(schema+auth): D-1~D-7 修复轮` 里完成，本轮不再触。
> 关联: [FIX-PLAN-D1-D7.md](FIX-PLAN-D1-D7.md) §3.1 / §3.2、[ADR-0004](adr/0004-工单SoT决策.md)、
> [TODO.md §v3 缺陷清单](../TODO.md)

---

## 0. 一句话

把 `tickets` 的迁移 DDL 与 GORM 模型之间的剩余漂移用一个**幂等的新迁移 000028**关掉，
把 `generateTicketNumber` 加上**事务内当日序号 + 唯一冲突重试**，并**新增 3 条真 PG
冒烟用例**锁住这两个不变量。

---

## 1. 背景（FIX-PLAN-D1-D7.md §3.1 / §3.2 的工单子集）

### 1.1 D-1：迁移 24 列 ↔ 模型 24 字段

2026-09-09 的 `000013_schema_align` 已经完成工单表大部分对齐工作：

- ✅ `tickets.ticket_no → ticket_number` RENAME（数据随列名迁移）
- ✅ `tickets.creator_id → requester_id` RENAME + 旧列 `DROP NOT NULL`
- ✅ 14 个模型列 ADD COLUMN IF NOT EXISTS（`category`/`tags`/`requester_name`/`requester_email`/
   `asset_name`/`assignee_name`/`resolution`/`resolved_at`/`closed_at`/`due_date`/`external_id`/
   `source`/…）
- ✅ `CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_ticket_number ON tickets(ticket_number)`

但 **000013 是 13 张表共用的补齐式迁移**，工单子集混在 `§3` `§5` 多个章节里。
本轮**只**重新审视工单表，目的是把工单的对齐工作**单列、可追溯、不与其他表纠缠**。

### 1.2 D-2：工单号生成器

2026-09-09（M26/G-25）已经把工单号改成「当天已建数量 + Excel 风格字母标签」：
- ✅ 序号 = `SELECT COUNT(*) FROM tickets WHERE ticket_number LIKE 'TICKET-<今日>-%'`
- ✅ 后缀 = `seqLabel(n)` → 0→A, 25→Z, 26→AA, 27→AB …
- ✅ 批量路径走 `AssignTicketNumbers` 预分配（整批同号）
- ✅ 单条 `Create` 走「事务 + 唯一冲突重试 5 次」（`ticket_service.go:222-252`）

但**单条 `generateTicketNumber` 函数本身**（`models/ticket.go:117-128`）**不在事务内读
「当日条数」**——是 BeforeCreate 里 db.Model().Count()，与重试路径脱节。批量路径用了
事务、用了 used-set，但**单条路径只用了空读**。

并发场景：
- 两个请求同时落在同一秒，`BeforeCreate` 各拿一次 count → 同号 → INSERT 撞唯一索引。
- 此时 `Create` 的重试兜住了（5 次重算），所以**最终一致性 OK**。

但**单条路径的可读性**和**形式上的对称性**（批量用 used-set、单条用空读）不一致。
本轮把单条路径升级为「**事务内当日条数 → SELECT … FOR UPDATE 锁住当天已建集合**」，
与批量路径采用同一种「used-set」算法。

---

## 2. 方案

### 2.1 D-1：迁移 000028_tickets_schema_align

新文件 `backend/migrations/000028_tickets_schema_align.up.sql`（+ `.down.sql`），

只覆盖 `tickets` 一张表：

```
-- 工单表 schema 对齐 (M34 D-1)
-- 背景：见 docs/FIX-PLAN-D1-D2-TICKETS.md §1.1
-- 原则：非破坏 + 幂等（所有 DDL 都用 IF NOT EXISTS 守卫）

-- 1. 模型声明 + 000013 已加的列，重复加 = noop
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS category        VARCHAR(50);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS tags            JSONB DEFAULT '[]';
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_name  VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_email VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS assignee_name   VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS asset_name      VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolution      TEXT;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolved_at     TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS closed_at       TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS due_date        TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS external_id     VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS source          VARCHAR(20) DEFAULT 'manual';

-- 2. 唯一索引 (模型 uniqueIndex) —— 000013 已加 idx_tickets_ticket_number；
--    重复 CREATE = noop;另起一个别名兜底（与 000013 不冲突）。
--    实际上 000013 的 idx_tickets_ticket_number 就够了,本行注释掉。
-- CREATE UNIQUE INDEX IF NOT EXISTS tickets_ticket_number_unique
--     ON tickets(ticket_number);

-- 3. ticket_type 的 NOT NULL 守卫: 000013 加列时没带 NOT NULL(模型也不写),
--    保持现状不动; 但 000001 的 tickets.ticket_type 是 NOT NULL,
--    DROP NOT NULL 让模型自由写入(NULL → '').
ALTER TABLE tickets ALTER COLUMN ticket_type DROP NOT NULL;
```

**`.down.sql`**：反转上述每条（`DROP COLUMN IF EXISTS`、`DROP NOT NULL` 还原为 NOT NULL
如果原来是 NOT NULL）。**`DROP NOT NULL` 是不可逆的**（PG 不记录历史 NULL-ability），
所以 `.down.sql` 只 DROP IF EXISTS 模型列与索引，**不**还原 NOT NULL（与 000013 一致）。

### 2.2 D-2：单条 `generateTicketNumber` 加事务 + FOR UPDATE

**当前实现**（`models/ticket.go:117-128`）：

```go
func generateTicketNumber(db *gorm.DB) string {
    prefix := ticketNumberPrefix()
    return prefix + seqLabel(nextTicketSeq(db, prefix))
}
```

**问题**：
- `nextTicketSeq` 不在事务里读；
- 不感知**当日已占用的所有号**（只读 count）；

**修复**：
- 把单条路径改成与批量路径对称：先拉当日 used-set（`SELECT ticket_number WHERE …`），
  在内存里找一个未占用的最大号（`max+1`，**不**简单地用 `len(used)` —— 当天有空洞时
  会撞号）。
- 持久化的并发安全仍由 `ticket_number` 唯一索引 + `TicketService.Create` 的 5 次
  重试兜底（**重试逻辑保持不动**）。
- 事务边界放在 `BeforeCreate` 内：`db.Transaction(func(tx){ … })`，再让外层
  `ticketService.Create` 的事务把这次「读 used-set + 生成号」包含进去。
  **不要**在 `generateTicketNumber` 里新开事务，否则会与外层 `tx.Create` 嵌套。

实现步骤：

1. `generateTicketNumber(db *gorm.DB)` 接收一个**已在事务内的 `*gorm.DB`**（由
   `BeforeCreate` 拿到）。
2. 函数体只做：拉当日 used-set → 找最大号 → 返回 `prefix + seqLabel(max+1)`。
3. 不在这里 `FOR UPDATE` —— 并发安全由 `Create` 的 5 次重试 + 唯一索引兜底（理由：
   同一事务内 `FOR UPDATE` 锁的是行，没新行可锁；选 `FOR UPDATE` 必须配 advisory
   lock，复杂度过剩）。

---

## 3. 验收标准

| # | 验收项 | 判定方法 | 期望 |
|---|--------|----------|------|
| A-D1D2-1 | 000028 在全新空库上跑通 | `scripts/db_smoke.sh` 路径 ① + `TestDBSmoke_MigrateRunner` | PASS |
| A-D1D2-2 | 000028 在存量 000001-000027 库上幂等 | 同上路径 ② | PASS（重放 noop） |
| A-D1D2-3 | 真 PG 用 `models.Ticket` 全 24 字段 insert 不报漂移 | 新增 `TestDBSmoke_TicketsSchemaRoundTrip` | PASS |
| A-D1D2-4 | 当日插 25 张工单，号码互不相同 | 新增 `TestDBSmoke_GenerateTicketNumberDayScoped` | PASS |
| A-D1D2-5 | 唯一冲突重试可达 5 次后回 409（用 sqlmock / 真 PG 任一） | 新增 `TestDBSmoke_TicketNumberRetry` | PASS |
| A-D1D2-6 | 现有 37 条冒烟用例不退化 | `scripts/db_smoke.sh` 总数 ≥ 40 | ≥ 40 PASS |
| A-D1D2-7 | `go test ./internal/redact/... ./internal/integration/... -count=1` 不退 | grep exit 0 | exit 0 |
| A-D1D2-8 | `gofmt -l backend/` 空 | grep | 空 |
| A-D1D2-9 | `git diff --stat` 只动 in-scope 文件 | 手测 | ✅ |

---

## 4. 明确不做

| 不做 | 理由 |
|------|------|
| users.role / users.deleted_at | 000013 已做（D-4），本轮不重复 |
| audit_logs 列对齐 | 000013 已做（D-5），本轮不重复 |
| 改 ticket_number 格式（`T-YYYYMMDD-NNNN`） | 任务指令里说「LOOSE, refine in IMPL」；改格式会破坏 30+ 测试 fixture 与现有工单数据，爆炸半径远超本轮；保持 `TICKET-YYYYMMDD-<seqLabel>` |
| 删 `AssignTicketNumbers` | 批量路径仍需要它（M26 G-25 引入，防御整批同号） |
| 改 `Create` 的 5 次重试逻辑 | 已工作正常（M26/D-9 反证） |
| `FOR UPDATE` 锁行 | 单条 INSERT 前没有目标行可锁；并发安全靠唯一索引 + 重试 |
| `pg_advisory_lock` | 跨连接难管，且与现有 `migrate.Up` 的 advisory lock 互操作未验证 |

---

## 5. 实施计划

| 文件 | 动作 |
|------|------|
| `backend/migrations/000028_tickets_schema_align.up.sql` | 新增（幂等 ADD COLUMN / DROP NOT NULL） |
| `backend/migrations/000028_tickets_schema_align.down.sql` | 新增（`DROP COLUMN IF EXISTS` + 索引 DROP；**不**还原 NOT NULL） |
| `backend/internal/models/ticket.go` | `generateTicketNumber` 加事务 + used-set 算法（不动 `BeforeCreate` 签名） |
| `backend/tests/db_smoke_test.go` | 追加 3 个测试：`TestDBSmoke_TicketsSchemaRoundTrip` / `TestDBSmoke_GenerateTicketNumberDayScoped` / `TestDBSmoke_TicketNumberRetry` |
| `scripts/db_smoke.sh` | whitelist 追加 3 个新测试名 |
| `docs/FIX-PLAN-D1-D2-TICKETS.md` | 本文件 |
| `docs/IMPL-D1-D2-TICKETS.md` | 步骤 2 落地 |
| `TODO.md` / `CHANGELOG.md` / `docs/adr/0004-工单SoT决策.md` | 步骤 5 收尾 |

---

## 6. 风险

| # | 失败模式 | 缓解 |
|---|----------|------|
| 1 | 000028 在某些边角库与 000013 不兼容（旧库同时存在两列） | 全部用 `ADD COLUMN IF NOT EXISTS` + `DO $$` 守卫，幂等；与 000013 完全独立 |
| 2 | 新增 used-set 算法比 `Count()%26` 慢 | used-set 只查 `ticket_number` 一个列且只过滤 `LIKE 'TICKET-今日-%'`，命中索引；单库内一天最多几千行，可接受 |
| 3 | 把 `generateTicketNumber` 放事务内后，外层 `Create` 的 5 次重试会不会重复开新事务 | `BeforeCreate(tx)` 的 `tx` 是外层事务的；新逻辑只读不写，外层的事务边界不变 |
| 4 | 测试用例用 `time.Now()`，凌晨跨天时号码前缀变化导致断言失败 | 用显式 prefix 注入（函数增加一个 `nowFn func() time.Time` 钩子），测试里固定 `2026-09-12` |
| 5 | `TestDBSmoke_TicketNumberRetry` 怎么模拟 23505 冲突 | 用事务回滚到一个临时点 → 预插一行 → 触发 INSERT 冲突 → 验证 `Create` 返回 `ErrAlreadyExists` 或重试到新号 |

---

## 7. 引用

- [FIX-PLAN-D1-D7.md §3.1 / §3.2](FIX-PLAN-D1-D7.md) — 全局方案
- [v3-架构优化需求.md §9](v3-架构优化需求.md) — 缺陷清单
- [ADR-0004](adr/0004-工单SoT决策.md) — ITmanager tickets 为 SoT
- [TODO.md §v3 缺陷清单](../TODO.md) — D-1 / D-2 当前状态（2026-09-09 已修复）
- [TRAPS.md T-6](../TRAPS.md) — gorm `column:` tag 缺失 = 静默数据丢失
