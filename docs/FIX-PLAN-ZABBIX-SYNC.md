# 需求文档：Zabbix 同步导入保真（M27）

承 M26（`docs/FIX-PLAN-SYNC-FIDELITY.md`）的同一主题：**外部数据导进来时，不许静默地
对不上真相**。M26 修的是「时间戳与词表」，M27 修同一函数里的另外三处 —— 去重键、
上限、以及「身份」只在 Go 侧查而不在库里兑现。

## 0. 一句话

Zabbix 同步的**去重键**选错了列（`status` 而不是「这是哪一次故障」），**上限**又是被一个
没人读的字段撑出来的 100 且截断无声；前者让运维的「确认」不收敛，后者让最老的进行中告警
永不入库。第三个问题与 M26/D-4 同形：**声明为「身份」的键只在 Go 侧查一次，数据库层不拦**。

## 1. 现状（读码 + 实测）

### 1.1 路径

`zabbix.go:GetTriggers` → `service.go:SyncFromZabbix` → 预过滤 → `CreateInBatches`。
与 M26 §1.1 同一结构：**不经 service 层**，因此也不发 `alert.created` 事件
（`TopicAlertCreated` 全仓无发布方，`notification/worker.go:94` 只有订阅者）。
那属下文 **C** 的范畴（§6），与 G-39 不是同一件事，本轮都不动。

### 1.2 A：去重键是「状态」而不是「哪一次故障」

`internal/integration/service.go:167-176`：

```go
var existing []models.Alert
if err := database.DB.WithContext(ctx).
    Where("trigger_id IN ? AND status = ?", triggerIDs, "problem").   // ← 就是这一行
    Find(&existing).Error; err != nil { ... }
existingSet := make(map[string]struct{}, len(existing))
for _, e := range existing {
    existingSet[e.TriggerID] = struct{}{}
}
```

`existingSet` 的语义被写成「这个 trigger 有没有**未处理**的行」，而下游拿它做的判断是
「这个故障**是不是已经在库里**」。两者在运维点了「确认」之后分叉：

`internal/service/alert_service.go:291-320` 的 `Acknowledge` 把行改成 `status='acknowledged'`
（写入在 `:306`）。

于是（已由 `upsert_test.go:524` 的用例钉住，非推测）：

| 步 | Zabbix 侧 | 本地 alerts 行 | 下次同步 `existingSet` | 结果 |
|---|---|---|---|---|
| 1 | trigger 100 firing | `problem` | 命中 | 跳过 ✓ |
| 2 | 运维点确认 | `acknowledged` | **不命中** | **又插一行 `problem`** |
| 3 | 运维再确认新行 | 两行都 `acknowledged` | 不命中 | **再插一行** |

净效果：**确认动作不收敛**。运维每确认一次，列表里就多出一条同 trigger 的未确认行；
`alerts` 表按 trigger 无界增长，KPI/统计把同一次故障数成多次。

### 1.3 为什么 M26 之后才修得了

**先纠正本文档 v1 的一处事实错误**：v1 写「M26 之前 `problem_start` 写的是同步时刻（`now`），
所以拿它当身份只会制造重复」。这是错的 —— 与 HEAD 代码和 M26 自己的文档都冲突：

- `git show HEAD~2:backend/internal/integration/service.go` 的 Zabbix INSERT 块里
  **根本没有 `ProblemStart` 字段**（`grep -c ProblemStart` = 0）。
  **注意是 `HEAD~2` 不是 `HEAD~`**：`HEAD~`（= M26 那笔）正是**引入** `ProblemStart` 的提交，
  在那里 grep 会得到 1 —— 本文档 v2 初稿就写成了 `HEAD~`，是一条可复现但结论为假的断言；
- `docs/FIX-PLAN-SYNC-FIDELITY.md:28` 与 `TODO.md:302` 都记着「`problem_start` **从未被写入**
  （落零值 `0001-01-01`）」。

真实的机理因此是**另一个**：M26 之前所有 Zabbix 行共享同一个零值，拿它当键不区分任何东西，
`(trigger_id, problem_start)` 会**退化成 `trigger_id`** —— 于是「源侧恢复后再触发」这类
真实的新故障会被永久吞掉（比现在的缺陷更糟）。

M26 **§2.3**（不是 D-8；D-8 讲的是 `created_at` 保持 `now`）把 `problem_start` 改成
Zabbix 的 `lastchange`（`service.go:192-196`）之后，它才第一次成为「源侧派生、随故障发生而变」
的身份：

- 同一个进行中的故障 → `lastchange` 恒定 → 同一身份；
- 源侧恢复后再触发 → `lastchange` 变成新的 → 新身份。

于是 `(trigger_id, problem_start)` 成为可用的去重键。结论（去重键需要真身份）不变，
但论据换了 —— 这一点必须写对，否则后人会按 v1 的错误论据去推「零值也无所谓」。

### 1.4 降级分支：本轮改动**自己引入**的失败模式

`service.go:192-196`：`lastchange` 不可用时 `problem_start` 回落到 `now`。

若只把去重键换成 `(trigger_id, problem_start)` 而不处理这一类行，则降级行的 key **每轮同步
都不同 → 每轮插一行 → 无限增长**。缓冲告警源一坏，同步就从「重复一行」变成「每轮加一行」。

这条必须与 A 同时定义，不能留给下一轮。判据见 §2.2。

### 1.5 B：上限 100，且截断无人知晓

`internal/integration/zabbix.go:162-174`：

```go
"selectItems":   "extend",
"sortfield":     "lastchange",
"sortorder":     "DESC",
"limit":         100,
```

三个独立问题叠在一起：

1. **上限低**：同一文件的 `GetMetricItems` 用 `limit: 5000`（`zabbix.go:133`），两者不一致，
   没有任何理由写下来过。
2. **截断无声**：`GetTriggers` 把截断后的结果原样返回，`SyncFromZabbix` 返回 `len(toInsert)`
   —— 一个 ≤100 的数。**调用方拿到的返回值在「有 100 条」与「有 5000 条只给了 100 条」
   两种情况下无法区分**，日志里也没有一行。
3. **丢的是最老的**：`sortorder: DESC`（按 lastchange 倒序）意味着被截掉的是**烧得最久的
   进行中告警** —— 对运维而言恰是最该看到的那批。

**根因不在 limit，在 `selectItems: extend`**：它让响应里每个 trigger 都带一整个 `items`
数组（关联 item 的完整对象）。而 `Trigger.Items`（字段在 `zabbix.go:244`，
`:259` 只是 `Item` 结构体里的一句注释）全仓**零读取点** —— `grep -rn "\.Items"` 在非测试 Go
里**零命中**；`ConvertToAlert`（`zabbix.go:270-296`）只读 `Hosts[0].Host`（`:272`）、
`Description`（`:274`、`:275`）、`Priority`（`:277` 的 `Severity: t.Priority`）。

