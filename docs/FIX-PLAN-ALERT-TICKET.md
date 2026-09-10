# 功能需求文档：告警 → 一键建单（收口 D-3）

> 状态: **已实现（含前端入口，§7）** —— 后端步骤 1–8 全落地；前端「建单」按钮按 §7 落地，2026-09-10
> 日期: 2026-09-10
> 基线: `main` @ 6a501e0
> 依据: `05-运维工单.md` §5.4、[ADR-0004](adr/0004-工单SoT决策.md)、`TODO.md` D-3
> 前置: `docs/FIX-PLAN-D1-D7.md` §3.5（D-3 只修了字段语义，功能「另立任务」——就是本文档）

---

## 0. 一句话

`alerts.ticket_id` 这个外键列**至今没有任何写入方**——告警详情里想建单，运维只能自己新开一张工单、手打一遍主机名和故障现象，两边从此再无关联。本轮补上「人在告警详情点一下建单」这一条**唯一**的写入路径（自动建单不做，ADR-0004 硬约束）。

---

## 1. 现状（实测，2026-09-10）

```bash
$ grep -rn "ticket_id\|TicketID" --include=*.go --include=*.ts --include=*.tsx backend/ frontend/src/ | grep -v _test
backend/internal/models/alert.go:45:	TicketID *uuid.UUID `json:"ticket_id" gorm:"type:uuid"`
```

| 事实 | 证据 |
|------|------|
| 列已在迁移里（`000013` 补列），模型有字段，注释已按 ADR-0004 改为「指向本系统 `tickets.id`」 | `models/alert.go:43-45` |
| **无任何写入方**：全仓库（含前端）只有定义处一处引用 | 上面的 grep |
| `tickets` 建单路径健全：`TicketService.Create` 有工单号撞号重试（D-2 修复） | `service/ticket_service.go:94-129` |
| 「建单由人点」是已定案的架构约束，不是遗留 TODO | `01-需求概述.md:70`、`05-运维工单.md:90`、`09-AI辅助.md:49` |
| 写操作对只读 API Key 已封死（D-7 修复） | `middleware/auth.go:165-166` `apiKeyAllows` |

---

## 2. 设计决策

| # | 决策 | 理由 / 被否方案 |
|---|------|----------------|
| K-1 | **只做「人点」入口，不做自动建单** | ADR-0004 硬约束：AI 层只读，写操作必须由人点。自动建单会绕过这条线 |
| K-2 | **幂等**：告警已关联工单时返回**既有**工单（`created:false`），不建第二张 | 被否：返回 409 让前端自己处理。重复点击是手指问题不是错误，让 UI 每次都拿到一张可跳转的票更简单 |
| K-3 | **建单 + 写回 `ticket_id` 同事务** | 被否：先建票再写回（两次独立提交）。中间挂掉就留下**无主工单**，运维再点一次又是一张 |
| K-4 | **认领优先**：先 `UPDATE alerts SET ticket_id=:newID WHERE id=:id AND ticket_id IS NULL`，**抢到才建票**；没抢到就读出既有票返回 | 见 §2.1。被否：`SELECT ... FOR UPDATE` 行锁——SQLite 不支持该语法，会让本包全部 sqlite 测试报语法错 |
| K-5 | 端点放在 `/alerts/{id}/ticket`（alert 的子资源），handler 实现在 `TicketHandler` | 被否：`/tickets/from-alert`。REST 语义上「为这张告警建票」读起来更顺；工单号生成 + 撞号逻辑都在 `TicketService`，放它那儿才不用把这套逻辑复制一份 |
| K-6 | 不加入 HolmesGPT toolset 清单 | `09-AI辅助.md:49`：ack / 建单 / 删除端点不进 toolset。API Key scope 层（K 的兜底）已由 D-7 封死 |

### 2.1 为什么是「认领优先」而不是「先查再写」

最直觉的写法是「先查 `ticket_id` 是否为空，为空就建票写回」。问题在于**两次点击同时到达时两边都查到空**，于是各建一张票——而两次点击都来自同一个运维的鼠标，这恰恰是现实中最常见的并发。

把条件 UPDATE 提到最前面，它就成了**唯一的裁决点**：`AND ticket_id IS NULL` 是数据库层面的原子条件，N 个并发请求里恰好一个 `RowsAffected=1`，其余全部为 0。后到者在 PG 的 READ COMMITTED 下会阻塞到先到者提交，再按**更新后的行**判 `IS NULL`——判定结果确定，不会两个都赢（这是 PG 的行锁 + EvalPlanQual 语义，不是我的假设）。

抢到的人才插票，所以：

- **输家从不建票**，无需「插入后再撤销」，事务里没有需要清理的中间态；
- 输家随后读出赢家写好的 `ticket_id`，返回 `created:false` + 那张票——**双击拿到的是同一张票，不是 409**。失败路径因此少一条；
- 若插入票那步失败（撞号等），整个事务回滚，`ticket_id` 的认领一并撤销，不留悬空指针。

