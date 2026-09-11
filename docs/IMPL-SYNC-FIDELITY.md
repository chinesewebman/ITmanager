# 可执行细节文档：同步导入保真（M26）

> 状态: **v2 —— 细节审查已跑（3 路），结论已合入**（见 §7 复审记录）
> 日期: 2026-09-11
> 基线: `main` @ a4627a4
> 上游: `docs/FIX-PLAN-SYNC-FIDELITY.md`（需求 v2，已定稿，8 项决策已拍板）
> 本文件只写「**怎么改**」；「为什么」一律回指需求文档，不重复。

---

## 0. 拍板摘要（实现必须遵守的硬约束）

| # | 约束 | 出处 |
|---|---|---|
| D-1 | 越界档位（`status ∉ 1..6`、`priority ∉ 0..6`）→ **跳过该票 + 计数 + 逐条日志**，不兜底 | 需求 §7 |
| D-2 | 导入**不发明时间**：源缺失 → `closed_at`/`resolved_at` = **NULL** | 需求 §2.4 |
| D-3 | GLPI 时区 = **`Asia/Shanghai`**（显式常量，**登记待核对**） | 需求 §2.2 |
| D-4 | 配 `ON CONFLICT ... DoNothing`（`TargetWhere` 必需） | 需求 §2.6 |
| D-5 | fail-closed：直接建索引 + 迁移内前置自检 DO 块；**不在迁移里删数据** | 需求 §2.6 |
| D-6 | `SyncFromGLPI` 返回 `(synced, skipped int, err error)`，`results` 增键 `glpi_skipped` | 需求 §7 |
| D-7 | **不**做 `host_id`/`asset_id` 资产关联 | 需求 §6 |
| D-8 | Zabbix 告警 `created_at` 保持 `now` | 需求 §2.3 |
| **D-9** | **`AssignTicketNumbers` 改间隙容忍**（D-4 的连带修复，见 §3.6） | **本文件新增** |

---

## 1. 改动文件清单

| 文件 | 类型 | 内容 |
|---|---|---|
| `backend/internal/integration/timeparse.go` | **新增** | tri-state 时间解析纯函数 + `glpiLoc` |
| `backend/internal/integration/timeparse_test.go` | **新增** | 表驱动单测（含时区钉住用例） |
| `backend/internal/integration/glpi.go` | 改 | 词表补 `6:"pending"` / `0:"normal"` |
| `backend/internal/integration/service.go` | 改 | `SyncFromZabbix` 写 `ProblemStart`；`SyncFromGLPI` 时间戳 + 越界 + `ON CONFLICT` + 签名；`SyncAll` 适配；三处假注释；import 加 `clause` |
| `backend/internal/models/ticket.go` | 改 | `AssignTicketNumbers` 间隙容忍 + 逃生门占位（D-9） |
| `backend/internal/api/handlers/integration_handler.go` | 改 | `:73-76` 适配 3 返回值 + `glpi_skipped` |
| `backend/internal/integration/upsert_test.go` | 改 | **`upsertTestSchema` 补部分唯一索引（否则新 `ON CONFLICT` 全线报错）**；`:485/:489/:528/:546` 调用点适配；`:50/:351/:458` 假注释；新用例 |
| `backend/internal/models/hooks_test.go` | 改 | `:414-482` 两条既有编号用例核对（预期绿）+ 新用例 T15 |
| `backend/internal/integration/glpi_e2e_test.go` | 改 | `:222` 断言更新 + per-key 对照 |
| `backend/migrations/000026_tickets_glpi_external_id_unique.{up,down}.sql` | **新增** | 自检 DO 块 + 部分唯一索引 |
| `backend/tests/db_smoke_test.go` | 改 | Down 链（`:1113/:1146/:1185/:1200/:1274/:1280/:740`）+ 2 个新用例 |
| `scripts/db_smoke.sh` | 改 | 白名单 `:198` / `:205` 各加新用例名 |
| `docs/FIX-PLAN-SYNC-FIDELITY.md` | 改 | §2.1 签名/标识符与本文件统一（见 §7 复审记录 A4） |
| `frontend/src/pages/Settings.tsx` | 改 | `:191-196` 同步完成提示带上 `glpi_skipped`（D-6 的可见性落到运维真正看的那个位置） |

**实现阶段新增的两处（计划外，均为实测暴露）**：
1. `logTimeFallback` → **`logTimeUnusable`**，日志文案去掉「按回落处理」（见 §7 R3）。
2. `models/ticket.go` 的 `usedTicketLabels` 拆为独立函数（D-9 分配逻辑需要「先建集合再分配」两步）。

---

## 2. 新增 `timeparse.go`

