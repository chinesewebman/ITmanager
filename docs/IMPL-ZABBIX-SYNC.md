# 可执行细节文档：Zabbix 同步导入保真（M27）

配套需求文档：`docs/FIX-PLAN-ZABBIX-SYNC.md`（下称「需求 §N」）。本文只写**怎么做**，
不重复论证；每个决定都指回需求里的 D-N / RN。

> 本文经三路对抗审查（可编译性/GORM/真 PG、测试可行性、一致性）后改写，
> 处置见 §9 与需求 §8 的「细节文档审查补记」。**行号一律以磁盘 `fd080c2` 为准**。

## 0. 拍板摘要（实现必须遵守的硬约束）

| # | 约束 | 出处 |
|---|---|---|
| 1 | 去重键 = `trigger_id + "\|" + problem_start.Unix()`，**只对 `timeOK` 的行成立** | D-1 |
| 2 | `lastchange` 不可用时降级判据 = 同 trigger 且 **Go 形态 `status != "resolved"`**（不得写成 SQL `<>`） | D-2、需求 §2.2 |
| 3 | SQL 预过滤**不再按 status 过滤**，但**必须加 `source = 'zabbix'`**（与索引谓词一致） | 需求 §2.1/§2.3 |
| 4 | 索引谓词 = `source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''`，`TargetWhere` **逐字相同** | D-4、需求 §2.3 |
| 5 | `synced` 用**同事务 COUNT 前后差**（带 `source` 收窄）。**理由不是「RowsAffected 会虚报」** —— 那个虚报是 `Ticket` 特有，`Alert` 实测准确，见 §3.5 | D-8 |
| 6 | 截断 = **0/1 标志**（`truncated`），不是条数；前端文案**不含数字** | D-6、D-7、D-10 |
| 7 | 上限 `zabbixTriggerLimit = 5000`，请求 `limit = 5000 + 1`（多要 1 条才分得清截断） | 需求 §2.4 |
| 8 | 删 `selectItems`（`Trigger.Items` 零读取点） | 需求 §1.5 |
| 9 | `service.go` 要**新增 `strconv` import**（`alertIdentityKey` 用；该文件目前没有它） | 审查 B1 |
| 10 | 每个阶段**单独 commit + push** | 用户 2026-09-11 指示 |

## 1. 改动文件清单

| 文件 | 改动 | 步骤 |
|---|---|---|
| `backend/migrations/000027_alerts_zabbix_identity_unique.{up,down}.sql` | 新增 | 3 |
| `backend/internal/integration/zabbix.go` | 删 `selectItems`；`limit` 100 → `zabbixTriggerLimit+1`；新增常量；`Item.Hosts` 注释纠正（`:258-259`） | 4 |
| `backend/internal/integration/service.go` | `SyncFromZabbix` 签名 +1、预过滤重写、两条判据、ON CONFLICT + COUNT；`SyncAll`（`:384`）适配；**import 加 `strconv`**；`:215-216` 的老注释核对 | 3–4 |
| `backend/internal/api/handlers/integration_handler.go` | `case "zabbix"`（`:65`）透出 `zabbix_truncated` | 4 |
| `backend/internal/integration/upsert_test.go` | **5 处调用点**（`:403`/`:414`/`:469`/`:503`/`:554`）+ 3 处用例改写 + 基座 DDL 补索引 + `:48-60` 注释块重写 + 新增六行行为表用例 | 3–5 |
| `backend/internal/integration/zabbix_truncate_test.go` | **新增**：截断标志 + 请求体断言 | 4 |
| `backend/tests/db_smoke_test.go` | 3 个新用例 + Down 链前推一层 + `Migration026BlockedByDuplicates` 改版本无关 | 3–5 |
| `scripts/db_smoke.sh` | `:198` / `:205` 白名单追加 | 5 |
| `frontend/src/pages/Settings.tsx` | `handleSyncZabbix`（`:255-271`）追加截断文案（不含数字） | 4 |
| `frontend/src/pages/Settings.test.tsx` | **追加**（该文件已存在，663 行）：文案不含数字 + 触发前置 | 4 |
| `docs/TODO.md` / `docs/TRAPS.md` / `CHANGELOG.md` | 台账（G-27 关闭、新 T-N） | 6 |

## 2. 迁移 `000027`

### 2.1 up

```sql
-- 000027_alerts_zabbix_identity_unique
--
-- M27/D-4：(trigger_id, problem_start) 被声明为「一次故障发生的身份」
-- （需求 §1.3：M26 §2.3 把 problem_start 改成 Zabbix 的 lastchange 之后，
--   它才第一次成为源侧派生、随故障发生而变的键）。
-- 只在 Go 侧查一次是 TOCTOU —— 与 GLPI 侧 M26/D-4 完全同形，故同样加索引 + ON CONFLICT。
--
-- 【为什么是部分唯一索引】谓词把约束收窄到「Zabbix 导入的告警」：
--   · trigger_id <> ''：手工告警不带 trigger_id（Alert.TriggerID 的非测试写入点
--     只有 Zabbix 同步与 cmd/seed），不该被这条约束管住；
--   · source = 'zabbix'：将来一旦有第二个写 trigger_id 的来源，不带这条会让
--     两边 (trigger_id, problem_start) 相撞 → ON CONFLICT 把**真实的 Zabbix 告警
--     静默跳过**（无日志、无返回差异）。带上源收窄后同类碰撞退化成可见的重复行。
--     与 GLPI 侧 uq_tickets_glpi_external_id 的 source='glpi' 是同一取向。
-- 谓词必须与 service.go 的 ON CONFLICT TargetWhere **逐字一致**，差一个字符 = PG 42P10
-- = 每一次 Zabbix 同步 500。（sqlite 的匹配是解析树比较，比这宽松；PG 才是判据。）
--
-- 【保留 000018 的普通索引 idx_alerts_trigger_id】同步预查是
-- `WHERE source = 'zabbix' AND trigger_id IN (...)`，它推不出 source='zabbix'，
-- 部分索引**无法替代**它（同 000026 保留 idx_tickets_external_id 的理由）。
--
-- 【锁语义】CREATE INDEX 取 SHARE 锁、阻塞 alerts 写入；迁移框架在事务内执行
-- （migrate.go execInTx），故不能用 CONCURRENTLY。大表走维护窗口。
--
-- 【自检】把「无 DETAIL 的 23505」变成带样本的异常。三个细节：
--   · problem_start IS NOT NULL 必须显式写 —— PG 唯一索引视 NULL 互不相等，
--     同 trigger 的多个 NULL 行**建索引时并不冲突**；自检若把它们算重复，
--     迁移会被拒且**永远无法满足**（只能靠删数据过关）。
--   · 但**零值行会命中自检，这是预期**：pre-M26 的 Zabbix 行 problem_start 落的是
--     零值哨兵 0001-01-01（**非 NULL**，非指针 time.Time 写不出 NULL），同 trigger
--     被同步过两次就是真重复 —— 自检报出来正是它的职责。
--   · trigger_id <> '' 本身已排除 NULL（NULL <> '' 是 NULL），仍显式写 IS NOT NULL
--     让谓词与索引定义字面可比。
DO $$ DECLARE n int; s text;
BEGIN
  SELECT count(*), string_agg(trigger_id || '@' || problem_start::text, ', ') INTO n, s FROM (
    SELECT trigger_id, problem_start FROM alerts
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''
      AND problem_start IS NOT NULL
    GROUP BY trigger_id, problem_start HAVING count(*) > 1
    ORDER BY trigger_id LIMIT 5
  ) x;
  IF n > 0 THEN
    RAISE EXCEPTION '存在重复的 zabbix 告警身份 (trigger_id, problem_start)（最多列 5 个）：%，请先人工清理后重跑迁移', s;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_zabbix_identity
    ON alerts(trigger_id, problem_start)
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> '';
```