> **【未实测】**具体放大倍数取决于环境里 trigger↔item 的关联数，本轮无真实 Zabbix 可测，
> 不编数字。**但处置不依赖这个倍数**：字段零读取，删它在任何倍数下都是安全的。

### 1.6 实测②：NULL 与零值的 `problem_start` 扫描形态（2026-09-11）

`alerts.problem_start` 是**可空** TIMESTAMP（`migrations/000013_schema_align.up.sql:247`，
无默认、无回填），而 `models.Alert.ProblemStart` 是**非指针** `time.Time`
（`models/alert.go:25`）。三路审查对「库里有一行 `problem_start IS NULL` 时新查询会不会报错」
给出**相反**结论（一路判 pgx/database-sql 抛 `unsupported Scan`，一路判 GORM 落零值）。
这类分歧只能实测（T-48 的教训）。真 PG（`postgres:18-alpine`）裸 SQL 造一行 NULL 后：

```
① Select 三列（新形态）     → err=<nil> len=1，读回 problem_start=0001-01-01 00:00:00 +0000 UTC，IsZero=true
② SELECT *（旧查询形态）    → err=<nil> len=1
裁定：两者同命 → 无回归
```

两处结论进入设计：

1. **NULL 行不会让查询报错** —— GORM 自己的扫描器把它落成零值，没走 `database/sql` 的
   convert（那一支才会抛 `unsupported Scan`）。
2. **NULL 与零值扫描后不可区分**（都 `IsZero`）→ §2.2 里「排除零值行」这一条**同时**覆盖
   两者，不需要为 NULL 单列一条处置。

## 2. 设计

### 2.1 预过滤：取三列、不再按状态过滤

```go
// before —— 只捞 problem 行，行内容全取
var existing []models.Alert
Where("trigger_id IN ? AND status = ?", triggerIDs, "problem").Find(&existing)
existingSet := map[string]struct{}{}; existingSet[e.TriggerID] = ...

// after —— 该 trigger 的 zabbix 行全取（判据要两种），但只取判据需要的三列
var existing []models.Alert
Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).
    Select("trigger_id", "problem_start", "status").
    Find(&existing)
```

`source = 'zabbix'` 的理由与 §2.3 的索引谓词同源（两边必须一致，否则 Go 侧与库侧对
「什么算同一身份」的判断会分叉）。既有代码没有这个条件，但 `Alert.TriggerID` 的非测试写入点
只有本同步与 `cmd/seed`（`TRG-5000`.. 各自唯一），故今天行为等价 —— 这是为将来收窄，不是修现状。

**为什么不再按 status 过滤**：新判据（§2.2）在 usable 分支需要看 `problem_start`，
在 degraded 分支需要看 `status`。把 status 过滤留在 SQL 里会让 usable 分支看不到
`resolved` 行 —— 而那正是「本地已解决、源侧仍在 firing」时必须命中的行。

**`len(triggers) == 0` 的早退（`service.go:159`）保留。** 顺带核实：即使不早退，
`trigger_id IN ?` 传空切片时 gorm 生成 `IN (NULL)`（永不匹配），不会退化成无 WHERE 或全表扫描
（gorm v1.30.0 `statement.go:247` + `clause/expression.go:202`）。仓库里所有 `IN ?` 调用点
都在前面 `len()==0` 早退，保持同一风格。

`idx_alerts_trigger_id`（migrations/000018）覆盖本查询的 WHERE，**查询本身不需要新迁移**。

### 2.2 去重判定：两条分支

```
for each trigger t from Zabbix:
    problemStart, st := parseUnixSeconds(t.LastChange)          // M26 既有
    if st == timeOK:
        key := t.TriggerID + "|" + strconv.FormatInt(problemStart.Unix(), 10)
        skip if exact[key]                    // 同一 trigger 的同一故障发生
    else:
        skip if open[t.TriggerID]             // 降级：同 trigger 且未解决
```

两个集合都从 §2.1 的查询结果构造：

- `exact[key]` —— 所有**非零** `problem_start` 的行，按 `trigger_id|unix秒` 建键
  （零值与 NULL 都经 `IsZero()` 排除，见 §1.6）；
- `open[triggerID]` —— 所有 `status != "resolved"` 的行。

**`open` 必须用 Go 形态 `!= "resolved"` 构造，不能写成 SQL 的 `status <> 'resolved'`。**
两者在 NULL 上分叉：SQL 三值逻辑下 `NULL <> 'resolved'` 是 NULL → 该行**不入** `open`；
Go 下 NULL 扫成 `""` → 该行**入** `open`。后者才与 D-2 的失败方向一致（宁可少插一行可见的
重复，也不静默吞掉一次真实故障）。

**用 `Unix()` 秒作键而不是格式化字符串**：`lastchange` 是 Unix 秒，且
`alerts.problem_start` 是 `TIMESTAMP`（无时区）。`Unix()` 与 Location 无关，因此在 pgx
与 sqlite 下得到同一个数 —— M26 已经在这两种驱动上各踩过一次（T-48），这里不再踩第三次。

#### 2.2.1 步骤 0 实测：键的 round-trip（2026-09-11，`postgres:18-alpine` + 同款 pgx v5.5.1）

这条键依赖一条**关于驱动的事实**（写进去再读回 `.Unix()` 不变）。探针走真 `SyncFromZabbix`
写入 `lastchange=1788266096`（`2026-09-01 12:34:56 UTC`），再读回：

```
写入 Unix=1788266096 | 落库文本="2026-09-01 12:34:56" | 读回 Unix=1788266096
读回 Location=UTC    | Equal(写)==true
```

结论：**§2.2 的键成立**，不需换方案。（同一次测量顺带确认 `TIMESTAMP` 列落的是 UTC
挂钟数字，与 M26 §1.7 一致。）

**该测量未覆盖的一格**（登记为残余，§4 R11）：降级分支写的 `time.Now()` 是 **Local**，
上面这个探针的输入来自 `lastchange`（恒 UTC）。TZ 非 UTC 的进程里降级行不 round-trip。

行为对照（+ = 允许插入，− = 跳过）：

| 场景 | 库中已有行 | 源侧 st | 旧行为 | 新行为 |
|---|---|---|---|---|
| 进行中故障，无人处理 | `problem` 同行 | OK | − | − |
| 进行中故障，运维已确认 | `acknowledged` 同行 | OK | **+（缺陷）** | − |
| 本地已解决，源侧仍 firing | `resolved` 同行 | OK | **+（缺陷）** | − |
| 源侧恢复后再触发 | 旧行（旧 lastchange） | OK | **+（仅当旧行非 problem）** | + |
| lastchange 缺失，运维已确认 | `acknowledged` 同行 | 降级 | **+（缺陷）** | − |
| lastchange 缺失，已解决 | `resolved` 同行 | 降级 | + | + |

