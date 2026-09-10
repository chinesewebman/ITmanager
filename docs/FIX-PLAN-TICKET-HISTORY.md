# 需求文档：工单经手历史（`ticket_history`）+ `resolved_at` 随状态收口（M25）

> 状态: **需求已审查**（两路对抗审查 26 组发现已逐条处置，见 §7）
> 日期: 2026-09-11
> 基线: `main` @ 453c4dd（M24 已收口）
> 依据: `docs/FIX-PLAN-UI-PERF.md` §8 台账 1.14「未做」②（`resolved_at` 语义待拍板）
> 决策（2026-09-11 燕如拍板）:
> ① 走 **A 方案** —— 新建 append-only 工单历史表（不复用/扩宽 `audit_logs`）
> ② 粒度 **宽一档** —— 字段级，含 assignee / priority / resolution 等，不只状态跃迁
> ③ 可见性 **跟工单本身** —— 不另收口到 `canAudit`
> 另一条同轮拍板: `resolved_at` 按**「当前状态」语义**（重开清空），但**历史必须完整保留**

---

## 0. 一句话

「重开清空 `resolved_at`」会丢掉「谁在何时解决的」，而全仓**没有任何领域级留痕**兜底 —— 所以「清理」与「留痕」必须同轮落地，不能只做一半。本轮建 `ticket_history`（append-only，形状沿用仓里**已设计但从未启用**的 `asset_history`），写入点接在 `TicketService.Update` 的**原始行字段级 diff** 与 `Create` 的出生记录上；随后把 `resolved_at` 纳入与 `closed_at` 同一套「随 status 走」的 SQL 表达式。

---

## 1. 现状（实测，2026-09-11；行号已由独立审查复核）

### 1.1 `resolved_at` 的全部读写方

| 角色 | 位置 | 说明 |
|---|---|---|
| 模型 | `models/ticket.go:31` | `ResolvedAt *time.Time` |
| **写 · 建单回灌** | `TicketService.Create` 绑整模型（`POST /tickets`） | 调用方传了就落库 |
| **写 · 裸 map** | `PUT /tickets/:id`（M17 的**禁改集合**只挡 `id`/`ticket_number`/`created_at`/`updated_at`，`ticket_service.go:327-338`） | 可显式写 `resolved_at` |
| 读 · 资产时间线 | `diagnostic_service.go:143,158-165` | `if t.ResolvedAt != nil` → 发 `SubKind:"resolved"` 的「工单已解决」事件 |
| 读 · 演示数据 | `cmd/seed/main.go:281-282` | 直接给值 |
| 解析但被丢弃 | `integration/glpi.go:171` | `ConvertToTicket` 解析了 `SolvedDate`，但 `integration/service.go:247` 构 model 时不带它（已登记，见 §8-1.14 未做①） |

**关键事实：`TicketService.Update` 完全不碰 `resolved_at`。** M24 只统一了 `closed_at`（`ticket_service.go:438-466`）。所以「重开残留 `resolved_at`」目前**只能由对接方显式写进来**才发生 —— 缺陷面比 `closed_at` 小，但同样是「时间戳与 status 不一致」，且一旦发生就永久留在资产时间线上。

### 1.2 「谁经手」现有留痕：实测为零

| 机制 | 实际记录什么 | 判定 |
|---|---|---|
| `audit_logs` 中间件（`middleware/audit.go:46`，挂在 `routes.go:242` 的 `protected` 组，覆盖 `PUT /tickets/:id`） | 谁（`user_id`/`username`）、何时、什么端点、状态码、IP/UA | ❌ **不记改了什么** —— 审计行只说「某人 PUT 过这张工单」，看不出他把 status 改成了 `resolved` |
| `audit_logs` 的 `changes` / `old_values` / `new_values` 列（`000001_init.up.sql:1100-1102`） | DDL 里有，但 `models.AuditLog`（`models/user.go:101-116`）**没有这三个字段** → gorm 永不写 | ❌ 想复用要先动模型 + 迁移 |
| `asset_history`（`000001_init.up.sql:280`） | **全仓零 Go 代码引用**（真 grep 复核） | ❌ 死表 —— 但它是本仓**为「谁改了什么」设计过的形状**，本轮沿用 |
| `step_progress_history`（`000001_init.up.sql:743`） | 同样零引用 | ❌ 死表 |
| `tickets.assignee_id` / `assignee_name` / `resolution` | 只有**当前值**，每次跃迁覆盖 | ❌ 无历史 |

**结论：清空 `resolved_at` 会真的丢「谁在何时解决的」。** 这就是 §0 说的「必须同轮做」。

### 1.3 落点事实（决定了实现形状）

