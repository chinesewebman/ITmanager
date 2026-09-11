# IMPL: 第三方文本截断保真（M33 / G-45）—— 可执行细节文档

- 状态：**rev2**（2026-09-12，已过两路审查，见 §10）
- 上游：[`docs/FIX-PLAN-TRUNCATION.md`](FIX-PLAN-TRUNCATION.md) rev2（拍板 D-1..D-10 以该文为准，本文只讲**怎么改**）
- 行号：以 2026-09-12 磁盘为准；**实现时按符号名定位，不按行号**（rev1 有多处行号漂移，rev2 已订正但仍可能再漂）

---

## §0 改动总览

| # | 文件 | 性质 | 一句话 |
|---|---|---|---|
| D1 | `internal/redact/redact.go` | 新增导出 | `TruncateRunes(s, max) (string, bool)` |
| D2 | `internal/integration/truncate.go` | **新文件** | 9 个列宽常量 + `sanitizeText` + `fieldCounter`（承载截断**与**剥离两种明细） |
| D3 | `internal/integration/service.go` | 改 4 处 | NetBox/Zabbix/GLPI 字面量套 helper；3 个 `SyncFrom*` 签名 + `SyncAll` 透出 |
| D4 | `internal/integration/metric_sync.go` | 改 3 处 | `Key` 套 helper；包级函数签名；**worker 接住计数**（现为 `if _, err :=` 丢弃） |
| D5 | `internal/api/handlers/integration_handler.go` | 改 4 处 | 接住新返回值写进 `results` |
| D6 | `frontend/src/pages/Settings.tsx` | 改 3 处 | 三个 handler 各补截断文案（+ `Settings.test.tsx` 3 条断言） |
| D7 | `internal/middleware/audit.go` + `internal/models/user.go` | 改 2 处 | **G-55**：`resource` 100 → 50 |
| D8 | `internal/api/openapi.yaml` | 改 description | `synced` 结构**不动**（自由 map），只补 description 的键清单 |
| D9 | 测试 **8** 个文件 + 冒烟白名单 | 用例/变异 | 见 §6 |
| D10 | 台账 5 个文件 | 结案/登记 | 见 §9 |

**不改**：`ConvertToAsset` / `ConvertToAlert` / `ConvertToTicket` 签名（FIX-PLAN D-4）；任何 schema / 迁移；`zabbix_truncated` 键名；既有两份私有 `truncateRunes`。

---

## §1 D1：`redact.TruncateRunes`

`internal/redact/redact.go` 末尾追加（紧随现有 `StripControl`）：

```go
// TruncateRunes 按**字符**截断到 max，并报告是否发生了截断。
//
// 为什么按 rune 不按 byte：PG 的 varchar(n) 数的是**字符**（实测 255 个中文 = 765 字节
// 可入库、256 个被拒），而按 byte 切会把多字节字符切成非法 UTF-8 → PG 22021 拒收，
// 把「超长」换成「编码非法」，故障照旧（本函数的存在理由，见 docs/FIX-PLAN-TRUNCATION.md F4/R1）。
//
// max <= 0：原样返回。列宽常量写错时宁可交给 DB 报错，也不静默把字段抹成空串。
//
// 快路：字节数 ≤ max ⇒ rune 数必 ≤ max，恒安全（UTF-8 每个 rune 至少 1 字节），
// 省掉一次 []rune 分配。注意该快路对**非法 UTF-8** 也走「原样返回」——
// 调用方要么先经 StripControl（真实管线如此），要么自行保证输入合法。
func TruncateRunes(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	r := []rune(s)
	if len(r) <= max {
		return s, false
	}
	return string(r[:max]), true
}
```

**与既有两份私有 `truncateRunes` 的差异（有意，非疏漏）**：

| 差异 | 既有（`middleware/audit.go:197`、`service/ticket_service.go:404`） | 本文 |
|---|---|---|
| 返回值 | 只有 `string` | `(string, bool)` —— 计数需要 |
| `max <= 0` | 无守卫（`max<0` 会 panic，`max==0` 返回空串） | 原样返回 |
| 快路 | 无 | 有 |

不迁移既有两处调用（FIX-PLAN 驳回项；登记 G-53）。

`redact_test.go` 追加 `TestTruncateRunes`。**断言要点（U1）**：

```go
// 关键：断言对象是**管线形态**，不是裸 TruncateRunes ——
// 裸函数对非法 UTF-8 输入会走快路原样返回，此时 utf8.ValidString 不成立（rev1 的断言是假的）。
got, hit := redact.TruncateRunes(redact.StripControl(src), max)
require.True(t, utf8.ValidString(got), "输出必须合法 UTF-8")
require.Equal(t, min(utf8.RuneCountInString(src), max), utf8.RuneCountInString(got),
	"rune 数必须精确 —— 中文夹具下这条是唯一能抓 byte 切法的断言")
```
表驱动夹具：空串 / n-1 / n / n+1 / `"中"×255` / `"中"×256` / `"😀"×255` / `"😀"×256` / `max<=0`（→ 原串、`hit==false`）。
另加一条**裸函数 + 非法字节**（`"\xff\xfe"`）的用例，把「快路原样返回」这一契约钉住（避免后人误以为它做归一化）。