第 4 行的「旧行为」有条件：旧判据是 `status='problem'`，若那个 trigger 的旧行**仍是
`problem`**（运维从未处理），旧代码会**跳过**这次真实的新故障 —— 即旧行为在常见子场景下是
静默丢告警，而不是表里 v1 写的 `+`。这一格正是新设计要修的东西之一。

**代价（必须一起看）**：新设计下同一 trigger 可以并存多行 `problem`（旧的永不关闭，因为
同步侧没有 resolve 路径 —— 全仓唯一把 alerts 置 `resolved` 的写入方是操作员的
`Acknowledge`/`Resolve`）。这是 D-1「再触发出新行」的既定取向，但要在 §4 R8 登记。

### 2.3 身份在数据库层兑现（迁移 000027）

D-1 把 `(trigger_id, problem_start)` 声明为一次故障发生的**身份**。只在 Go 侧查一次
（§2.1 的预过滤）是 TOCTOU：两个同步并发进入会各自判定「不存在」然后各插一行。
**GLPI 侧 M26/D-4 正是为同一个问题加了部分唯一索引 + `ON CONFLICT`**，Zabbix 侧跟上：

文件 `backend/migrations/000027_alerts_zabbix_identity_unique.{up,down}.sql`
（现最新是 000026，编号空闲），up 照抄 `000026_tickets_glpi_external_id_unique.up.sql` 的骨架：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_zabbix_identity
    ON alerts(trigger_id, problem_start)
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> '';
```

**谓词里为什么有 `source = 'zabbix'`**（§2.2 的 Go 侧查询同步加同样的收窄，两边必须一致）：
D-4 的原文是「与 GLPI 侧 M26/D-4 **同形**」，而 GLPI 的谓词正是一段 source 收窄
（`WHERE source='glpi' AND external_id <> ''`）。不带 source 的后果是**静默**的：将来一旦出现
第二个写 `trigger_id` 的来源（或人工告警带了 trigger_id），两边(trigger_id, problem_start)相撞
→ `ON CONFLICT DO NOTHING` 把**真实的 Zabbix 告警悄悄跳过**，无日志、无返回差异 ——
正是 M27 要消灭的失败类。带上 source 后，同类碰撞退化成**可见的重复行**（自检与巡检看得见）。
反过来的代价（source 非 `'zabbix'` 的存量行不再参与抑制）登记为 §4 R16。

前置自检（同 000026/D-5 的形态）：检测到重复则 `RAISE EXCEPTION` 并 `string_agg` 出最多 5 个
重复键，**不删任何数据**。两条细节，写错任一条都会让自检变成永久性阻塞：

- WHERE 里必须显式写 **`problem_start IS NOT NULL`**。PG 唯一索引默认把 NULL 视作互不相等
  → 同 trigger 的多个 NULL `problem_start` 行**建索引时并不冲突**；自检若把它们算成重复，
  迁移会被拒且**永远无法满足**（删了行才能过，而删除是运维不该被逼着做的事）。
- `trigger_id <> ''` 本身就排除了 NULL trigger_id（`NULL <> ''` 是 NULL → 不入结果集），
  但索引谓词里仍**显式写出** `IS NOT NULL`，让两边字面可比（谓词必须与
  `TargetWhere` 逐字一致，见下）。

**自检触发的含义要写清**：它说明库里**已经**有重复 —— 而 pre-M26 的 Zabbix 行
`problem_start` 全落同一个零值（§1.3），同一 trigger 被同步过两次就是重复。
用户已确认无真实存量数据（D-9），故**本轮不提供清理脚本**：自检触发时人工定位后清理重跑。
这是刻意的 —— 自动删告警数据比让迁移失败危险得多。

**down 文件必须带 000026 down 那样的警告**：滚掉索引会**静默改变运行语义** ——
`ON CONFLICT (trigger_id, problem_start) WHERE ...` 失去仲裁者 → PG 立刻 42P10
（`there is no unique or exclusion constraint matching the ON CONFLICT specification`）
→ **每一次 Zabbix 同步都 500**。也就是说这一层滚下去，代码必须一起回退，不能只滚迁移。

```go
// service.go 的插入（照抄 SyncFromGLPI 的 M26/D-4 写法）
tx.Clauses(clause.OnConflict{
    Columns: []clause.Column{{Name: "trigger_id"}, {Name: "problem_start"}},
    TargetWhere: clause.Where{Exprs: []clause.Expression{
        clause.Expr{SQL: "source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''"},
    }},
    DoNothing: true,
}).CreateInBatches(toInsert, 100)
```

**`TargetWhere` 必须与上面索引谓词逐字一致（含 `source = 'zabbix'`）。** PG 只要求
「ON CONFLICT 的推断谓词**蕴含**索引谓词」，方向上是安全的；但漏掉 `source` 就**不蕴含**，
直接 42P10。本文档 v2 初稿这段漏了 `source`，而同一节的索引 SQL 有它 —— 自相矛盾，
细节文档 §3.5 已是对的。两边必须同改。

**`synced` 用同事务 COUNT 前后差，不用 `res.RowsAffected`。** 注意**论据要写对**：
T-49 记录的虚报（`RowsAffected == len(batch)`）是 **`Ticket` 特有**的 —— `Ticket` 有
`BeforeCreate` 钩子自己填 ID，gorm 因此不加 `RETURNING`，走的是另一条语句路径。
**`Alert` 实测不虚报**（sqlite 3.45.1，1 冲突 + 2 新 → `RowsAffected=2`，与真实插入数一致）。
保留 COUNT 的理由是另外两条，不是「会虚报」：① 它对 INSERT 语句路径的变化免疫
（哪天给 `Alert` 加个 `BeforeCreate`，`RowsAffected` 的含义就会跟着变）；② 与同一文件里
`SyncFromGLPI` 的计数法一致，读者不必去分辨两种写法的差别。计数语句同样要带 `source` 收窄
（`Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs)`），否则非 Zabbix 的同
trigger_id 行会被算进 `synced`。

**为什么这个索引不会误伤存量**（已核实）：
- `cmd/seed/main.go:218-230` 的 6 条告警 `TriggerID` 各异（`TRG-5000`..`TRG-5005`）、
  `ProblemStart` 各异 → 不撞；
- `backend/tests/db_smoke_test.go` 零个 `TriggerID:`（grep 确认）、`scripts/db_smoke.sh`
  无 `INSERT INTO alerts` → 升级路径的存量数据里没有 alerts 行；
- `trigger_id IS NOT NULL` 把手工告警（`trigger_id` 为空/NULL）排除在索引外；
- NULL `problem_start` 在唯一索引里按 PG 默认语义互不相等，不会互相冲突。

### 2.4 B：请求瘦身 + 抬上限 + 让截断可见

```go
// zabbix.go
// zabbixTriggerLimit 是 trigger.get 的**本条 API 自己的**上限，与 GetMetricItems 的 5000
// 恰好同值但互不耦合（本包既有的做法是每个请求**内联**写自己的上限：zabbix.go:133 的 5000、
// :173 原本的 100 —— 这里提成常量只因为它在 service.go 的截断判定里还要再用一次）。
const zabbixTriggerLimit = 5000

