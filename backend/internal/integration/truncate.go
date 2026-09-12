package integration

import (
	"log"
	"sort"
	"strconv"
	"strings"

	"network-monitor-platform/internal/redact"
)

// 第三方字段的列宽上限（TODO G-45 / docs/FIX-PLAN-TRUNCATION.md）。
//
// 这些常量是**冗余副本**，权威来源是迁移 DDL。三处必须一致：
//
//	迁移 DDL（权威）== 模型 gorm:"size:N" tag == 这里的常量
//
// 守卫：U7a（常量 == 模型 tag，纯 Go）+ U7b（常量 == information_schema，真 PG）。
// 改列宽的迁移必须同步改另外两处，否则守卫红。G-55 就是「第三处常量（audit 的 100）
// 大于真实列宽 50」漂了几轮没人发现 —— 所以这里的每个常量都必须有守卫。
const (
	colAssetName        = 255 // assets.name
	colAssetBrand       = 100 // assets.brand
	colAssetModel       = 100 // assets.model
	colAssetSN          = 100 // assets.sn
	colAssetSiteName    = 100 // assets.site_name（000013 追加）
	colAlertTriggerName = 500 // alerts.trigger_name（000013 追加）
	colAlertHostName    = 255 // alerts.host_name（000013 追加）
	colTicketTitle      = 255 // tickets.title
	colMetricKey        = 100 // metric_snapshots.key
	colAuditResource    = 50  // audit_logs.resource（M36 G-55：原值 100 与列宽 50 漂移；此处即权威源）
)

// ColumnWidths 把上面的 10 个常量打包成「table.column → 期望列宽」的映射，供跨包测试
// （db_smoke_test.go 里的 U7b）实时读取。这是 G-58 的修：U7b 之前把字面量钉在断言里，
// truncate.go 的常量若漂移（Go 常量漂、DDL 没漂）测试反而绿灯；现在走这张表，常量漂
// 直接红。
//
// 表的列与顺序与上方 const 块一一对应（5 个 assets、2 个 alerts、tickets.title、
// metric_snapshots.key、audit_logs.resource）；这是 G-55 的教训 —— 字面量必须跟源常量绑死，
// 而不能再复刻一份独立的「魔法数字」。
func ColumnWidths() map[string]int {
	return map[string]int{
		"assets.name":          colAssetName,
		"assets.brand":         colAssetBrand,
		"assets.model":         colAssetModel,
		"assets.sn":            colAssetSN,
		"assets.site_name":     colAssetSiteName,
		"alerts.trigger_name":  colAlertTriggerName,
		"alerts.host_name":     colAlertHostName,
		"tickets.title":        colTicketTitle,
		"metric_snapshots.key": colMetricKey,
		"audit_logs.resource":  colAuditResource,
	}
}

// sanitizeText 第三方字符串 → TEXT 列：只剥控制字符，**不截断**。
//
// TEXT 没有长度约束，但**照样拒 NUL**（PG 22021，字面量与 bind 参数都报），而它与有界列
// 处在同一条 CreateInBatches 事务里 —— 一条带 NUL 就整批回滚（FIX-PLAN F13）。所以
// 「TEXT 列不用管」是错的：这里不管长度，但仍必须过 StripControl。
//
// 保持纯函数，剥不剥由调用方经 fieldCounter.text 记账。
func sanitizeText(s string) (string, bool) {
	cleaned := redact.StripControl(s)
	return cleaned, cleaned != s
}

// fieldCounter 收集「哪些字段被截断 / 被剥离、各几次」，供日志明细与 API 计数用。
//
// 这是本包**新引入**的模式：既有代码只用裸计数器（service.go 的 truncated/skipped、
// metric_sync.go 的 skipped/written）。裸计数器不够，是因为需求要求日志含**逐字段
// 明细**（trigger_name×3），且「只剥不截」（如 problem 含 NUL）也必须可见 —— 后者若只
// 丢一个 bool 就是静默失败，正是这一轮要防的东西。
//
// 只记字段名与次数，**不记原值**：原值可能含 StripControl 不覆盖的 U+2028/U+202E 等
// 方向控制符，写进日志等于把不可见字符搬了个地方。
//
// 必须在使用它的函数内创建（函数局部，计数经返回值透出），不得落 IntegrationService
// 字段或包级变量：该 struct 是长生命周期共享对象，HTTP handler 与 worker goroutine
// 会并发调用（FIX-PLAN R5/D-10）。
type fieldCounter struct {
	truncated map[string]int // 字段 → 被截断次数（进 API 计数）
	stripped  map[string]int // 字段 → 仅被剥控制字符次数（只进日志，不进 API 计数）
}

// truncate 第三方字符串 → 有界列：先剥控制字符，再按字符截断。
//
// 顺序不可颠倒：先截断会把控制字符算进配额，等于用「不可见字符」挤掉真实内容
// （M9 的变异就是抓这个）。
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

// text 第三方字符串 → TEXT 列：只剥不截，剥离明细同样进日志。
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

// count 是透出给 API 的「被截断的字段处数」。**不含**只剥不截的（那个只进日志）：
// API 计数的语义是「有多少处的值被改短了」，剥离并未改短，混进去会让前端文案说谎。
func (c *fieldCounter) count() int {
	n := 0
	for _, v := range c.truncated {
		n += v
	}
	return n
}

// String 形如 "host_name×2,trigger_name×3,problem(stripped)×1"。
// 字段名排序，保证日志可逐字比对。零值 fieldCounter 返回 ""。接收者必须是指针：
// 值接收者会让 *fieldCounter 之外的值类型不满足 fmt.Stringer，go vet 的 printf
// 检查会红。
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

// logFieldSanitization 只在真有截断或剥离时打一行（无事发生的同步不该刷日志）。
//
// c 必须是指针（String 是指针接收者）；把 fieldCounter 按值传进 log.Printf 同样会
// 让 go vet 报 "format %s has arg of wrong type"。
func logFieldSanitization(path string, c *fieldCounter) {
	if c == nil || (c.count() == 0 && len(c.stripped) == 0) {
		return
	}
	log.Printf("[%s] 字段截断 %d 处，明细：%s", path, c.count(), c)
}