| 事实 | 值 | 影响 |
|---|---|---|
| `TicketService.Update` 生产调用方 | **1 处**：`ticket_handler.go:139` | 改签名代价低 |
| 测试调用点 | **13 处**（`ticket_service_test.go:126,201,214,322,334,529,560,582,605,717,740,753,766`）+ **接口 mock 1 处**（`rack_ticket_handler_test.go:124,138` 的 `updateFunc`/`Update`） | 接口加 `ListHistory` 后 mock 也要同步补，否则编译红 |
| 迁移编号 | ...000021、**000023**、000024 —— **000022 缺号**（被本 plan 的 P20 `pg_trgm` 预占、尚未落地，见 `db_smoke_test.go` 注释） | 本轮用 **000025**；`internal/migrate/migrate.go:103-160` 的 `Load()` 用 `byVer` map 排序、**不检查连续性**（缺号容忍）→ 补 000022 会让已部署库多跑一个「新」迁移，不做 |
| actor 在 ctx 里的可用形态 | `user_id`（claims.UserID 字符串）、`username`、`role`、`api_key_id`（`middleware/auth.go:120-122,194-197`） | handler 两者都能取到 |
| 传 actor 的既有先例 | `assetService.Retire(ctx, id, reason, userID uuid.UUID)`（`asset_service.go:178`）、`alertService.Acknowledge(ctx, id, userID string)`（`alert_service.go:291`） | 显式参数是本仓既定模式 |
| 服务端单测基座 | in-memory sqlite（`newTicketSQLiteDB`） | 见 §2.4 的行锁兼容性 |
| 工单删除路径 | **不存在**（无 DELETE 端点；真 grep 复核：`routes.go` 的 DELETE 只有 apiKeys/assets/alert-rules/channels/suppressions/oncall×3/runbooks，生产代码零 `Unscoped()`、零 `Delete(&models.Ticket`） | 历史不会被应用层删除 |

### 1.4 建单路径有四条 —— 覆盖不到的要写明（不许静默漏）

| # | 路径 | 是否经过 `TicketService.Create` | 本轮覆盖 |
|---|---|---|---|
| 1 | `POST /tickets` | ✅ | ✅ |
| 2 | `CreateFromAlert`（告警一键建单，`ticket_service.go:207` **直接 `tx.Create`**） | ❌ 绕开 | ✅ 显式补写（同事务） |
| 3 | GLPI 同步（`integration/service.go:247` 构 model → `:271` `CreateInBatches`） | ❌ 绕开 | ❌ **本轮不做**（见下） |
| 4 | `cmd/seed` 演示数据（`cmd/seed/main.go:278`） | ❌ 绕开 | ❌ 不做（演示数据无经手语义） |

**路径 3 的措辞纠正**（审查发现）：`integration/service.go:245` 的注释写着「工单状态走 PATCH 更新，不在同步阶段覆盖」，但 **全仓没有任何 PATCH 路由**，`TopicTicketCreated`/`TopicTicketResolved`（`eventbus.go:50-51`）只有常量、无发布方无订阅方。即 GLPI 侧**既不建历史，也不更新已存在工单** —— 这是数据缺口，不是单纯的「历史缺口」。

---

## 2. 设计

### 2.1 表结构（沿用 `asset_history` 形状，加 `kind` 与批次）

```sql
CREATE TABLE ticket_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    batch_id    UUID NOT NULL,                       -- 一次请求 = 一个批次（N 行共享）
    kind        VARCHAR(20) NOT NULL,                -- created | updated
    field_name  VARCHAR(50),                         -- kind=updated 时非空；created 时为 NULL
    old_value   TEXT,
    new_value   TEXT,
    actor_id    UUID,                                -- 无 FK，见下
    actor_name  VARCHAR(100),                        -- 快照：用户改名/删号后仍可读
    source      VARCHAR(20),                         -- 开放值（manual/email/api/glpi/alert/zabbix）
    request_id  VARCHAR(50),
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ticket_history_ticket ON ticket_history(ticket_id, created_at DESC, id DESC);
```

**为什么一字段一行而不是 `changes JSONB` 数组**：`asset_history` 就是本仓为这件事设计过的形状（`field_name`/`old_value`/`new_value`/`operator_id`/`operator_name`/`created_at`），沿用它可以少发明一套约定；`audit_logs` 那种 JSONB 数组是**HTTP 级**留痕的用法（且同样没被实现）。代价是「一次操作 = N 行」，靠 `batch_id` 分组还原。

**与 `asset_history` 的有意差异**（逐条说明，避免被当成疏漏）：
- 加 `kind`：`asset_history` 没有「出生」概念，工单有（建单即是一次经手）。
- 加 `batch_id`：没有它，UI 只能靠「`created_at` 相同」猜分组 —— 同秒的两次操作会并成一次。
- 加 `source`：工单有 `source` 列，「经手」的来路要能分辨。⚠️ `models/ticket.go:29` 注释写 `manual, email, api, glpi`，但代码里实际还有 `alert`（`ticket_service.go:281`）与 `zabbix`（`cmd/seed/main.go:281`）→ **按开放值处理**，不校验枚举。
- **`actor_id` 不建外键**（`asset_history` 与 `audit_logs` 也都不建）：这是本轮唯一现实的「历史插入失败」来源 —— JWT 路径只信 claims、**不复查用户是否仍存在**（`middleware/auth.go:112-124`），带外删号后未过期 token 仍带 `user_id`，若建 FK 则插入报错 → **该用户此后每次改工单都 500**。去掉 FK 后历史插入在构造上不可能失败（§2.4 的「失败即回滚」策略才敢成立），并与 `audit_logs.resource_id`（裸 UUID，`000001:1089`）一致。
- 索引按 `(ticket_id, created_at DESC, id DESC)`：与 alert/ticket 的 `created_at DESC, id DESC` 全序约定一致（T-45：无 `ORDER BY` 的「最新一条」没有定义）。