**自检触发时怎么办**：说明库里**已经**有重复（零值行或真重复行）。D-9 判定无真实存量数据，
故**不提供清理脚本**（自动删告警数据比让迁移失败危险）。运维按异常文本里的样本键人工核对。
**注意失败的代价**：DDL 与版本记录同事务 → 版本不落 → 每次重启重放 → 服务持续不可用
（需求 §4 R15）。

### 2.2 down

```sql
-- 000027_alerts_zabbix_identity_unique 回滚
--
-- 本迁移**可逆**：只建了一个索引，无存量数据改写。回滚即删索引。
--
-- ⚠️ 但滚掉它会**静默改变运行语义**，不只是「少个索引」：
--   service.go 的 ON CONFLICT (trigger_id, problem_start) WHERE ... 依赖这个索引当仲裁者。
--   索引一没，PG 立刻 42P10（there is no unique or exclusion constraint matching
--   the ON CONFLICT specification）→ **每一次 Zabbix 同步都 500**。
-- 也就是说：这一层滚下去，代码必须跟着回退到 000027 之前的版本，不能只滚迁移。
-- 若只是想临时关掉重复保护，正确做法是停 Zabbix 同步，而不是滚这一层。
--
-- 保留 idx_alerts_trigger_id（000018 的普通索引）—— 它不归本迁移管，且预查查询靠它。

DROP INDEX IF EXISTS uq_alerts_zabbix_identity;
```

## 3. `service.go`：`SyncFromZabbix`

### 3.1 import 与签名

```diff
 import (
 	"context"
 	"errors"
 	"fmt"
 	"log"
+	"strconv"
 	"time"
```

（`gorm.io/gorm/clause` **已在** `:11` import（GLPI 用它）→ 不必新增。`strconv` **不在**，
新函数要用，必须加 —— 漏了就是 `undefined: strconv`。）

```diff
-func (s *IntegrationService) SyncFromZabbix(ctx context.Context) (int, error) {
+func (s *IntegrationService) SyncFromZabbix(ctx context.Context) (synced, truncated int, err error) {
```

命名返回值是照 `SyncFromGLPI` 的形态（`(synced, skipped int, err error)`），并且
§3.5 的 `synced = int(after - before)` 写在闭包里，**必须**是命名返回值才赋得上。

### 3.2 头部：截断检测

```go
	triggers, err := s.zabbix.GetTriggers(ctx)
	if err != nil {
		return 0, 0, err
	}
	// M27/B：GetTriggers 请求 limit = zabbixTriggerLimit+1，所以「收到超过上限」是可判定的。
	// 恰好多要 1 条是必须的：源侧正常返回正好等于 limit 时（就是这么多），
	// 与「被截断到 limit」不可区分。
	if len(triggers) > zabbixTriggerLimit {
		truncated = 1
		// 日志只插值常量（不含 t.TriggerID 等源侧可控文本）→ 不引入日志注入面。
		// 按 lastchange 倒序（zabbix.go 的 sortorder=DESC），故被丢的是**最老的**进行中告警。
		log.Printf("Zabbix 返回的告警数超过上限 %d，源侧仍有告警本次未导入（按 lastchange 倒序，被丢的是最老的）",
			zabbixTriggerLimit)
		triggers = triggers[:zabbixTriggerLimit]
	}
	if len(triggers) == 0 {
		return 0, truncated, nil
	}
```

### 3.3 预过滤与两个集合

```diff
 	triggerIDs := make([]string, 0, len(triggers))
 	for _, t := range triggers {
 		triggerIDs = append(triggerIDs, t.TriggerID)
 	}
+
+	// M27/A：判据要两种，所以不再按 status 过滤 —— 留 status 过滤会让 usable 分支看不到
+	// resolved 行，而那正是「本地已解决、源侧仍 firing」时必须命中的行（需求 §2.1）。
+	// 只取判据需要的三列（problem TEXT 等不拉）。
+	// source = 'zabbix' 与 000027 的索引谓词一致，否则 Go 侧与库侧对「同一身份」判断分叉。
 	var existing []models.Alert
 	if err := database.DB.WithContext(ctx).
-		Where("trigger_id IN ? AND status = ?", triggerIDs, "problem").
+		Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).
+		Select("trigger_id", "problem_start", "status").
 		Find(&existing).Error; err != nil {
-		return 0, fmt.Errorf("Zabbix 已存在查询失败: %w", err)
+		return 0, 0, fmt.Errorf("Zabbix 已存在查询失败: %w", err)
 	}
-	existingSet := make(map[string]struct{}, len(existing))
-	for _, e := range existing {
-		existingSet[e.TriggerID] = struct{}{}
-	}
+
+	// exact：同一 trigger 的同一「故障发生」。键用 Unix() 秒（lastchange 就是 Unix 秒，
+	// 且 Unix() 与 Location 无关 —— M26 在 pgx/sqlite 上各踩过一次，T-48）。
+	// 零值/NULL 的 problem_start 都经 IsZero() 排除：NULL 被扫成零值、两者不可区分
+	// （真 PG 实测，需求 §1.6）。
+	exact := make(map[string]struct{}, len(existing))
+	// open：**仅**降级分支用。**必须**是 Go 形态 != "resolved"：SQL 三值逻辑下
+	// `status <> 'resolved'` 对 NULL 求值为 NULL → 该行不入集合；Go 下 NULL 扫成 ""
+	// → 入集合。后者才与 D-2 的失败方向一致（需求 §2.2）。
+	open := make(map[string]struct{}, len(existing))
+	for _, e := range existing {
+		if !e.ProblemStart.IsZero() {
+			exact[alertIdentityKey(e.TriggerID, e.ProblemStart)] = struct{}{}
+		}
+		if e.Status != "resolved" {
+			open[e.TriggerID] = struct{}{}
+		}
+	}
```