---

## §2 D2：`internal/integration/truncate.go`（新文件）

**import 必须写全**（rev1 漏了 `log`/`strconv`，照抄会编译失败）：

```go
import (
	"log"
	"sort"
	"strconv"
	"strings"

	"network-monitor-platform/internal/redact"
)
```

```go
// 列宽常量：与 backend/migrations/*.sql 的真实 VARCHAR(n) 逐一对齐。
// **三处必须一致**：迁移 DDL（权威）== 模型 gorm:"size:N" tag == 这里的常量。
// 守卫：U7a（常量 == 模型 tag，纯 Go）+ U7b（常量 == information_schema，真 PG）。
// 改列宽的迁移必须同步改另外两处，否则守卫红。
const (
	colAssetName        = 255 // assets.name
	colAssetBrand       = 100 // assets.brand
	colAssetModel       = 100 // assets.model
	colAssetSN          = 100 // assets.sn
	colAssetSiteName    = 100 // assets.site_name
	colAlertTriggerName = 500 // alerts.trigger_name
	colAlertHostName    = 255 // alerts.host_name
	colTicketTitle      = 255 // tickets.title
	colMetricKey        = 100 // metric_snapshots.key
)

// sanitizeText 第三方字符串 → TEXT 列：只剥控制字符，**不截断**。
// TEXT 没有长度约束，但**照样拒 NUL**（22021）—— 且它与有界列在同一条
// CreateInBatches 事务里，一条带 NUL 就整批回滚（FIX-PLAN F13）。
func sanitizeText(s string) (string, bool) {
	cleaned := redact.StripControl(s)
	return cleaned, cleaned != s
}

// fieldCounter 收集「哪些字段被截断/被剥离、各几次」，供日志明细与 API 计数用。
//
// 这是本包**新引入**的模式：既有代码只用裸 int 计数器（service.go 的 truncated/skipped、
// metric_sync.go 的 skipped/written）。裸 int 不够，是因为 FIX-PLAN §3.2 要求日志含
// **逐字段明细**（trigger_name×3），而 R3-b 还要求把「只剥不截」（如 description 含 NUL）
// 也露出来 —— 后者若只丢一个 bool 就是静默，正是 R3-b 要防的失败模式。
//
// 只记字段名与次数，**不记原值**：原值可能含 StripControl 不覆盖的 U+2028/U+202E 等（R6）。
//
// 必须在使用它的函数内创建（函数局部，计数经返回值透出），不得落 IntegrationService
// 字段或包级变量：该 struct 是长生命周期共享对象，HTTP handler 与 worker goroutine
// 会并发调用（FIX-PLAN R5/D-10）。
type fieldCounter struct {
	truncated map[string]int // 字段 → 被截断次数（进 API 计数）
	stripped  map[string]int // 字段 → 仅被剥控制字符次数（只进日志，不进 API 计数）
}

// truncate 第三方字符串 → 有界列：剥控制字符 + 按字符截断。
func (c *fieldCounter) truncate(field, v string, max int) string {
	out, hit := redact.TruncateRunes(redact.StripControl(v), max)
	if hit {
		if c.truncated == nil {
			c.truncated = make(map[string]int, 4)
		}
		c.truncated[field]++
	}
	return out
}

// text 第三方字符串 → TEXT 列：只剥不截，剥离明细进日志（R3-b）。
func (c *fieldCounter) text(field, v string) string {
	out, changed := sanitizeText(v)
	if changed {
		if c.stripped == nil {
			c.stripped = make(map[string]int, 2)
		}
		c.stripped[field]++
	}
	return out
}

// count 透出给 API 的「被截断的字段处数」。**不含**只剥不截的（那个只进日志）。
func (c *fieldCounter) count() int {
	n := 0
	for _, v := range c.truncated {
		n += v
	}
	return n
}

// String 形如 "host_name×2,trigger_name×3,problem(stripped)×1"
// （字段名排序，保证日志可逐字比对）。
func (c *fieldCounter) String() string {
	keys := make([]string, 0, len(c.truncated)+len(c.stripped))
	for k := range c.truncated {
		keys = append(keys, k)
	}
	for k := range c.stripped {
		if _, dup := c.truncated[k]; !dup {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if n := c.truncated[k]; n > 0 {
			parts = append(parts, k+"×"+strconv.Itoa(n))
		}
		if n := c.stripped[k]; n > 0 {
			parts = append(parts, k+"(stripped)×"+strconv.Itoa(n))
		}
	}
	return strings.Join(parts, ",")
}

// logFieldSanitization 只在真有截断或剥离时打一行（避免每次同步都刷）。
func logFieldSanitization(path string, c *fieldCounter) {
	if c.count() > 0 || len(c.stripped) > 0 {
		log.Printf("[%s] 字段截断 %d 处，明细：%s", path, c.count(), c)
	}
}
```

**指针 vs 值（实测，两处都踩过）**：`String()` 是指针接收者 ⇒ **值类型 `fieldCounter` 不满足
`fmt.Stringer`**，`go vet` 的 printf 检查会红（`format %s has arg c of wrong type fcprobe.fieldCounter`）。
故：
- `logFieldSanitization(path string, c *fieldCounter)` 的 `c` **必须是指针**；
- 日志里也**不能**把 `fc` 按值传（`log.Printf("%s", fc)` 同样被拦），`&fc` 即可。