```go
package integration

import (
	"log"
	"strconv"
	"time"
)

// timeParseStatus 时间解析三态。区分「源没给」与「给了但解析不了」是 M26 的核心不变式：
// 导入路径**不发明**时间（docs/FIX-PLAN-SYNC-FIDELITY.md §2.4）。
//
// 不导出：本包自用，无外部消费者；测试文件同包（upsert_test.go / glpi_e2e_test.go
// 都是 package integration），非导出同样可测。
type timeParseStatus int

const (
	timeAbsent  timeParseStatus = iota // 源明确表示"没有这个时间"（""、null、0000-00-00 哨兵）
	timeInvalid                        // 源给了值但解析不了
	timeOK
)

// D-3：GLPI 实例时区**暂不确定**，按仓库 docker-compose.yml:181 的 GLPI 容器 TZ 取
// Asia/Shanghai。待 GLPI 实际部署时核对（登记见需求 §6）。不自动探测、不读服务器本地时区
// —— api 容器 TZ 是 UTC，与 GLPI 不同源（需求 §1.8 低-3）。
const glpiTZName = "Asia/Shanghai"

var glpiLoc = mustLoadLocation(glpiTZName)

// mustLoadLocation 失败即 panic（init 期，进程起不来）。
// 不静默降级成 UTC：降级正好把「8 小时偏移」这个本函数存在的唯一理由引回来，
// 而且是最难发现的那种（数据看着有值、只是全错 8 小时）。
//
// 不内嵌 time/tzdata：backend/Dockerfile:24 已 `apk add --no-cache … tzdata`，
// 运行时镜像自带时区库。（若哪天换成 scratch/distroless 基础镜像，这里的 panic
// 会立刻暴露，是响的失败而非静默错值。）
func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("加载 GLPI 时区 " + name + " 失败: " + err.Error())
	}
	return loc
}

// glpiLayouts GLPI REST v1 的挂钟格式，由长到短尝试。
// 实测：Go 会静默接受并截断尾随小数秒（"…10:00:00.123" 可解析），故无需单列毫秒格式。
var glpiLayouts = []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

// glpiZeroDates MySQL/GLPI 表示"未设置"的哨兵值。
// 实测 "0000-00-00 00:00:00" / "0000-00-00" 会被上面三种 layout **全部拒绝**
// （month out of range），不显式识别就会落到 timeInvalid —— 但语义上它是"没有"。
var glpiZeroDates = map[string]struct{}{
	"0000-00-00 00:00:00": {},
	"0000-00-00":          {},
}

// parseUnixSeconds 解析 Zabbix 的 Unix 秒字符串（lastchange）。
func parseUnixSeconds(raw string) (time.Time, timeParseStatus) {
	if raw == "" {
		return time.Time{}, timeAbsent
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, timeInvalid
	}
	return time.Unix(sec, 0).UTC(), timeOK
}

// parseGLPITime 解析 GLPI 的挂钟时间字符串：按包级 glpiLoc 解释，再转 UTC 返回。
//
// 为什么必须 .UTC()：pgx 对 TIMESTAMP（无时区）列会丢弃 Location、只写挂钟数字
// （pgtype/timestamp.go 的 discardTimeZone，binary/text 两条路径都走它）。
//   按 UTC 直接解析：10:00 → 落库 "10:00" → 读回 10:00Z → dayjs 北京渲染 18:00 ❌
//   按 glpiLoc 再 .UTC()：10:00 → 落库 "02:00" → 读回 02:00Z → 渲染 10:00 ✅
func parseGLPITime(raw string) (time.Time, timeParseStatus) {
	if raw == "" {
		return time.Time{}, timeAbsent
	}
	if _, ok := glpiZeroDates[raw]; ok {
		return time.Time{}, timeAbsent
	}
	for _, layout := range glpiLayouts {
		if t, err := time.ParseInLocation(layout, raw, glpiLoc); err == nil {
			return t.UTC(), timeOK
		}
	}
	return time.Time{}, timeInvalid
}

// logTimeUnusable 时间源不可用时的统一日志。
//
// **只说事实，不说处置**：处置按字段而异 —— created_at 是 NOT NULL 非指针列，只能回落 now；
// resolved_at/closed_at 是 D-2 明确要求留 NULL 的。早先这里统一写成「按回落处理」，
// 于是在留 NULL 的分支上撒谎，正好把排查「closed_at 为什么是空」的人引向错误方向。
// 处置属于调用点（代码在那里），日志只负责让「源不可用」这件事可见。
//
// st 的取值见 timeParseStatus：0=源缺失 1=解析失败。
func logTimeUnusable(what, id, field, raw string, st timeParseStatus) {
	log.Printf("M26: %s %s 的 %s=%q 不可用（status=%d）", what, id, field, raw, st)
}
```

**注意 `log` 的用法**：包内既有代码用标准库 `log`（如 `service.go:148/211/274`），保持一致。

---

## 3. 逐文件改动

### 3.1 `glpi.go`：词表补全

```go
 func (t *GLPITicket) ConvertToTicket() *LocalTicket {
-	statusMap := map[int]string{1: "open", 2: "in_progress", 3: "pending", 4: "resolved", 5: "closed"}
+	// M26/D-1：补 6（GLPI「待批准」）。取值必须在 openapi Ticket.status 的 enum 内
+	// （open/in_progress/pending/resolved/closed）—— GLPI 6 无独立的本地语义，
+	// 归入 pending（等待批准 == 等待）。
+	statusMap := map[int]string{1: "open", 2: "in_progress", 3: "pending", 4: "resolved", 5: "closed", 6: "pending"}
 	// M16：取值必须是 openapi Ticket.priority 的词表（low/normal/high/critical）。
 	...
-	priorityMap := map[int]string{1: "low", 2: "low", 3: "normal", 4: "high", 5: "critical", 6: "critical"}
+	// M26/D-1：补 0（GLPI「未指定优先级」）。归入 normal —— 与表单默认值一致。
+	// 越界（∉ 0..6）**不在此处兜底**，由调用方按 D-1 跳过该票（判据是 _, ok := map[v]）。
+	priorityMap := map[int]string{0: "normal", 1: "low", 2: "low", 3: "normal", 4: "high", 5: "critical", 6: "critical"}
```

`GetStatusName`（中文名表，`:148`）**不动** —— 它已含 `6: "待批准"`，且与契约词表是两张表（前者给人看、后者落库）。

### 3.2 `service.go`：`SyncFromZabbix` 写 `ProblemStart`

```go
 		alert := t.ConvertToAlert()
+		problemStart, st := parseUnixSeconds(t.LastChange)
+		if st != timeOK {
+			logTimeUnusable("Zabbix trigger", t.TriggerID, "lastchange", t.LastChange, st)
+			problemStart = now // 回落同步时刻；D-8：created_at 仍保持 now，两列语义不同
+		}
 		toInsert = append(toInsert, models.Alert{
 			TriggerID:    t.TriggerID,
 			HostName:     alert.HostName,
 			TriggerName:  alert.TriggerName,
 			Problem:      alert.Problem,
 			Severity:     alert.Severity,
 			SeverityName: alert.SeverityName,
+			ProblemStart: problemStart,
 			Status:       "problem",
 			Source:       "zabbix",
 			CreatedAt:    now,
 			UpdatedAt:    now,
 		})
```

`:204-206` 的注释（「不加 ON CONFLICT …… alerts.trigger_id 上没有唯一索引」）**仍然为真**，不动。

### 3.3 `service.go`：`SyncFromGLPI` 重写核心循环

**签名变更**（D-6）：

```go
-func (s *IntegrationService) SyncFromGLPI(ctx context.Context) (int, error) {
+func (s *IntegrationService) SyncFromGLPI(ctx context.Context) (synced, skipped int, err error) {
```

**主循环**（`skipped` 是命名返回值，**不要**用 `:=` —— 同块重声明编译不过，见 §7 E2b）：