新增小函数（放 `SyncFromZabbix` 上方）：

```go
// alertIdentityKey 是 M27/A 的去重身份：同一 trigger 的**同一次故障发生**。
// 用 Unix 秒而不是格式化字符串：lastchange 是 Unix 秒，且 Unix() 与 Location 无关。
func alertIdentityKey(triggerID string, problemStart time.Time) string {
	return triggerID + "|" + strconv.FormatInt(problemStart.Unix(), 10)
}
```

> 抽成函数而不是内联两次字面量：`exact` 的**构造**与**查询**两处必须同源，
> 写作两遍就是给将来「只改一处」留一个静默失效的口子（改错的方向是**漏判** → 插重复行）。

### 3.4 循环：两条判据

```diff
 	now := time.Now()
 	toInsert := make([]models.Alert, 0, len(triggers))
 	for _, t := range triggers {
 		if len(t.Hosts) == 0 {
 			continue
 		}
-		if _, ok := existingSet[t.TriggerID]; ok {
-			continue // 跳过已存在（避免重复）
-		}
 		alert := t.ConvertToAlert()
 		// M26：problem_start 取 Zabbix 的 lastchange（故障实际开始时刻），不是同步时刻。
 		// ...（原注释整段保留）
 		problemStart, st := parseUnixSeconds(t.LastChange)
-		if st != timeOK {
+		if st == timeOK {
+			// M27/A：同一 trigger 的同一故障只入一次 —— 无论本地是 problem / acknowledged
+			// 还是 resolved（旧判据只认 problem，运维点一次确认就多一条未确认行，需求 §1.2）。
+			if _, ok := exact[alertIdentityKey(t.TriggerID, problemStart)]; ok {
+				continue
+			}
+		} else {
+			// M27/A 降级：lastchange 不可用 → 身份不可知，退到「同 trigger 且未解决即算已存在」。
+			// 失败方向是「多一行可见的重复」，不是「静默吞掉后续再触发」（需求 §2.2、D-2）。
 			logTimeUnusable("Zabbix trigger", t.TriggerID, "lastchange", t.LastChange, st)
 			problemStart = now
+			if _, ok := open[t.TriggerID]; ok {
+				continue
+			}
 		}
 		toInsert = append(toInsert, models.Alert{ /* 不变 */ })
 	}
```

**`logTimeUnusable` 的位置变了、语义没变**（仍对 `st != timeOK` 的每一行打一次）。
`timeAbsent` 与 `timeInvalid` 都走降级 —— 与 M26 一致。

### 3.5 插入：ON CONFLICT + 同事务计数

```diff
 	if len(toInsert) == 0 {
-		return 0, nil
+		return 0, truncated, nil
 	}
-	// 不加 ON CONFLICT：同一 trigger 会「触发 → 恢复 → 再触发」，多行历史是预期语义
-	// （上面的预过滤只跳过「当前未恢复」的），且 alerts.trigger_id 上没有唯一索引 ——
-	// 加了只会让整条语句在真 PG 上 42P10 失败（见 docs/FIX-PLAN-NETBOX-UPSERT.md §1.2-3）。
-	if err := database.DB.WithContext(ctx).
-		CreateInBatches(toInsert, 100).Error; err != nil {
-		return 0, fmt.Errorf("Zabbix 批量插入失败: %w", err)
-	}
-	log.Printf("从 Zabbix 同步了 %d 个告警", len(toInsert))
-	return len(toInsert), nil
+	// M27/D-4：迁移 000027 建了部分唯一索引 uq_alerts_zabbix_identity。上面的 exact/open
+	// 预过滤是 TOCTOU，并发同步会漏进重复行；CreateInBatches 又是**整批原子**的
+	// （gorm finisher_api.go），撞索引会让整批新告警一起回滚。故配 ON CONFLICT 把
+	// 「硬失败」变成「幂等跳过」。谓词必须与索引逐字一致 —— 不带 TargetWhere 的
+	// ON CONFLICT (trigger_id, problem_start) 在部分索引下 42P10（同 GLPI 侧 service.go:322-326）。
+	//
+	// 计数用同事务 COUNT 前后差，而不是 res.RowsAffected。**理由不是「RowsAffected 会虚报」**
+	// —— T-49 记录的那个虚报是 Ticket 特有的（Ticket 的 BeforeCreate 自己填 ID，gorm 因此
+	// 不加 RETURNING，走另一条语句路径）；Alert 实测**不虚报**（sqlite 3.45.1：1 冲突 + 2 新
+	// → RowsAffected=2，与真实插入数一致）。保留 COUNT 是因为 ① 它对 INSERT 语句路径的变化
+	// 免疫（哪天给 Alert 加个 BeforeCreate，RowsAffected 的含义就会跟着变）；
+	// ② 与同一文件 SyncFromGLPI 的计数法一致，读者不必分辨两种写法。
+	if err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
+		var before, after int64
+		if err := tx.Model(&models.Alert{}).
+			Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).Count(&before).Error; err != nil {
+			return err
+		}
+		if err := tx.Clauses(clause.OnConflict{
+			Columns: []clause.Column{{Name: "trigger_id"}, {Name: "problem_start"}},
+			TargetWhere: clause.Where{Exprs: []clause.Expression{
+				clause.Expr{SQL: "source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''"},
+			}},
+			DoNothing: true,
+		}).CreateInBatches(toInsert, 100).Error; err != nil {
+			return err
+		}
+		if err := tx.Model(&models.Alert{}).
+			Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).Count(&after).Error; err != nil {
+			return err
+		}
+		synced = int(after - before)
+		return nil
+	}); err != nil {
+		return 0, truncated, fmt.Errorf("Zabbix 批量插入失败: %w", err)
+	}
+
+	log.Printf("从 Zabbix 同步了 %d 个告警（截断标志 %d）", synced, truncated)
+	return synced, truncated, nil
```