`var fc fieldCounter`（值）+ 指针接收者方法 = 合法（`fc` 可寻址）；在结构体字面量内部调用
`fc.truncate(...)` 无坑（实测：各字段写不同 map 键，求值顺序无关）。实测输出：
`[zabbix] 字段截断 1 处，明细：a×1,problem(stripped)×1`，`count()==1`（剥离项不进 API 计数）。

---

## §3 D3：`service.go` 四处

### 3.1 NetBox 循环（字面量 `service.go:125-139`）

**before**
```go
	toUpsert := make([]models.Asset, 0, len(devices))
	seen := make(map[int]struct{}, len(devices))
	for _, d := range devices {
		asset := d.ConvertToAsset()
		if _, dup := seen[d.ID]; dup {
			continue
		}
		seen[d.ID] = struct{}{}
		toUpsert = append(toUpsert, models.Asset{
			Source:       "netbox",
			NetBoxID:     asset.NetboxID,
			Name:         asset.Name,
			AssetType:    asset.AssetType,
			Status:       asset.Status,
			Brand:        asset.Brand,
			Model:        asset.Model,
			SN:           asset.SN,
			SiteName:     asset.SiteName,
			RackName:     asset.RackName,
			Tags:         "[]",
			CustomFields: "{}",
			UpdatedAt:    now,
		})
	}
```

**after**（`var fc fieldCounter` 必须在 `for` **之前** —— 循环内有 `continue`，写在里面会丢计数）
```go
	toUpsert := make([]models.Asset, 0, len(devices))
	seen := make(map[int]struct{}, len(devices))
	var fc fieldCounter // D-10：函数局部，经返回值透出
	for _, d := range devices {
		asset := d.ConvertToAsset()
		if _, dup := seen[d.ID]; dup {
			continue
		}
		seen[d.ID] = struct{}{}
		toUpsert = append(toUpsert, models.Asset{
			Source:       "netbox",
			NetBoxID:     asset.NetboxID,
			Name:         fc.truncate("name", asset.Name, colAssetName),
			AssetType:    asset.AssetType,
			Status:       asset.Status,
			Brand:        fc.truncate("brand", asset.Brand, colAssetBrand),
			Model:        fc.truncate("model", asset.Model, colAssetModel),
			SN:           fc.truncate("sn", asset.SN, colAssetSN),
			SiteName:     fc.truncate("site_name", asset.SiteName, colAssetSiteName),
			RackName:     asset.RackName, // F9：ConvertToAsset 不赋值，恒 ""，非风险面
			Tags:         "[]",
			CustomFields: "{}",
			UpdatedAt:    now,
		})
	}
```

日志：现有 `log.Printf("从 NetBox 同步了 %d 个设备 (新增+更新)", len(toUpsert))` 之后加
`logFieldSanitization("netbox", &fc)`。

签名（**全部 return 点都要改**：`service.go:104/107/147` 附近 + 函数尾）：
```go
// before: func (s *IntegrationService) SyncFromNetBox(ctx context.Context) (int, error)
func (s *IntegrationService) SyncFromNetBox(ctx context.Context) (synced, fieldsTruncated int, err error)
```
成功 `return len(toUpsert), fc.count(), nil`；早退/错误 `return 0, 0, ...`。

### 3.2 Zabbix 循环（字面量 `service.go:240-252`）

**注意**：本函数已有名为 `truncated` 的具名返回（**源侧 0/1 标志**，与本文的字段计数是两回事）
→ 新计数命名 `fieldsTruncated`，**不得复用 `truncated`**（FIX-PLAN D-5）。

**声明位置**（本循环有 3 处 `continue`，`service.go:217/230/237` 附近 —— 三个循环里最容易写错的一个）：
```go
	toInsert := make([]models.Alert, 0, len(triggers))
	var fc fieldCounter // 必须在循环外
```

**before**
```go
		toInsert = append(toInsert, models.Alert{
			TriggerID:    t.TriggerID,
			HostName:     alert.HostName,
			TriggerName:  alert.TriggerName,
			Problem:      alert.Problem,
			Severity:     alert.Severity,
			SeverityName: alert.SeverityName,
			ProblemStart: problemStart,
			Status:       "problem",
			Source:       "zabbix",
			CreatedAt:    now,
			UpdatedAt:    now,
		})
```

**after**
```go
		toInsert = append(toInsert, models.Alert{
			TriggerID:    t.TriggerID,
			HostName:     fc.truncate("host_name", alert.HostName, colAlertHostName),
			TriggerName:  fc.truncate("trigger_name", alert.TriggerName, colAlertTriggerName),
			Problem:      fc.text("problem", alert.Problem), // TEXT：只剥不截
			Severity:     alert.Severity,
			SeverityName: alert.SeverityName,
			ProblemStart: problemStart,
			Status:       "problem",
			Source:       "zabbix",
			CreatedAt:    now,
			UpdatedAt:    now,
		})
```