```go
 	toUpsert := make([]models.Ticket, 0, len(tickets))
 	now := time.Now()
-	for _, t := range tickets {
+	for _, t := range tickets {
 		local := t.ConvertToTicket()
 		if _, ok := existingSet[local.ExternalID]; ok {
-			continue // 工单状态走 PATCH 更新，不在同步阶段覆盖
+			// 已存在：按 ADR-0004，ITmanager 是工单 SoT、GLPI 降级为可选只读参考，
+			// 不在同步阶段覆盖本地状态（这是设计，不是缺口）。全仓无 PATCH 路由。
+			continue
 		}
+		// D-1：越界档位不兜底、不静默改写 —— 跳过并计数，由调用方暴露给运维。
+		// 判据是词表命中（ConvertToTicket 越界时留空串），不是数值范围硬编码。
+		if local.Status == "" || local.Priority == "" {
+			skipped++
+			log.Printf("M26: GLPI 工单 %s 档位越界（status=%d priority=%d），已跳过",
+				local.ExternalID, t.Status, t.Priority)
+			continue
+		}
+		created, st := parseGLPITime(local.CreatedAt)
+		if st != timeOK {
+			logTimeUnusable("GLPI 工单", local.ExternalID, "date", local.CreatedAt, st)
+			created = now
+		}
-		toUpsert = append(toUpsert, models.Ticket{
+		nt := models.Ticket{
 			ExternalID:  local.ExternalID,
 			Title:       local.Title,
 			Description: local.Description,
 			Status:      local.Status,
 			Priority:    local.Priority,
 			TicketType:  local.TicketType,
 			Source:      "glpi",
-			CreatedAt:   now,
+			CreatedAt:   created,
 			UpdatedAt:   now,
-		})
+		}
+		// D-2：导入不发明时间。源缺失/解析失败 → 保持 NULL。
+		// 补 now 会伪造：diagnostic_service.go:158/:166 只看指针非空、不看 status，
+		// 会凭空长出一条「工单解决/关闭」时间线事件，并污染 dashboard SLA 窗口。
+		if local.Status == "resolved" || local.Status == "closed" {
+			if v, st := parseGLPITime(local.ResolvedAt); st == timeOK {
+				nt.ResolvedAt = &v
+			} else {
+				logTimeUnusable("GLPI 工单", local.ExternalID, "solvedate", local.ResolvedAt, st)
+			}
+		}
+		if local.Status == "closed" {
+			if v, st := parseGLPITime(local.ClosedAt); st == timeOK {
+				nt.ClosedAt = &v
+			} else {
+				logTimeUnusable("GLPI 工单", local.ExternalID, "closedate", local.ClosedAt, st)
+			}
+		}
+		toUpsert = append(toUpsert, nt)
 	}
 
 	if len(toUpsert) == 0 {
-		return 0, nil
+		return 0, skipped, nil
 	}
```

> `&v` 安全性：`v` 由 `if` 语句的短变量声明引入，**每次执行该语句都新建变量**，与循环变量语义无关；`&v` 逃逸到堆、逐迭代独立。（已核，见 §7 E6。）

**插入段**（D-4）：

```go
-	// 不加 ON CONFLICT：工单已存在时上面已跳过（状态更新走 PATCH），且 tickets.external_id
-	// 上没有唯一索引 —— 加了只会在真 PG 上 42P10 失败（见 docs/FIX-PLAN-NETBOX-UPSERT.md §1.2）。
-	if err := database.DB.WithContext(ctx).
-		CreateInBatches(toUpsert, 100).Error; err != nil {
-		return 0, fmt.Errorf("GLPI 批量插入失败: %w", err)
-	}
-	log.Printf("从 GLPI 同步了 %d 个工单", len(toUpsert))
-	return len(toUpsert), nil
+	// M26/D-4：迁移 000026 建了部分唯一索引 uq_tickets_glpi_external_id
+	// （WHERE source='glpi' AND external_id <> ''）。existingSet 预过滤是 TOCTOU，
+	// 并发同步会漏进重复行；CreateInBatches 又是**整批原子**的（gorm finisher_api.go），
+	// 撞索引会让整批新票一起回滚。故配 ON CONFLICT 把「硬失败」变成「幂等跳过」。
+	// 谓词必须与索引一致 —— 不带 TargetWhere 的 ON CONFLICT (external_id) 在部分索引下 42P10。
+	//
+	// synced **不能**用 res.RowsAffected：Ticket.ID 带 default:gen_random_uuid()，
+	// gorm 会自动追加 RETURNING "id" → create 回调走 QueryContext + gorm.Scan，
+	// 而 scan.go 的 slice 分支拿 RowsAffected 当下标，导致
+	// 「只要有 ≥1 行插入，RowsAffected 就等于 len(batch)」—— 实测 v1.30，PG 与 sqlite 皆然：
+	//   1 冲突 + 2 新 → 实际插 2，RowsAffected = 3；101 行（1 冲突）→ 实际 100，RowsAffected = 101。
+	// 恰好就在 D-4 存在的那个场景虚报。改用同事务内 COUNT 前后差（真 PG 实测 synced=2）。
+	// 已排除的替代：db.Omit("RETURNING") 无效（SQL 里 RETURNING 还在）；
+	// clause.Returning{} 空列会 panic（scan.go 对不可寻址 slice 做 SetLen）。
+	if err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
+		extIDs := make([]string, 0, len(toUpsert))
+		for i := range toUpsert {
+			extIDs = append(extIDs, toUpsert[i].ExternalID)
+		}
+		var before, after int64
+		if err := tx.Model(&models.Ticket{}).
+			Where("source = ? AND external_id IN ?", "glpi", extIDs).Count(&before).Error; err != nil {
+			return err
+		}
+		if err := tx.Clauses(clause.OnConflict{
+			Columns:     []clause.Column{{Name: "external_id"}},
+			TargetWhere: clause.Where{Exprs: []clause.Expression{
+				clause.Expr{SQL: "source = 'glpi' AND external_id <> ''"},
+			}},
+			DoNothing: true,
+		}).CreateInBatches(toUpsert, 100).Error; err != nil {
+			return err
+		}
+		if err := tx.Model(&models.Ticket{}).
+			Where("source = ? AND external_id IN ?", "glpi", extIDs).Count(&after).Error; err != nil {
+			return err
+		}
+		synced = int(after - before)
+		return nil
+	}); err != nil {
+		return 0, skipped, fmt.Errorf("GLPI 批量插入失败: %w", err)
+	}
+	log.Printf("从 GLPI 同步了 %d 个工单（跳过越界 %d 条）", synced, skipped)
+	return synced, skipped, nil
```

