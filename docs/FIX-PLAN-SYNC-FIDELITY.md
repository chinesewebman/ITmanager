# 需求文档 v2：同步导入保真 —— 外部时间戳与词表的落库（M26）

> 状态: **需求已定稿**（4 路对抗审查完成 + 8 项决策已拍板）；下一步 = 可执行细节文档
> 日期: 2026-09-11（v1 同日，v2 同日）
> 基线: `main` @ a4627a4（M25 已收口）
> 依据: `docs/adr/0004-工单SoT决策.md`、`docs/FIX-PLAN-TICKET-HISTORY.md` §6（未做①）、`docs/FIX-PLAN-UI-PERF.md` §8 台账 1.9 ①
>
> **拍板（2026-09-11 燕如）**
> ① M26 范围 = **同步导入保真**，覆盖**两条**路径（Zabbix 告警 + GLPI 工单）—— 同一根因、同一个时间解析纯函数、同一批测试基座。
> ② **不做**「已存在工单的更新」—— 按 ADR-0004，ITmanager 是工单 SoT、GLPI 是「可选只读参考」，「不在同步阶段覆盖」**是设计而非缺口**；只修那句与事实不符的注释。
> ③ `external_id` 的幂等索引**纳入本轮**（ADR-0004 Risk-3 承诺过、从未落地）。
> ④ **D-1** = (b) 越界档位**跳过该票 + 计数 + 逐条日志**（与 M18「拒绝而非静默改写」同立场）。
> ⑤ **D-2** = **导入不发明时间**：源缺失 → `closed_at`/`resolved_at` 写 **NULL**；与 `TicketService.Create` 的补 `now` **刻意不同**（那是实时建单，`now` 是真实时刻）。
> ⑥ **D-3** = GLPI 时区**暂不确定**，按 **`Asia/Shanghai`** 实现（仓库 compose 的 GLPI 容器值），写成显式常量并**登记为待核对项**；不猜、不自动探测。
> ⑦ **D-4** = **配 `ON CONFLICT ... DoNothing`**（兑现 ADR-0004 的「幂等 upsert」）。
> ⑧ **D-5** = (a) **fail-closed**：直接建索引，失败就失败 + 迁移内前置自检 DO 块给样本；**不在迁移里删数据**。
> ⑨ **D-6** = (a) `SyncFromGLPI` 返回 `(synced, skipped int, err error)`，`results` 增键 `glpi_skipped`（该端点无 openapi 定义，加字段无漂移）。
> ⑩ **D-7** = (a) **不纳入** `host_id`/`asset_id` 资产关联；收窄 §3 验收 + 登记为前置项。
> ⑪ **D-8** = (a) Zabbix 告警的 `created_at` 保持 `now`（`problem_start` 才是业务时刻）。
>
> **v2 修订说明**：v1 经 4 路独立对抗审查（正确性/数据完整性、并发幂等迁移、契约与测试断裂面、时区与时间语义），
> **推翻 3 条阻断级错误**（假承诺、伪选择、伪造事件），修正 3 处症状归因，收窄 1 处范围。逐条处置见 §8。

---

## 0. 一句话

两条同步路径都把外部系统的**业务时间**丢掉：Zabbix 告警的 `problem_start` **从未被写入**（落零值 `0001-01-01`），GLPI 工单的 `created_at` 被改写成同步时刻、`closed_at`/`resolved_at` 整条丢弃。**同一根因**：`internal/integration` 自己拼 `models.Xxx{}` 落库，绕开了 `TicketService` 里 M16/M18/M24/M25 建立的不变式。

后果分两类，**不要混为一谈**：

| 类 | 症状 | 严重度 |
|---|---|---|
| **静默为空** | 告警的**仪表盘 KPI**（MTTR/MTTD/密度/resolved-acked 计数）恒空或恒 0 —— 列表不按 `problem_start` 过滤，所以**界面看起来正常** | 高（数据长期缺失且不可见） |
| **值失真** | 工单列表排序把历史票顶到最前（`ticket_service.go:87`） | 中 |

> ⚠️ **v1 的假承诺已删除**：「拓扑窗口 / 资产时间线变为有数」**不成立于本轮**。那两处的闸门是 `alerts.host_id`
> 全库为 NULL，不是 `problem_start`。见 §1.8 阻断-1。

---

## 1. 现状（实测）

### 1.1 根因：两条同步路径都不经 service 层

| 路径 | 入口 | 落库方式 | 绕开的不变式（**精确**，见 A-低-1） |
|---|---|---|---|
| Zabbix 告警 | `POST /integrations/sync {type:"zabbix"}`（`integration_handler.go:71`） | `CreateInBatches`（`service.go:208`） | 告警无 service 写入口，规则散在各处 |
| GLPI 工单 | `POST /integrations/sync {type:"glpi"}`（`integration_handler.go:74`） | `CreateInBatches`（`service.go:271`） | priority 默认、M18 枚举校验、M24 `closed_at`、M25 `resolved_at` + 出生留痕 |

**不是「全部不变式」**：`TicketService.Create` 的 8 项不变式里，status 默认 `open`（gorm tag `default:open`）、tags 默认 `[]`、source 显式写 `"glpi"` 这 3 项**不被违反**（前两项被 gorm tag 兜住，§1.6 实测）。

### 1.2 Zabbix 告警路径：`problem_start` 从未写入

**全仓告警只有两个写入方**（`command grep -rn "models.Alert{"` 生产命中仅 2 处）：`integration/service.go:187`（生产）与 `cmd/seed/main.go:218`（演示）。**无 webhook / ingest 端点**（`routes.go:314-328` 只有 bulk-ack/resolve/delete 等）。

```go
// integration/service.go:187-198
toInsert = append(toInsert, models.Alert{
    TriggerID: t.TriggerID, HostName: alert.HostName, TriggerName: alert.TriggerName,
    Problem: alert.Problem, Severity: alert.Severity, SeverityName: alert.SeverityName,
    Status: "problem", Source: "zabbix",
    CreatedAt: now, UpdatedAt: now,
    // ← ProblemStart 不在列内 → time.Time 零值
})
```

源数据**就在手上**：`Trigger.LastChange`（Zabbix `lastchange`，Unix 秒）在结构体里（`zabbix.go:242`），查询按它排序（`zabbix.go:171`），`ConvertToAlert`（`zabbix.go:270-295`）却从不读。