**与既有迁移约定的对照**（审查逐条核过）：主键 `UUID DEFAULT gen_random_uuid()` ✓（PG13+ 内置，全仓 40+ 处直接使用、无 `CREATE EXTENSION`）；`TIMESTAMP` ✓（`000001` 里 84 处，与 `tickets`/`asset_history` 同）；索引命名 `idx_<表>_<列>` ✓（同 `000001:292`）。`created_at` 由 gorm 的 `NowFunc: time.Now().UTC()`（`database.go:44-46`）填，`DEFAULT NOW()` 仅兜底 —— 与既有 `TIMESTAMP` 列同源的已知取舍。

**已知取舍（登记，不修）**：`ticket_id` 的 `ON DELETE CASCADE` 与 append-only 承诺相抵 —— 若将来加工单硬删端点（`assets` 有先例：`routes.go:295` `DELETE /:id` 硬删不可逆），历史会被级联抹掉。当前无删除路径故不可达；**模型注释里写明「加工单删除端点时必须重新评估这里」**。备选是学 `audit_logs` 用裸 UUID（删工单留历史），但那样历史行失去 join 目标、读端点 `/tickets/:id/history` 也不可达，收益为零 —— 故选 CASCADE。

### 2.2 写入点与 actor 传递

**签名变更**（`service` 接口 + 实现 + 1 handler + 13 测试 + 1 mock，见 §1.3）：

```go
// 现在
Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error)
// 改为
Update(ctx context.Context, id string, updates map[string]interface{}, actor Actor) (*models.Ticket, error)

// 新增（service 包）
type Actor struct {
    ID   *uuid.UUID // 可能是 API Key / 系统调用，取不到就是 nil
    Name string     // 展示用快照
}
```

传 `Actor` 而不是两个裸 string：需要 **id（稳定）+ name（可读快照）** 两个字段，13 个调用点乘 2 个位置参数会让签名难读且容易传反。`*uuid.UUID` 而不是 `uuid.UUID`：API Key 路径与内部调用拿不到 user id，用零值 UUID 表示「无」会和真实零值混淆。

handler 侧（`ticket_handler.go:139`）：
```go
actor := service.Actor{Name: c.GetString("username")}
if id, err := uuid.Parse(c.GetString("user_id")); err == nil {
    actor.ID = &id
}
```
**注意**：`CreateTicketFromAlert` 现在取的是 `c.GetString("username")`（`ticket_handler.go:107`，空则 `"unknown"`，`:107-110`）—— 与 `audit_logs` 的 `username` 同源，抽一个小 helper 复用同一段解析，避免两处各自 parse 而分叉。

### 2.3 diff 怎么算：**原始行 map 的 pre/post 比较**

三个候选，前两个都有实测坑：

| 方案 | 做法 | 判定 |
|---|---|---|
| i. 从 `updates` map 算 | 记调用方给的键值对 | ✗ 记的是**调用方写的形态**，不是落库值；`closed_at` 被换成 `clause.Expr`（`ticket_service.go:462`）Go 侧无法求值 |
| ii. struct pre/post 比较 | 用 `models.Ticket` 前后各读一次 | ✗ 两个坑，见下 |
| iii. **原始行 map pre/post** | `SELECT *` 成 `map[string]interface{}`，逐列比 | ✓ **选它** |

**方案 ii 的两个实测坑**（gorm v1.30.0 源码）：
1. **`Updates(map)` 会原地回写 struct**：`callbacks/update.go:13,151,221` —— `Model(&t).Updates(m)` 把 map 里的值 `field.Set` 回 `t`。用同一个 `t` 当 pre，等于拿 post 跟 post 比，**diff 恒空**。（M24 只登记了反方向：`clause.Expr` 那一列不回写。）
2. **struct 看不见「库里有、模型里没有」的列**：`tickets` 表有 `alert_id`/`attachments`/`progress`/`reviewer_id`/`assignee_group`/`cc_users`/`planned_start`/`planned_end`/`actual_start`/`actual_end`/`result`/`satisfaction`（`000001_init.up.sql:689-712`），`models.Ticket` 一个都没有；而 `{"alert_id": X}` 能绕过 M17 禁改集合**真的写库**（`callbacks/update.go:230-231` 的裸列兜底）→ **改了却不留痕**。
3. 附带：gorm 对 map 更新**无条件补 `updated_at`**（`callbacks/update.go:238-241`）→ 每次 PUT 必生一条伪历史，「空 diff 不写」不可达。

**方案 iii 的形状**：

```go
// 同事务、持锁读 pre
var pre map[string]interface{}
err := tx.Table("tickets").Clauses(clause.Locking{Strength: "UPDATE"}).
    Where("id = ?", id).Take(&pre).Error
// … 原 UPDATE（含 closed_at / resolved_at 表达式）…
var post map[string]interface{}
err = tx.Table("tickets").Where("id = ?", id).Take(&post).Error
// 逐列 diff → N 行 ticket_history（batch_id 相同），批量一次 INSERT
```

- 覆盖**全部库列**（含模型里没有的），把「谁改了哪个字段」从承诺变成事实。
- 字段名用 **DB 列名**（snake_case），与 `asset_history.field_name` 语义一致。
- **排除系统列**：`updated_at`（每次 PUT 必变）、`created_at`、`id`、`ticket_number`。排除集写进契约，并有用例钉住「无实质变化的 PUT → **0 行历史**」。
- 值比较用 `valuesEqual(a, b any) bool`：`time.Time` 用 `.Equal()`（别用 `==`，单调时钟/时区会骗人）、`[]byte`→string、nil 与空串**不**等同。
- 响应体仍需一次 struct 重读（`First(&fresh)`，M24 既有模式）→ 每次 PUT 共 **3 次按主键单行 SELECT**（pre / post / fresh），比 M24 多一次。当前按**最小代码**取三次；若将来压测显示有影响，可用 gorm schema 元数据把 post map 回填 struct 省掉这一次。
- **长文本截断**：`description`/`resolution` 等超过 500 字符时截断并标记（否则一次 PUT 会把整篇描述存 N 份）。截断只作用于 `old_value`/`new_value` 的**存储**，不影响 diff 判定。