Params: map[string]interface{}{
    "only_true":     true,
    "skipDependent": true,
    "filter":        map[string]interface{}{"value": 1},
    "selectHosts":   "extend",
    // "selectItems" 已移除：Trigger.Items 零读取点，值为 nil 的数组仍占响应体积
    "sortfield":     "lastchange",
    "sortorder":     "DESC",
    "limit":         zabbixTriggerLimit + 1,   // 多要 1 条：恰好等于上限是巧合，无法与截断区分
},
```

`SyncFromZabbix` 内：

```go
truncated := 0
if len(triggers) > zabbixTriggerLimit {
    truncated = 1   // 0/1 标志，不是条数（理由见 §3）
    log.Printf("M27: Zabbix 返回的告警数超过上限 %d，源侧仍有告警本次未导入（按 lastchange 倒序，被丢的是最老的）", zabbixTriggerLimit)
    triggers = triggers[:zabbixTriggerLimit]
}
```

该日志**只插值常量**，不含任何源侧可控文本（`t.TriggerID` 不出现）→ 不引入日志注入面。
（既有 `timeparse.go:104` 的 `logTimeUnusable` 用 `%s` 打印 `id`，那是本轮不改的既有缺口，
见 §4 R12。）

### 2.5 透出链路（对齐 M26/D-6 的 `glpi_skipped`）

```
SyncFromZabbix → (synced, truncated, err)
  └ SyncAll → results["zabbix_truncated"]
  └ handler  case "zabbix" → results{"zabbix", "zabbix_truncated"}
      └ 前端 Settings.tsx：message.success 追加「另有告警因超过条数上限未导入（详见后端日志）」