**真实消费方**（v1 表格已按审查修正）：

| 消费方 | 条件 | 后果 |
|---|---|---|
| `dashboard_service.go:157-162` MTTR | `problem_start IS NOT NULL AND >= ?` | 零值过 `IS NOT NULL`、过不了范围 → `AVG` 无行 → KPI `null` ✅恒空 |
| `dashboard_service.go:174-178` MTTD | 同上 | 同上 ✅恒空 |
| `dashboard_service.go:190` 告警密度 | 同上 | ✅恒 0 |
| `dashboard_service.go:204` resolved/acked 计数 | 同上 | ✅恒 0 |
| `topology_service.go:106` | `status='problem' AND problem_start >= ?` | ✅恒 0，**但该查询同时 `Group("host_id")`**，见 §1.8 阻断-1 |
| `diagnostic_service.go:97` 资产时间线 | `... OR ack_time >= ? OR resolve_time >= ?` | ⚠️**已 ack/resolve 的仍会出现**，只是时间戳是 `0001-01-01`；只有**未处理**的告警缺 triggered 事件 |
| `diagnostic_service.go:258` 主机告警窗口 | `status='problem' AND problem_start >= ?` | ✅恒 0（但同受 host_id 影响） |

**v1 归因错误已修正（3 处）**：

- ❌ **「趋势恒空」为假**。`dashboard_service.go:113-116` 是按 `created_at` 聚合的**告警**趋势，而 `created_at` 被显式写成 `now` → **有数**，不为空。已从受影响清单移除。
- ❌ **`diagnostic_service.go:294` 不是 `problem_start` 消费方**。其 WHERE 是 `host_id = ? AND status='resolved' AND resolve_time >= ?` —— **窗口建在 `resolve_time` 上**。`problem_start` 只出现在 `:300` 的 Go 端减法 `r.ResolveTime.Sub(r.ProblemStart)` 里；`if delta > 0`（`:301`）挡不住 0001 年 → 若该行能命中会得到约 6.39e10 秒的垃圾 MTTR。它现在为空是 `host_id` 的原因。**建议本轮顺手在 `:301` 加上下界防护**（一行，属同一根因的可达后果）。
- ❌ **G-1 引用了不存在的「工单趋势」**。全仓无工单趋势查询；`tickets.created_at` 的真实消费方是**工单列表排序与游标分页**（§1.3）。

**第七处后果（v1 遗漏）**：`alert_service.go:334-338` 的 `if !alert.ProblemStart.IsZero()` 是**缓解措施**（非受害者）——它挡住 `duration = now.Sub(0001-01-01) ≈ 6.39e10` 落进 `alerts.duration`，代价是静默写成 `0`。修好 `problem_start` 后它自动转正。

**为什么长期没被发现**：① 前端**完全不显示** `problem_start`（`command grep -rn problem_start frontend/` 零命中，生成物 `api.types.ts` 也无）；② 告警列表不按它过滤（`alert_service.go:107-125` 只有 status/severity/host/FP 四条件）。于是**列表是满的、KPI 是空的**，读起来像「最近没有告警」。

### 1.3 GLPI 工单路径：三个时间戳 + 两个词表

```go
// integration/service.go:243-257
local := t.ConvertToTicket()
if _, ok := existingSet[local.ExternalID]; ok { continue }   // 见 §1.4：设计，非缺口
toUpsert = append(toUpsert, models.Ticket{
    ExternalID: ..., Title: ..., Description: ...,
    Status: local.Status, Priority: local.Priority, TicketType: local.TicketType,
    Source: "glpi", CreatedAt: now, UpdatedAt: now,
    // ← local.CreatedAt / ResolvedAt / ClosedAt 三个都不在列内
})
```

| # | 缺陷 | 真实后果（v2 修正后） |
|---|---|---|
| G-1 | `created_at` = 同步时刻，`local.CreatedAt` 解析了不用 | **工单列表排序**（`ticket_service.go:87` `Order("created_at DESC, id DESC")`）→ 一批历史票全被顶到列表最顶端、像刚建的；且同批 `created_at` 完全相同 → 批内相对顺序退化为 id 序。**游标分页**（`:90` `(created_at, id) <`）排序键同样被污染 |
| G-2 | `closed_at`/`resolved_at` 整条丢弃 | `dashboard_service.go:221` SLA（`status='closed' AND closed_at >= ?`）与 `diagnostic_service.go:166`「工单关闭」时间线事件**同时看不见**这些票。这是 M24/M25 连续两轮登记的「第三个 closed 无 closed_at 入口」 |
| G-3 | GLPI status 6（待批准）取不到键 → `""` | **实测**（§1.6）：gorm 把 tag 默认 `open` **显式写进 INSERT** → 落成「新建」。运营看到「新建」，实际是「待批准」 |
| G-4 | GLPI priority 0 取不到键 → `""` | **实测**：落空串 → 工单页优先级筛选选不中、`PRIORITY_WEIGHT[未知] ?? 0` 排序垫底、统计卡不计数。M18 刚在写入口封住的同一形态，这是绕过写入口的第二个来源 |

**既有测试覆盖（审查 C 实测）**：G-1/G-2/G-3 **在整套测试里零覆盖**（无用例喂 status=6，无断言钉住写入字段集合）；**只有 G-4 有一条既有红**（`glpi_e2e_test.go:222`）。→ §5 的 step 2/3/4 守门人**全是新用例**。

### 1.4 与 ADR-0004 对账：两条登记不成立

M24/M25 把「已存在工单永不更新」写作「**数据缺口**」，与 ADR-0004 相抵：ADR 明写「GLPI 降级为可选只读参考……**不做 GLPI 写回**」→ 不用 GLPI 状态覆盖本地**是设计**。

**唯一为假的是那句注释**（`service.go:245`）：全仓**无 PATCH 路由**（`grep -n "PATCH" routes.go` 零命中），`TopicTicketCreated`/`TopicTicketResolved`（`eventbus.go:50-51`）**只有常量，无发布方无订阅方**。本轮把它改成真话。

> **v2 新增**：`service.go:268-269` 与 `upsert_test.go:50/:351/:458` 的注释「`tickets.external_id` 上没有唯一索引」在 000026 落地后**也会变假**，须一并修正（§5 步骤 5）。

### 1.5 ADR-0004 Risk-3 从未落地

