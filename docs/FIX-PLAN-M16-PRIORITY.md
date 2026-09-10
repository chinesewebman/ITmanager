# 需求文档：工单优先级词表归一（收口 M16）

> 状态: **已实施**（实现 + 测试 + 文档同步完成，待提交）
> 日期: 2026-09-10
> 基线: `main` @ e10eac9
> 依据: `docs/FIX-PLAN-UI-PERF.md` §8 已知阻塞 · M16
> 决策: 2026-09-10 用户拍板「按 (a) 的最佳实践做」——以契约为准，数据与写入方向 `normal` 收敛
> 审查: 两路只读审查（反方/最小方案、一致性回归面）已完成，发现全部逐条核实后处置，见 §8

---

## 0. 一句话

`tickets.priority` 用**两套词**表示同一个「普通」：契约（openapi enum）与手工建单表单用 `normal`，GLPI 同步与告警一键建单用 `medium`。该列**无 CHECK 约束**，两套值都能落库。后果是工单页按「普通」筛选（`WHERE priority='normal'`）**查不到告警派生出来的票**。

本轮是选项 (a)：以契约为准，把**存量数据**与**三个 medium 写入方**一起收敛到 `normal`。

---

## 1. 现状（实测，2026-09-10）

### 1.1 全部读写方

| 角色 | 位置 | 用词 |
|------|------|------|
| **筛选**（唯一功能性读者） | `service/ticket_service.go:53` `q.Where("priority = ?", f.Priority)` | 前端下发 `normal` |
| 写入 · 手工建单 | `components/TicketFormModal.tsx`（options + 默认值） | `normal` |
| 写入 · GLPI 同步 | `integration/glpi.go:161` `priorityMap` | `medium` → 本轮改 `normal` |
| 写入 · 告警建单 | `service/ticket_service.go:259` `priorityFromSeverity` | `medium` → 本轮改 `normal` |
| 写入 · **seed 演示数据** | `cmd/seed/main.go:280` | `medium` → 本轮改 `normal` |
| 写入 · **HTTP 建单（不传 priority）** | `service/ticket_service.go` `Create` | `''` → 本轮兜底 `normal`（§1.4） |
| 写入 · GLPI 同步 upsert | `integration/upsert.go` | 透传上面的映射结果，无独立词表 |
| 显示 · 工单表 | `components/TicketTable.tsx`（`PRIORITY_LABEL` + `PRIORITY_WEIGHT`） | **已兜底**：两个词都映射「普通」，排序同权 |
| 显示 · 工单详情 | `components/TicketDetailModal.tsx` | **已兜底**：同上 |
| 注释 · 模型 | `models/ticket.go:17` | `// low, medium, high, critical` → 本轮改 `normal` |

### 1.2 契约

```yaml
# backend/internal/api/openapi.yaml:2448-2450  Ticket（响应）
priority:
  type: string
  enum: [critical, high, normal, low]      # ← 契约早就写了 normal，且被 CI 钉着

# backend/internal/api/openapi.yaml:2475  TicketInput（请求）
priority:
  type: string                             # ← 无 enum：请求可以塞任意值
```

**契约侧无需改动** —— enum 已经是 `normal`。改的是数据与写入方，让它们符合既有契约。

`normal` 胜出的**硬论据**：响应 enum 是**机器校验**的契约 —— CI 有 `npm run gen:api && git diff --exit-code -- src/services/api.types.ts` 钉着生成物，按 spec 生成的消费方会把 `medium` 当非法值。prose 注释不是契约。仓库已有同款先例：`TicketStatsCards.tsx:3-7` 明确写下「档位取**契约**域，不取 `models` 注释」，并因此挡掉了 GLPI 状态 3 的 `pending` 被漏统计。

### 1.3 影响面比「两套词」听起来小