### 2.4 事务、行锁、失败策略

diff 要正确，pre-image 必须**属于本次写入**。当前 `Update` 不在事务里，`First` 读到的是**无锁快照** —— 两个并发 PUT 会让第二条历史的 `from` 归错。

**`Update` 包事务，pre-image 用 `FOR UPDATE` 取：**

- **Postgres**：真行锁，同一张票上的并发 PUT 串行化，diff 的 `from` 精确。⚠️ **硬条件：必须与事务同用** —— 无事务时 gorm 照常渲染 `FOR UPDATE`，但 autocommit 下语句结束即释放，锁等于没用（不报错、不 panic，静默无效）。
- **sqlite（单测基座）**：`gorm.io/driver/sqlite@v1.6.0/sqlite.go:122-126` 在自己的 `"FOR"` builder 里对 `clause.Locking` **直接 return（不渲染）**，注释「SQLite3 does not support row-level locking」→ 单测不会因语法不支持而红；postgres driver 无 `"FOR"` 覆盖，走默认。
- ⚠️ **测试盲区，如实记**：单测因此**验不到**加锁行为。真 PG 冒烟能验「路径通」，但「并发不交错」是**由构造保证**（行锁语义）而非由测试保证；要真正验需要两条连接 + 时序控制，属独立小步。**不宣称有测试守住了这一点。**
- ⚠️ **与 M24 的关系（别误读成回退）**：M24 把 `closed_at` 判据下推到 SQL 是为了消除**无锁**的读-改-写窗口。本轮为记准 `from`/`to` 重新引入读-改-写 —— 但**在行锁保护下**，读与写属于同一次原子操作，不构成 M24 要治的那种竞态。
- **闭包内所有语句必须用 `tx` 而不是 `s.db`**：`s.db` 的 ConnPool 是 `*sql.DB` → gorm 会**另开一个事务**（历史与 UPDATE 不原子），sqlite `:memory:` 下还会拿到新连接 → 新空库 → `no such table`。用 `tx` 时 ConnPool 是 `*sql.Tx` → `BeginTransaction` 判定为已在事务中而**不嵌套**（`finisher_api.go:684-691`、`callbacks/transaction.go:8-13`）。
- **404 路径无悬挂事务**：PG 下 0 行匹配的 `FOR UPDATE` 不报错不加锁，`First` 返回 `ErrRecordNotFound` → gorm 的 defer 触发 `Rollback`（`finisher_api.go:647-651`），连接归还。
- **无死锁序**：`TicketService` 对 `tickets` 的更新只有 `:467` 一处且每次一张票；`CreateFromAlert` 的锁序是「alerts 行 → 插 tickets」，与「tickets 行 → 插 ticket_history」无交叉表环。

**失败策略（审查补的漏项）**：历史插入与 UPDATE **同事务，失败即整单回滚**。这条只有在「历史插入不可能失败」时才敢选 —— 所以 §2.1 去掉了 `actor_id` 的外键、`old_value`/`new_value` 用 `TEXT` 不设长、必填列只有 `ticket_id`/`batch_id`/`kind`（全部服务端生成）。剩下的失败面只有「DB 整体不可用」，那时 UPDATE 本来也会失败。**代价写明白：记不上历史 = 用户改不了工单**；这是刻意的完整性优先，不是疏漏。

**`Create` 的重试循环与事务（第二个审查漏项）**：`Create`（`ticket_service.go:154-160`）有 `ticket_number` 唯一冲突的**重试循环**。绝不能在「一个长事务里循环重试」—— 真 PG 下第一次唯一冲突会让整个事务进入 aborted（25P02），后续尝试全部失败，自愈退化成硬 500（sqlite 单测里**看不见**这个差异）。正确形状是**每次尝试各开一个事务、出生历史行写在同一个事务里**：

```go
for attempt := 1; ; attempt++ {
    err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(t).Error; err != nil { return err }
        return tx.Create(&birthRows).Error      // 同事务
    })
    if err == nil { break }
    if !isUniqueViolation(err) || attempt >= maxAttempts { return nil, err }
}
```
唯一冲突 → 该次事务整体回滚（含历史行）→ 换新事务重试，语义与现状一致且自愈保持。

### 2.5 `resolved_at` 随状态收口（本轮的第二半）

与 `closed_at`（M24）同一套语义：**时间戳跟着 status 走**，判据落在 SQL 表达式里。

```
status ∈ {open, in_progress, pending}  → NULL      （重开/退回：当前状态语义，用户 2026-09-11 拍板）
status = resolved                      → COALESCE(调用方显式值, resolved_at, now)
status = closed                        → 保留不动（resolved→closed 时 MTTR 仍要算得出）
```

写成与 `closed_at` 同一处、同一风格的表达式（`ticket_service.go:456-466` 旁），**不是**两个 if。实现要点：**先取显式值存局部变量，再覆盖 `updates` 键**（照 `closed_at` 的 `:458-461` 写法，顺序反了就丢显式值）。