ADR 原文：「导入走**幂等 upsert**；`external_id` 加部分唯一索引（仅 `source='glpi'` 且非空）」。

实际：`000019_tickets_external_id_index.up.sql:5` 只有**普通**索引（服务于 W6/P16 的性能修复）。`existingSet` 预过滤（`service.go:230-244`）是 TOCTOU。

**v2 修正两处**（审查 B）：

1. **「加索引」≠「幂等 upsert」**。`CreateInBatches` 撞唯一索引是**整批原子回滚**（gorm v1.30 `finisher_api.go:59-63`：`len > batchSize` 走 `tx.Transaction(...)` 包全部 chunk；`len <= batchSize` 由 `callbacks/transaction.go` 的 `gorm:begin_transaction` 自开事务。**两分支都全有或全无**，且 `SkipDefaultTransaction` 未设置）。→ 并发同步时冲突行会**带走整批新票**，`synced=0`、500。兑现 ADR 的「幂等」必须配 `ON CONFLICT`（§2.6）。
2. **TOCTOU 的因果链不精确**：真正**先**炸的是 `ticket_number`。`AssignTicketNumbers`（`models/ticket.go:64-74`）按当天已建条数分配（`nextTicketSeq` `:94-99` 裸 `Count`，**非原子**——同文件 `:80-82` 注释已自认「并发下仍可能算出同一个号」），而 `idx_tickets_ticket_number` 是**唯一索引**（`000013_schema_align.up.sql:276`）→ 两次并发同步算出同批号 → 先撞它。「真重复工单」只在更窄的交错下可达（A 提交发生在 B 的 `existingSet` 读取之后、`AssignTicketNumbers` 之前）。

→ 000026 的定位改为「**ADR 承诺 + 索引层兜底**」，不是「让并发同步可用」。`ticket_number` 分配竞态**单独登记**（§6，超本轮范围）。

### 1.6 步骤 0 实测①：gorm 落库形态（2026-09-11，in-memory sqlite + DryRun，探针已删）

| # | 命题 | 实测结果 |
|---|---|---|
| a | `Status: ""` 配 `gorm:"default:open"`，gorm 是**省略该列**还是**写入 tag 默认值**？ | **写入 tag 默认值** —— INSERT 列清单含 `status`，值 `"open"`。故 `tickets.status VARCHAR(20) DEFAULT 'created'`（`000001_init.up.sql:699`，词表外值）**根本不参与**；只有裸 SQL 才会撞上 |
| b | `Priority: ""`（无 default tag，PG 侧 `NOT NULL`）落什么？ | 落**空串**（列清单含 `priority`，值 `''`），不违反 `NOT NULL` |

(a) 修正 M24 的措辞「`""` 会撞 `NOT NULL` 或成为静默消失源」——status 是**落成 `open`**，比落词表外值更隐蔽。

### 1.7 步骤 0 实测②：时区往返（2026-09-11，`postgres:18-alpine` + 仓库同款 gorm v1.30.0 / pgx v5.5.1）

**结论：pgx 对 `TIMESTAMP`（无时区）列丢弃 location，按挂钟数字原样写入。**

源码：`pgtype/timestamp.go:215-221` 的 `discardTimeZone` 把 `t.Location()` 丢掉、用挂钟字段重建为 UTC；binary（`:159`）与 text（`:189`）**两条编码路径都调它**，与 `QueryExecMode` 无关。读回路径 `:263-266` 固定 `.UTC()`。

| Go 写入（挂钟 10:00） | PG 列文本 | 读回 Go | API JSON |
|---|---|---|---|
| UTC 10:00 | `2026-06-15 10:00:00` | 10:00 UTC | `...T10:00:00Z` |
| **Asia/Shanghai 10:00 (+08)** | `2026-06-15 10:00:00` | 10:00 UTC | `...T10:00:00Z` |
| America/Los_Angeles 10:00 (-07) | `2026-06-15 10:00:00` | 10:00 UTC | `...T10:00:00Z` |

**三行逐字节相同** → 决定显示结果的**不是** `Location`，而是挂钟数字本身。前端 `dayjs` 再把它按浏览器时区渲染（`time.ts:20-24`；实测 `2026-06-15T10:00:00Z` → 北京时间显示 `18:00`）。

**列类型全为 `TIMESTAMP`（无时区），无一处 `TIMESTAMPTZ`**：`tickets.created_at`/`updated_at`（`000001_init.up.sql:712/:713`）、`tickets.resolved_at`/`closed_at`（`000013_schema_align.up.sql:271/:272`）、`alerts.problem_start`（`:247`）、`alerts.created_at`（`000001_init.up.sql:653`）、`alerts.problem_end`/`ack_time`/`resolve_time`（`000013:248/250/252`）。

### 1.8 审查发现的额外事实

**阻断-1：`alerts.host_id` / `alerts.asset_id` 生产无写入方。**

- `HostID *uuid.UUID`（`models/alert.go:13`）在 Zabbix 写入路径（`service.go:187-198`）**不在列内**；`cmd/seed/main.go:218-241` 也不设。
- 迁移里**无 trigger/rule**（`grep -rn "CREATE TRIGGER|CREATE RULE"` 零命中），不存在 DB 侧回填。
- `command grep -rn "HostID"` 的非测试命中**全是读**：`topology_service.go:113`（`alertMap[a.HostID]`）、`alert_service.go:117`（过滤）、`diagnostic_service.go`（过滤）。`AssetID`（`models/alert.go:46`）同理（`rack_service.go:142` 只读）。
- **后果**：`topology_service.go:103-113` 的 `Group("host_id")` 与 `diagnostic_service.go:97/:258/:295` 的 `host_id = ?` 永不命中 → **拓扑窗口与资产时间线在本轮之后仍恒空**。修 `problem_start` 只有**仪表盘 KPI** 会变有数（它不按 host_id 过滤）。

**低-2：`tickets`/`alerts` 的 DB 默认值依赖 PG 会话时区。** `created_at TIMESTAMP DEFAULT NOW()`（`000001_init.up.sql:712`/`:653`）走 `now()::timestamp`，按 PG 会话 TimeZone 取挂钟；DSN 无 `TimeZone`（`config.go:47-50`）→ 取服务器默认（现为 UTC），与 `NowFunc` 恰好一致。**未 pin 的耦合**：任何给 postgres 加 `TZ` 的人会同时改掉两边。