```

**前端文案不许插值数字。** 现成的 `glpi_skipped` 样板（`frontend/src/pages/Settings.tsx:194-197`）
写的是 `` `另有 ${skipped} 条档位越界被跳过` `` —— **照抄它会把静默丢票反转成少报丢票**：
源侧 6000 条时 `truncated=1`，UI 会显示「另有 **1** 条未导入」，运维看到 1 就不会去查那 1000 条。
故本轮的文案不带数字，并加一条前端单测钉住「文案不含数字」。

## 3. 契约

| 项 | 值 | 备注 |
|---|---|---|
| `SyncFromZabbix` 签名 | `(synced, truncated int, err error)` | 与 `SyncFromGLPI` 的 `(synced, skipped, err)` 同形 |
| `truncated` 语义 | **0/1 标志**，非条数 | 源侧真实总数本轮不可知（要 `countOutput` 二次查询）。宁可是个诚实的标志，不编一个假数字。**禁止**把它当条数（聚合/求和/插值都错） |
| `SyncAll` 结果键 | 新增 `zabbix_truncated` | 既有键不变 |
| HTTP | `data.synced.zabbix_truncated` | 无新增端点 |
| 前端 | `frontend/src/pages/Settings.tsx` 的 `handleSyncZabbix` 提示文案（**不含数字**）+ 单测 | 与 `glpi_skipped` 同位置 |
| 迁移 | **新增 000027**（部分唯一索引 + 前置自检 + down） | §2.3 |
| OpenAPI | **不需要改** | spec 在 `backend/internal/api/openapi.yaml`（2729 行，`paths:` 段起于 `:48`）。`grep -in "integration\|zabbix\|sync"` 全文件**只有 1 处命中**（`:2599`，是 `audit_logs.source` 的一句 description），`paths:` 段里没有任何 integrations/zabbix/sync 路径 → 该端点整体未文档化。M26/D-6 已判定「gin.H 直出 → 不触发漂移」，`glpi_skipped` 当时也没进 spec |
| 生成物 | **不需要改** | `frontend/src/services/api.ts:239` 的 `syncZabbix` 无显式返回类型标注（推导为 `Promise<AxiosResponse<any>>`），消费侧一律 `res: any` + `?.` 取值，不存在可改的类型声明 |

## 4. Risk

**R1（高）降级分支下，该 trigger 的后续再触发在人工解决前全部不入库。**
`open` 一旦命中就不再插，而同步侧**没有 resolve 路径**，所以这条不会自愈。
缓解：只在 `lastchange` 不可用时发生（正常 Zabbix 一定给）；该状态本身有 M26 的
`logTimeUnusable` 日志（但它报的是「源不可用」，**不会**报「有一条告警被吞了」）。
登记为残余 —— 不修。**注意本文档 v1 把降级分支的失败方向写成「多一行可见的重复」，
那是不完整的**：真实失败方向是「可见的重复」与「静默吞掉后续再触发」两者，后者不可见。

**R2（中）预过滤不再按 status 过滤 → 查询返回行数随历史增长。**
失败模式：某 trigger 反复触发过几千次的库，单次同步要拉回几万行。
缓解：① 只 `Select` 判据需要的三列，省掉 `problem TEXT` 等列的分配；② WHERE 命中
`idx_alerts_trigger_id`；③ 行数上界受 §2.4 的 `zabbixTriggerLimit` 约束。
**纠正 v1 的措辞**：`Select` 三列**不改变内存数量级** —— GORM 的 `Find(&existing)` 每条记录
仍分配整个 `models.Alert` 结构体，`Select` 只减少网络字节与其余列的字符串分配，
**行数才是主项**。残余：仍可构造出「一个 trigger 一万行历史」的库让单次查询变重。

**R3（中）上限从 100 提到 5000 → 单次响应内存变大。**
失败模式：瘦身（去 `selectItems`）不足以抵消 50 倍的数量增长，`json.Unmarshal` 峰值内存上升。
缓解：先去 `selectItems`；仍设上限而非无上限；真 PG 冒烟带大数量用例。
**残余且更根本**：真正的无界点在 `internal/httpx/httpx.go:164` 的 `io.ReadAll(resp.Body)`
**没有大小上限** —— `limit` 只是对源站的**请求建议**，被攻陷或行为异常的 Zabbix 可以忽略它
返回任意大 body → API 进程 OOM。这是**基线既有**问题，本轮的 limit 挡不住（§4 R10）。

**R4（低）签名变更漏改调用点。**
7 处（生产 2：`service.go:384`、`handlers/integration_handler.go:65`；测试 5：全在
`upsert_test.go`）。**这不算真风险** —— Go 编译期会列出全部，正是 T-31 说的「红在编译上」
要区别于「红在断言上」的场景。列在此处是为了说明不必去找调用点。

**R5（中）`truncated` 被下游当成条数用。**
失败模式：有人写 `if truncated > 10`，或前端渲染出「丢了 1 条」。
缓解：§3 契约表写死语义 + 前端文案不含数字 + 单测钉住；字段名与注释都写明 0/1 标志。

**R6（中）`WriteTimeout = 30s` 才是真正的响应预算，不是 ctx 的 5 分钟。**
`integration_handler.go:34` 的 `syncTimeout` 是 5min，但 `cmd/server/main.go:98` 的
`http.Server.WriteTimeout` 是 30s。失败模式：5000 条时「取数 + 预过滤 + 50 批插入」超 30s →
**后端已插完、前端收到网络错误** → 运维重试 → 放大负载（并撞上 R7 的窗口）。
旧上限 100 时几乎不会触发，提到 5000 后窗口放大 50 倍。登记为残余（既有约束，本轮不改
WriteTimeout —— 改它属另一个模块）。

**R7（中）`CreateInBatches` 逐批提交，失败却 `return 0, err`。**
`service.go:218-221`：第 30 批失败时前 29 批已提交，但返回值是 `0` + error，handler 落 500
「同步失败」—— 而绝大多数行其实已入库，本次插入条数也无从得知。与 R6 叠加时最糟。
登记为残余。

**R8（中）同一 trigger 可并存多行 `problem`。**
同步侧没有任何 resolve 路径，旧行永不关闭。（§2.2 的代价，D-1 的既定取向。）登记为残余。

**R9（低）并发重复窗口在加索引后由「重复行」变为「静默跳过」。**
`ON CONFLICT DO NOTHING` 让并发下的第二次插入无声跳过 —— 这是期望行为，但它也意味着
「同步明明跑了却少插了行」不会有任何日志。可接受（D-1 的语义就是幂等），登记备查。

**R10（中）源侧响应体无大小上限。** 见 R3 的「残余」段。基线既有，本轮不修。

**R11（低）降级行的 `problem_start` 用 `Local` 时钟，`Unix()` 不 round-trip。**
`service.go` 的 `now := time.Now()` 是 Local，而 pgx 对 `TIMESTAMP` 丢 Location 只写挂钟数字
（§2.2.1 只覆盖了恒 UTC 的 `lastchange` 输入）。TZ 非 UTC 的进程里，降级行的
`problem_start` 读回会偏一个时区差 → SLA 窗口偏，且该值会进 `exact` 键。
`docker-compose.yml` 的 api 服务**未设 TZ** → 容器内是 UTC，线上无影响；直接跑进程会中。
登记为残余。

**R12（低）`logTimeUnusable` 用 `%s` 打印源侧可控的 `id`。**
`timeparse.go:104` 的 `id` 即 `t.TriggerID`（来自 Zabbix 响应，未转义）（同一行的 `raw` 用了
`%q`，是转义过的）。被攻陷的 Zabbix 可回一个带换行的 triggerid 伪造日志行。
现实中 triggerid 是数字串；仓库对第三方文本的清洗惯例（`notification/worker.go:279-291`
的 `redact.Text` → `ToValidUTF8` → `stripControlChars`）在集成层没有对应处置。
**非本轮引入**，本轮也不新增 per-trigger 日志（§2.4 的日志只插常量）。登记备查。

**R13（低）截断判定的假阴性。**
`len(triggers) > zabbixTriggerLimit` 不可能假阳性；假阴性只可能来自「Zabbix 服务端自身把
返回压到 limit 以下」（如服务端级结果上限），本仓无真实 Zabbix 可验 —— **不下结论**。
另：假源（httptest）无视请求里的 `limit` 直接吐 N 行时，单测在「请求侧被改成 100」时仍会绿
→ §5 的单测必须**同时断言请求体里的 `limit` 值**，否则 `+1` 这个前提没被钉住。

**R14（中）测试基座与生产 DDL 漂移，且漂移的「修法」是删掉生产代码里的保护。**
`upsertTestSchema` 是**手写 DDL**、不走迁移，所以 000027 不会自动作用于它。若忘了补
alerts 的部分唯一索引，Zabbix 用例会红在 sqlite 的
`ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`。
**具体的失败路径**：后来者看到「加了 ON CONFLICT 就红」，最省事的修法是**从
`service.go` 删掉 `clause.OnConflict`** —— 测试立刻全绿，而生产**静默退回 TOCTOU**，
且不留任何痕迹（R9 那条「并发少插一行无日志」正好让这个退步不可观测）。
缓解：§5 步骤 3 把补索引写成独立步骤；schema 注释显式写明「这是基座缺件，不是被测代码的
问题」（照 `:96-104` 已有的写法）；真 PG 冒烟从另一侧钉住索引真的存在（DP 路径走真迁移）。

**R15（中）自检在**有**重复的库上阻塞升级，而迁移跑在启动路径上。**
失败模式（000026 up 的注释已记录同一机理）：DDL 与版本记录同事务 → 失败则版本不落 →
**每次重启重放** → 服务持续不可用，而错误文本只给出最多 5 个样本键。
缓解：自检 `RAISE EXCEPTION` 时把重复键写进异常文本（运维一次定位）；D-9 已判定无真实存量
数据，故这是纯保险；**不提供自动清理**（自动删告警数据比让迁移失败危险）。

**R16（低）`source = 'zabbix'` 收窄会让 source 非 Zabbix、却带 trigger_id 的存量行不再参与抑制。**
§2.3/§2.2 把「什么算同一身份」收窄到 `source='zabbix'`（照 GLPI 的谓词形态）。失败模式：若库里
已存在一行 `trigger_id='100'` 但 `source` 为 `''`/`'manual'` 的告警，新代码**不再**因它而跳过，
且库侧索引也不拦 → 多出一行 Zabbix 告警（**可见的重复**，不是静默丢弃）。
缓解：`Alert.TriggerID` 的非测试写入点只有本同步与 `cmd/seed`（`TRG-5000..` 各自唯一），
D-9 又判定无真实存量数据 → 现状下不可达。取此方向是因为它的失败方向**可见**，
而不带 source 的那一侧失败是**静默跳过真实告警**（§2.3 已展开）。

**R18（低）身份粒度是 1 秒，源侧「同一秒内恢复再触发」会被静默跳过。**
`exact` 的键是 `trigger_id|problem_start.Unix()`，而 `lastchange` 是 Unix **秒**。
源侧在同一秒内「恢复 → 再触发」时，新故障与旧行的 key 相同 → Go 侧预过滤跳过、
库侧 `ON CONFLICT DO NOTHING` 兜住 → **无日志、无返回差异**。低频，但确实是静默丢告警 ——
与 R9（并发）同属「收集键精度不足」这一类。缓解：D-1 的既定语义（身份就是「哪一次故障发生」，
秒是 `lastchange` 能给的最细粒度）；登记备查，不修。

**R19（低）`alerts.source` 有 DB 默认值 `'zabbix'`，不只模型 tag。**
`migrations/000013_schema_align.up.sql:255` 给了该列默认值。失败模式：将来某个写入方
漏设 `source`，落库就拿到 `'zabbix'` → 会被卷进本轮的 identity 判据与索引。今天不可达
（唯一的另一写入方 `cmd/seed` 显式设了；冒烟里的裸 `INSERT INTO alerts` 都不带 `trigger_id`，
落在索引谓词外）。登记备查。

**R17（低）同事务 COUNT 差值在并发写入下会多计。**
`synced = after - before` 统计的是「该批 trigger_id 的 zabbix 告警行数增量」。若另一个写入方
在两次 COUNT 之间插入了同 trigger_id 的 zabbix 行，差值会把这部分算进本次 `synced`。
这与 GLPI 侧 M26/D-4 的计数法是**同一个**已知性质（照抄即继承）。
缓解：`synced` 只用于展示与日志，不参与任何写决策；并发同步本身由 §2.3 的索引兜住幂等。
登记为残余。

## 5. 执行步骤

0. **前置实测** ✅ 已完成（§2.2.1 键 round-trip、§1.6 NULL 扫描裁定）。
1. 需求文档（本文）→ 三路对抗审查（正确性 / 边界+安全 / 一致性）→ 处置见 §8。
2. 细节文档 `docs/IMPL-ZABBIX-SYNC.md`：签名、diff、断言清单、变异计划 → 审查。
3. **迁移 000027**（§2.3）+ up/down + 自检，并**同步改测试基座**：
   - 新用例按路径分挂白名单：索引形态/EXPLAIN 走**全新路径**（`db_smoke.sh:198`），
     「有重复时迁移被拒」走**升级路径**（`:205`，照 `TestDBSmoke_Migration026BlockedByDuplicates`
     的形态 —— 全新库里造不出重复）。
     T-42：**不加入白名单的新用例会静默不跑**，绿得毫无意义。
   - **`upsertTestSchema`（`upsert_test.go:61` 起）必须补上 `alerts` 的部分唯一索引**，
     谓词与迁移逐字相同（照它给 `tickets` 加 `uq_tickets_glpi_external_id` 的先例，
     `:94-99`）。**漏了这一步，所有 Zabbix 用例会一起红在**
     `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint` ——
     那是**基座缺件，不是被测代码的问题**（schema 注释自己写着这句话）。
   - 同处 **`:48-60` 的 `upsertTestSchema` 注释必须重写**：它现在明说
     「alerts.trigger_id **没有**唯一索引 —— 生产也没有（000013 只加列）」，
     000027 之后这句是假的。改写成与 tickets 同一段落的形态（部分索引 +「带匹配
     TargetWhere 才可仲裁」的方向说明）。
4. **实现 A**（§2.1 + §2.2）+ **B**（§2.4）+ **D-1 的库层兑现**（§2.3 的 ON CONFLICT 与计数）。
5. **改写既有测试**（三路审查一致指出的缺口，v1 漏了）。全部 5 处调用点已定位：
   `upsert_test.go:403`、`:414`、`:469`、`:503`、`:554`；前四个是 `n, err :=` /
   `n, err =`，末一个是 `n, err :=`。
   - **`:524` `TestSyncFromZabbix_本地已确认的告警会重复插入` —— 断言必翻。** fixture **不带
     lastchange**（服务端响应无该字段）→ 走降级分支 → `open` 命中 ack 行 → `n` **1→0**、
     `len(rows)` **2→1**。用例名、`:519-523` 的说明段、`:556`/`:560` 的断言文案
     （含 `TODO G-27`）全部失效，需一并改写为表达「本地已确认的告警不再重复插入」。
     `:561`「ack 行必须原样保留」保留，`:562`（断言 `rows[1].Status == "problem"`）删除。
   - **`:363-370` 与 `:404` —— 注释与诊断文案变成假话（断言本身不变）。**
     `:363` 「守「Zabbix 路径不生成 ON CONFLICT」」、`:365-366`
     「alerts.trigger_id 在生产与测试 DDL 里都**没有**唯一索引」、`:369-370`
     「预过滤只跳过 status='problem' 的」、以及 `:404` 的失败文案
     「加回了 ON CONFLICT？alerts.trigger_id 没有唯一索引」—— 四处都是 M27 要推翻的陈述。
     `:404` 尤其要紧：**它会指错方向**（把首次同步失败引向「ON CONFLICT 加错了」，
     而新设计里 ON CONFLICT 是**该在**的）。注意 `buildUpsertClause("trigger_id", …)`
     那条反证本身仍成立（`trigger_id` 单列上没有唯一索引），但论据要从「没有任何索引」
     改成「仲裁者只在 `(trigger_id, problem_start)` 上」。
   - **`:371` / `:461` / `:490` 三条不动**，逐条理由：`:371` 首轮插 1（唯一的 `resolved`
     行不在 `open`）、次轮 0（新插的 `problem` 行在 `open`）→ 与 `:405`/`:414` 的断言一致；
     `:461` 有 lastchange 但库空 → usable 分支 `exact` 空 → 插 1；`:490` 库空 → 降级插 1。
     三条都不涉及「同 trigger 二次同步」的计数，故新语义下取值不变。
6. **新增测试**：usable/degraded 两分支 × 6 行行为表；截断（含**断言请求体 limit**，R13）；
   迁移的幂等/自检/负循环；真 PG 冒烟 `TestDBSmoke_*`（加白名单）。
7. **前端**：`Settings.tsx` 文案（不含数字）+ 单测钉住「文案不含数字」（R5）。
8. 分支覆盖 ≥80%；变异逐条确认**红在断言上**；代码审计后迭代；台账（TODO/TRAPS/CHANGELOG）。

## 6. 边界（本轮不做，明确登记）

- **C：`alert.created` 无发布方、新告警零通知**（`TopicAlertCreated` 有订阅无发布；
  `notification_logs` 唯一写入点只被 Acknowledge/Resolve 调用）。价值最高但属功能建设，
  且必须先设计通知风暴抑制 —— 单独立项。**与 G-39 不是同一件事。**
- **D/G-39：`AlertRule.NotifyChannels` 只写不读**。根因比条目里记的更深：`alert_rules`
  是纯 CRUD，**全仓没有把告警与规则匹配的地方**（`alerts.alert_rule_id` 列在
  `migrations/000001_init.up.sql:639` 就存在，但无写入者）。要修得先建规则评估引擎。
- **同步侧的 resolve 路径**（源侧恢复时把本地行置 `resolved`）：R8 的根治手段，
  但那是新语义（本地已 resolve 的行要不要被源侧覆盖？），单独立项。
- **`WriteTimeout` / `CreateInBatches` 部分提交 / `httpx` 无大小上限**（R6/R7/R10）：
  都是既有约束，各自属独立模块。
- **`Trigger.Items` / `LastEvent` / `Value` 等零读取字段**：本轮只从请求里摘掉 `selectItems`；
  struct 字段是否一并删除留给将来（删字段会动到导出的 API 形状）。
- **源侧 `countOutput` 取真实总数**：能给出真实条数，代价是每轮多一次 API 调用且两次调用间
  数字会漂。本轮用 0/1 标志（§3）。

## 7. 决策表（**已拍板** 2026-09-11）

| # | 决策 | 取值 | 理由 |
|---|---|---|---|
| D-1 | A 的去重键 | `(trigger_id, problem_start.Unix())` | M26 §2.3 之后才有这个身份（§1.3） |
| D-2 | 降级判据 | 同 trigger 且 `status != "resolved"`（**Go 形态**） | 失败方向是「多一行可见的重复」而非「静默吞掉新告警」；SQL 形态在 NULL 上分叉（§2.2） |
| D-3 | 降级时是否入库 | 入库（无 `open` 行时） | 拒绝导入会丢弃真实告警 |
| D-4 | 身份是否在库层兑现 | **兑现**：迁移 000027 部分唯一索引 + `ON CONFLICT DO NOTHING` | 与 GLPI 侧 M26/D-4 同形；只在 Go 侧查是 TOCTOU |
| D-5 | B 的根因处置 | 删 `selectItems` + 上限提到 5000 | 字段零读取点；与 `GetMetricItems` 同值但**互不耦合** |
| D-6 | 截断可见性 | **标志**透出到 UI（对齐 `glpi_skipped` 的链路） | 「静默丢票比报错更难发现」逐字适用 |
| D-7 | `truncated` 语义 | 0/1 标志，非条数 | 真实总数本轮不可知，不编假数字 |
| D-8 | `synced` 计数 | 同事务 COUNT 前后差 | T-49：ON CONFLICT + RETURNING 下 `RowsAffected` 虚报 |
| D-9 | 存量数据兼容 | **不做** | 用户确认无真实存量数据（只有测试/冒烟数据） |
| D-10 | 前端文案 | **不含数字** | 插值会渲染出「另有 1 条未导入」，把静默丢票反转成少报丢票 |

## 8. 三路对抗审查结论与处置（2026-09-11）

三路独立只读审计（正确性 / 边界+安全 / 一致性），findings 与处置：

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| 1 | 三路一致 | §5 漏了「要改哪些既有测试」 | 已补 §5 步骤 5，逐条列明改/不改及理由 |
| 2 | 一致性 | §1.3「M26 之前 problem_start 写 now」与 HEAD 代码及 M26 文档冲突 | 已改写 §1.3（含「先纠正本文档 v1 的一处事实错误」） |
| 3 | 一致性 | §1.3 误引 `M26/D-8` | 已改为 `M26 §2.3`，并注明 D-8 讲的是 `created_at` |
| 4 | 一致性 | §3 的 OpenAPI 行是假条件 + 死引用「见 §5 步骤 5」 | 已改为「不需要改」并给出 `grep` 证据 |
| 5 | 一致性 | §2.5 要求改前端但 §5 无前端步骤、§3 无前端行 | 已补 §3 前端行 + §5 步骤 7 |
| 6 | 边界 | 照抄 `glpi_skipped` 会渲染「丢了 1 条」 | 已定 D-10（文案不含数字）+ §3 + §4 R5 + §5 步骤 7 |
| 7 | 正确性 | 降级判据的 SQL/Go 形态在 NULL 上分叉 | 已定 D-2 写死 Go 形态 + §2.2 展开 |
| 8 | 正确性 | 表第 4 行「旧行为」有条件（旧行仍为 `problem` 时旧代码静默丢告警） | 已修表 + 加说明 |
| 9 | 正确性 | usable 分支不查 `open` → 可并存多行 `problem` | 已登记 §4 R8 + §6 |
| 10 | 边界+正确性 | 「无唯一索引 → TOCTOU」与 GLPI 侧处置不对称 | 已定 D-4（纳入 §2.3） |
| 11 | 边界 | `WriteTimeout=30s` 才是真约束 | 已登记 §4 R6 |
| 12 | 正确性 | `CreateInBatches` 部分提交 + 返回 0 | 已登记 §4 R7 |
| 13 | 边界 | 真无界点是 `httpx` 的 `io.ReadAll` | 已登记 §4 R10 |
| 14 | 正确性 | 探针只覆盖 UTC 输入，降级行用 Local | 已登记 §4 R11 |
| 15 | 边界 | `Select` 三列不改内存数量级 | 已纠正 §4 R2 措辞 |
| 16 | 边界 | 空 `triggerIDs` 的 SQL 形态 | 已核实无缺陷（gorm `IN (NULL)`）并写入 §2.1 |
| 17 | 边界 | 常量注释声称的耦合不存在 | 已改 §2.4 的常量注释 |
| 18 | 正确性 | `synced` 在 ON CONFLICT 下虚报 | 已定 D-8（同事务 COUNT） |
| 19 | 正确性+边界 | NULL `problem_start` 是否让查询报错（两路结论相反） | **已实测裁定**（§1.6）：不报错，与旧查询同命 |
| 20 | 一致性 | `zabbix.go:259` 行号错位、`grep "\.Items"` 描述不实 | 已修 §1.5（`:244` / `:270-296` / 零命中） |
| 21 | 边界 | 既有 `logTimeUnusable` 的 `%s` 未转义 | 已登记 §4 R12（非本轮引入） |
| 22 | 正确性 | 截断判定的假阴性 + 单测钉不住 `+1` 前提 | 已登记 §4 R13 + §5 步骤 6 要求断言请求体 limit |
| 23 | 一致性 | D-5「计数」与 D-6「0/1 标志」措辞冲突 | 已改为 D-6「**标志**透出到 UI」 |

**复审补记（2026-09-11，提交前逐条核对证据时发现，v1 与三路审查都漏了）**：

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| 24 | 边界 | `upsertTestSchema` 是手写 DDL、不走迁移 → 000027 对它无效，`ON CONFLICT` 会让**所有 Zabbix 用例一起红** | 已加 §5 步骤 3 的独立子步骤 + §4 R14 |
| 25 | 一致性 | `upsert_test.go:363-370` **与 `:404`** 的注释/诊断文案变假；`:404` 会**指错方向** | 已扩写 §5 步骤 5（v1 只提了 `:369-370`） |
| 26 | 一致性 | §3 的 OpenAPI 证据行引的是**不存在的路径**（`backend/openapi.yaml`），等于未验证；实际 spec 在 `backend/internal/api/openapi.yaml` | 已用真实路径重验：全文件仅 `:2599`（`audit_logs.source` 的 description）命中，`paths:` 段确无该端点 → 结论「不需要改」仍成立，证据已换 |
| 27 | 边界 | 自检若把 NULL `problem_start` 算作重复 → 同 trigger 的多个 NULL 行**永远无法满足**，迁移被永久阻塞（PG 唯一索引视 NULL 互不相等） | 已在 §2.3 写死 `problem_start IS NOT NULL` |
| 28 | 一致性 | `TestDBSmoke_AlertsTriggerIDIndex`（`:1607`）只验证索引形态与 EXPLAIN，**不插入任何 alerts 行** → 不会与新索引冲突；但它注释里的查询形态「`trigger_id IN (...) AND status='problem'`」在本轮之后**不再是真实查询** | 已记入 §2.3 的存量核实；该注释留待步骤 3 顺带更新 |

**细节文档审查补记（2026-09-11，`docs/IMPL-ZABBIX-SYNC.md` 的三路审查）**：

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| 29 | 一致性/正确性 | §2.3 的 `TargetWhere` 片段**漏了 `source = 'zabbix'`**，与同节索引谓词矛盾 → 正是该节自己写的 42P10 | 已补（本轮最重要的一条：它会让每次同步 500） |
| 30 | 正确性 | §2.3 的 COUNT 片段也漏 `source` 收窄 | 已补 |
| 31 | 正确性 | §1.3 的证据命令 `git show HEAD~` 取错层（`HEAD~` 正是引入 `ProblemStart` 的 M26 提交，grep 得 1 而非 0） | 已改 `HEAD~2`，并把这处自纠写进正文 |
| 32 | 正确性（**实测推翻**） | 「`RowsAffected` 虚报 → 故用 COUNT」的论据对 `Alert` **不成立**：实测 sqlite 3.45.1，1 冲突 + 2 新 → `RowsAffected=2`（= 真实插入数）。虚报是 `Ticket` 特有（`BeforeCreate` 填 ID → 不走 `RETURNING`） | 已保留 COUNT（D-8 不变，理由改为「对 INSERT 路径变化免疫 + 与 GLPI 侧一致」），并**把假论据从正文与将来的注释里删掉** |
| 33 | 测试 | 变异 M5（`RowsAffected`）**不可证伪** —— 第二次同步在预过滤就早退，INSERT 根本不执行 | 已从变异表移除，改为一条**注入式**用例（照 `upsert_test.go:801` 的 `TestSyncFromGLPI_预查后漏进冲突行仍幂等`）：预过滤后注入冲突行 → 1 冲突 + 2 新 → 断言 `synced==2` |
| 34 | 测试 | M1/M3/M9 的「应红行」写错或不可达 | 已逐条修正（M1 红在第 3、4 行；M3 需「同日不同秒」的 fixture；M9 需 NULL 行的裸 SQL） |
| 35 | 测试 | 真 PG 用例④（NULL `problem_start` 不冲突）**用 `models.Alert`+`db.Create` 造不出来** —— 非指针 `time.Time` 落零值哨兵（非 NULL），第二行直接 23505 | 已在细节文档写明这是「不手写 INSERT」那条规矩的**例外**，该格必须显式写 NULL |
| 36 | 正确性 | 迁移自检的 `problem_start IS NOT NULL` 意味着**零值**行（pre-M26 存量）**会**被算作重复 → 自检命中 → 启动阻塞。需求 v2 只讲了 NULL 那一半 | 已补：零值行命中自检**正是预期**（它们确实是重复），并把「自检触发时怎么办」写进细节文档 §2.1 |
| 37 | 一致性 | 多处行号失准，其中 `:1274`/`:1280` 会把人引到 `TestDBSmoke_GLPITimeZoneWallClock` 里改坏代码（实际在 `:1498`/`:1504`）；`:1362`→`:1374`；`:1372-1378`→`:1385-1390`；`:96-104`→`:94-99`；`zabbixAuthTTL` 不是「每请求上限」的先例 | 已逐处更正 |
| 38 | 测试 | 前端用例缺**触发前置**：默认 mock 下 `zabbix.enabled` 为 undefined → 按钮 disabled → 点击不触发 handler，断言读到 undefined | 已在细节文档 §6 写明三个 mock 前置 |
| 39 | 测试 | `fakeZabbixServer` 表达能力不足（写死单行、单一 lastchange、`r.Body.Read` 单次短读） | 已在细节文档 §7.3 要求参数化 fake + `io.ReadAll` |

**新增实测记录（2026-09-11，sqlite 3.45.1 / go-sqlite3 v1.14.22）**：

- 基座缺索引时的报错**逐字**为 `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`
  —— 与细节文档 §7.1 的预测一致，且与 GLPI 侧 M26 的 E5b 同源。
- `Alert` 路径 `RowsAffected` 与真实插入数一致（见 #32）。
- sqlite 对 conflict target 的匹配是**解析树结构比较**（`sqlite3UpsertAnalyzeTarget`），
  不是文本比较 —— 空白/引号风格/`!=` 与 `<>` 的差异不影响。故「谓词逐字一致」这个要求
  实际比 sqlite 所需更严，方向安全。

**审查确认无误的**（同样有价值，记录以免后人重复怀疑）：命名体系与既有习惯一致
（`zabbixTriggerLimit` ↔ `zabbixAuthTTL`；`zabbix_truncated` ↔ `glpi_skipped`；日志前缀
`M27:` ↔ `M26:`）；T-31/T-42/T-48 三处引用准确；§1.5「三个独立问题」恰为三个；
D-1/D-2/D-4/D-6/D-7/D-8 与正文一致；有界性主张成立（两条分支都收敛，不会每轮加一行）；
`Acknowledge`/`Resolve`/`Bulk*` 都不改 `problem_start`（行为表第 2/3/5 行的前提成立）；
`idx_alerts_trigger_id` 覆盖新 WHERE；迁移不会误伤 seed 与冒烟数据（§2.3 已核实）。