日志：现有 `log.Printf("从 Zabbix 同步了 %d 个告警（截断标志 %d）", synced, truncated)` **之后**加
`logFieldSanitization("zabbix", &fc)`（**两行各自表述，不合并** —— 一个是源侧标志，一个是字段计数，
合并正是 D-5 要避免的混淆）。

签名（return 点在 `service.go:176/192/291` 附近 + 函数尾）：
```go
// before: (synced, truncated int, err error)
// after:  (synced, truncated, fieldsTruncated int, err error)
```
成功 `return synced, truncated, fc.count(), nil`；早退/错误 `return 0, truncated, 0, ...`；
`if len(toInsert) == 0 { return 0, truncated, 0, nil }`。

### 3.3 GLPI 循环（字面量 `service.go:361-371`）

**声明位置**：`toUpsert := make(...)` / `now := time.Now()`（`service.go:335-336` 附近）之后、
`for` **之前**：`var fc fieldCounter`（循环内同样有 `continue`）。

**before**
```go
		nt := models.Ticket{
			ExternalID:  local.ExternalID,
			Title:       local.Title,
			Description: local.Description,
			...
		}
```

**after**
```go
		nt := models.Ticket{
			ExternalID:  local.ExternalID,
			Title:       fc.truncate("title", local.Title, colTicketTitle),
			Description: fc.text("description", local.Description), // TEXT：只剥不截
			...
		}
```

日志：`log.Printf("从 GLPI 同步了 %d 个工单（跳过越界 %d 条）", synced, skipped)` 之后加
`logFieldSanitization("glpi", &fc)`。

签名（return 点在 `service.go:314/317/328/443/447` 附近）：
`(synced, skipped, fieldsTruncated int, err error)`。

### 3.4 `SyncAll`（`service.go:454-487`）

```go
	if n, ft, err := s.SyncFromNetBox(ctx); err != nil {
		...
	} else {
		results["netbox"] = n
		results["netbox_field_truncations"] = ft
	}
	if n, trunc, ft, err := s.SyncFromZabbix(ctx); err != nil {
		...
	} else {
		results["zabbix"] = n
		results["zabbix_truncated"] = trunc
		results["zabbix_field_truncations"] = ft
	}
	if n, skip, ft, err := s.SyncFromGLPI(ctx); err != nil {
		...
	} else {
		results["glpi"] = n
		results["glpi_skipped"] = skip
		results["glpi_field_truncations"] = ft
	}
```

> ⚠️ **`SyncAll` 只有 3 个键，这是设计不是遗漏**：`SyncAll` 从不调用指标路径
> （`service.go:454-487` 只调三个 `SyncFrom*`；`SyncMetricsFromZabbix` 的调用点全集是
> `handler:71`（手动 `type=zabbix_metrics`）与 `metric_sync.go:110`（worker））。
> 故 `zabbix_metrics_field_truncations` **只在单路径响应里出现**，同一次响应里 4 个键
> **永不同时存在**。openapi description 与前端文案都要按「键随 `type` 而变」表述。

**失败分支不写键**（与 `glpi_skipped` 同形，前端 `?? 0` 兜住）—— 这是 U8 的断言对象，
且**只能在 `SyncAll` 层测**：handler 失败时走 `apierr.Internal`，`apierr.Respond` 不写
`data.synced`，HTTP 层根本观测不到（F5）。

---

## §4 D4/D5：`metric_sync.go` 与 handler

### 4.1 metric_sync.go 三处

**a) `Key` 字面量（`:190-195`）** —— `var fc fieldCounter` 放在构造循环（`:168-196`，内含
4 处 `continue`）**之前**：
```go
		snaps = append(snaps, models.MetricSnapshot{
			AssetID: assetID,
			Key:     fc.truncate("key", it.Key, colMetricKey),
			Value:   v,
			TS:      now,
		})
```

**b) 包级函数签名** —— `SyncMetricsFromZabbix` 有 **7 个 return 点**（`:126/131/135/147/157/199/211`
附近），**每个都要补第三值**（rev1 只列了 2 个，不完整）：
```go
// before: func SyncMetricsFromZabbix(...) (int, error)
// after:  func SyncMetricsFromZabbix(...) (written, fieldsTruncated int, err error)
```
未配置早退 `return 0, 0, nil`；`len(snaps)==0` 早退 `return 0, 0, nil`；分批失败
`return written, fc.count(), err`；成功 `return written, fc.count(), nil`。
汇总日志（`:215`）之后加 `logFieldSanitization("zabbix_metrics", &fc)`。

**c) worker（`:110`）** —— 现在是 `if _, err := SyncMetricsFromZabbix(...)`，**计数被丢弃**：
```go
		case <-t.C:
			written, ft, err := SyncMetricsFromZabbix(ctx, w.svc.zabbix, w.db, w.batchLimit)
			if err != nil {
				log.Printf("[zabbix metric sync] tick error: %v", err)
				continue
			}
			if ft > 0 {
				// 这条路径没有 HTTP 面也没有前端入口，日志是唯一出口（FIX-PLAN §1.3-B）
				log.Printf("[zabbix metric sync] tick ok: written=%d, field_truncations=%d", written, ft)
			}
```
（`logFieldSanitization` 已在函数内打过明细；worker 这行补「本次 tick 的可见性」。）