- **没有任何按 priority 聚合的地方** —— Dashboard / KPI / 报表都没有 `GROUP BY priority`。
- **显示层已兜底**（1.1 最后两行），所以分歧唯一真正露头的地方是**服务端筛选**。
- `ticket_sla.priority`（`000001:816`）看似同族，但**全仓库零引用**（无 Go 代码、无端点、无测试）—— 死表，不在本轮范围。
- `alert_rules.priority`（`000013:233`）是 `BIGINT DEFAULT 0`，语义是规则排序，与词表无关。
- 无外部消费方：GLPI 写回路径不存在（客户端只有 `InitSession`/`GetTickets`/`KillSession`，无写方法），Zabbix 同步只写告警不写工单。

### 1.4 缺陷面：未被约束的写入口

`POST /tickets` → `TicketHandler.CreateTicket`（`ticket_handler.go:79`）→ `TicketService.Create` 只校验 `Title != ""`，**priority 不校验**；`PUT /tickets/:id` 收的是 `map[string]interface{}` 直落 `Updates()`，更宽（可写任意列）。

**其中「不传 priority」是活路径，本轮已修**：`models.Ticket.Priority` 没有 `default:` tag，`Create` 的兜底链（`Status`/`Source`/`Tags`）**独独漏了 `Priority`** → `POST {"title":"x"}` 落一行 `priority=''`，既筛不出也不显示。已按同款模式补 `if t.Priority == "" { t.Priority = "normal" }`。

**其余（非空但词表外的值，如 `"urgent"`）登记为独立缺陷，不在本轮修**（理由见 §6）：堵它要动契约（加 enum + 重新生成），且 `PUT` 的 mass-assignment 面比 priority 一个字段宽得多。

---

## 2. 设计决策

| # | 决策 | 理由 / 被否方案 |
|---|------|----------------|
| N-1 | **以 `normal` 为准** | 契约（openapi enum）被 CI 钉着、是机器校验的响应契约；手工建单表单（人的主路径）用 `normal`；ITmanager 是工单 SoT（ADR-0004）。被否：以 `medium` 为准 —— 要改 openapi enum + 重新生成 `api.types.ts`（会触发 CI 的 `git diff --exit-code`）+ 前端 3 处字典 + 表单选项，且**要迁的存量行更多**（手工建单是主路径）。注：`05-运维工单.md:55` 曾把「代码注释」指定为权威来源，而 `models/ticket.go:17` 那条注释写的恰是 `medium` —— 这正是把注释一并改掉的原因 |
| N-2a | **迁移序号取 `000023`，不是 `000022`** | 实测 `ls backend/migrations/*.up.sql`：**盘上没有 `000022`**，它只是被 `docs/FIX-PLAN-UI-PERF.md:242/310` 的 P20（pg_trgm）在计划里预占。让号而不是抢号：撞号是静默事故，而 P20 那份文档不会被本轮同步更新，抢号会制造一次真实的文档/代码冲突。代价是一个版本号空洞 —— 无功能影响（`migrate.Up` 按版本升序、`Down` 只回滚**已应用的最大版本**），且 §5 步骤 10 的撞号守卫已把这类事故从「静默」变成「启动即失败」。详见 §7 |
| N-2 | **迁移 000023 改存量数据**，`UPDATE tickets SET priority='normal' WHERE priority='medium'` | 只改一个已知同义词，天然幂等（重复执行第二次命中 0 行）。被否：应用启动时归一 —— 隐式写库、不可审计、无法在升级路径上验证 |
| N-3 | **down 显式声明不可逆**，不做数据改写 | 迁移后**无法区分**「原本就是 `normal`」与「从 `medium` 迁来的」行。写 `UPDATE ... SET priority='medium' WHERE priority='normal'` 会把本来正确的行一起污染。真正的回滚走备份恢复 |
| N-4 | **三个写入方与迁移同一提交** | 只改数据不改写入方 = 下次 GLPI 同步把 `medium` 写回来，迁移白做且在生产静默发生 |
| N-5 | 前端同义词字典（`TicketTable` 的 `PRIORITY_LABEL`/`PRIORITY_WEIGHT`、`TicketDetailModal` 的 `PRIORITY_LABEL` —— **共三处**）**本轮保留** | 未跑迁移的库、或迁移后被外部写入的库，行仍能正确显示（含排序档位）。删它要等一个版本之后。**保留后两侧都无自动化守门人**，所以注释里必须写明「为什么留、何时可删」 |
| N-6 | **不加 DB `CHECK` 约束** | 三条理由：① 仓库 22 个迁移里**零 CHECK 先例**，引入新范式；② §1.4 的写入口只封了「空值」这一半，加约束会把「静默接受任意值」变成「500」——那是行为变更，属另一类改动；③ 测试 schema 是各测试手写的，迁移里的约束在 sqlite 测试中不存在，会造成「约束只在生产生效」的假安全感 |
| N-7 | 前端筛选/下拉**不改** | 归一后 `WHERE priority='normal'` 自然能查到告警票 —— 这正是本轮要修的东西，不需要前端配合。`Tickets.tsx` 的下拉里本来就没有 medium 项 |