`resolved→closed` 必须保留 `resolved_at`：资产时间线对同一张票**同时**发「已解决」与「工单关闭」两个事件（`diagnostic_service.go:158-173`），清掉会让已关闭的票丢掉解决时刻，MTTR 归零。

**被清掉的值进历史**：这就是 §0 说的两半必须同轮 —— 重开时历史里留下 `field_name='resolved_at', old_value=<被清掉的时间>, new_value=NULL`，`actor_name` 记下是谁重开的。

**两条必须如实登记的后果（审查发现，不修但要说）**：
1. **资产时间线在重开后不可重现**：`diagnostic_service.go:158` 的「已解决」事件由**当前** `resolved_at` 派生，且 `:144` 的窗口过滤是 `resolved_at IS NOT NULL AND resolved_at >= ?`。重开后旧事件**整体消失**（不是截断，是不可重现）。`ticket_history` 保住了事实，但**那条时间线不读历史表**。→ 登记为后续小步「时间线改读 `ticket_history`」，本轮不做，但**不许静默**。
2. **「只传 `resolved_at` 不传 `status`」的口子**：与 `closed_at` 同一个窗口（M24 已在 `ticket_service.go:453-455` 显式登记「不进这段逻辑」）。`resolved_at` 变成 status 驱动列后，同样的请求能写出 `open + resolved_at` 的不一致行。本轮**对称登记**，不顺手加拒绝（会改既有 API 行为，属独立决策）。

### 2.6 读端点与可见性

```go
tickets.GET("/:id/history", ticketH.ListTicketHistory)   // 准入与 GET /:id 同级
```

**准入层**：与 `GET /tickets/:id`（`routes.go:341`）同组、同权限（`canWrite` 只挂 POST/PUT，GET 无角色守卫；`roles.go:90-92` 的读地板 = 任何已认证身份放行；`ticket_service.go:88-97` 的 `Get` 无归属过滤）。按拍板③执行。

⚠️ **必须如实说明的事实（审查发现）**：这与仓里对同类数据的**既有先例相反** —— `routes.go:286` 的 `GET /audit-logs` 收在 `canAudit`（admin/ops_admin/auditor，`roles.go:60`），而它记的正是「actor 归属」这一类信息。历史端点把同类信息下放到读地板，是**放宽**而非「同级」。同时它**新增了两类数据面**：`GET /:id` 拿不到的**旧值**（含被改掉的 `resolution` 草稿、改正前的 `requester_email`、从 `description` 里删掉的内容）与 **actor 归属**。拍板③已定「跟工单本身」，本方案照办；此处只把「这是放宽」记进文档与 §8 台账，供将来复核。

**其它实现约束**：
- **分页 clamp**：`page_size` 上限 **500**，在 service 层硬编码（照 `ticket_service.go:60-65` 的既有写法）。契约层 `page_size` 无 `maximum`（`openapi.yaml:746-750`），不能指望 schema 兜底 —— 不 clamp 就是可拖库。
- 排序 `created_at DESC, id DESC`（全序，T-45）。
- 返回**平铺行**，前端按 `batch_id` 分组：后端不做展示层聚合，契约保持「一行一个字段变更」的直白形状。
- **路由分类闸门**：`routes_integration_test.go:1110-1131` 的 `TestRoutes_所有路由都已分类` 要求新路由**显式**列入 `gatedRoutes` 或 `ungatedRoutes`（现有 `GET /api/tickets/:id` 在 `:625-626`），漏了 CI 直接红。

### 2.7 待定细节（结论）

| # | 问题 | 结论 | 理由 |
|---|---|---|---|
| D-1 | 长文本要不要截断？ | 截断到 500 字符 + 末尾标记 | 一次 PUT 可能带 2KB 描述，历史不该存 N 份；`asset_history` 用无限制 `TEXT` 但它是死表、没被真实数据检验过 |
| D-2 | `requester_email` 等 PII 要不要记？ | **记** | 受众不变（`models/ticket.go:21` 的当前值本来就随 `GET /tickets` 返回给所有已认证用户）—— 但理由**不能**写成「不扩大可见范围」：append-only **永久保留已被改掉的旧值**，这是新增的数据面（见 §2.6） |
| D-3 | 历史保留期？ | 不设清理（append-only，无删除路径） | 加保留期需定时任务，属独立决策 |
| D-4 | `kind` 词表 | `created` / `updated` | 宽粒度下「字段级」已由 `field_name` 表达；不预设 `deleted`（无删除路径） |
| D-5 | 解析不出 actor 时？ | `actor_id=NULL` + `actor_name` 取得到就存，都取不到存 `"unknown"` | 与 `ticket_handler.go:107-110` 现有处理一致 |

---

## 3. 契约

- `openapi.yaml`：新增 `GET /tickets/{id}/history`（`page`/`page_size` + `TicketHistoryEntry` schema，**只声明实现真会返的码**：`200`/`404`；不写 `403`，因为没有额外角色要求）。
- 顺带补 `Ticket` schema 的 `resolved_at`/`closed_at`（M24 已登记缺口：响应里没有 `closed_at`，PUT 却会自动写它）。
  ⚠️ **收益有限，别高估**：生成物 `frontend/src/services/api.types.ts` 的唯一引用是 `apiClient.ts:27` 的 `TicketDTO` 别名，**全仓无使用**；前端渲染用的是**手写**类型（`frontend/src/types/index.ts:100`、`components/TicketTable.tsx:7`）。该 schema 本身已长期漂移（写 `requester`/`assignee`，实际返回 `requester_name`/`assignee_name` 等）。若前端时间线要展示 `resolved_at`，**必须同步改手写类型**。