### 4.2 handler（`integration_handler.go:59-81`，4 个 case + default）

```go
	case "netbox":
		count, ft, e := h.svc.SyncFromNetBox(ctx)
		results = map[string]int{"netbox": count, "netbox_field_truncations": ft}
		err = e
	case "zabbix":
		count, truncated, ft, e := h.svc.SyncFromZabbix(ctx)
		results = map[string]int{"zabbix": count, "zabbix_truncated": truncated, "zabbix_field_truncations": ft}
		err = e
	case "zabbix_metrics":
		count, ft, e := h.svc.SyncMetricsFromZabbix(ctx)
		results = map[string]int{"zabbix_metrics": count, "zabbix_metrics_field_truncations": ft}
		err = e
	case "glpi":
		count, skipped, ft, e := h.svc.SyncFromGLPI(ctx)
		results = map[string]int{"glpi": count, "glpi_skipped": skipped, "glpi_field_truncations": ft}
		err = e
```
（`default` 走 `SyncAll`，已在 §3.4 覆盖。）

方法包装（`service.go:492-493`）→ `func (s *IntegrationService) SyncMetricsFromZabbix(ctx context.Context) (int, int, error)`。

---

## §5 D6/D7/D8：前端、G-55、契约

### 5.1 `Settings.tsx`（3 处，**不是「各补一行」**）

实测该页全是**具名读取**（无 `Object.entries` 遍历 `synced`）→ 新增键不会多出 UI 行，
但也不会被显示。三个 handler 写法各不相同，要分别处理：

| handler | 现状 | 改法 |
|---|---|---|
| `handleSyncNetBox`（`:126-139`） | **没有 `base` 变量**，`message.success` 内联模板串 | 先抽 `const base = \`NetBox 同步完成，新增 ${synced ?? 0} 条资产\``，再条件追加 |
| `handleSyncGLPI`（`:187-204`） | 有 `base` + `skipped`（**条数，可插值**） | 再加一个 `ft` 条件（可两条件拼接或合成一句） |
| `handleSyncZabbix`（`:255-279`） | 有 `base` + `truncated`（**0/1 标志，注释明写不许插值**） | 加**第三分支**（不是补一行）：`ft > 0` 时追加字段截断文案 |

**硬约束（来自既有测试，rev1 未提）**：`Settings.test.tsx:727/743` 用 `toBe(...)` **精确匹配**
`"Zabbix 同步完成，新增 N 条告警"`，`:717` 还有 `expect(msg).not.toMatch(/另有\s*1\s*条/)`。所以：
- 新文案**必须是条件追加**：`truncated==0 && ft==0` 时逐字等于 `base`；
- Zabbix 的截断文案**不能用「另有 N 条」句式**（会撞 `:717` 的正则）。建议用「另有 N 处字段超长被截断」
  （「处」而非「条」）。

`api.ts` 只有 `syncZabbix`/`syncNetBox`/`syncGLPI`（无 metrics）→ `zabbix_metrics`
**无前端入口**，本页不加，计数只在响应体与日志里。

### 5.2 G-55（`audit_logs.resource` 常量 > 真实列宽）

```diff
- // internal/middleware/audit.go:164
- return sanitizeField(p, 100)
+ // 真实列宽是 varchar(50)（迁移 000001_init.up.sql:1097 —— :64 那处是 permissions 表，
+ // 别改错）；按 100 截会留下 51..100 字符的值 → PG 22001 → 审计整行丢失
+ // （与 G-45 同一机制，常量 > 列宽的失败模式）。用户输入路径无可达性，属潜伏。
+ return sanitizeField(p, 50)
```
```diff
- // internal/models/user.go:106
- Resource   string     `json:"resource" gorm:"size:100"`
+ Resource   string     `json:"resource" gorm:"size:50"`
```
- 唯一性已核实：全仓只有 `audit.go:164` 一处按 100 截 `resource`。
- `tests/schema_drift_test.go` 只比对**列名集合**（无 `Size` 相关代码）→ **不受影响**。
- AutoMigrate 兜底路径（仅 `MigrationsFS == embed.FS{}` 时可达，生产恒注入 → 不可达）：
  改 `size:50` 是**减少**漂移（当前 `size:100` 反而每次兜底都试图把列撑到 100）；
  唯一残余是某个已被撑到 100 的 dev 库缩列时可能失败 → 删库重建即可。
- **把 `resource` 也纳入 U7b 的列宽校验清单**（否则下次再漂没人知道 —— 这正是 G-55 的成因）。
- 追加断言到既有 `TestDBSmoke_AuditFieldTruncation`（已在冒烟白名单内，**不需改白名单**）：
  注册一条首个静态段 > 50 字符的路由，请求后断言审计行**仍然落库**。

### 5.3 `openapi.yaml`

`synced` 是 **`additionalProperties: {type: integer}` 自由 map**（`:3410-3412`）→ **结构不改**。
改的是 `SyncResult.description`（`:3398-3403`）里**手写枚举的键清单**（现只列
`zabbix_truncated`/`glpi_skipped`），补 4 个新键 + 两条说明：
> - `*_truncated`（zabbix）= 源侧超条数上限的 **0/1 标志**；`*_field_truncations` = **被截断的字段处数**（计数）。
> - 键随 `type` 而变：`all` 只出 netbox/zabbix/glpi 三组；`zabbix_metrics` 单独请求时才出该键。