代价：已关联的告警每次多一条命中 0 行的 UPDATE。可忽略。

### 2.2 事务里为什么不复用 `TicketService.Create` 的重试循环

`Create` 的撞号重试依赖「每次 `Create` 是独立的隐式事务」——第 N 次失败后事务已回滚，第 N+1 次是干净的新事务。**放进显式事务里这个前提就没了**：PG 事务内任一语句报错，整个事务进入 aborted 状态，后续语句一律 `current transaction is aborted`，重试必然失败（除非用 SAVEPOINT，那是为 5 次重试引入一层嵌套事务——不值）。

所以本路径**不做重试**：撞号（同日并发建单，概率极低）→ 事务整体回滚 → 返回 5xx → 人再点一次。**不产生孤儿票、不产生重复票**，这个不变量比「自动重试一次」值钱。

---

## 3. 接口契约

```
POST /api/alerts/{id}/ticket
权限：canWrite（admin / ops_admin / ops_user；只读 API Key 被 D-7 的 scope 拦截）
请求体：无
```

| 状态码 | 响应 | 含义 |
|--------|------|------|
| 201 | `{"code":0,"data":{"ticket":{…},"created":true}}` | 抢到认领：新建并已关联 |
| 200 | `{"code":0,"data":{"ticket":{…},"created":false}}` | 已被关联（自己重复点 / 别人先点到），幂等返回既有票 |
| 404 | `{"code":"not_found",…}` | 告警不存在，或其关联的工单已失效（见 R-3） |

### 工单字段派生

| 工单字段 | 来源 | 说明 |
|----------|------|------|
| `title` | `[主机名] 触发器名` | 触发器名为空退化用 `problem`，再空退化用 `告警 <alert_id>`；**按 rune 截断到 255**（`varchar(255)` 在 PG 里数字符，但触发器名最长 500） |
| `description` | 固定模板 | 来源告警 ID / 主机 / 触发器 / 级别 / 开始时间 / 现象，纯文本 |
| `ticket_type` | `incident` | 告警即故障 |
| `priority` | severity 映射 | 0,1→`low`；2,3→`medium`；4→`high`；5→`critical`。**原写「与前端 Tickets 页下拉一致」是错的**（实测下拉是 `normal`，见 M16）——本函数跟的是 GLPI 同步的 `medium`，代价是工单页按「普通」筛选查不到这些票 |
| `status` | `open` | 由 `Create` 默认值填 |
| `source` | `alert` | 模型注释的枚举 `manual, email, api, glpi` 之外**新增 `alert`**，需同步注释 |
| `asset_id` / `asset_name` | 告警的 `asset_id` / `host_name` | 资产关联能带上就带上 |
| `requester_name` | JWT `username` | 与 `AcknowledgeAlert` 取用户名的写法一致 |
| `tags` | `[]` | 由 `Create` 默认值填 |

---

## 4. Risk

| # | 失败模式 | 影响 | 缓解 |
|---|----------|------|------|
| R-1 | 同一告警被建出两张工单（双击 / 两人同时点） | 重复工单 | K-4 条件 UPDATE 认领：只有 `RowsAffected=1` 的那个请求会插票。**测试**：连调两次，断言第二次 `created=false`、返回**同一张**票、`tickets` 表只有 1 行。变异「去掉 `AND ticket_id IS NULL`」必须让这条变红（去掉后第二次会覆盖认领并多插一张票） |
| R-2 | 同日并发建单撞工单号 | 建单失败（5xx） | 整个事务回滚，认领一并撤销，无孤儿票无重复票；人重试即成功。残留边界：`generateTicketNumber` 按当天条数取号，**删过工单后仍会撞**（G-25 已记录的同一条边界） |
| R-3 | `alerts.ticket_id` 指向的工单行不存在（人工改库 / 外部写入） | 该告警建不了单 | 返回 404 并在 handler 文案里点明「关联工单已失效」，不静默改写别人写的关联。本系统**无删除工单的端点**，理论上不可达，属防御性分支 |
| R-4 | 标题超 255 字符被 PG 拒绝 | 建单 500 | 按 rune 截断（不是 byte——中文截半会写出非法 UTF-8） |
| R-5 | 端点被 AI 层调用 | 越过「只读」红线 | 不进 toolset 清单（K-6）；只读 API Key 在 `apiKeyAllows` 就 403 |

---

## 5. 执行步骤