---

## 3. 契约与生成物

| 产物 | 是否变化 |
|------|---------|
| `openapi.yaml` | **不变**（enum 已是 `normal`） |
| `frontend/src/services/api.types.ts` | **不变**（生成自 openapi） |
| `frontend/src/types/index.ts` / `TicketFormModal` 的 priority union | **不变**（本就是 `'critical'\|'high'\|'normal'\|'low'`） |

CI 的 `git diff --exit-code -- src/services/api.types.ts` 不会触发。

---

## 4. Risk

| # | 失败模式 | 影响 | 缓解 |
|---|----------|------|------|
| R-1 | **迁移改存量数据且不可逆**（down 无法忠实回滚） | 误改后无法用迁移本身撤回 | ① 迁移只命中 `priority='medium'` 这一个已知同义词，不做「未知值归一」；② down 文件显式写明不可逆 + 走备份恢复（不写会污染正确行的反向 UPDATE）；③ 上线前 `pg_dump` 备份（运维流程，文档写明）。**实测评估**：三条 medium 写入路径里，GLPI 同步默认不启动（`profiles:["aux"]`）、告警建单今天才上线、seed 只在全新库跑 —— 真实存量库命中 0 行的概率不低。0 行时这条迁移是空转，无害；但**不能据此删掉它**：GLPI 一旦被启用过，那批行就永久筛不出来 |
| R-2 | **漏改写入方** → 迁移被下次同步/灌数据抵消 | 生产静默复发，且比修复前更难发现（以为已经修了） | **三个**写入方（`glpi.go:161` · `priorityFromSeverity` · seed `main.go:280`）与迁移**同一提交**；两个映射函数各加单测断言「输出 ∈ 词表」+「永不产出 medium」，变异反证必须红。**seed 是最阴险的一个**：它是一个字面量、没有函数可测，`make db-reset` 与 `smoke-compose.sh` 又都在**迁移之后**灌数据 → 改回 `medium` 不会有任何用例变红。缓解：dbsmoke 升级路径的全表兜底断言（`priority NOT IN (...)` 计数为 0）+ 代码审计 |
| R-3 | 迁移在存量库上执行失败（表不存在 / 无权限 / 锁超时） | 整个 up 事务回滚，升级卡住 | 迁移只依赖 `000001` 建的 `tickets` 表，无新 DDL；在 dbsmoke 的**升级路径库**（真 PG + 预置 legacy 行）上实测，不靠推理 |
| R-4 | 存量/新增行存在 `medium` 之外的意外值（`'urgent'` / `'中'`） | 这些行仍筛不出来 | **本轮不凭空定义语义**（把未知值一律改 `normal` 是臆造）。`''` 这条活路径已封（§1.4）；残余风险是客户端显式传词表外的非空值。dbsmoke 的全表兜底断言会在升级路径上现形，登记待处置 |
| R-5 | 前端同义词字典被顺手删掉 | 未迁移行的优先级显示成英文原文 | 本轮**不删**（N-5），且在三处字典的注释里写明「保留原因 + 何时可删」。**已知缺口**：`PRIORITY_LABEL`/`PRIORITY_WEIGHT` 没有任何测试覆盖，删掉不会红 —— 靠注释守 |
| R-6 | `TicketDetailModal` 编辑回写把显示值写回库 | 显示虽对，编辑一次又写出 `medium` | 已核实该弹窗**只读展示**（无 priority 表单项）；`TicketFormModal`（唯一可写表单）的取值本就是 `normal` |