**低-3：仓库存在两个「现在」。** `database.go:44-46` 的 `NowFunc` 是 `time.Now().UTC()`，而 `ticket_service.go:204/:213` 与 `models/ticket.go:86`（`ticketNumberPrefix`）用**裸 `time.Now()`**（本地时区）。今天相等只因 api 容器 TZ=UTC——`TZ=Asia/Shanghai` 在 **glpi** 服务块（`docker-compose.yml:181`），**不在 api 块**（`:54-72`），且 `Dockerfile:22-31` 装了 tzdata 但没设 TZ。

**低-4：`TranslateError` 未开启。** `database.go:42-47` 的 `gorm.Config` 只有 `Logger` + `NowFunc` → 唯一索引冲突是 `*pgconn.PgError`（`Code=="23505"`），**不是** `gorm.ErrDuplicatedKey`。实现里写 `errors.Is(err, gorm.ErrDuplicatedKey)` 会永远不成立。

### 1.9 落点事实（决定实现形状）

| 事实 | 值 | 影响 |
|---|---|---|
| `Trigger.LastChange` | `lastchange`，Unix 秒字符串（`zabbix.go:242`）；`GetTriggers` **未**请求 `selectLastEvent`（`:165-174`，另有 `filter:{value:1}` + `only_true:true`） | 只能用 `LastChange`；只返回当前 problem → 无终态行，`problem_end`/`duration` 不用动 |
| GLPI 时间格式 | GLPI REST v1 返回 `Y-m-d H:i:s`；未设置时 JSON `null` → Go `""`。既有 fixture 喂过 `"2026-06-15 10:00"`（`glpi_e2e_test.go:47-48`）、`"2026-06-15"`（`:156`）、**`""`（`:114-117` 四行，其中 `:117` 是 status=5 closed 且整份 fixture 无 `closedate` 键）** | 解析需容多格式 + **必须区分「源缺失」与「解析失败」**（§2.2） |
| 哨兵值 | `"0000-00-00 00:00:00"` 实测**被 Go 三种 layout 全部拒绝**（`month out of range`），不会产生公元 0 年；GLPI 官方 schema `solvedate datetime DEFAULT NULL` → 现代 REST 返 `null` → `""`。两者**走同一条回落分支** | 哨兵只作为「空」的一条输入，危害由 §2.2 的 tri-state 承担 |
| 年份边界 | `"0001-01-01 00:00:00"` 解析成功且 `IsZero()==true` → `Ticket.CreatedAt` 是**非指针**（`models/ticket.go:34`），gorm 的 `autoCreateTime` 会**静默替换成 `NowFunc`** → 断言「落库值==解析值」在该输入上**假绿** | 测试用例须避开或显式覆盖此输入 |
| 迁移编号 | 最高 **000025**（000022 缺号） | 本轮用 **000026** |
| Down 链断言 | **真实改动点是 `db_smoke_test.go:1146-1173`**（不是 v1 写的 `:1113-1116`，那是注释）：`:1146-1164` 前置 3 三条 Fatal（「000025 必须是下一次 Down 的对象」）、`:1168-1178` 第一个 Down 块断言的是「ticket_history 表没了」。另 `:740`/`:1185`/`:1200` 的序数文案**本来就已写歪**（「第一次 Down 必须滚掉 000024/000023」） | §5 步骤 5 的编辑面比 v1 描述大 |
| 已核安全 | `TestDBSmoke_MigrateRunner`（`:97-109`）用 `len(want)` 对照 embed 里的 `*.up.sql` 数 → up+down 同增，**自动适配**；`schema_drift_test.go:164-260` 只解析 CREATE TABLE/ALTER/RENAME/DROP，`CREATE UNIQUE INDEX` 对它透明；`routes_integration_test.go` 用**自己的** `testdata/migrations`（7 个静态文件） | 加 000026 这三处不会红 |
| 冒烟白名单 | `scripts/db_smoke.sh:198`（fresh，20 个用例名）与 `:205`（upgrade，4 个）；`:201` 的 upgrade 段**只在 fresh `rc==0` 时执行**；`TEST_DATABASE_URL` 未设时 `openSmokeDB` 直接 skip（`db_smoke_test.go:47-52`）→ `go test ./...` **完全不覆盖** | 新用例必须手动加进这两条正则，否则**永不执行** |
| 部分唯一索引无法被 sqlite 覆盖 | `models.Ticket.ExternalID` 只有 `gorm:"size:100"`（`models/ticket.go:28`），AutoMigrate 建不出部分索引 | PG 冒烟是这条索引的**唯一防线** |

---

## 2. 设计

### 2.1 时间解析：一个包内纯函数文件（tri-state）

新增 `backend/internal/integration/timeparse.go`（无 I/O、无 DB，表驱动单测）：

```go
// timeParseStatus 三态：源缺失 / 解析失败 / 成功。（非导出：本包自用，无外部消费者）
type timeParseStatus int

const (
    timeAbsent  timeParseStatus = iota // 源明确表示"没有这个时间"（""、null、0000-00-00 哨兵）
    timeInvalid                        // 源给了值但解析不了
    timeOK
)

// parseUnixSeconds 解析 Zabbix 的 Unix 秒字符串（lastchange）。
func parseUnixSeconds(raw string) (time.Time, timeParseStatus)

// parseGLPITime 解析 GLPI 的挂钟时间字符串，接受三种格式：
//   "2006-01-02 15:04:05" / "2006-01-02 15:04" / "2006-01-02"
// 按**包级常量 glpiLoc**（GLPI 实例时区，见 D-3）解释挂钟；返回 UTC 时刻。
// glpiLoc 不做入参：它是配置常量、不是每次调用变化的量，入参只会让调用点噪音化。
func parseGLPITime(raw string) (time.Time, timeParseStatus)
```

**三态是本次审查最重要的修正**：v1 只有「成功/失败」两态，把「GLPI 说这张票没有关闭时间」与「值存在但格式不认识」混为一谈，两者都回落 `now` → 伪造事件（D-P0-2）。

**已实测的格式边界**（审查 D）：
- `"2026-06-15 10:00:00.123"` 用 `15:04:05` **能**解析（Go 允许尾随小数秒，静默截断）→ 毫秒不是缺口。
- `"2026-06-15T10:00:00Z"`（GLPI 高层 API v2 的 ISO 形态）三种 layout **全拒** → 会走 `timeInvalid`。**登记**：若目标 GLPI 未来升到 v2 API 会中招（§6）。