然后 `npm run gen:api` 重生成 `frontend/src/services/api.types.ts`（**生成物，禁手改**；
CI 有漂移硬门禁 `.github/workflows/ci.yml:197-198`）。改 description **确实会改生成物**
（`api.types.ts:2037-2043` 把该 description 逐字生成为 JSDoc）→ 重生成**是必须的**，不是可选。

---

## §6 测试清单

| 文件 | 改动 |
|---|---|
| `internal/redact/redact_test.go` | **U1**：`TestTruncateRunes`（管线形态断言 + 裸函数非法字节契约） |
| `internal/integration/truncate_test.go` | **新增**：**U2**（剥控制字符，夹具含 `\x00`/`\x7f`/`\n`/`\r`/`\t`）、**U5**（未超长不计数、值逐字未变）、**U7a**（常量 == 模型 `size:` tag）、**M9 的顺序反例** |
| `internal/integration/upsert_test.go` | 签名连带（NetBox ×7 `:225/248/270/296/334/360/367`、Zabbix ×5 `:412/423/478/512/567`、GLPI ×9 `:610/614/653/671/718/744/764/792/835`） |
| `internal/integration/zabbix_truncate_test.go` | 签名连带 `:54/71/88` + 新计数断言 |
| `internal/integration/zabbix_identity_test.go` | 签名连带 `:138/170/182/204` |
| `internal/integration/metric_sync_test.go` | 签名连带 `:142/176/213/229/253/276` + **M11 worker 日志断言**（见 §7） |
| `internal/api/handlers/integration_handler_test.go` | 走 HTTP 的计数断言（该文件不直调 `SyncFrom*`，需真 `*IntegrationService` + httptest 假上游，配方抄 `db_smoke_test.go:622-667`） |
| `tests/db_smoke_test.go` | **签名连带 `:643/667`（NetBox）、`:1310`（GLPI）、`:1489`（Zabbix）**；**新增** U3/U4/U6/U7b、U8；追加 G-55 断言 |
| `frontend/src/pages/Settings.test.tsx` | **新增 3 条**：`ft=3` → 文案含「另有 3 处」；`ft` 缺失 → 与旧文案逐字相同（护住既有 `toBe` 精确匹配） |
| `scripts/db_smoke.sh` | 白名单追加 `TestDBSmoke_ThirdPartyFieldTruncation` / `TestDBSmoke_SyncAllFailureOmitsKeys`（**T-42**；命名风格与既有 `TestDBSmoke_XXX` 一致、无正则元字符、与既有条目无前缀冲突） |

### U6 的断言规格（rev1 缺失，而它决定 M7b 能否红）

`TestDBSmoke_ThirdPartyFieldTruncation`（真 PG）必须写成：
1. 夹具覆盖**全部 11 个字段**：9 个有界字段各喂「真实列宽 + 1」个汉字，2 个 TEXT 字段各喂一个含 `\x00` 的串；
2. 真实列宽**从 `information_schema.columns` 现查**（不硬编码），得到 `limit[col]`；
3. 同步后**逐字段断言**：`utf8.RuneCountInString(落库值) == min(utf8.RuneCountInString(原始值), limit[col])`
   —— **必须是 `==` 精确值**，`≤ limit` 抓不到「常量偏小」（M7b）；
4. 同步返回成功（`synced > 0`）且 `*_field_truncations` 计数正确；
5. 含 NUL 的两个 TEXT 字段**落库成功**（旧代码这里是 22001/22021 → 整批 0 行）。

---

## §7 变异表（T-31：先 `go build` 验证可编译，编译失败判 INVALID）

| # | 变异（**可编译**形态） | 预期红点 |
|---|---|---|
| M1 | `TruncateRunes` 改 byte：`return s[:max], true` | U1 的 **rune 计数**（中文夹具）；emoji 夹具另抓 `utf8.ValidString` |
| M2 | 两处同步改（只改一处会因另一处仍调用 `StripControl` 而漏检）：`truncate` 里去掉剥 → `out, hit := redact.TruncateRunes(v, max)`；`text` 改为 `return v`（不记 `stripped`）**且** `sanitizeText` 改为 `return s, false` —— 这样 `redact` 仍被 `TruncateRunes` 引用，**无 unused import**，可编译 | U2 + U6 的 NUL 样本（`truncate` 侧）、`problem(stripped)×1` 的日志断言（`text` 侧） |
| M3 | `max <= 0` 改 `return "", false` | U1 的 `max<=0` 分支 |
| M4 | `truncate` 里去掉 `c.truncated[field]++` | U3/U6 |
| M5 | `SyncAll` 失败分支也写 `*_field_truncations` | U8（`SyncAll` 层） |
| M6 | NetBox 少包一个字段（`Brand: asset.Brand`） | U6 |
| M7 | `colAlertTriggerName = 600`（偏大） | U7a + U7b + U6（旧代码式 22001 复现） |
| M7b | `colAlertTriggerName = 400`（偏小） | U7a + U7b + **U6 的 `==` 精确值断言** |
| M8a | `truncate` 恒把 `hit` 当 true | U5 |
| M8b | `truncate` 恒把 `hit` 当 false | U3/U6 |
| M9 | 顺序反转：`TruncateRunes` 之前不剥、剥在之后 | U2 夹具 `("\x00"×9+"abcdefg", 10)`：正确 `"abcdefg"`，反转 `"a"`（9 个真实字符被静默丢弃） |
| M10 | `fc.text(...)` 改成 `fc.truncate(..., 255)` | U6 的 `description` 长度断言 |
| M11 | worker `:110` 改回 `if _, _, err := ...`（丢弃计数） | **可抓**：`metric_sync_test.go` 用 `log.SetOutput` 捕获日志 + 假 Zabbix（`newFakeZabbix`/`newZabbixWithFake`，见 `:288-320` 的 worker 测试基座），断言 tick 日志含 `field_truncations=1`。先例：`upsert_test.go:505-506`、`notification/notification_test.go:573-574` |