**新增 import**：`"gorm.io/gorm/clause"`。（`gorm.io/gorm` 已在 `service.go:10`，不必加。）

> `AssignTicketNumbers(database.DB…)` 调用（`:266`）**留在事务外、位置不变** —— 它只读当天已用标签，与插入非同一致点；放进去反而扩大事务窗口。
> gorm 渲染形态（真 PG 实测）：`ON CONFLICT ("external_id")  WHERE source = 'glpi' AND external_id <> '' DO NOTHING RETURNING "id"` —— 语义与预期一致，只有引号/空格的字面差异。

### 3.4 `service.go`：`SyncAll` 适配

```go
-	if n, err := s.SyncFromGLPI(ctx); err != nil {
+	if n, skip, err := s.SyncFromGLPI(ctx); err != nil {
 		log.Printf("GLPI 同步失败: %v", err)
 		errs = append(errs, fmt.Errorf("glpi: %w", err))
 	} else {
 		results["glpi"] = n
+		results["glpi_skipped"] = skip
 	}
```

### 3.5 `integration_handler.go:73-76`：精确 diff

现文（已核）：

```go
	case "glpi":
		count, e := h.svc.SyncFromGLPI(ctx)
		results = map[string]int{"glpi": count}
		err = e
```

改为：

```diff
 	case "glpi":
-		count, e := h.svc.SyncFromGLPI(ctx)
-		results = map[string]int{"glpi": count}
+		count, skipped, e := h.svc.SyncFromGLPI(ctx)
+		results = map[string]int{"glpi": count, "glpi_skipped": skipped}
 		err = e
```

**只增不改**：该端点在 `openapi.yaml` 里无定义 → 不触发 `gen:api` 漂移。`integration_handler_test.go:214/:240` 用 `svc=nil` + recover 抓 panic，改完仍 panic，不受影响。

### 3.6 `models/ticket.go`：`AssignTicketNumbers` 间隙容忍（**D-9**）

**为什么必须连带改**（**注意：不是「删工单会留空洞」—— 那条路径在生产不可达**，见 §7 G）：

```
D-4 的 DoNothing 在「预查之后、插入之前」有并发同步插了同一 external_id 时会跳过该行
→ 行的号已分配但未落库 → 当天编号出现空洞
→ 实际入库 B,C，当天已用 = {B,C}，条数 = 2
→ 下次同步 nextTicketSeq 返回 2 → seqLabel(2) = "C" → 撞已有的 C
→ idx_tickets_ticket_number 拒绝 → 同步 500，
  且当天后续每次同步都失败（跨天自愈，运维无自助恢复手段）
```

触发面很窄（TOCTOU 竞态），但失败模式很差：**不是丢一张票，是当天 GLPI 同步整体停摆**。修法是让分配**跳过已占用的标签**，而不是依赖连续性：

```go
-func AssignTicketNumbers(db *gorm.DB, tickets []Ticket) {
-	prefix := ticketNumberPrefix()
-	next := nextTicketSeq(db, prefix)
-	for i := range tickets {
-		if tickets[i].TicketNumber != "" {
-			continue
-		}
-		tickets[i].TicketNumber = prefix + seqLabel(next)
-		next++
-	}
-}
+// 已在 tickets[i].TicketNumber 里填了号的行走 BeforeCreate 的逃生门
+// （TicketNumber != "" 时不覆盖），原样保留、不占用本批的自动序号。
+//
+// M26/D-9：按「当天已占用的标签集合」分配并跳过空洞，而不是按条数线性推进。
+// 条数法只在编号连续时成立 —— M26 的 ON CONFLICT DoNothing 跳过冲突行会留空洞，
+// 使条数小于最大序号，回绕后撞上已占用的号（见 docs/IMPL-SYNC-FIDELITY.md §3.6）。
+//
+// 边界：本函数**不解决并发**。两个同步各自读到同一份 used 快照仍会撞号
+// （需求 §1.5 已登记为超范围）。T15 只守空洞，不守并发。
+func AssignTicketNumbers(db *gorm.DB, tickets []Ticket) {
+	prefix := ticketNumberPrefix()
+	used := usedTicketLabels(db, prefix)
+	// 逃生门行也要占位：否则同批内「已填号 = TICKET-今天-A」与「自动分配」会各自拿到 A，
+	// 整批被 ticket_number 唯一索引拒绝。旧法同样有此洞，顺手关掉。
+	for i := range tickets {
+		if n := tickets[i].TicketNumber; n != "" {
+			used[n] = struct{}{}
+		}
+	}
+	next := int64(0)
+	for i := range tickets {
+		if tickets[i].TicketNumber != "" {
+			continue
+		}
+		for {
+			label := prefix + seqLabel(next)
+			next++
+			if _, taken := used[label]; !taken {
+				used[label] = struct{}{}
+				tickets[i].TicketNumber = label
+				break
+			}
+		}
+	}
+}
+
+// usedTicketLabels 取当天已占用的工单号集合。
+func usedTicketLabels(db *gorm.DB, prefix string) map[string]struct{} {
+	var nums []string
+	db.Model(&Ticket{}).Where("ticket_number LIKE ?", prefix+"%").Pluck("ticket_number", &nums)
+	used := make(map[string]struct{}, len(nums))
+	for _, n := range nums {
+		used[n] = struct{}{}
+	}
+	return used
+}
```

**不死循环**：`used` 有限、`next` 单调增、`seqLabel` 无界 → 必然终止。签名与调用点（`service.go:266`）不变。

**`nextTicketSeq`（`:94-99`）保持原样不动** —— 它仍被 `generateTicketNumber`（逐条 Create 路径）使用。逐条路径靠 `TicketService.Create` 的冲突重试兜底；且既有用例 `internal/service/ticket_from_alert_test.go:200-210` 把该路径的间隙当作失败注入用，改它会连带打红。**登记为已知残留**（见 §7 G-c）。

