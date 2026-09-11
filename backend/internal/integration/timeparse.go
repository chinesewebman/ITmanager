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

// glpiTZName 是 GLPI 实例所在时区（D-3）。
// 取值依据：仓库 docker-compose.yml:181 的 GLPI 容器 TZ=Asia/Shanghai。
// **待核对**：GLPI 实际部署时须确认（登记见 docs/FIX-PLAN-SYNC-FIDELITY.md §6）。
//
// 刻意不自动探测、不读服务器本地时区 —— api 容器 TZ 是 UTC，与 GLPI 不同源，
// 拿它当 GLPI 时区会稳定错 8 小时（需求 §1.8 低-3）。
const glpiTZName = "Asia/Shanghai"

var glpiLoc = mustLoadLocation(glpiTZName)

// mustLoadLocation 失败即 panic（init 期，进程直接起不来）。
// 不静默降级成 UTC：降级正好把「8 小时偏移」这个本函数存在的唯一理由引回来，
// 而且是最难发现的那种 —— 数据看着有值，只是全错 8 小时。
//
// 不内嵌 time/tzdata：backend/Dockerfile:24 已 `apk add --no-cache … tzdata`，
// 运行时镜像自带时区库。若哪天换成 scratch/distroless 基础镜像，这里的 panic
// 会立刻暴露 —— 是响的失败，不是静默错值。
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
//
//	按 UTC 直接解析：10:00 → 落库 "10:00" → 读回 10:00Z → dayjs 北京渲染 18:00 ❌
//	按 glpiLoc 再 .UTC()：10:00 → 落库 "02:00" → 读回 02:00Z → 渲染 10:00 ✅
//
// glpiLoc 不做入参：它是配置常量、不是每次调用变化的量，入参只会让调用点噪音化。
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