**已知抓不到**（不假装全覆盖）：
- `U+2028/2029/202E` 是否被剥 —— `StripControl` 本就不覆盖（R6），无断言可红；
- 常量 ↔ **迁移** 的漂移在纯 `go test` 下抓不到，只能靠 U7b（真 PG）。

---

## §8 执行顺序与门禁

### 1. U9 前置：红证明**必须走 HTTP 面**（rev1 的写法做不到）

**为什么做不到**：Go 按包编译。新 e2e 若引用新签名（`_, _, err := svc.SyncFromNetBox(ctx)`），
在旧代码上整包编译失败 → `go test ./tests/ -run X` 直接不进测试。`git stash` 也不适用
（会把新测试一起收走）。

**可操作做法**：写一个**不依赖新签名**的 HTTP 级用例（新旧代码都能编译）：
```
httptest 假 NetBox（一条 name = 300 汉字的设备）+ 真 PG + 真 SetupRouter
  → POST /api/integrations/sync {"type":"netbox"}
旧代码断言：响应 500，且 assets 行数**不增长**   ← 红
新代码断言：响应 200，assets 行数 +1，data.synced.netbox_field_truncations > 0，落库 name 被截到 255
```
配方抄 `tests/db_smoke_test.go:622-667`（「httptest 假 NetBox + 真 SyncFromNetBox + 真 PG」）。
**红判据必须是可观测项（HTTP 码 / 行数）**，不能断言 `22001` 文本 —— 那个错误码只在 PG 侧，
HTTP 面看不到。这条用例实现后**保留**，作为 U3/U6 的 HTTP 回归面。

精确到「逐字段 `==`」的断言（U6 正身）用直调版，放在真 PG 直调用例里；它无法在旧代码上跑，
红证明由上面那条 HTTP 用例承担。

### 2. 之后

2. D1 → D2 → `go test ./internal/redact/... ./internal/integration/... -run 'Truncate|Sanitiz'`（U1/U2 绿）。
3. D3/D4/D5 → 编译器点出全部签名连带调用点（§6 已列全，但**以编译器为准**）→ 逐个修。
4. D6/D7/D8 → 生成物重生成。
5. 按 §7 逐条做变异反证，记录红点。
6. 门禁（全绿才 push）：

```bash
cd backend && gofmt -l ./internal ./cmd ./tests   # 必须空
go vet ./... && go test ./... -count=1 && go build ./...
go test -race ./internal/integration/...          # R5/D-10 补充
cd .. && ./scripts/db_smoke.sh                    # 含新用例（白名单已加，T-42）
cd frontend && npx tsc --noEmit && npx eslint src --ext .ts,.tsx && npx vitest run src/pages/Settings.test.tsx
```

---

## §9 台账

| 文件 | 内容 |
|---|---|
| `TODO.md` | G-45 结案（按 FIX-PLAN §1.2 更正叙述：三条手动路径 = 500；指标路径 = 后台静默卡死）；新登记 G-53..G-57 |
| `CHANGELOG.md` | M33 条目 + ⚠️ 行为告知（照 M32 的详略：变更内容 / 受影响下游 / 自校验方法 / 附带副作用） |
| `docs/TRAPS.md` | 新增 **T-61**：sqlite 不强制 `VARCHAR(n)`（测试基座那几列是 `TEXT`）→ 长度类用例须真 PG，否则假绿；来源 = M29-C 先例 + M33 复核 |
| `08-部署运维.md`（**仓库根**，不是 docs/） | 新增「第三方字段上限表（超出丢弃 / NUL 剥离）+ 计数键含义」；修正 `:364` 已陈旧的 G-45 引用 |
| `docs/FIX-PLAN-TRUNCATION.md` | 回填 §10 实现记录 |

---

## §10 审查记录（rev1 → rev2）

两路审查（可执行性 / 一致性）于 2026-09-12 完成，各自在 `/tmp` 副本实证（含真实编译）。
**采纳 16 条，无驳回。**

### 高（2 条）