**连带核对**（预期全绿，逻辑已走查）：
- `hooks_test.go:440` 批量预分配：空库 → A,B,C；第二批 → used={A,B,C} → 跳过至 D ✅
- `hooks_test.go:471` 已填号不覆盖：`GLPI-42` 占位不影响 `A`（`GLPI-42` ≠ prefix+A）✅

### 3.7 注释纠正（三处假注释）

| 位置 | 现文 | 改为 |
|---|---|---|
| `service.go:245` | `// 工单状态走 PATCH 更新，不在同步阶段覆盖` | 见 §3.3（全仓无 PATCH 路由，是 ADR-0004 的只读设计，不是缺口） |
| `service.go:268-269` | `// …tickets.external_id 上没有唯一索引 —— 加了只会在真 PG 上 42P10 失败` | 「`external_id` 上已有部分唯一索引（`source='glpi' AND external_id<>''`）；**不带** `TargetWhere` 的 `ON CONFLICT (external_id)` 才 42P10」 |
| `upsert_test.go:50/:351/:458` | `alerts.trigger_id / tickets.external_id **没有**唯一索引 —— 生产也没有` | 「`tickets.external_id` 只有**部分**唯一索引（`source='glpi' AND external_id<>''`）。带匹配 `TargetWhere` 的 `ON CONFLICT (external_id)` 可仲裁；**不带** `TargetWhere` 才 42P10 —— 故本用例的失败模式不变」 |

> 第二行与第三行必须写成同一个事实：**配了匹配 `TargetWhere` 就合法**。原稿把方向写反了（见 §7 EB）。

### 3.8 迁移 `000026`

`backend/migrations/000026_tickets_glpi_external_id_unique.up.sql`：

```sql
-- M26/D-5：ADR-0004 Risk-3 承诺的 external_id 幂等索引，从未落地。
--
-- 保留 000019 的普通索引 idx_tickets_external_id：它服务于
-- `WHERE external_id IN (...)` 的同步预查（W6/P16 性能修复），而该查询既推不出
-- external_id <> '' 也推不出 source='glpi'，部分索引的两段谓词都不可被蕴含 —— 即
-- **无法替代**。真 PG 18 实测（5 万行 / 200 个 ID 的字面量 IN 列表）：
--   两索引并存          → Bitmap Index Scan on idx_tickets_external_id，buffers hit=91
--   删掉普通索引只留部分 → Seq Scan，Rows Removed by Filter: 49800，buffers hit=375
--
-- 锁语义：CREATE INDEX 取 SHARE 锁、阻塞 tickets 写入；迁移框架在事务内执行
-- （migrate.go execInTx），故不能用 CONCURRENTLY。大表走维护窗口。

-- 前置自检：把「无 DETAIL 的 23505」变成带样本的异常。
-- 迁移 DDL 与版本记录同事务，失败会导致版本不落、每次重启重放、服务持续不可用；
-- 而 PG 默认的 unique index 冲突日志不含是哪几行（见 TODO.md G-22）。
DO $$ DECLARE n int; s text;
BEGIN
  SELECT count(*), string_agg(external_id, ', ') INTO n, s FROM (
    SELECT external_id FROM tickets
    WHERE source = 'glpi' AND external_id <> ''
    GROUP BY external_id HAVING count(*) > 1
    ORDER BY external_id LIMIT 5
  ) x;
  IF n > 0 THEN
    RAISE EXCEPTION '存在重复的 glpi external_id（最多列 5 个）：%，请先人工清理后重跑迁移', s;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_glpi_external_id
    ON tickets(external_id) WHERE source = 'glpi' AND external_id <> '';
```

`.down.sql`：

```sql
DROP INDEX IF EXISTS uq_tickets_glpi_external_id;
```

---

## 4. 测试清单

> 纪律：**变异反证** —— 每条新用例都要先让对应改动「写错」，确认红色集合与预判精确一致。
> 「真实分支」门槛：断言必须走被测代码的真实路径；mock 只用于外部依赖（GLPI HTTP、时钟），不 mock 被测逻辑。