**`:215-216` 的老注释要一起核对**：`SyncFromGLPI` 上方那段「**不加 ON CONFLICT**：……
alerts.trigger_id 上没有唯一索引」描述的是 M27 之前的 Zabbix 事实，现在两句都假
（加了 ON CONFLICT；`(trigger_id, problem_start)` 上有部分唯一索引）。改为指向 000027。

### 3.6 `SyncAll`（`:384`）

```diff
-	if n, err := s.SyncFromZabbix(ctx); err != nil {
+	if n, trunc, err := s.SyncFromZabbix(ctx); err != nil {
 		log.Printf("Zabbix 同步失败: %v", err)
 		errs = append(errs, fmt.Errorf("zabbix: %w", err))
 	} else {
 		results["zabbix"] = n
+		// M27/D-6：截断标志一并透出 —— 静默丢告警比同步报错更难发现。
+		results["zabbix_truncated"] = trunc
 	}
```

> 失败分支**不**写 `zabbix_truncated`：与 `glpi_skipped` 一致（失败时 `results` 里连键都没有，
> 前端 `?? 0` 兜住）。保持同一形态。

## 4. `zabbix.go`

```diff
+// zabbixTriggerLimit 是 trigger.get 的**本条 API 自己的**上限，与 GetMetricItems 的 5000
+// 恰好同值但互不耦合（本包既有的做法是每个请求**内联**写自己的上限：:133 的 5000、
+// :173 原本的 100 —— 这里提成常量只因为 service.go 的截断判定还要再用一次）。
+const zabbixTriggerLimit = 5000
+
 // GetTriggers 获取告警列表（C-P7：ctx 透传）。
 	...
 		Params: map[string]interface{}{
 			"only_true":     true,
 			"skipDependent": true,
 			"filter":        map[string]interface{}{"value": 1},
 			"selectHosts":   "extend",
-			"selectItems":   "extend",
 			"sortfield":     "lastchange",
 			"sortorder":     "DESC",
-			"limit":         100,
+			// +1 是必须的：源侧恰好有 zabbixTriggerLimit 条时，
+			// 「正好等于 limit」与「被截断到 limit」不可区分（需求 §2.4）。
+			"limit": zabbixTriggerLimit + 1,
 		},
```

**`Item.Hosts` 的注释要改**（`zabbix.go:258-259`）—— 现文写
「GetMetricItems selectHosts=extend 时填充。**trigger.get 的 selectItems 不会填充**，留 nil 不影响已有调用方」。
`selectItems` 已从 `trigger.get` 移除，这句话的对象没了。改为：
「GetMetricItems 的 selectHosts=extend 时填充；`trigger.get` 不请求 items（M27/B 已移除
selectItems），故本字段在告警路径恒为 nil —— 无读取点。」

**`Trigger.Items` 字段本身本轮不删**（需求 §6：删字段会动到导出的 API 形状，留给将来）。
`LastEvent`（`:245`）、`Value`（`:239`）同理。

## 5. `integration_handler.go`（`:65`）

```diff
 	case "zabbix":
-		count, e := h.svc.SyncFromZabbix(ctx)
-		results = map[string]int{"zabbix": count}
+		// M27/D-6：truncated 是 0/1 标志（不是条数），与 glpi_skipped 同位置透出。
+		count, truncated, e := h.svc.SyncFromZabbix(ctx)
+		results = map[string]int{"zabbix": count, "zabbix_truncated": truncated}
 		err = e
```

## 6. 前端

`frontend/src/pages/Settings.tsx` 的 `handleSyncZabbix`（`:255-271`）：

```diff
       const res: any = await integrationApi.syncZabbix()
       const synced = res?.data?.data?.synced?.zabbix
+      // M27/D-6/D-10：截断必须露出来，但**文案不许插值数字** —— truncated 是 0/1 标志，
+      // 写成「另有 ${truncated} 条未导入」会把「静默丢票」反转成「少报丢票」
+      // （源侧 6000 条时 UI 显示「另有 1 条」，运维看到 1 就不会去查那 1000 条）。
+      const truncated = res?.data?.data?.synced?.zabbix_truncated ?? 0
       if (res?.data?.code === 0) {
-        message.success(`Zabbix 同步完成，新增 ${synced ?? 0} 条告警`)
+        const base = `Zabbix 同步完成，新增 ${synced ?? 0} 条告警`
+        message.success(truncated > 0
+          ? `${base}；另有告警因超过条数上限未导入（详见后端日志）`
+          : base)
       } else {
```

**与 `handleSyncGLPI`（`:189-207`）的差别是有意的**：那边是 `` `另有 ${skipped} 条档位越界被跳过` ``，
因为 `glpi_skipped` **是条数**。注释里要写明这一点，否则后来者会「统一风格」把它改成数字。

### 6.1 `Settings.test.tsx` 的**触发前置**（缺了就在错误方向变红）

`Settings.tsx:603-606` 的按钮是 `disabled={!integrationStatus?.zabbix?.enabled}`，而该文件
`:67-69` 的默认 `getStatus` mock 只返回 `data: {}` → `zabbix.enabled` 为 `undefined` →
按钮 disabled → `fireEvent.click` 不触发 handler → `message.success` 从未被调用 →
`mock.calls[0][0]` 是 `undefined`，断言会抛 TypeError。三件事缺一不可：

1. `getStatus` mock 返回 `zabbix: { enabled: true }`；
2. `syncZabbix` mock 返回 `synced: { zabbix: 2, zabbix_truncated: 1 }` ——
   **`zabbix_truncated` 必须为 1**，否则截断文案根本不渲染，用例空转；
3. 先切到「第三方集成」tab 再点。

断言形态（钉「不含 truncated 的数字」而不是「不含任何数字」）：
`base` 里本来就有 `新增 2 条告警` 这个数字，所以 `not.toMatch(/\d/)` 是错的。
应断言文案**包含**「超过条数上限」**且**不含「另有 1 条」这种把标志当条数的句式：

```ts
const msg = vi.mocked(message.success).mock.calls[0][0] as string
expect(msg).toContain('超过条数上限')
expect(msg).not.toMatch(/另有\s*1\s*条/)   // truncated 被当条数插值就会命中
```

## 7. 测试