- `gen:api` 重跑，`api.types.ts` 差异随提交（CI 有漂移闸门）。

---

## 4. Risk

| # | 失败模式 | 缓解 |
|---|---|---|
| R-1 | **历史表把工单写入放大成 N+1 次 INSERT**（一次 PUT 改 6 个字段 = 6 行），同事务 → 事务变长、锁持有变久；高并发下连接池被拖住 | ① diff 排除系统列后只记**真实变化**（§2.3）；② 行锁只在同一张票上竞争（不同票互不影响）；③ 历史行用**单条批量 INSERT**（`CreateInBatches`）而非 N 次 `Create` —— 实现步必须验 SQL 语句数 |
| **数据完整性** R-2 | **pre-image 取错（无锁）→ 历史里 `from` 是别人写过的值**，经手链条错乱；错的历史比没有历史更糟（会被当证据） | 行锁，且**硬条件：必须与事务同用**（§2.4）。若审查/实测否掉行锁，必须在契约与文档里写明「并发下 `from` 可能交错」，不许留成隐性假设 |
| **数据完整性** R-3 | **`resolved_at` 清空是破坏性写入** —— 窗口写错（如把 `closed` 也清掉）会让**已关闭工单的 MTTR 归零**且不可逆 | ① 表达式三态与 `closed_at` 对齐，必须有 `resolved→closed 保留` 的**反面用例**；② 上真 PG 验方言（同 M24 手法）；③ 值被清时历史表留 `old_value` —— 这就是**先落历史再做清空**的原因（步骤顺序不可颠倒） |
| **可用性** R-4 | **历史 INSERT 失败 → 整单回滚 → 用户改不了工单** | 由构造消除失败面：`actor_id` 无 FK（JWT 路径不复查用户存在，带 FK 则带外删号后该用户永久 500）、`old/new_value` 为 `TEXT`、必填列全部服务端生成（§2.1/§2.4）。**残余风险如实承认**：DB 不可用时两者一起失败 |
| **自愈退化** R-5 | **`Create` 的重试循环被包进单个长事务 → PG 25P02 aborted → 唯一冲突退化成硬 500**，且 sqlite 单测看不见 | 每次尝试各开一个事务（§2.4 代码形状）；实现步必须在**真 PG** 上制造一次 `ticket_number` 冲突，确认第 2 次尝试仍成功 |
| R-6 | 新表/新路由**漏登记清单** → 静默失效或 CI 红 | 四处必须同步：`models.TicketHistory.TableName()` 返 `"ticket_history"`（全仓 19 个模型全部显式实现，不写则 gorm 复数化成 `ticket_histories` → 运行时报 relation 不存在）；`tests/schema_drift_test.go:36-38 liveModels()`（漏了漂移守门对新表空转）；`internal/database/database.go:80-92 autoMigrate()`（dev 库不建表）；`routes_integration_test.go` 的 `ungatedRoutes`（CI 红） |
| R-7 | 迁移 000025 在**已有数据的库**上建表 + 建索引，SQL 有误会在启动时炸 | 沿用 000021/000023 的 `CREATE TABLE`/`CREATE INDEX`/Down 写法；`db_smoke.sh` 的升级链必须从**上一版**库真跑一次（不只跑 up 全链） |
| R-8 | 历史表被当审计物后被要求不可篡改，而代码里有 UPDATE/DELETE 路径就破坏 append-only 承诺 | 代码层零 UPDATE/DELETE 路径 + 模型注释写明不变式；DB 级强制（REVOKE）属独立决策（登记） |

---

## 5. 执行步骤（小步，每步可独立验证）

| 步 | 内容 | 验证 |
|---|---|---|
| ~~0~~ ✅ | **实测前置**：`clause.Locking` 在 sqlite 基座与真 PG 上的实际渲染；gorm `Updates(map)` 回写 struct 与自动补 `updated_at` 的行为 | **已完成，四条全部运行验证为真** —— 见下方「步骤 0 实测结论」 |
| ~~1~~ ✅ | 迁移 `000025` + `models.TicketHistory`（含 `TableName()`）+ `liveModels()` + `autoMigrate()` + `db_smoke.sh` 白名单 + Down 链断言 + `TestDBSmoke_TicketHistory` | **已完成**：真 PG 冒烟两轮全绿（`✓ applied 25_000025_ticket_history`、Down 链 `25→24→23→21→…→13`）；`TestDBSmoke_TicketHistory` 确认在白名单里**真跑**（T-42 假绿已排除）；变异 V-1/V-2/V-4/V-5 全部红在预判断言上 |
| 2 | `Actor` + `Update` 签名变更（1 handler + 13 测试 + 1 mock）+ 事务/行锁 + **原始行 map diff** + 批量插历史 + `CreateFromAlert` | 单测：字段级 diff、**无变化 PUT → 0 行**、`updated_at` 不入历史、**模型外列（如 `alert_id`）也留痕**、actor 快照、同事务回滚、**pre 必须在 UPDATE 前拷贝**（形态守卫：否则黑盒用例全绿，同 M24 V-5 同族） |
| 3 | `Create` 出生事件（**每次尝试各一事务**，含重试） | 单测：`created` 行 + `source`；真 PG 制造 `ticket_number` 冲突验自愈仍活（R-5） |
| 4 | `resolved_at` 随状态收口 | 单测（真 sqlite）：三态 + **`resolved→closed` 保留的反面用例** + 变异反证 + 真 PG 方言 |
| 5 | 读端点 + `ungatedRoutes` 登记 + 分页 clamp 500 + openapi + `gen:api` + 前端手写类型 | handler 用例（分页/404/排序/上限）+ 路由分类闸门 + 契约漂移闸门 |
| 6 | 前端工单详情时间线（按 `batch_id` 分组） | vitest + tsc + eslint |