### 2.2 时区口径（v2 整条重写，推翻 v1 的 D-4）

**v1 的「(a) 按 UTC 解析 vs (b) 按服务器本地时区解析」是伪选择**——§1.7 实测两者落库逐字节相同，且**都让运维看到的时间与 GLPI 差 8 小时**。

**正确形态**：解析时按 **GLPI 实例的时区**解释挂钟，再 `.UTC()` 转成绝对时刻。

```go
// 10:00 的 GLPI 挂钟（Asia/Shanghai）→ 02:00 UTC → 写入 TIMESTAMP
// → 读回 02:00Z → dayjs 在北京浏览器显示 10:00 ✅ 与 GLPI 一致
t, st := parseGLPITime(raw)   // 内部：time.ParseInLocation(layout, raw, glpiLoc).UTC()
```

对照 v1 的错误写法：`time.Parse(layout, raw)` → 10:00 UTC → 落 `10:00` → 显示 `18:00` ❌。

**`glpiLoc` 从哪来**：**必须显式配置**，不是「服务器本地时区」（§1.8 低-3：api 容器 TZ 是 UTC，与 GLPI 容器的 `TZ=Asia/Shanghai` 不同源）。取值待 D-3 拍板。

**「不做时区自动探测」与「必须显式配置一个 GLPI 时区」不矛盾**：前者是拒绝猜，后者是必要条件。最小形态 = 一个配置项/常量。

> §5 步骤 0① 的「DSN 无 TimeZone 的往返实测」**可以取消** —— `TimeZone` DSN 参数只影响 `TIMESTAMPTZ` 与 DB 侧 `now()::timestamp`，对本轮全 `TIMESTAMP` 列的显式写入无关。已由 §1.7 的实测覆盖。

### 2.3 Zabbix：`problem_start` ← `lastchange`

```go
switch ts, st := parseUnixSeconds(t.LastChange); st {
case timeOK:
    problemStart = ts
default: // timeAbsent / timeInvalid
    problemStart = now
    log.Printf("Zabbix trigger %s 的 lastchange=%q 解析失败（%v），回落同步时刻", t.TriggerID, t.LastChange, st)
}
```

`created_at` 保持 `now`（**D-8**）：对告警而言 `created_at` 是**本行入库时刻**，`problem_start` 才是业务时刻 —— `models/alert.go:24-25` 两列并存本身就是这个语义分工。`problem_end`/`duration` 不动（§1.9：只拉当前 problem，无终态行）。

### 2.4 GLPI 时间戳：只有在源提供时才写（**不发明**）

| 字段 | 规则 |
|---|---|
| `CreatedAt` | `timeOK` → 用之；`timeAbsent`/`timeInvalid` → `now` + 日志（`created_at` 非指针，无 NULL 语义） |
| `ResolvedAt` | status ∈ {resolved, closed} **且 `timeOK`** → 用之；否则 **nil** |
| `ClosedAt` | status = closed **且 `timeOK`** → 用之；否则 **nil** |

**与 `TicketService.Create` 的差异是刻意的，不是不一致**：

`Create` 是**实时建单**——「现在」就是真实的解决/关闭时刻，所以 M25 让它给 `status=resolved` 补 `now`。
`SyncFromGLPI` 是**导入历史数据**——源没说时间就是**不知道**，补 `now` 是**伪造**。

伪造的代价是具体的：`diagnostic_service.go:158/:166` 只看 `ResolvedAt != nil` / `ClosedAt != nil`、**不看 status** → 一批 2019 年关闭、GLPI 侧 `closedate` 为 `null` 的票导入后会凭空长出一串「工单关闭」时间线事件（时间戳 = 同步时刻），并污染 `dashboard_service.go:221` 的 SLA 窗口。**仓库自带 fixture 就会踩中**（`glpi_e2e_test.go:117`：status=5、`date:""`、整份 fixture 无 `closedate` 键）。

**清空那一半**（status 非 closed 就不写 `closed_at`）是插入路径的天然性质：字段本就是 `nil`，「不写」==「清空」，**不是** `Create` 立的不变式（`ticket_service.go:202-216` 只有单向设置，**无 else 分支**）。

**已知边界（登记）**：源缺 `date` 时 `created_at` 回落 `now` → 可能出现 `created_at > closed_at` 的倒挂行。不修（§6），但测试须钉住该行为不是静默的。

### 2.5 GLPI 词表：键补全 + 越界处置

```go
// 原：{1: open, 2: in_progress, 3: pending, 4: resolved, 5: closed}
statusMap := map[int]string{1: "open", 2: "in_progress", 3: "pending", 4: "resolved", 5: "closed", 6: "pending"}
// 原：{1: low, 2: low, 3: normal, 4: high, 5: critical, 6: critical}
priorityMap := map[int]string{0: "normal", 1: "low", 2: "low", 3: "normal", 4: "high", 5: "critical", 6: "critical"}
```

**关键澄清（审查 C 的 P2）**：6→`pending`、0→`normal` 是**键补全**（GLPI 合法域内的值），与「越界兜底」是**两件事**。判据必须写成：

```go
st, ok := statusMap[t.Status]
if !ok { /* 越界：∉ 1..6，按 D-1 处置 */ }
pr, ok := priorityMap[t.Priority]
if !ok { /* 越界：∉ 0..6，按 D-1 处置 */ }
```

**为什么不再留空串**：`glpi_e2e_test.go:219-224` 的注释声称「落空串……真出现越界值应当看得见」——但 §1.3 G-4 表明**它看不见**（筛选器选不中、排序垫底、统计不计数），且 G-3 实测落的是 `open` 而非空串。这条注释描述的机制不存在。

### 2.6 `external_id` 幂等（000026）—— 索引 + `ON CONFLICT` 才算幂等

**索引**：不能替换普通索引（`idx_tickets_external_id` 服务于 `WHERE external_id IN (...)` 预查，W6/P16 的性能修复）。PG 部分索引只在查询条件**蕴含**索引谓词时可用，而该查询既推不出 `external_id <> ''`、更推不出 `source='glpi'` —— **两段谓词都不可蕴含**（审查 B 起真 PG 实测确认：替换后计划里部分索引完全不可用）。所以**新增一条、保留原索引**：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_glpi_external_id
    ON tickets(external_id) WHERE source = 'glpi' AND external_id <> '';