### 7.1 测试基座（`upsert_test.go`）—— **不做这一步，所有 Zabbix 用例一起红**

`upsertTestSchema`（`:61` 起）是**手写 DDL、不走迁移**，所以 000027 不会自动作用于它。
照它给 tickets 加索引的先例（`:94-99`）追加：

```sql
-- 000027 的部分唯一索引，谓词与迁移逐字相同（sqlite 支持部分索引）。
-- 少了它，SyncFromZabbix 的 ON CONFLICT (trigger_id, problem_start) WHERE ... 会直接报
-- "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint"，
-- 本文件的 Zabbix 用例会全红 —— 那是基座缺件，不是被测代码的问题。
CREATE UNIQUE INDEX uq_alerts_zabbix_identity
    ON alerts(trigger_id, problem_start)
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> '';
```

**已实测**（2026-09-11，sqlite 3.45.1 / go-sqlite3 v1.14.22）：缺这个索引时，
上面那句报错**逐字**出现 —— 与预测一致。补上之后，同键二插为 no-op（表仍 1 行）。

同处 **`:48-60` 的注释块必须重写**：它现在明说「alerts.trigger_id **没有**唯一索引 ——
生产也没有（000013 只加列），且同一 trigger 的多行历史是预期语义。把 ON CONFLICT 加到这里
会立刻报……」。000027 之后这段全假。改写成与 tickets 同一段落的形态：**有**部分唯一索引
（000027 建的，谓词与生产逐字相同），并保留「带匹配 TargetWhere 才可仲裁、不带才报错」的
方向说明（那句话本身对，只是不再适用于 alerts「没有索引」这个前提）。

### 7.2 既有用例：改哪几条、为什么

| 位置 | 处理 | 理由 |
|---|---|---|
| `:524`（func 声明；调用在 `:554`）`TestSyncFromZabbix_本地已确认的告警会重复插入` | **改写**：`n` 1→**0**、`len(rows)` 2→**1**；删 `:562` 的 `rows[1]` 断言；用例名改为「本地已确认的告警不再重复插入」；`:519-523` 的说明段（含 `TODO G-27`）重写 | fixture 无 lastchange → 降级分支 → `open` 命中 ack 行 |
| `:363-370`（`TestSyncFromZabbix_保留历史且不重复` 的说明） | **重写** | 三处陈述变假：`:363`「守『Zabbix 路径不生成 ON CONFLICT』」、`:365-366`「alerts.trigger_id 在生产与测试 DDL 里都没有唯一索引」、`:369-370`「预过滤只跳过 status='problem' 的」。**断言本身不变**（`:416`=1、`:420`=2 行、`:414` 调用的第二次同步为 0） |
| `:404`（同上用例内的失败文案） | **重写** | 现文「首次同步失败 —— 加回了 ON CONFLICT？alerts.trigger_id 没有唯一索引」**会指错方向**：新设计里 ON CONFLICT 是**该在**的。改为「首次同步失败 —— 000027 的部分唯一索引/谓词不匹配（42P10）？」。`:365-367` 的 `buildUpsertClause("trigger_id", …)` 反证仍成立（单列上没有唯一索引），论据改成「仲裁者只在 `(trigger_id, problem_start)` 上」 |
| `:403`、`:414`、`:469`、`:503` | 补 `_`：`:403`/`:469`/`:503` 是 `:=`，`:414` 是 `=`（**写错会 `no new variables on left side of :=`**） | 签名 +1 |
| `:371`、`:461`、`:490` | **不动** | `:371` 首轮插 1（唯一 `resolved` 行不在 `open`）/ 次轮 0（新插的 `problem` 行在 `open`）；`:461` 有 lastchange 但库空 → `exact` 空 → 插 1；`:490` 库空 → 降级插 1。三条都不涉及「同 trigger 二次同步」的计数 |

`fakeZabbixServer`（`:425` 起）**表达力不足，不能直接复用**：它把 `triggerid":"100"`、单行
`result`、单一 `lastChange`（构造期固定）写死，且 `r.Body.Read` 只读一次（短读会静默截断 JSON）。
§7.3 与六行表需要一个**可参数化**的 fake（payload 可换、`io.ReadAll(r.Body)`）——
新建在 `zabbix_truncate_test.go` 里，两处共用。

### 7.3 新增单测

**六行行为表**（需求 §2.2 的表；用可参数化 fake）：

| # | 库中预置 | 源侧 lastchange | 期望 `n` | 钉住什么 |
|---|---|---|---|---|
| 1 | `problem` 同行 | 有 | 0 | usable 分支命中 |
| 2 | `acknowledged` 同行 | 有 | **0** | **G-27 的靶心**（旧行为是 1） |
| 3 | `resolved` 同行 | 有 | **0** | 旧行为是 1 |
| 4 | 旧行 `problem` + 旧 lastchange | 有（**新值，与旧值同日不同秒**） | **1** | **旧行为是 0（静默丢真实新故障）** |
| 5 | `acknowledged` 同行 | 无 | **0** | 降级分支命中 |
| 6 | `resolved` 同行 | 无 | **1** | 降级不算「已存在」（D-3） |

- 第 4 行是**反向用例**：钉「源侧恢复后再触发必须入库」，防有人把 `exact` 写成
  「按 trigger_id 命中即跳过」。**两个 lastchange 必须同日不同秒**（如 `1756728000` 与
  `1756728060`）—— 否则将来若有人把键改成日粒度（变异 M3），第 4 行照样绿，测不出来。
  并额外断言新行的 `problem_start` == 新 lastchange。
- 预置行走 `db.Create(&models.Alert{...})`，**不要手写裸 SQL**：裸 SQL 存
  `"2026-09-01 12:00:00"`、gorm 存带偏移的形态，两者字面不等 → 根本不冲突，
  用例会**静默测不到东西**（本次审查第一版 RowsAffected 探针就踩了这个坑）。

**`zabbix_truncate_test.go`（新文件）** —— fake 吐 `zabbixTriggerLimit+1` 行：

- ① 收到 `zabbixTriggerLimit+1` 行 → `truncated == 1`，入库行数 == `zabbixTriggerLimit`；
- ② 收到恰好 `zabbixTriggerLimit` 行 → `truncated == 0`（**边界**：多要 1 条正是为了区分它）；
- ③ 日志里出现「超过上限」；
- ④ 解析请求体，断言 `params["limit"]` **等于 `zabbixTriggerLimit+1`**（解进
  `map[string]any` 后是 `float64` → 用 `assert.EqualValues`，否则红在类型上），
  **且 `params` 里没有 `selectItems` 键**（写成「不含该键」而非「值为空」——
  值为 `nil` 的数组仍占响应体积）。