| # | 文件 | 用例 | 断言 | 变异反证 |
|---|---|---|---|---|
| T1 | `timeparse_test.go` | `TestParseUnixSeconds` | `"1750000000"`→OK 且值正确；`""`→Absent；`"abc"`→Invalid | 把 Absent 与 Invalid 合并 → 必红 |
| T2 | `timeparse_test.go` | `TestParseGLPITime_格式` | 三种 layout 各一例 → OK；`"0000-00-00 00:00:00"`/`"0000-00-00"`/`""`→**Absent**；`"not-a-date"`→Invalid | 去掉哨兵分支 → 哨兵用例红（实测三种 layout 全拒哨兵，故哨兵表必需） |
| T3 | `timeparse_test.go` | **`TestParseGLPITime_时区`** | **① `got.Location() == time.UTC`**（钉 `.UTC()`）；② 落库挂钟 `== 02:00`；③ 回转 `glpiLoc` 逐字还原源挂钟（钉时区名）。另加 `TestGLPILocIsUTC8` 钉 `glpiTZName` 偏移 = +08:00 | **去掉 `.UTC()` → ① 必红**；**`glpiLoc` 换成 UTC → ②③ 必红**（R2 的唯一防线） |
| T4 | `timeparse_test.go` | `TestParseGLPITime_边界` | `"2026-06-15 10:00:00.123"`→OK；`"2026-06-15T10:00:00Z"`→**Invalid**（登记 v2 API 形态） | —— |
| T5 | `upsert_test.go` | `TestSyncFromZabbix_ProblemStart来自LastChange` | 带 `lastchange` 的 fixture → 落库 `problem_start` == 该时刻（**不是** `now`） | `problemStart = now` 写死 → 必红 |
| T6 | `upsert_test.go` | `TestSyncFromZabbix_LastChange缺失回落` | 无 `lastchange` → 回落 `now` 且**日志非静默** | 去掉日志 → 断言日志的用例红 |
| T7 | `upsert_test.go` | `TestSyncFromGLPI_CreatedAt来自GLPI` | 喂 `"2026-09-09 10:00"` → 从库读回后 `.UTC().Format("15:04") == "02:00"`。**用 Format 比较，不用 `assert.Equal(time.Time)`** —— 往返会带 Location 差异。**本用例只守「`CreatedAt` 来自 GLPI 而非 `now`」这条管道；它守不到 `.UTC()`**（原因见下方基座注意），时区那一半由 T18 守 | `CreatedAt: now` → 必红 |
| T8 | `upsert_test.go` | **`TestSyncFromGLPI_ClosedAt不发明`** | status=5 且无 `closedate` → `closed_at IS NULL` | 改成回落 `now` → 必红（**P0-2 的唯一防线**） |
| T9 | `upsert_test.go` | `TestSyncFromGLPI_ClosedAt有值` | status=5 + `closedate` → 非空且 `.UTC().Format(...)` 等于解析值；`resolved_at` 同法 | —— |
| T10 | `upsert_test.go` | `TestSyncFromGLPI_越界跳过并计数` | status=7 → 不入库、`skipped==1`、`synced==N-1` | 改成兜底成 `open` → 必红 |
| T11a | `upsert_test.go` | `TestSyncFromGLPI_两次同步不重复`（既有，**改名/改注释**） | 同一 fixture 喂两遍 → 第二遍 `synced==0`、总行数不变、不报错 | **不守 D-4**：第二遍 `existingSet` 已全剔除 → `len(toUpsert)==0` 提前返回，INSERT 根本不执行。注释须写明它守的是**预过滤**，不是 ON CONFLICT |
| T11b | `upsert_test.go` | **`TestSyncFromGLPI_预查后漏进冲突行仍幂等`** | 注册一次性 `db.Callback().Query().After("gorm:query")` 钩子：预查 `Find` 返回后、插入前，用同一 DB 插一行同 `external_id` 的 glpi 票（bool 守卫只触发一次）。断言：**不报错**、总行数 == 1（预置）+ (N-1)、`synced == N-1` | **删掉 `ON CONFLICT` → 23505 必红**；**改用 `res.RowsAffected` → `synced==N` 必红**（这条同时守 Q2 的计数修正） |
| T12 | `glpi_e2e_test.go` | **改 `:222`** | `priority=0` → `"normal"`（替换原「期望空串」） | —— |
| T13 | `glpi_e2e_test.go` | `TestGLPIE2E_词表per键对照_status` | `statusMap` 的 **1..6 每键**对照 `openapi.yaml:2506` enum 字面量 | 删掉 `6:` 键 → 必红（**值域对照抓不到缺键**，这正是本用例存在的理由） |
| T14 | `glpi_e2e_test.go` | `TestGLPIE2E_词表per键对照_priority` | `priorityMap` 的 **0..6 每键**对照 `openapi.yaml:2503` enum | 删掉 `0:` 键 → 必红 |
| T15 | `models/hooks_test.go` | `TestTicket_AssignTicketNumbers_跳过空洞` | 预置 `{prefix+"A", prefix+"C"}` → `AssignTicketNumbers(db, batch)`，batch 一行 → 结果**确定性为 `prefix+"B"`**（不是「B 或 D」）；插入不报错 | 退回条数法（`nextTicketSeq`）→ 必红（**D-9 的防线**） |
| T15b | `models/hooks_test.go` | `TestTicket_AssignTicketNumbers_逃生门占位` | batch = `[{TicketNumber: prefix+"A"}, {}]`，空库 → 第二行**不得**拿到 `prefix+"A"`；整批 `CreateInBatches` 成功 | 去掉逃生门占位循环 → 唯一索引拒绝整批，必红 |
| T16 | `db_smoke_test.go` | `TestDBSmoke_TicketsGLPIExternalIDUnique` | `pg_indexes` 有 `uq_tickets_glpi_external_id` 且 `indexdef` 含 `WHERE`；插两条同 `external_id` 的 `source='glpi'` → 第二条 23505；`source='manual'` 同 `external_id` → **不冲突** | —— |
| T17 | `db_smoke_test.go` | `TestDBSmoke_Migration026BlockedByDuplicates` | **三步**：① `migrate.Down` 滚掉 000026（索引消失）② 插入两行同 `external_id` 的 glpi 票 ③ `migrate.Up` **必须失败**且异常文本含 `存在重复的 glpi external_id`。收尾：清理重复行并重跑 Up 复原，避免污染同进程后续用例 | 去掉 DO 自检 → 异常文本断言红（PG 原生 23505 不含样本） |

| T18 | `db_smoke_test.go` | **`TestDBSmoke_GLPITimeZoneWallClock`** | 真 PG：`parseGLPITime("2026-06-15 10:00")` 的结果经 gorm 写入 `tickets.created_at`，`SELECT created_at::text` **必须逐字为 `2026-06-15 02:00:00`** | **去掉 `.UTC()` → 落库变成 `10:00:00`，必红**（sqlite 抓不到，见下） |

**测试基座注意（实测，§7 R1）**：

**sqlite 与 pgx 对 `time.Time` 的处理相反，任何 sqlite 用例都无法区分 `.UTC()` 的有无。**

| 写入同一时刻 | pgx（真 PG）落库 | sqlite 落库 | 读回渲染 |
|---|---|---|---|
| `10:00` Location=`Asia/Shanghai` | `10:00:00`（**丢偏移**） | `10:00:00+08:00`（保留偏移） | PG **18:00 ❌** / sqlite 10:00 |
| `02:00` Location=`UTC` | `02:00:00` | `02:00:00+00:00` | 两者均 10:00 ✅ |

原因：pgx 对 `TIMESTAMP` 列**丢弃 Location、只写挂钟数字**（`pgtype/timestamp.go` 的 `discardTimeZone`）；sqlite 驱动把偏移一起写进字符串、读回时按偏移还原。**同一时刻的两种表示 `Equal()` 为 true，差别只在 Location** —— 所以 sqlite 上两种写法都"对"。

因此 `.UTC()` 的防线是**两层、缺一不可**：纯函数层断言 `Location()`（T3①，sqlite 可跑）+ 真 PG 层断言落库字面值（T18）。T7 不承担这一职责。