### 步骤 0 实测结论（2026-09-11，运行验证；探针文件已删）

四条都在本机实跑过，不再是读源码推断：

| # | 命题 | 实测结果 |
|---|---|---|
| a | `Updates(map)` **原地回写** struct（`callbacks/update.go:13,151,221`） | **为真** —— 拿同一个 `t` 当 pre-image 等于用 post 比 post，diff 恒空。§2.3 因此取原始行 map 而非 struct |
| b | `Updates(map)` **无条件补 `updated_at`**（`:238-241`），即便同值 PUT | **为真** —— 「空 diff 就不写历史」这条路不可达，§2.3 改为按列比 |
| c | `clause.Locking` 在 sqlite 基座被**静默丢弃** | **为真** —— DryRun SQL = `SELECT * FROM \`tickets\` WHERE id = ? …`，不含 `FOR UPDATE`（`sqlite@v1.6.0/sqlite.go:122-126` 的 `"FOR"` builder 直接 return）。故行锁只影响真 PG，单测基座不报错也不验证 |
| d | 原始行 map 能读到**模型外列** | **为真** —— sqlite 基座 `ALTER TABLE tickets ADD COLUMN alert_id TEXT` 后，map 里 25 列含 `alert_id` |

(c)(d) 合起来是 §2.3「原始行 map diff」可行、**且能在 sqlite 单测里复现模型外列场景**的依据。
**仍待实跑**（不在本步范围）：PG 25P02（R-5，步骤 3）、真 PG 上 autocommit 的 `FOR UPDATE` 立即释放（步骤 2）。

> ⚠️ **变异反证的一条陷阱（本轮踩到，登记在案）**：`scripts/db_smoke.sh:197` 的第二轮（upgrade 库）
> 以 `[[ "$rc" -eq 0 ]]` 为门禁 —— **fresh 轮一红，upgrade 轮整轮不执行**。
> 于是「落在 fresh 轮的变异」与「落在 upgrade 轮的变异」**不能合并成一次运行**：
> 前者的失败会让后者的守门人根本没跑，报告成「未变红 —— 该变异没被断言守住」，
> 把人骗去怀疑断言空转（本轮 V-5 白查一轮，最后发现是合并跑的锅）。
> 省一次容器启动，换来的是**假证据** —— 逐条独立运行。

每步按既有纪律收尾：变异反证（红在断言、红色集合与预判精确一致）→ 门禁（`npx tsc --noEmit` + `npm run lint` + 相关 vitest + `go test ./...`）→ §8 台账 → commit + push。

---

## 6. 边界

- **不做**：GLPI 路径的历史（§1.4 路径 3）—— 该路径连「更新已存在工单」都还没有，是数据缺口，与「GLPI 丢弃 `ClosedAt`/`ResolvedAt`」同处，一起做才有意义，已登记。
- **不做**：`cmd/seed` 演示数据的历史。
- **不做**：历史表的保留期/清理任务、DB 级 append-only 强制（登记）。
- **不做**：`diagnostic_service` 时间线改读 `ticket_history`（§2.5-1，登记为后续小步）。
- **不做**：给历史端点上 `canAudit`（拍板③：跟工单本身 —— 但「这是相对 `/audit-logs` 先例的放宽」已记入 §2.6 与台账）。
- **不动**：`audit_logs`（保持 HTTP 级语义，不与领域事件混表 —— 这是拍板选 A 的理由）。

---

## 7. 审查发现与处置

两路独立对抗审查（正确性/并发/数据完整性 + 边界/安全/一致性），共 34 条，归并为 26 组。**两条被两路独立撞到**（标注 ★），是本节最重的部分。

### 7.1 阻断级：设计按现有 gorm 行为跑不通 —— 已改设计

| # | 发现 | 处置 |
|---|---|---|
| ★1 | **`Updates(map)` 原地回写 struct**（`callbacks/update.go:13,151,221`）→ 「pre 读到的 `t`」在 UPDATE 后已是 post 值，diff 恒空 | §2.3 改为**原始行 map** diff；§5 步骤 2 加形态守卫用例 |
| ★2 | **struct 看不见模型外列**（`tickets` 有 12 个列不在 `models.Ticket` 里，且能经裸列兜底真的写库，`callbacks/update.go:230-231`） | 同上，map diff 覆盖全部库列 |
| 3 | 方案 ii 附带坑：gorm 对 map 更新**无条件补 `updated_at`**（`callbacks/update.go:238-241`）→ 每次 PUT 必生一条伪历史，「空 diff 不写」不可达 | §2.3 显式排除系统列 + 「无变化 PUT → 0 行」用例 |

### 7.2 中级：设计有缺口 —— 已补