```

**`ON CONFLICT`（v2 新增，D-4）**：光有索引只得到「整批硬失败」（§1.5），要兑现 ADR 的「幂等 upsert」须配：

```go
db.Clauses(clause.OnConflict{
    Columns:     []clause.Column{{Name: "external_id"}},
    TargetWhere: clause.Where{Exprs: []clause.Expression{
        clause.Expr{SQL: "source = 'glpi' AND external_id <> ''"},
    }},
    DoNothing: true,
}).CreateInBatches(toUpsert, 100)
```

（部分唯一索引下 `ON CONFLICT (external_id)` 不带谓词会 **42P10**；`TargetWhere` 是必需的。）

**迁移安全（D-5）**：存量重复行会让 `CREATE UNIQUE INDEX` 报 `23505`。迁移的 DDL 与版本记录在**同一事务**（`migrate.go:311-347`）→ 版本不落 → **每次重启重放 → 服务持续不可用**（`database.go:62-66` → `main.go:37-39` `logger.Fatal`）。

且仓库既有实测（`TODO.md` G-22 段）表明：**迁移失败日志不含 PG 的 DETAIL**，只剩 `ERROR: could not create unique index … (SQLSTATE 23505)`，运维定位不到是哪几行。→ 迁移文件内加**前置自检 DO 块**，把无诊断的 23505 变成带样本的 `RAISE EXCEPTION`：

```sql
DO $$ DECLARE n int; s text;
BEGIN
  SELECT count(*), string_agg(external_id, ', ') INTO n, s FROM (
    SELECT external_id FROM tickets
    WHERE source='glpi' AND external_id <> ''
    GROUP BY 1 HAVING count(*) > 1 LIMIT 5) x;
  IF n > 0 THEN
    RAISE EXCEPTION '存量重复 glpi external_id（前 5 个）：%', s;
  END IF;