另：
- T11b 的钩子必须**一次性**（bool 守卫），否则后续 `Count` 查询会反复插入冲突行。
- **`upsertTestSchema`（`upsert_test.go:52-96`）必须补索引**，否则新 `ON CONFLICT` 在 sqlite 上直接报错，`TestSyncFromGLPI_两次同步不重复` / `一次同步多张工单不撞号` 全线红。加进 DDL 常量，并改掉 `:48-51` 那句「与生产一致」的注释：
  ```sql
  CREATE UNIQUE INDEX uq_tickets_glpi_external_id
      ON tickets(external_id) WHERE source = 'glpi' AND external_id <> '';
  ```
  （sqlite 支持部分索引，实测：无索引 → `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`；加索引 → 通过。）

---

## 5. 冒烟白名单与 Down 链

**白名单**（`scripts/db_smoke.sh`，漏加 = CI 静默不跑）：

| 用例 | 加到 | 理由 |
|---|---|---|
| `TestDBSmoke_TicketsGLPIExternalIDUnique` | `:198` 的 `-run` 正则**末尾追加** `\|TestDBSmoke_TicketsGLPIExternalIDUnique` | 验证索引存在 + 部分性（`manual` 不冲突） |
| `TestDBSmoke_GLPITimeZoneWallClock`（T18） | 同上 `:198` 正则**末尾追加** `\|TestDBSmoke_GLPITimeZoneWallClock` | **必须**：sqlite 抓不到 `.UTC()`，这是该修法在真 PG 上的唯一防线 |
| `TestDBSmoke_Migration026BlockedByDuplicates` | `:205` 的 `-run` 正则**末尾追加** `\|TestDBSmoke_Migration026BlockedByDuplicates` | 升级路径专属：存量重复挡住迁移 |

**已核实**：`TestDBSmoke_TicketsExternalIDIndex` **已在** `:198` 白名单内（000019 的普通索引用例），其断言不受本迁移影响，不必改。

**Down 链**（`db_smoke_test.go`，行号已逐个核过）：

| 位置 | 现值 | 改为 |
|---|---|---|
| `:1113-1114` | 「当前最高版本是 000025」「故十二次 Down = 25 → … → 13」 | `000026`；**十三次** Down = `26 → 25 → 24 → 23 → 21 → … → 13` |
| `:1146-1163` 前置 3 | 三条 Fatal 钉 `000025/000024/000023` 是「下一次 Down 的对象」 | 改为 `000026/000025/000024`（000026 必须是下一次 Down 的对象），文案同步 |
| `:1168-1178` | 第一个 Down 块滚 000025 + 断言 ticket_history 表消失 | **新增**在最前：滚 000026 + 断言 `uq_tickets_glpi_external_id` 消失；原 ticket_history 块顺延为第二个 |
| `:1185` | `"第一次 Down 必须滚掉 000024"` | 「**第二次** Down …」（本来就写歪了，顺手改对） |
| `:1200` | `"第二次 Down 必须滚掉 000023"` | 「**第三次** Down …」（同上） |
| `:1274` | 「**十二次** Down 会一路全绿」 | **十三次** |
| `:1280` | `"十一次 Down 应止步于 000013"` | **十三次**（现值本就比实际少 1，加 000026 后再少 2） |
| `:740` | 「后者第一次 Down 就是回滚 000023」 | 去掉具体版本号（已过期）：改为「后者会把库一路 Down 回 000013」——version-proof |

**实测补充（计划外，但必须记）**：新写 `TestDBSmoke_TicketsGLPIExternalIDUnique` / `_Migration026BlockedByDuplicates` 时，
手写 `INSERT INTO tickets (...) VALUES (...)` **在真库上直接红**：
`null value in column "ticket_type"` / `creator_id` 等 —— 真 `tickets` 表有 `ticket_no`/`ticket_type`/`creator_id`
等 NOT NULL 列，手抄列清单等于给测试自己埋一次 schema 漂移。已改为走 `models.Ticket` + `gorm.Create`
（同时也就是生产建单路径）。**教训：真库用例不要手写 INSERT 列表。**

**已核不会红**（不必改）：`TestDBSmoke_MigrateRunner`（`:97-109`，`len(want)` 自动适配）、`schema_drift_test.go`（不解析 `CREATE UNIQUE INDEX`）、`routes_integration_test.go`（用自己的 `testdata/migrations`）。

---

## 6. 门禁序列

```bash
export PATH=$PATH:/usr/local/go/bin
cd /root/work/itmanager/backend

gofmt -l ./internal ./cmd ./tests     # 必须空
go vet ./...
go test ./... -count=1                 # 全绿
go build ./...

# 真 PG 冒烟（两条路径）
cd /root/work/itmanager && ./scripts/db_smoke.sh

cd frontend
npx tsc --noEmit
npx eslint src --ext .ts,.tsx
npx vitest run
```

M26 **不改 openapi** → 无需跑 `gen:api`；但须确认 `git status` 无生成物漂移。

**实测结果（2026-09-11，全绿）**：

| 门禁 | 结果 |
|---|---|
| `gofmt -l` | 空 |
| `go vet ./...` | 干净 |
| `go build ./...` | 通过 |
| `go test ./... -count=1` | 全绿（无 FAIL 行） |
| `./scripts/db_smoke.sh` | **两条路径全绿**（fresh 20 条 + upgrade 4 条，`结果: ✅`） |
| 前端 `tsc --noEmit` / `eslint` | 干净 |
| 前端 `vitest run` | 39 文件 / **327 passed** |

**变异反证汇总（9/9 全红）**：步骤 4 六项在 sqlite 基座（`/tmp/m26_mutate.py`），
步骤 5 三项在真 PG（`/tmp/m26_smoke_mutate.py`）。**注意 M1 的第一次尝试是「编译失败」型假信号**
（`created` 变成未使用 → `build failed`，不是断言红）—— 按 T-47 信号 1 换了个可编译的等价变异
（`created, st := now, timeOK`）重跑，红在断言上（`+2026-09-11 13:40:27` vs `-2026-09-09 02:00:00`）才算数。

---

## 7. 复审记录（3 路细节审查结论与处置）