**fixture 硬约束**：① 的 5001 行、② 的 5000 行，`triggerid` 必须**互不相同**。
降级分支下所有行的 `problemStart` 都是循环外同一个 `now`，撞上 §7.1 的部分唯一索引后
`ON CONFLICT DO NOTHING` 会静默吞行 → 「入库行数 == zabbixTriggerLimit」会假红。
（若那批行都带 lastchange，则同 trigger 同 lastchange 也会撞 —— 同样必须互不相同。）

**日志断言要自己管 `log.SetOutput`**：范式在 `upsert_test.go:494-497`
（`oldOut := log.Default().Writer()` + `t.Cleanup`）。不复原会污染同进程后续用例
（`:490` 那条依赖 `buf.String()`）。新文件**不要**用 `t.Parallel`。

### 7.4 真 PG 冒烟（`db_smoke_test.go`）

| 新用例 | 挂哪条路径 | 覆盖 |
|---|---|---|
| `TestDBSmoke_AlertsZabbixIdentityUnique` | `:198`（全新） | ① 索引存在 + `UNIQUE` + 含 `WHERE` + 三段谓词逐段断言；② 两条同 `(trigger_id, problem_start)` 的 zabbix 行 → 第二条 23505；③a 同键但 `source='manual'` → **不冲突**；③b 同键但 `trigger_id=''` → **不冲突**；④ 同 trigger 但 `problem_start` 为 **NULL** 的两行 → **不冲突** |
| `TestDBSmoke_ZabbixSyncOnConflict` | `:198`（全新） | **走真调用点**（假 Zabbix server → 真 `SyncFromZabbix` → 真 PG）：注入式造「预过滤后漏进冲突行」的混合批（**1 冲突 + 1 新**）→ 断言无错、`synced==1`、表内 2 行 |
| `TestDBSmoke_Migration027BlockedByDuplicates` | `:205`（升级） | 滚掉 000027 → 造两条同键 zabbix 行 → `Up` 必须失败且异常文本含样本键 |
| `TestDBSmoke_Migration027AllowsNullProblemStart` | `:205`（升级） | **反向对照**（§8 的 M9）：滚掉 000027 → 造两条同 trigger、`problem_start` 全 NULL 的行 → `Up` 必须**成功**且索引建出来 |

**① 断言的是 PG 规范化后的文本，不是迁移源码字面**（实测 `pg_indexes.indexdef`）：
`WHERE (((source)::text = 'zabbix'::text) AND (trigger_id IS NOT NULL) AND ((trigger_id)::text <> ''::text))`。
照抄源码里的 `source = 'zabbix'` 会**永远红**（红在断言写法上，不是红在漂移上）。

**混合批的规模**：落地时收成 **1 冲突 + 1 新**（计划里写的是 1+2）。理由：多一条新行不增加
证伪力 —— 删掉 `TargetWhere` 时**第 1 条**就 42P10 整批失败，规模只影响「红得多快」。

**关于 `ZabbixSyncOnConflict` 的形态**：**不要**写成「连跑两次、第二次 `synced==0`」——
那个 0 由**预过滤**产生（`toInsert` 为空 → 事务根本不进），删掉 `TargetWhere` 它**照样绿**
（`upsert_test.go:567-572` 记着这条教训：「本条依然全绿（假绿）」）。
真正能钉住 ON CONFLICT 的只有「批次里含冲突行」这条路径，照
`TestSyncFromGLPI_预查后漏进冲突行仍幂等`（`upsert_test.go:801-834`）的
`sync.Once` + `db.Callback().Query().After("gorm:query")` 注入法搬过来。

**④ 是「不要手写 INSERT」那条规矩的例外**：`models.Alert.ProblemStart` 是非指针
`time.Time`，`db.Create` 写的是零值哨兵 `0001-01-01`（**非 NULL**）→ 两行都落在索引谓词内
→ 第二条直接 23505，用例在错误方向变红。该格必须用一次**定向**裸 SQL 写入 NULL
（`INSERT INTO alerts(...) VALUES (..., NULL)`）—— 只写一格，不抄整张列清单。

**其余**：真库用例一律走 `models.Alert` + `db.Create`（M26 实测教训：手抄列清单 =
给自己埋一次 schema 漂移）。

### 7.5 Down 链（**加 000027 会让现有用例真红，必须一起改**）

`migrate.Down` 只回滚**最新已应用版本**（`internal/migrate/migrate.go:239-287`）。加了 000027 后：

| 位置 | 现值 | 问题 / 改为 |
|---|---|---|
| `:1194`（`TestDBSmoke_Migration026BlockedByDuplicates`） | `require.NoError(t, migrate.Down(db), "回滚 000026 失败")` 后断言 `uq_tickets_glpi_external_id` 消失 | **必红**：Down 现在滚的是 000027，000026 的索引还在 → `:1199` 的 `require.True(idxGone)` 失败。**改为版本无关**（见下） |
| `:1314-1315` | 「当前最高版本是 000026（000022 空缺）… 故**十三次** Down」 | 改 `000027`；**十四次** Down = 27 → 26 → 25 → 24 → 23 → 21 → 20 → 19 → 18 → 17 → 16 → 15 → 14 → 13 |
| `:1347` 前置 3 | **四条** Fatal 钉 `000026/25/24/23` 是「下一次 Down 的对象」（`:1350` 起 `has26, has25, has24, has23`；Fatal 在 `:1353`/`:1358`/`:1363`/`:1368`） | **新增 `has27` 一条在最前**（000027 必须是下一次 Down 的对象），原四条文案里的序数各 +1 |
| 第一个 Down 块（`:1374` 起，滚 000026 + 断言 glpi 索引消失） | — | **在其前新增**一块：滚 000027 + 断言 `uq_alerts_zabbix_identity` 消失 + 断言 `idx_alerts_trigger_id` **仍在**（照 `:1385-1390` 对 000019 的写法）；原 000026 块顺延为第二块 |
| 后续各 Down 块的序数文案（「第二次」「第三次」…） | — | 各 +1 |
| `:1498`「**十三次** Down 会一路全绿」、`:1504`「十三次 Down 应止步于 000013」 | — | 均改**十四次**（**不是** `:1274`/`:1280` —— 那两行落在 `TestDBSmoke_GLPITimeZoneWallClock` 的 httptest 处理器里，照着改会改坏另一个用例） |