---

## 5. 执行步骤

1. **迁移** `backend/migrations/000023_ticket_priority_normalize.{up,down}.sql`
   - up：`UPDATE tickets SET priority='normal' WHERE priority='medium';`
   - down：注释说明不可逆 + 一条 no-op 语句（保持 down 链可执行，见 §5.1）
2. **写入方 1**：`integration/glpi.go:161` 映射 `3: "medium"` → `3: "normal"`
3. **写入方 2**：`service/ticket_service.go:259` `priorityFromSeverity` 的 `sev >= 2` 分支 `"medium"` → `"normal"`；同步把注释里「已知分歧 M16」段落改成陈述归一后的事实
4. **写入方 3（seed）**：`cmd/seed/main.go:280` 的 `Priority: "medium"` → `"normal"`
5. **写入方 4（HTTP 空值兜底）**：`TicketService.Create` 补 `if t.Priority == "" { t.Priority = "normal" }`（§1.4）
6. **单测**
   - 两个映射函数各加「输出 ∈ {low,normal,high,critical}」+「不产出 medium」的断言
   - **GLPI 侧此前零覆盖**：`TestGLPIE2E_ConvertToTicket` 只喂过 `Priority: 4`（断言 `high`），其余五档写成什么都行、没有用例会红 —— 本轮补 `TestGLPIE2E_ConvertToTicket_优先级限定契约词表`
   - **既有断言必须同步改**（否则改完即红）：`service/ticket_from_alert_test.go:100`（`assert.Equal("medium", ...)`）与映射表 `2: "medium", 3: "medium"`
   - `service/ticket_service_test.go` 的 `Create_成功_默认值生效` 补 `Priority` 默认断言、`Create_传值保留` 补「已传值不被覆盖」
   - `integration/glpi_e2e_test.go` 里的 `// in_progress / medium` 只是注释（断言的是 `GetPriorityName()` 的中文），同步改注释
   - 顺手修 `handlers/rack_ticket_handler_test.go` 的 mock 夹具 `Priority: "medium"` → `"normal"`（归一后没有任何写入方能产出它）
7. **dbsmoke 升级路径**：`scripts/db_smoke.sh` 的预置 SQL 加一行 **legacy `medium` 工单**，插在 users 插入**之后**（`creator_id` 是 FK 指向 `users(id)`）：
   ```sql
   INSERT INTO tickets (ticket_no, ticket_type, priority, title, status, creator_id)
   SELECT 'LEGACY-M16-1', 'incident', 'medium', '存量 medium 工单', 'open', u.id
   FROM users u WHERE u.username = 'legacy_admin';
   ```
   - **插入用旧列名**（`ticket_no`/`creator_id` 是 000013 之前的名字，插入发生在那之前，正确）；
   - **断言必须用新列名 `ticket_number`** —— `000013_schema_align.up.sql:46` 把 `ticket_no` RENAME 成了它。仍按 `ticket_no` 查会直接报 `column does not exist`；
   - 新增 `TestDBSmoke_TicketPriorityNormalize`：断言 migrate.Up 后该行 `priority='normal'` **且行还在**（防「归一」被写成「删除」），外加一条全表兜底 `priority NOT IN ('low','normal','high','critical')` 计数为 0；
   - **位置是隐式契约**：必须放在跑过 `migrate.Up` 的用例之后、`TestDBSmoke_DownPreservesLegacyColumns` 之前。Go 按源文件顺序串行跑测试，放错会假绿或误红；
   - 把用例加进 `db_smoke.sh` **升级路径**那行的 `-run` 列表（`-run` 是白名单，不加就永远不跑）