| # | 发现 | 处置 |
|---|---|---|
| A-1 | **`sanitizeText` 的 bool 被 `_` 丢弃** → FIX-PLAN R3-b 承诺的「剥离计数进日志明细」完全落空，且文档既不实现也不承认（最差的一种）。`fieldCounter` 只装截断，装不下剥离 | `sanitizeText` 保留为纯函数，但**调用入口改为 `fc.text(field, v)` 方法**，剥离明细并入 `fieldCounter.stripped`；`String()` 输出 `problem(stripped)×1`；`logFieldSanitization` 守卫放宽为「截断或剥离任一 >0」。**不新增抽象**（顺带消解了「这个 struct 是否过度设计」的疑问 —— 它现在是 R3-b 的必备载体） |
| A-2 | **§8 第 1 步「旧代码上跑新 e2e」按字面不可执行**：新用例引用新签名 → 旧代码下 `tests` 包**整包**编译失败，进不了测试；`git stash` 会连新测试一起收走 | 红证明改走 **HTTP 面**（不依赖新签名）：httptest 假 NetBox + 真 PG，旧代码断言 500 + 行数不增长，新代码断言 200 + 计数。红判据用**可观测项**（HTTP 码/行数），不断言 22001 文本（HTTP 面看不到） |

### 中（6 条）

| # | 发现 | 处置 |
|---|---|---|
| B-1 | §2 原样拷贝**编译失败**：缺 `log`/`strconv` import（rev1 只写「按需补」） | §2 给出**完整 import 块**；并记 `logFieldSanitization` 的形参必须是指针（值接收者会让 `go vet` printf 报错，已实测） |
| B-2 | §6 **漏 4 个签名连带调用点**：`db_smoke_test.go:643/667/1310/1489` | 已补进 §6 |
| B-3 | §4.1b 返回值清单不全 —— `SyncMetricsFromZabbix` 有 **7 个 return 点**；§3.3 GLPI 连 `fc` 声明位置和 return 点都没给 | 已补（7 个 return 点列全；三个循环各写明 `var fc fieldCounter` 的声明位置，Zabbix 那处标为「三个循环里最易写错」） |
| B-4 | **U6 零规格**（细节文档最该给的一段），而它决定 M7b 能否红 | §6 新增「U6 的断言规格」5 条：11 字段夹具、列宽从 `information_schema` 现查、**`==` 精确值**（`≤` 抓不到常量偏小）、计数、NUL 样本落库 |
| B-5 | **M11 被误判为「可能抓不到」** —— 仓库已有手段：`log.SetOutput` 捕获日志 + 假 Zabbix worker 基座 | §7 M11 改为**可抓**，给出具体做法与先例（`upsert_test.go:505-506`、`metric_sync_test.go:288-320`） |
| B-6 | 「SyncAll 只有 3 个键」在文档里没解释，读者无法判断是设计还是遗漏；openapi 要求列 4 个键也会误导为「同一次响应共存」 | §3.4 加警示块：`SyncAll` 不调指标路径 → 4 键**永不同时存在**，键随 `type` 而变；§5.3 openapi description 同步注明 |
| B-7 | **前端 3 处新文案零测试**（同类先例 `zabbix_truncated` 恰有专门用例） | §6 增 `Settings.test.tsx` 3 条断言；§5.1 补既有测试的**精确匹配硬约束**（`:727/743` 的 `toBe`、`:717` 的 `not.toMatch(/另有\s*1\s*条/)`）→ 文案必须是条件追加，且 Zabbix 不能用「另有 N 条」句式 |

### 低（7 条）

| # | 发现 | 处置 |
|---|---|---|
| C-1 | U1 的 `utf8.ValidString 恒真` **是假的**：裸 `TruncateRunes` 对非法 UTF-8 走快路原样返回（实测 `"\xff\xfe"`） | §1 断言改为**管线形态** `TruncateRunes(StripControl(src), max)`；另加一条裸函数 + 非法字节用例把这个契约钉住 |
| C-2 | §5.2 引错迁移行号：`000001_init.up.sql:64` 是 **permissions** 表，`audit_logs.resource` 在 `:1097` | 已订正（数值 50 本来就是对的） |
| C-3 | `audit.go:166` 实为 `:164` | 已订正 |
| C-4 | `08-部署运维.md` 在**仓库根**不在 `docs/`；G-45 引用在 `:364` | 已订正 |
| C-5 | `Settings.tsx` 的 NetBox handler **没有 `base` 变量**（rev1 示例引用了不存在的 `${base}`）；Zabbix 需**第三分支**而非「补一行」 | §5.1 改为逐 handler 的差异表 |
| C-6 | D9 写「测试 6 个文件」实为 **8** 个；U1 在两份文档里归属不一致 | 已订正为 8；U1 归 `redact_test.go`（更合理）并在 §6 写明 |
| C-7 | `fieldCounter` 是**新引入的模式**（全仓 `func String() string` 零命中；既有只有裸 int 计数器），文档未说明这一偏离 | §2 注释点明「新引入、既有先例是裸 int、裸 int 不够是因为 FIX-PLAN §3.2 要逐字段明细 + R3-b 要剥离明细」 |
| C-8 | `audit_logs.resource` 的 50 没有守卫（U7a/U7b 清单里没有它）→ 下次再漂没人知道 | §5.2 要求把 `resource` 纳入 U7b 校验清单 |

---

## §11 实现记录

（待实现后回填）