**`:1194` 的版本无关改法**：

```go
	// 把 26 以上的层全部滚掉 —— 写成循环而不是「多写一次 Down」：
	// 每新增一个迁移本用例就要再改一次，而漏改的症状是 require.True(idxGone) 变红，
	// 错误信息指向「down 000026 没删掉索引」这个**错误方向**。
	for {
		var above int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version > 26`).
			Scan(&above).Error)
		if above == 0 {
			break
		}
		require.NoError(t, migrate.Down(db), "回滚 26 以上的迁移失败")
	}
	require.NoError(t, migrate.Down(db), "回滚 000026 失败")
```

`defer migrate.Up(db)` 不用改：`Up` 会把所有未应用层一次补齐（含 000027）。

**`TestDBSmoke_DownPreservesLegacyColumns` 本身的链不做版本无关化**：它的用途就是**逐层**
断言每层 down 的产物，递推是固有的。版本无关化（拿 `max(version)` 与 `migrate.FS` 里的最大值
比对）登记为**候选改进**，不在本轮做 —— 那是重构一个已经写得很紧的用例，超出 M27 范围。

## 8. 门禁序列（照 M26，逐条真跑）

```bash
export PATH=$PATH:/usr/local/go/bin
cd /root/work/itmanager/backend
gofmt -l ./internal ./cmd ./tests     # 必须空
go vet ./...
go test ./... -count=1                # 全绿
go build ./...

cd /root/work/itmanager && ./scripts/db_smoke.sh

cd frontend
npx tsc --noEmit && npx eslint src --ext .ts,.tsx && npx vitest run
```

M27 **不改 openapi** → 无需 `gen:api`；但须确认 `git status` 无生成物漂移。

**变异反证**（每条都必须红在**断言**上，不是编译失败 —— T-47）：

| # | 变异 | 应红在哪 |
|---|---|---|
| M1 | usable 分支改查 `open[t.TriggerID]` | **第 3、4 行**（resolved 行不在 `open` → 插 1，期望 0；旧 problem 行在 `open` → 0，期望 1）。**第 1、2 行在该变异下是绿的**，它们不是这条变异的守门人 |
| M2 | `open` 的构造 `e.Status != "resolved"` → `e.Status == "problem"` | 第 5 行（降级 + ack 行） |
| M3 | `alertIdentityKey` 的 `Unix()` → `Format("2006-01-02")` | 第 4 行 —— **前提是第 4 行的两个 lastchange 同日不同秒**（§7.3） |
| M4 | 删掉 `TargetWhere` | sqlite 基座即红（实测报错 `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`），不必等真 PG |
| M5 | — | **已删除**：`res.RowsAffected` 在 `Alert` 路径上实测与真实插入数一致，该变异**不会红在任何断言上**（不可证伪）。计数法的价值由 §7.4 的注入式用例覆盖，不由变异覆盖 |
| M6 | `limit` 的 `+1` 去掉 | 截断单测②（恰好等于上限 → 误判为截断） |
| M7 | `selectItems` 加回来 | 截断单测④ |
| M8 | 前端文案改成插值 `${truncated}` | `Settings.test.tsx` 的「不含『另有 1 条』」断言 |
| M9 | 迁移自检的 `problem_start IS NOT NULL` 删掉 | **需要用 NULL 行的那个格**（§7.4 ④ 的裸 SQL 写法）：两行同 trigger、`problem_start` 全 NULL，断言 `migrate.Up` **成功**。只造非 NULL 重复行的话，删不删 `IS NOT NULL` 都会抛异常，M9 测不出来 |

## 9. 复审记录（三路细节审查结论与处置）

审于 2026-09-11。E＝可编译性/GORM/真 PG，F＝测试可行性，G＝一致性。
（与需求 §8 的「细节文档审查补记」同源，此处按细节文档的改动面重列。）

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| E-B1 | E | **阻断** `strconv` 未 import → `undefined: strconv` | 已加进 §0 硬约束 + §3.1 diff |
| G-B1 | G | **阻断** 需求 §2.3 的 `TargetWhere` 漏 `source` → 42P10（同节索引谓词有它） | 已改需求文档；§3.5 本就正确 |
| G-B2 | G | **阻断** §7.5 的 `:1274`/`:1280` 行号错位，会引到 `TestDBSmoke_GLPITimeZoneWallClock` 里改坏代码 | 已改 `:1498`/`:1504` 并加警语 |
| E-I2/F-F2 | E+F | **重要** 「RowsAffected 虚报」对 `Alert` 不成立（两人各自探针实测一致） | 已改论据（§0 #5、§3.5）；M5 已删 |
| F-F3 | F | **重要** M9 的两个「应红点」都红不了 | 已在 §8 给出唯一可证伪的形态 |
| F-F1/E-I1 | F+E | **重要** §7.4 ④ 用 `models.Alert` 造不出 NULL | 已在 §7.4 定为「不手写 INSERT」的例外 |
| E-I4 | E | **重要** 工作区有未跟踪探针 `zz_tmp_probe_test.go`，`git add -A` 会提交进去 | 已删除（`git status` 干净） |
| E-I3 | E | **重要** 自检只讲了 NULL 那一半，零值行会命中自检（预期行为） | 已写进 §2.1 注释与「怎么办」段 |
| F-F4 | F | 重要 M1 的应红行写错 | 已改（第 3、4 行） |
| F-F5 | F | 重要 M3 需「同日不同秒」前提 | 已写进 §7.2 表第 4 行 + §8 M3 |
| F-F6 | F | 重要 `fakeZabbixServer` 表达力不足（写死单行/单 lastchange/单次短读） | 已在 §7.2 末 + §7.3 要求参数化 fake |
| F-F7 | F | 重要 `ZabbixSyncOnConflict` 的「第二次 0」是假绿 | 已改为注入式混合批 |
| F-F8 | F | 重要 前端用例缺触发前置（按钮 disabled） | 已加 §6.1 |
| F-F9/G-I3/G-I4 | F+G | 重要 §7.5 多处行号失准（`:1362`→`:1374`、`:1372-1378`→`:1385-1390`、前置 3 是**四条**不是三条） | 已逐处更正 |
| F-F10 | F | 提示 `params["limit"]` 是 `float64` | 已加 `assert.EqualValues` |
| F-F11 | F | 提示 截断 fixture 的 triggerid 必须互不相同 | 已加「fixture 硬约束」 |
| F-F12 | F | 提示 §1「7 处调用点」对不上（该文件 5 处） | 已改 |
| F-F13 | F | 提示 日志断言要管 `log.SetOutput` | 已加 |
| F-F14/E-T | E | 提示 §3.3「NULL 被 GORM 扫成零值」的机理表述含糊 | 已简化为「NULL 被扫成零值、两者不可区分」（结论不变，不再声称机理） |
| G-I1 | G | 重要 需求 §2.3 的 COUNT 片段漏 `source` | 已改需求文档，§3.5 一致 |
| G-I2 | G | 重要 需求 §1.3 的 `git show HEAD~` 取错层（grep 得 1 非 0） | 已改 `HEAD~2` |
| G-T1..T9 | G | 提示 多处小行号（`:96-104`→`:94-99`、`:275`→`:277`、`:520-523`→`:519-523`、`zabbixAuthTTL` 不是 per-request-limit 先例、`Promise<any>` 措辞等） | 已逐处更正 |
| E-T1 | E | 提示 `:414` 是 `=` 不是 `:=` | 已写进 §7.2 表格 |

**已核实无误**（记录以免后人重复怀疑）：GORM v1.30.0 对两列 + 部分索引的渲染
（单 `clause.Expr` 不会被包成 `WHERE (...)`，谓词逐字相同 → 不 42P10）；命名返回值 + 闭包
赋值可编译；调用点全集 = `integration_handler.go:65` + `service.go:384` +
`upsert_test.go:403/414/469/503/554`；`sync_log_redact_test.go:40` 只调 `SyncAll` 不受影响；
`clause` 确已在 `service.go:11` import；`string_agg` 无 NULL 传播；`problem_start TIMESTAMP`
可空、`alerts` 非 hypertable、无既存唯一索引；`migrate.go` 的 `splitStatements` 能正确处理
`$$ ... $$`，`//go:embed all:migrations` 会自动收进新文件；`000022` 空缺不影响新版本身份
（14 次 Down 正确）；sqlite 对 conflict target 的匹配是解析树比较（比「逐字」宽松，
方向安全）；§7.2 三条「不动」用例逐条代入推演成立；升级路径无存量 alerts 行，新用例自造数据即可。