END $$;
```

（`migrate.go:349-356` 明写 `splitStatements` 支持 dollar-quote，可行。）

### 2.7 防分叉：per-key 对照测试（**不是**值域对照）

**v1 的对照测试对 G-3/G-4 变异免疫**（审查 C 的 P1）：加 `6: "pending"` 不改变 `statusMap` 的**值域**（`pending` 本来由 3 号产生），加 `0: "normal"` 也不改变 `priorityMap` 的值域 → 检查在改动前后**都是绿的**，守不住它配对的缺陷。

正确形态是**逐键对照**（镜像 `glpi_e2e_test.go:206` 既有的 `want` map 写法），把 `openapi.yaml:2503/2506` 的 enum 抄成字面量：

- `statusMap` 的 **1..6 每个键** → 期望值
- `priorityMap` 的 **0..6 每个键** → 期望值
- 越界（`status=7`、`priority=-1`）→ 按 D-1 的既定行为

并注明：**值域对照只能防「新增词表外值」，不能防「缺键」**。

---

## 3. 契约

| 项 | 变化 |
|---|---|
| HTTP 响应**形状** | **不变**。`problem_start` 由 handler 手工序列化（`alert_handler.go:420/445`），**openapi 未声明**（`grep problem_start openapi.yaml` 零命中）；`Ticket` schema 也没有 `closed_at`/`resolved_at` |
| openapi | **不动** → `gen:api`（`frontend/package.json:17`）产物无漂移。✅ `pending`/`normal` 均在 `openapi.yaml:2506/2503` 的 enum 内 |
| `/integrations/sync` 响应 | 该端点**无 openapi 定义**（`gin.H` 直出）→ 若按 D-6 增加 `skipped` 字段**不触发漂移**。原则：**只增不改** |
| 行为可见变化 | ① **仅对同步路径写入的数据**：仪表盘告警 KPI（MTTR/MTTD/密度/resolved-acked）**从恒空变为有数**；② 工单列表不再把历史票顶到最前；③ GLPI 已关闭票若源提供 `closedate` → 进入 SLA 口径；④ 并发同步从「整批硬失败」变为幂等跳过 |

> ⚠️ **①的限定**：`cmd/seed/main.go:218` 的演示数据也不写 `ProblemStart` → **纯 seed 部署**在修完后 KPI 仍恒空。
> ⚠️ 「拓扑窗口 / 资产时间线」在**任何**部署下仍恒空（§1.8 阻断-1）——**不在本轮验收内**。
> ⚠️ ③ 会让 SLA 口径**跳变一次**（历史票补上 `closed_at` 后若落在窗口内会被计入）。这是修复的预期效果，如实记录。

---

## 4. Risk

| # | 失败模式 | 缓解 |
|---|---|---|
| R1 | **改 G-4 打红既有测试** `glpi_e2e_test.go:222`（钉住 `priority=0 → ""`） | **故意的行为变更**：更新该用例并写明立场（旧注释声称的「看得见」机制不存在，§2.5）。**不是放宽断言**——断言本身建立在错误前提上。红色集合精确等于这一条（`:206/:210/:214` 均不受影响） |
| R2 | **时区偏移 8 小时**（§2.2） | D-3 拍板 + 显式 `glpiLoc`；`parseGLPITime` 内部 `.UTC()`。**测试必须钉住**：`("2026-06-15 10:00", Asia/Shanghai)` → 落库挂钟必须是 `02:00`，不是 `10:00` |
| R3 | **源缺失被当成解析失败 → 回落 `now` → 伪造「工单关闭」事件**（§2.4，D-P0-2） | tri-state（§2.1）+ 表驱动测试覆盖 `""`/null/哨兵/非法四类输入；**用仓库自带 fixture（`glpi_e2e_test.go:117`）做回归**，断言 `closed_at IS NULL` |
| R4 | **000026 在存量库上失败 → 服务持续不可用、日志无 DETAIL** | 迁移内前置自检 DO 块（§2.6）+ 升级冒烟里预置一行重复 `glpi external_id`、断言 `migrate.Up` **失败**（负循环，照 `TestDBSmoke_NetBoxUpsert` 先例）。**注意**：现有升级冒烟库的存量票不写 `external_id` → 这条路径**默认永不触发**，必须显式构造 |
| R5 | **并发同步从「静默重复」变成「整批回滚 + 500」**（若 D-4 不采纳 `ON CONFLICT`） | 采纳 `ON CONFLICT ... DoNothing`（§2.6）；若否，§3/§1.5 必须写明后果并修 `service.go:268-269` 注释 |
| R6 | **KPI 从「空」变「有数」被误读为数据异常** | §3 如实记录 |
| R7 | **`created_at > closed_at` 倒挂行**（源缺 `date`） | 登记为已知边界（§6）；不修，但不静默——日志 + 测试钉住 |
| R8 | **`AssignTicketNumbers` 并发同号**（既有缺陷，非本轮引入） | **不修**（超范围，§6 登记）；000026 不解决它，文档不得声称「并发同步可用」 |

---

## 5. 执行步骤

| 步 | 内容 | 验证 |
|---|---|---|
| 0 | **实测前置**（①已由 §1.7 覆盖，取消）：② 确认新增部分索引后 `WHERE external_id IN (...)` 的 EXPLAIN **仍走** `idx_tickets_external_id`；③ `CreateInBatches` + `OnConflict{TargetWhere}` 撞部分唯一索引时的 SQLSTATE（验证 42P10 是否按预期规避） | 真 PG 探针，跑完即删（同 M25 步骤 0 做法）；结论回填 |
| 1 | `timeparse.go`（tri-state）+ 表驱动单测 | 纯函数单测；覆盖 `""`/`null`/`0000-00-00`/`0001-01-01`/多格式/`2026-06-15T10:00:00Z`；**时区用例钉住 `.UTC()`**；变异反证 |
| 2 | Zabbix：`problem_start` ← `lastchange` + 回落日志 | **既有红集合为空**（无用例喂 `lastchange`）→ 守门人**全是新用例**；须新增带 `lastchange` 的 fixture（现有 `upsert_test.go:370/473/514` 的 trigger JSON **无该字段**）；变异：写死 `now` → 必须红 |
| 3 | GLPI：三个时间戳落库 + 三态处置 | **既有红集合为空** → 全新用例；用 `glpi_e2e_test.go:117` 做回归（断言 `closed_at IS NULL`）；变异：`created_at` 保持 `now` → 必须红 |
| 4 | GLPI 词表补全 + 越界处置 + per-key 对照 | 打红并更新 `glpi_e2e_test.go:222`；**新增 status 1..6 per-key 对照**（现无）；G-3 既有覆盖为零 → 全新用例 |
| 5 | 注释纠正（`service.go:245` + `:268-269` + `upsert_test.go:50/:351/:458`）+ 迁移 000026（含 DO 自检）+ `db_smoke.sh` 白名单 | 真 PG 冒烟两轮。新用例归位：索引形态 → `:198`（fresh）；**存量重复挡住迁移的负循环 → `:205`**（upgrade）。Down 链编辑面：插入一个 Down 块（滚 000026 + 断言 `uq_tickets_glpi_external_id` 消失）+ `:1146-1164` 前置 3 加 has26 守卫 + `:1168-1178` 首个 Down 块改写/前移 + 顺手修 `:740/:1185/:1200` 已写歪的序数 |
| 6 | 台账（`FIX-PLAN-UI-PERF.md` §8）+ 交接 | —— |

每步收尾：变异反证（红在断言、红色集合与预判精确一致）→ 门禁（`gofmt` + `go vet` + `go test ./...` + 相关真 PG 冒烟）→ 台账 → commit + push。

---

## 6. 边界

- **不做**：已存在工单/告警的更新（拍板②；ADR-0004）。
- **不做**：`alerts.host_id` / `alerts.asset_id` 的资产关联解析（**D-7**）—— 它是独立的一整块（hostname → assets 匹配），塞进 M26 会把「导入保真」变成「资产关联」。**登记为前置项**：未做之前，拓扑窗口与资产时间线的告警数据恒空（§1.8 阻断-1），本文档**不得**把它写进验收。
- **不做**：`AssignTicketNumbers` 的并发同号竞态（§1.5；R8）。登记。
- **不做**：`cmd/seed` 演示数据补 `ProblemStart`（导致纯 seed 部署 KPI 仍空，§3）。登记。
- **不做**：GLPI 的 `ticket_type`（`glpi.go:168` 硬编码 `"incident"`，忽略 GLPI `type` 1=incident/2=request）。`ticket_type` 不在 openapi `Ticket` schema 里、无消费者 → 独立决策。登记。
- **不做**：GLPI 工单的出生历史（M25 已拍板；ADR-0004 下它们是导入的参考数据，无经手人）。
- **不做**：未来时间校验、时区自动探测（§2.2 已给显式配置的正解）。
- **不做**：`created_at > closed_at` 倒挂的修正（R7）。登记。
- **登记**：GLPI 高层 API v2 的 ISO 时间形态（§2.1）；DB 默认值对 PG 会话时区的未 pin 耦合（§1.8 低-2）；仓库两个「现在」（§1.8 低-3）。
- **登记**：`alerts` 没有进度/历史表，「已同步过的告警在 Zabbix 侧 ack 了」不会回灌。

---

## 7. 决策表（**已拍板** 2026-09-11）

> 下表「拍板」列即最终决定；「推荐」列保留以备回溯当时的备选项与理由。

| # | 问题 | 选项 | 推荐 → **拍板** |
|---|---|---|---|
| **D-1** | GLPI **越界**档位（`status ∉ 1..6`、`priority ∉ 0..6`）怎么办？ | (a) 兜底成契约默认（`open`/`normal`）；(b) **跳过该票 + 计数 + 逐条日志**；(c) 整批失败 | **(b)** —— 与 M18「拒绝而非静默改写」同立场；(a) 把上游异常藏起来。须同时定 D-6 → ✅ **已拍板 (b)** |
| **D-2** | 时间解析的**三态**处置 | 源缺失→`nil`（`closed_at`/`resolved_at`）/`now`（`created_at`）；解析失败→同左 + 日志 | 即 §2.4 表。v2 的核心修正 → ✅ **已拍板：写 NULL，不发明**（与 `Create` 的补 `now` 刻意不同） |
| **D-3** | **GLPI 实例的时区**（决定能否与 GLPI 界面对齐） | 一个具体值 | 仓库 `docker-compose.yml:181` 的 GLPI 容器是 `TZ=Asia/Shanghai` → ✅ **已拍板：暂不确定，先按 `Asia/Shanghai` 实现**，写成显式常量 + **登记待核对**（不猜、不自动探测） |
| **D-4** | 000026 是否配 `ON CONFLICT ... DoNothing`？ | (a) 配；(b) 不配（索引只作兜底） | **(a)** —— ADR-0004 Risk-3 承诺的就是幂等；代价是 `synced` 改用 `RowsAffected` → ✅ **已拍板 (a)** |
| **D-5** | 000026 遇**存量重复** `external_id` | (a) **直接建，失败就失败**（fail-closed）+ 前置自检 DO 块；(b) 迁移里先 `DELETE` 再去重（**静默删业务行**）；(c) 本轮不建索引 | **(a)** —— (b) 在迁移里静默删数据不可接受 → ✅ **已拍板 (a)** |
| **D-6** | D-1 的「跳过数」怎么暴露？ | (a) 返回 `(synced, skipped int, err error)`，`results` 增键 `glpi_skipped`；(b) 只记日志 | **(a)** —— 前端 `Settings.tsx:191-193` 直接渲染「新增 N 条」，跳过的票**永久不可见**；该端点无 openapi 定义 → 无漂移 → ✅ **已拍板 (a)** |
| **D-7** | `alerts.host_id` 缺口是否纳入本轮？ | (a) **不纳入**，收窄验收 + 登记为前置项；(b) 纳入，做 hostname→assets 匹配 | **(a)** —— (b) 会把 M26 从「导入保真」扩成「资产关联」 → ✅ **已拍板 (a)** |
| **D-8** | Zabbix 告警的 `created_at` | (a) 保持 `now`；(b) 也给 `lastchange` | **(a)** —— `models/alert.go:24-25` 两列并存就是「入库时刻 vs 业务时刻」的语义分工 → ✅ **已拍板 (a)** |

---

## 8. 审查结论与处置（4 路对抗审查，2026-09-11）

| 来源 | 结论 | 处置 |
|---|---|---|
| A-阻断-1 | `alerts.host_id`/`asset_id` 无生产写入方 → §3 的「拓扑/时间线变为有数」是假承诺 | ✅ **收窄 §3**；D-7 拍板；登记为前置项 |
| A-中-1 | `diagnostic_service.go:294` 的窗口建在 `resolve_time` 上，非 `problem_start` | ✅ 从消费方表移除；改述为「host_id 导致空 + 若修好会产出垃圾 MTTR」；建议加下界防护 |
| A-中-2 | 「趋势恒空」为假（`dashboard_service.go:113-116` 是 alerts 且按 `created_at`） | ✅ 从受影响清单移除 |
| A-中-3 | G-1 引用了不存在的「工单趋势」 | ✅ 改引真实消费方（工单列表排序 + 游标分页，`ticket_service.go:87/:90`） |
| A-中-4 | §1.2 表格与正文自相矛盾（`:97` 的 ack/resolve OR） | ✅ 表格改为「未处理的缺 triggered；已处理的以零值时间戳出现」 |
| A-低-1/2/3 | 「绕开全部不变式」不精确；§2.3 误称 Create 立过「非 closed 不写」；遗漏 `alert_service.go:334-338` | ✅ 全部修正（§1.1/§2.4/§1.2 第七处） |
| B-F1 | `CreateInBatches` 是整批原子回滚，非逐行；`service.go:268-269` 注释将变假 | ✅ §1.5 + §2.6 加 `ON CONFLICT`（D-4）；步骤 5 补注释修正 |
| B-F2 | 先炸的是 `ticket_number` 竞态，不是 `external_id`；000026 价值被高估 | ✅ §1.5 重写；R8 登记 |
| B-F3 | D-3 运维代价被低估：持续 down + 日志无 DETAIL + 失败路径无自动化验证 | ✅ §2.6 加 DO 自检；R4 + 步骤 5 加负循环冒烟 |
| B-F4 | D-1 跳过使 `synced` 静默变小，前端当「新增」展示 | ✅ D-6；§3 放宽为「只增不改」（该端点无 openapi 定义） |
| B-F5/6/7 | Down 链真实行号 `:1146-1173`；白名单归位；`CREATE UNIQUE INDEX` 非 CONCURRENTLY 的锁语义 | ✅ §1.9 + 步骤 5 |
| C-P1 | §2.5 的**值域**对照对 G-3/G-4 变异免疫 | ✅ §2.7 改为 **per-key** 对照 |
| C-P2 | §2.3 代码块 = D-1(a)，与 D-1 推荐 (b) 矛盾 | ✅ §2.5 澄清「键补全」vs「越界兜底」，判据写成 `_, ok :=` |
| C-P3 | G-1/G-2/G-3 既有覆盖为零，守门人全是新用例 | ✅ §1.3 + 步骤 2/3/4 显式标注 |
| C-P4 | §3 的 ① 未限定，纯 seed 部署仍恒空 | ✅ §3 加限定 |
| C-P5/6 | Down 链编辑面被低估（含已写歪的序数）；`upsert_test.go` 三处注释将变假 | ✅ §1.9 + 步骤 5 |
| D-P0-1 | **D-4 是伪命题**：pgx 丢弃 location，(a)/(b) 落库相同且都差 8 小时 | ✅ §2.2 整条重写；D-3 改问 GLPI 时区 |
| D-P0-2 | 源缺失回落 `now` → 伪造「工单关闭」事件；自带 fixture 即踩中 | ✅ §2.1 tri-state + §2.4「不发明」；R3 |
| D-P0-3 | `0000-00-00` 实测被 Go 拒绝（方向反了）；`0001-01-01` 触发 autoCreateTime 静默替换 → 假绿 | ✅ §1.9 登记；测试须覆盖该输入 |
| D-P0-4 | api 容器 TZ 是 UTC；仓库有两个「现在」 | ✅ §1.8 低-3；§2.2 改用显式 `glpiLoc` |
| D-P0-5 | R2 把「显式配置 GLPI 时区」一起否掉了 | ✅ §2.2 区分「自动探测」（拒）与「显式配置」（必需） |
| D-P1-1/2/3 | `TranslateError` 未开；DB 默认值依赖 PG 会话时区；ISO v2 形态 | ✅ §1.8 低-4/低-2 + §2.1/§6 |
| C-复核 | 3 处断裂面全部成立；`TestDBSmoke_MigrateRunner` 等 3 处**不会红**；前端 `problem_start` 零命中 | ✅ §1.9 已核安全 |