8. **回滚链同步**（**原计划的「补两次 Down」是错的**）：`TestDBSmoke_DownPreservesLegacyColumns` 的前置从「已应用到 000021」加上「已应用到 000023」，并在回滚链最前面**补一次** `migrate.Down`。
   - 只能补**一次**：盘上没有 `000022`，第二次 Down 会直接滚掉 `000021`，整条断言链后移一位；
   - **后移不会红**：链上全是 `assert.False(索引还在)` 与末尾的「000001 建的列还在」，晚一步仍然为真 → 会**全绿地多回滚一个迁移**。所以必须加一条**正向断言**（第一次 Down 之后 `schema_migrations` 里不再有 23），把「滚的确实是 000023」钉死。原计划写的「漏了会硬失败、是响的不是静默的」与实测相反
9. **变异反证**（V-1..V-9，九条全红在业务断言上）：覆盖 R-2（把任写入方改回 `medium` → 对应断言变红，三个写入方各一条）、`Create` 的 priority 兜底去掉、「up 的 WHERE 去掉 → 把 `low` 也改坏」、「删掉迁移里的 UPDATE → dbsmoke 升级路径断言变红」、「Down 链多补/少补一次 → 首尾正向断言变红」、「`Load()` 撞号守卫失效（up / down 各一条 → 撞号用例变红）」
10. **（独立小步）堵住撞号本身**：`internal/migrate/migrate.go` 的 `Load()` 在 `byVer[ver]` 已有同号文件时**报错**而不是覆盖，up / down 两侧都守。配一条测试：构造两个同号 up 文件（或两个同号 down 文件）→ `Load()` 必须返回 error。
    - 测试必须用 `testing/fstest.MapFS`，**不能**往 `testdata/migrations/` 里塞第二个同号文件 —— 那个目录被 `migrate_internal_test.go` 的 6 个用例共用（`setupFS`），塞进去会让它们集体变红；
    - 反面用例覆盖「第一个文件是 0 字节」：用 `upSQL != ""` / `downSQL != ""` 兼作「已加载」标志位会漏检，所以判据是文件名（`upFile` / `downFile`），顺带还能在报错里指名撞的是哪个文件；
    - 当前零重复（四个 testdata 迁移目录均已核对），改动零风险。
    变异反证：去掉报错（V-4 打 up 守卫、V-9 打 down 守卫）→ 测试红
11. **文档同步**：见 §8。

### 5.1 为什么 down 里放一条 no-op 而不是空文件

`migrate.splitStatements` 会把注释原样累积成一条「语句」，注释-only 文件的行为依赖 PG 对「纯注释查询」的容忍度 —— 这是没必要的边缘依赖。放一条永真的 `SELECT 1;` 让 down 链在任何执行器下都确定可跑，语义由注释写明。

---

## 6. 边界（本轮不做）

| 不做 | 原因 |
|------|------|
| `POST/PUT /tickets` 的 priority **取值**校验 | 属 §1.4 的写入口缺陷。`PUT` 是任意 `map` 直落 `Updates()`（mass-assignment 可写任意列），比 priority 一个字段宽得多；只封 priority 会留下更大的洞却造成「已封住」的错觉。另立任务。（「空值」这一半本轮已修） |
| 删除前端同义词字典 | N-5：先留一个版本做未迁移行的安全网 |
| `ticket_sla.priority` | 死表、零消费方、零数据来源。动它是无的放矢 |
| 加 DB `CHECK` 约束 | N-6 |
| 未知 priority 值的语义 | R-4：不臆造。先诊断列出，再决定 |
| 前端筛选器支持多选 priority | 归一后不需要 |

---

## 7. 附：迁移撞号是静默事故（本轮的额外发现）

审查中发现 `000022` 被 P20（pg_trgm）在计划里预占，于是去核对了撞号的实际后果 —— 比预期严重：