## 10. 落地记录（as-built，2026-09-11）

**提交序列**（每步独立 commit + push，按「小步前进」）：

| 步骤 | commit | 内容 |
|---|---|---|
| 2 | `2bd5e7d` | 需求/细节文档定稿 + 审查补记 |
| 3 | `d38e581` | 迁移 000027（up/down）+ 测试基座索引 + Down 链改造 |
| 4 | `f6d31c5` | A：去重键 `(trigger_id, problem_start)` + 降级判据 `open` |
| 5 | `b029469` | B：去 `selectItems` + 上限 100→5000（请求 +1）+ 截断透出到 UI |
| 6 | 本文所在提交 | 真 PG 冒烟 4 条 + 变异反证 M1–M9 |

**变异反证的实测结果**（每条都红在**断言**上，无一是编译失败 —— T-47）：

| # | 变异 | 实测 |
|---|---|---|
| M1 | usable 分支改查 `open[t.TriggerID]` | ✅ 第 ③、④ 行红 |
| M2 | `open` 构造 `!= "resolved"` → `== "problem"` | ✅ 第 ⑤ 行红 |
| M3 | `alertIdentityKey` 的 `Unix()` → 日粒度 | ✅ 第 ④ 行红（首次写成 `Format("2006-01-02")` 时**编译失败**，那不是证据，改回 `Unix()/86400` 重跑才作数） |
| M4 | 删掉 `TargetWhere` | ✅ sqlite 基座红（`ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`）；真 PG 冒烟**另行**红在 `SQLSTATE 42P10` |
| M6 | `limit` 的 `+1` 去掉 | ✅ 截断单测②红 |
| M7 | `selectItems` 加回来 | ✅ 截断单测④红 |
| M8 | 前端文案改成插值 `${truncated}` | ✅ `Settings.test.tsx` 的「不含『另有 1 条』」红 |
| M9 | 自检删掉 `problem_start IS NOT NULL` | ✅ `..._AllowsNullProblemStart` 红（`Up` 失败，异常指向自检） |
| S-1 | 删 `TargetWhere` → 真 PG | ✅ `..._ZabbixSyncOnConflict` 红，报错逐字 `there is no unique or exclusion constraint matching the ON CONFLICT specification (SQLSTATE 42P10)` |
| S-2 | 删 000027 的 DO 自检 | ✅ `..._BlockedByDuplicates` 红在**两条**断言（异常退化成裸 23505，样本键丢失） |
| S-3a | 索引谓词去掉 `source = 'zabbix'` | ✅ 红在**两处**：indexdef 断言 + ③a（manual 同键行插不进去） |
| S-3b | 索引谓词去掉 `trigger_id <> ''` | ✅ 红在**两处**：indexdef 断言 + ③b（空 trigger_id 第二行插不进去） |
| S-4 | down.sql 不 `DROP INDEX` | ✅ `..._BlockedByDuplicates` 红在 `require.True(idxGone)`（少了它整条用例会空转） |

**M5 已删除**：`res.RowsAffected` 在 `Alert` 路径上与真实插入数一致（两人各自探针实测），
该变异不会红在任何断言上 —— 不可证伪的变异留在表里只会让人以为它被验过。

**已知的假绿（写进测试注释，不假装守住）**：把 `service.go` 里 `exact` 的**查询**那两行删掉，
六行表**全绿**。第 ①②③ 行走的是整条链（Go 判据 + 索引 + ON CONFLICT），真正的兜底是
000027 的索引 —— 由 `TestDBSmoke_ZabbixSyncOnConflict` 的注入式混合批守。`exact` 的价值是
「少一次注定失败的往返 + 把语义写在代码里」，那部分不可证伪，故不声称。

**覆盖**：`SyncFromZabbix` 89.8% 语句覆盖、`alertIdentityKey` 100%、包 85.2%。
唯一未覆盖块是预过滤的 DB-error 包装 —— 按项目口径（脚本/胶水豁免，核心逻辑 ≥80%）接受，
不为了刷覆盖率去 mock 数据库错误。

**残留（已登记进 TODO）**：`zabbix_truncated` 这个 key 名是前后端**跨语言**约定，
两侧各写一遍字面量，改名会静默失效（前端读不到 → 永远显示「未截断」）。