1. `service/ticket_service.go`：`TicketService` 接口 + `ticketService` 增 `CreateFromAlert`；新增 `ticketFromAlert` / `priorityFromSeverity` / `truncateRunes` 三个包内 helper
2. `api/handlers/ticket_handler.go`：`CreateTicketFromAlert` handler（201/200 分支）
3. `api/routes.go`：`alerts.POST("/:id/ticket", canWrite, ticketH.CreateTicketFromAlert)`
4. `api/handlers/rack_ticket_handler_test.go`：`mockTicketService` 补新方法（否则接口不满足，编译失败）
5. 测试：service 用 `newDiagTestDB`（真 sqlite，本包已有 alerts + tickets 表）测端到端——首次建单、二次幂等、告警不存在、关联悬空、字段派生（标题截断/优先级映射/资产带过去）；handler 用 mock 测 201/200/404 分支
6. 变异反证：至少覆盖 R-1（去掉 `AND ticket_id IS NULL` → 第二次变成建重复票）与 R-4（截断改 byte → 中文标题截出非法 UTF-8）
7. 契约：`openapi.yaml` 加路径 + `npm run gen:api`（CI 会校验生成物无差异）
8. 台账：`TODO.md` D-3 收口；`05-运维工单.md` §5.4 口径同步

---

## 6. 边界（本轮不做）

| 不做 | 原因 |
|------|------|
| 自动建单 / 规则触发建单 | ADR-0004 硬约束 |
| ~~前端「建单」按钮~~ | **✅ 已完成**（§7，2026-09-10）。入口位见 §7.1（原写「`Alerts.tsx` 详情抽屉」，实测**该页没有详情抽屉**，已纠正） |
| 工单 → 告警的反向导航 | 无需求；`alerts.ticket_id` 是单向链接 |
| 建单事件发 eventbus / 通知 | 无需求（通知链路当前只订阅 `alert.created`/`alert.resolved`） |
| `/tickets` 列表按来源筛选 | `source='alert'` 是新值，前端筛选器不认——**这是本轮的已知副作用**，下一轮随前端一起处理 |

---

## 7. 前端入口（下一轮，2026-09-10 补设计）

### 7.1 入口位置：`getAlertActions`，不是详情抽屉

§6 原写「`Alerts.tsx` 详情抽屉是入口位」——**实测该页没有详情抽屉/弹窗**。告警页的操作只有两处，且共用同一个决策函数：

| 界面 | 文件 | 说明 |
|------|------|------|
| 桌面表格操作列 | `components/AlertTable.tsx:130` | `getAlertActions(record, handlers)` |
| 移动端卡片 | `components/AlertCard.tsx:31` | 同一个 `getAlertActions` |

`getAlertActions`（`AlertTable.tsx:57`）是 M13 抽出来的**纯函数**，存在的理由就是「桌面端与移动端不漂移」（H9 教训）。建单入口挂这里，一处实现两个界面同时生效，且天然有测试缝（`AlertCard.test.tsx` 已按这个模式测另外 4 个动作）。

### 7.2 可见性：用 `ticket_id` 判断，但**正确性不依赖它**

`alerts` 列表的 `items` 是 `[]models.Alert` 直出（`alert_handler.go:61`），**含 `ticket_id`** —— 前端 `Alert` interface（`AlertTable.tsx:9`）此前没声明这个字段，本轮补上。

| `record.ticket_id` | 渲染 |
|---|---|
| 空 / 未定义 | 「建单」，可点，调 `POST /alerts/{id}/ticket` |
| 非空 | 「已建单」，`disabled` |

**关键**：列表里的 `ticket_id` 是**查询时的快照**。快照为 null 而实际已被别人建单时，点击后后端幂等返回 `created=false` → 前端提示「该告警已建单」。所以前端的可见性判断**只是省一次请求的优化**，正确性由后端幂等兜底 —— 反过来（让前端判断承担防重职责）就会在并发下建出两张票，正是后端 K-2 认领优先要解决的问题。

### 7.3 文案分流（`created` 是给用户看的，不只是给代码看的）

| 响应 | 提示 |
|------|------|
| `created=true` | `success`「已建单 TICKET-…」 |
| `created=false` | `info`「该告警已建单：TICKET-…」（**不是 error**——这不是失败） |
| 请求失败 | `error`「建单失败」 |

成功后 `refetch()`：让 `ticket_id` 刷新，按钮自动翻成「已建单」。

### 7.4 Risk

| # | 失败模式 | 缓解 |
|---|---------|------|
| R-1 | `AlertAction.onClick` 改可选后，既有 4 个动作漏传 onClick 不再编译报错（类型保护变弱） | 只有 disabled 项省略 onClick；渲染处 antd `Button` 本就接受 `undefined`；既有动作的点击行为已有 `AlertCard.test.tsx` 4 条断言守着 |
| R-2 | `Alerts.tsx` 新增的回调没进 `AlertTable` columns 的 `useMemo` 依赖 → memo 失效（P4 教训） | `handleCreateTicket` 走 `useCallback`，并计入 columns 依赖数组 |
| R-3 | 提示文案把「已建单」说成失败，运维重复操作 | `created=false` 走 `info` 不走 `error`；测试断言两条文案互不相同 |