```go
// internal/migrate/migrate.go  Load()（加固前）
byVer := make(map[int64]*migration)
for _, e := range entries {
    ...
    m, ok := byVer[ver]
    if !ok { m = &migration{version: ver}; byVer[ver] = m }
    content, _ := fs.ReadFile(FS, "migrations/"+base)
    if strings.HasSuffix(base, ".up.sql") {
        m.upSQL = string(content)   // ← 后读到的直接覆盖先读到的
    }
}
```

`fs.ReadDir` 按文件名**字典序**返回。若同时存在 `000022_p20_trgm.up.sql` 与 `000022_ticket_priority_normalize.up.sql`：字典序 p < t，trgm 先加载、被后者覆盖 → **pg_trgm 迁移永不执行**，而 `schema_migrations` 照样记下版本 22。没有报错、没有日志、没有测试会红。

这类「做了 A 却记成做了 A+B」的静默丢失，比报错难查得多 —— 半年后没人会想到去怀疑迁移执行器。所以：

- 本轮 M16 让号（取 `000023`，盘上 `000022` 空缺）；
- 同时按 §5 步骤 10 给 `Load()` 加上撞号即报错，把这一类问题从「静默」变成「启动即失败」。

> 登记：`docs/TRAPS.md` **T-41**（撞号 → 静默覆盖 → 版本已记录但迁移未跑）。
> 该条同时记下了「让号留下的空洞无害、但 Down 链断言要数清楚层数」这个伴随坑。

---

## 8. 审查发现与处置（两路只读审查）

| 发现 | 处置 |
|------|------|
| **回滚链「补两次 Down」是错的**（盘上无 000022），且后移是**静默**的（全链 `assert.False` 挡不住） | 已改：补**一次** + 新增正向断言。见 §5 步骤 8 |
| §5 步骤 6 的理由「`TestDBSmoke_UpgradePath` 是唯一跑 `migrate.Up` 的地方」不实 | 已改措辞（`AssetJSONBBackfill` 也调 `migrate.Up`，幂等；结论不变） |
| 「21 个迁移」计数错 | 实有 **22** 个 `.up.sql`（本轮前 21）。§2 N-6、`FIX-PLAN-AUDIT-2026-09-10.md` 同改 |
| N-1 论据不完整：`05-运维工单.md:55` 指定的权威来源（代码注释）其实写的是 `medium` | 已把论据换成「CI 钉着的生成契约 + `TicketStatsCards.tsx` 已有先例」，并同步改 `models/ticket.go:17` 的注释 |
| GLPI 映射的 3 号档**零测试覆盖** | 已补 `TestGLPIE2E_ConvertToTicket_优先级限定契约词表` |
| `POST /tickets` 不传 priority → `''`，R-4 却把它当存量脏数据 | 已确认是**活路径**并在 `Create` 里封掉（§1.4）；R-4 改写为「非空但词表外的值」 |
| seed 写入方无回归网 | 已补 dbsmoke 的全表兜底断言 + 记入 R-2 |
| 步骤 10 的字面做法会让 6 个既有测试集体变红 | 已改用 `fstest.MapFS` |
| 步骤 9 文档清单漏 `models/ticket.go:17`、`FIX-PLAN-ALERT-TICKET.md`、`FIX-PLAN-UI-PERF.md` §4.2/§6 的三处状态、`PRIORITY_WEIGHT` 这第三处字典 | 已逐项补进本轮改动 |
| 各处行号偏差（`glpi.go:158`→161、`ticket_service.go:260`→259、openapi `2449`→2448/2450、`2471`→2475、`seed:264`→280） | 已按实测校正 |
| 建议「先 `SELECT priority, count(*)` 查存量，0 行就删掉迁移/夹具/回滚链」 | **不采纳**。迁移是一次性的、空转无害，而删掉它意味着「GLPI 一旦被启用过的那批行永远筛不出来」。诊断 SQL 作为上线检查项保留（R-1） |