| # | 发现 | 处置 |
|---|---|---|
| 4 | **`Create` 的重试循环不能包进单个事务**（PG 25P02 aborted 会让重试全废，sqlite 单测看不见） | §2.4 定为「每次尝试各一事务」；R-5 + 真 PG 验证步骤 |
| 5 | **闭包内必须用 `tx` 否则历史与 UPDATE 不同事务**（且 sqlite `:memory:` 会另开连接 → `no such table`） | §2.4 写成硬约束并给出源码依据 |
| 6 | **历史 INSERT 失败会阻断工单更新**（「记不上历史 = 改不了工单」），文档原来只把它当好事 | §2.1 去掉 `actor_id` 外键（消除唯一现实的失败源）+ §2.4 明写策略与残余风险；R-4 |
| 7 | **`TableName()` 漏写 → `ticket_histories` 表不存在**（全仓 19 个模型全部显式实现） | R-6 + §5 步骤 1 |
| 8 | **漏登记 `liveModels()` / `autoMigrate()`** → drift 守门对新表空转、dev 库不建表 | R-6 + §5 步骤 1 |
| 9 | **漏登记 `ungatedRoutes`** → 路由分类测试直接红 | §2.6 + R-6 + §5 步骤 5 |
| 10 | **分页无上限**（契约层 `page_size` 无 `maximum`，真实上限在 service 层硬编码） | §2.6 明确 clamp 500 |
| 11 | **可见性「跟工单本身」是相对 `/audit-logs` 先例的放宽**，且新增「旧值」与「actor 归属」两类数据面 | §2.6 如实记录（拍板③已定，照办），并记入台账 |
| 12 | **PII 结论的理由不成立**（「受众不变」对，但 append-only 永久保留旧值是新数据面） | D-2 改理由；§2.6 |
| 13 | **重开清空会让资产时间线的「已解决」事件不可重现**（当前态派生 + 窗口过滤，`diagnostic_service.go:144,158`） | §2.5-1 登记后果 + 「时间线改读历史表」列为后续小步 |
| 14 | **`resolved_at` 缺 `closed_at` 那个「只传时间戳不传 status」口子的对称登记** | §2.5-2 对称登记（不顺手改 API 行为） |
| 15 | **`ON DELETE CASCADE` 与 append-only 相抵**（`assets` 有硬删先例，将来加工单删除就抹历史） | §2.1 定为「已知取舍 + 模型注释写明加工单删除端点时重估」；R-8 |

### 7.3 低级：事实/行号/措辞纠正 —— 已改

| # | 发现 | 处置 |
|---|---|---|
| 16 | `audit_logs` 三列行号 `1103-1105` → 实际 `1100-1102` | §1.2 已改 |
| 17 | handler 取 `username` 行号 `106` → 实际 `107`（`:107-110` 为 unknown 兜底） | §2.2 已改 |
| 18 | 把 M17 机制叫「白名单」，代码注释写的是**「禁改集合」**（`ticket_service.go:327-338`） | §1.1 已改 |
| 19 | GLPI 行号混用（`:247` 是构 model，`CreateInBatches` 在 `:271`） | §1.4 已分标 |
| 20 | 改动面漏了接口 mock（`rack_ticket_handler_test.go:124,138`） | §1.3/§2.2 已补 |
| 21 | `source` 词表不全（实际还有 `alert`/`zabbix`） | §2.1 改为开放值 |
| 22 | GLPI 措辞：「历史缺口」低估了 —— 已存在工单的字段变更**根本不落库** | §1.4 已改 |
| 23 | `DEFAULT NOW()` 与 gorm 的 `NowFunc: time.Now().UTC()`（`database.go:44-46`）不同源 | §2.1：`created_at` 由 gorm 填 UTC，`DEFAULT NOW()` 仅兜底 |
| 24 | 「顺带补 openapi 的 `Ticket` schema」收益被高估 | §3 补「生成物无人消费、前端用手写类型」的事实 |

### 7.4 核实为「不成立 / 无需处理」（避免实现时过度设计）

| # | 被担心的 | 核实结果 |
|---|---|---|
| 25 | 404 留悬挂事务、多表死锁序、嵌套事务有害、`clause.Locking` 无事务会 panic | 均**不成立**：gorm defer 会 Rollback（`finisher_api.go:647-651`）；锁序无交叉环；`ErrInvalidTransaction` 被吞不嵌套；无事务时静默无效不报错 |

### 7.5 核实为真的既有结论（已被独立复核，保留）

`Update` 完全不碰 `resolved_at`；生产调用方 1 处、测试 13 处；**四条建单路径穷尽**（真 grep：无 webhook/复制/导入/定时任务/裸 INSERT）；**无删工单路径**（真 grep：零 `Unscoped()`、零 `Delete(&models.Ticket`）；`asset_history`/`step_progress_history` 零 Go 引用；迁移 000025 空闲且缺号容忍；`clause.Locking` 被 sqlite 驱动丢弃。

**未核实项（实现步必须实跑验证，不当作已验证事实）**：PG 25P02（R-5，步骤 3 验）、autocommit 下 `FOR UPDATE` 立即释放（§2.4，步骤 2 验）—— 两条都是读源码/标准语义得出的，需真 PG 才能确认。

~~`Updates(map)` 回写 struct 与自动时间戳、`clause.Locking` 在 sqlite 被丢弃~~ —— **已实测为真，见 §5「步骤 0 实测结论」**。