审于 2026-09-11。E＝可编译性/GORM API（含真 PG 18.4 实测），F＝测试可行性，G＝D-9 必要性。

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| E2 | E | **阻断** `res.RowsAffected` ≠ 实际插入数（真 PG 实测 1 冲突+2 新 → 3） | 已改：同事务 COUNT 前后差（§3.3） |
| E2b | E | **阻断** `skipped := 0` 与命名返回值同块 → 编译错 | 已改：删行（§3.3） |
| E5 | E | **阻断** 5 个调用点未覆盖（handler + 4 处测试） | 已补精确 diff（§3.5、§4） |
| E5b | E | **阻断** `upsertTestSchema` 无 `external_id` 索引 → 新 `ON CONFLICT` 全线报错 | 已补（§4 基座注意） |
| EA | E/F | **中** T11 假绿（预过滤短路，变异抓不到） | 已拆 T11a/T11b，T11b 用一次性 Query 钩子造真冲突（§4） |
| E3-1 | E | 低 逃生门行不占位（同批撞号） | 已修：3 行占位 + T15b |
| E3-2 | E | 中 并发不同步覆盖 | **不修**：需求 §1.5 已登记超范围；已在 §3.6 注释与 T15 说明中显式排除 |
| EB | E | 低 §3.7 row3 拟改注释方向写反 | 已改（§3.7） |
| E4 | E | 低 两文档标识符/签名不一致 | 已统一为实现版（非导出 + 1 参）；**需求 §2.1 待同步**（列入 §1 清单） |
| E6/Q1 | E | `&v` 取址、`clause.OnConflict` 字段名/类型/渲染 —— 全部正确 | 无需改 |
| E-待审6 | 本文件 | `time/tzdata` 内嵌代价 | **关闭**：`backend/Dockerfile:24` 已 `apk add tzdata`，不内嵌（§2） |
| F-H3 | F | 中 T17 需三步构造 | 已改为三步序列（§4） |
| F-H4 | F/G | 低 Down 链序数错位 | 已逐个核行号并修正（§5），其中 `:1280` 现值本就少 1 |
| F-M1 | F/G | 中 逐条路径 `generateTicketNumber` 仍用条数法 | **不修**：G 证 `internal/service/ticket_from_alert_test.go:200-210` 依赖该间隙做失败注入，改它超范围。登记为残留（§3.6） |
| G | G | D-9 机制成立（真 PG 复现），但**原写的原因（删工单留空洞）是假的** | 已重写为「D-4 引入的 TOCTOU 空洞」，删去不可达路径（§0 D-9、§3.6） |
| G-b | G | 候选 (b) `DoUpdate` 无效 | 已剔除（不再列为备选） |
| E-待审2 | 本文件 | `nextTicketSeq` 双轨是否成立 | **成立**，理由见 F-M1 |
| E-待审4 | 本文件 | handler 未读 | **已读**，现文与精确 diff 见 §3.5 |
| **R1** | **实现阶段（步骤 1）实测发现，3 路审查均未捕获** | **T3 原设计是假绿**：原断言 `got.UTC().Format(...)` 自己又 `.UTC()` 了一次，把 `.UTC()` 的效果抹平 —— 变异「去掉 `.UTC()`」**不红**。且 sqlite 基座**结构上不可能**测出该差异（保留偏移），故 T7 也不承担此职责 | 已修：T3 增 `Location()` 断言；新增 T18（真 PG 落库字面值）；T3/T7 职责重新划清。证据：变异 4 路全红，见 §4 基座注意 |

### 实现阶段实测记录（步骤 4–5）

| # | 来源 | 结论 | 处置 |
|---|---|---|---|
| **R1** | 步骤 1 | T3 原设计假绿（断言里自己又 `.UTC()` 一次）；sqlite 基座结构上测不出 `.UTC()` | 已修：T3 增 `Location()` 断言；新增 T18 真 PG 落库字面值 |
| **R2** | 步骤 4 | 6 个变异全部变红（含 M5「改用 `res.RowsAffected`」→ 数字虚报 2、M6「去掉 `TargetWhere`」→ 仲裁失效） | T11b 的钩子路径**实测可行**，未关闭项 1 关闭 |
| **R3** | 步骤 4 | **`logTimeFallback` 在留 NULL 的分支上撒谎**：日志写「按回落处理」，而 D-2 的 `resolved_at`/`closed_at` 分支根本不回落，正是把排查「closed_at 为什么是空」的人引向反方向 | 已改名为 `logTimeUnusable`，文案改为只陈述事实（`不可用（status=N）`），处置归调用点 |
| **R4** | 步骤 5 | 三个真库变异全部变红，**其中 M-T18（去掉 `.UTC()`）落库从 `02:00:00` 变 `10:00:00`** —— R1 指出的「sqlite 测不到」由真 PG 兜住 | 无（防线成立） |
| **R5** | 步骤 5 | 真库用例手写 `INSERT INTO tickets` 撞 NOT NULL（见 §5 补充） | 已改为 `models.Ticket` + `gorm.Create` |

**未关闭项**（两条）：
- **`"0001-01-01 00:00:00"` 这个输入的处置未定**（需求 §1.9 登记项的复核结果）：
  §1.9 原判断是「它 IsZero()==true → gorm `autoCreateTime` 会静默替换成 `NowFunc`」。
  实现阶段实测**方向不对**（见 `timeparse_test.go` 的对应用例）：
  - `.UTC()` 前后 `IsZero()` **都是 false** —— 因为 tzdata 对 1901 年前的上海用 LMT(+08:05:43)，
    把瞬间推离零值（落库 `0000-12-31 15:54:17`）。所以既不会被 gorm 换成 `now`，也不会走回落；
  - 真值是**公元 0 年的时间戳原样落库**。对 `closed_at`/`resolved_at` 而言这仍是个脏值，
    但触发它需要 GLPI 真的发这个串（GLPI/MySQL 的约定是 `0000-00-00`，实测三种 layout 全拒 → 已归 absent）。
  **当前处置**：归 `timeOK`（不替 GLPI 发明语义），用测试把真实行为钉死并加注释说明它守的是
  「`glpiLoc` 不许被换成 UTC」这条变异（实测：`glpiLoc = time.UTC` → 4 条子用例红）。
  **若将来观测到 GLPI 真发这个串** → 加进 `glpiZeroDates` 归 absent，是一行的事。
- **`Asia/Shanghai` 仍是待核对常量**（D-3）：登记在需求 §6，GLPI 实际部署时核对。
  注意它现在**有真库防线**了 —— T18 会在时区名被改错时变红（`glpiTZName` 改成 UTC 之外的任意值时，
  落库字面值不再是 `02:00:00`），所以「核对」是确认业务事实，不是补测试。
