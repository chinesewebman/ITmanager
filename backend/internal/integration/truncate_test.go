package integration

import (
	"bytes"
	"log"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"network-monitor-platform/internal/models"
)

// ==================== U2：sanitizeText / 剥离 ====================

func TestSanitizeText_剥控制字符保留可见内容(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		want        string
		wantChanged bool
	}{
		{"NUL", "a\x00b", "ab", true},
		{"DEL", "a\x7fb", "ab", true},
		{"LF", "a\nb", "ab", true},
		{"CR", "a\rb", "ab", true},
		{"HTAB", "a\tb", "ab", true},
		{"全是控制字符", "\x00\x01\x1f", "", true},
		{"干净串原样", "problem on web-01", "problem on web-01", false},
		{"空串", "", "", false},
		{"中文与 emoji 不受影响", "磁盘 🚨 告警", "磁盘 🚨 告警", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := sanitizeText(c.in)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.wantChanged, changed)
		})
	}
}

// ==================== U5：未超长不计数，且值逐字未变 ====================

func TestFieldCounter_未超长不计入且值不变(t *testing.T) {
	var fc fieldCounter

	in := "web-01"
	got := fc.truncate("host_name", in, colAlertHostName)

	assert.Equal(t, in, got, "未超长必须逐字未变")
	assert.Equal(t, 0, fc.count())
	assert.Equal(t, "", fc.String(), "无事发生时不产生明细")
	assert.Empty(t, fc.stripped)
}

// 正好等于上限也不算截断（边界：PG 允许 char_length == n）。
func TestFieldCounter_正好等于上限不计入(t *testing.T) {
	var fc fieldCounter

	in := strings.Repeat("中", 255)
	got := fc.truncate("name", in, colAssetName)

	assert.Equal(t, in, got)
	assert.Equal(t, 0, fc.count())
}

func TestFieldCounter_超长按字符截断并计数(t *testing.T) {
	var fc fieldCounter

	got := fc.truncate("name", strings.Repeat("中", 300), colAssetName)

	assert.Equal(t, 255, len([]rune(got)))
	assert.Equal(t, 1, fc.count())
	assert.Equal(t, "name×1", fc.String())
}

// 同一字段多次截断要累加；不同字段要能同时在明细里出现。
func TestFieldCounter_多字段累加(t *testing.T) {
	var fc fieldCounter

	long := strings.Repeat("中", 600)
	fc.truncate("host_name", long, colAlertHostName)
	fc.truncate("trigger_name", long, colAlertTriggerName)
	fc.truncate("host_name", long, colAlertHostName)

	assert.Equal(t, 3, fc.count())
	assert.Equal(t, "host_name×2,trigger_name×1", fc.String(), "明细按字段名排序")
}

// 只剥不截：不进 API 计数，但必须进日志明细（否则就是静默 —— 这一轮要防的正是它）。
func TestFieldCounter_只剥不截进明细不进计数(t *testing.T) {
	var fc fieldCounter

	got := fc.text("problem", "NUL\x00在这里")

	assert.Equal(t, "NUL在这里", got)
	assert.Equal(t, 0, fc.count(), "剥离没有改短字段，不得计入 API 计数")
	assert.Equal(t, "problem(stripped)×1", fc.String())
}

// 同一字段既剥又截：两种明细都要出现，且计数只算截断。
func TestFieldCounter_同字段既剥又截(t *testing.T) {
	var fc fieldCounter

	in := "\x00" + strings.Repeat("中", 300)
	got := fc.truncate("name", in, colAssetName)

	assert.Equal(t, 255, len([]rune(got)))
	assert.Equal(t, 1, fc.count())
	assert.Equal(t, "name×1", fc.String(), "剥与截同字段时不该出现两行同名明细")
}

// ==================== 日志守卫 ====================

func captureLog(t *testing.T, f func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	f()
	return buf.String()
}

func TestLogFieldSanitization_有截断或剥离才打日志(t *testing.T) {
	t.Run("有截断打明细", func(t *testing.T) {
		var fc fieldCounter
		fc.truncate("host_name", strings.Repeat("中", 300), colAlertHostName)

		out := captureLog(t, func() { logFieldSanitization("zabbix", &fc) })

		assert.Contains(t, out, "[zabbix] 字段截断 1 处")
		assert.Contains(t, out, "host_name×1")
	})

	t.Run("只剥不截也要打", func(t *testing.T) {
		var fc fieldCounter
		fc.text("problem", "a\x00b")

		out := captureLog(t, func() { logFieldSanitization("zabbix", &fc) })

		assert.Contains(t, out, "字段截断 0 处", "剥离不改短字段，计数为 0")
		assert.Contains(t, out, "problem(stripped)×1")
	})

	t.Run("无事发生不打", func(t *testing.T) {
		var fc fieldCounter
		fc.truncate("host_name", "web-01", colAlertHostName)

		out := captureLog(t, func() { logFieldSanitization("zabbix", &fc) })

		assert.Empty(t, out)
	})

	t.Run("nil 不 panic", func(t *testing.T) {
		out := captureLog(t, func() { logFieldSanitization("zabbix", nil) })
		assert.Empty(t, out)
	})
}

// ==================== U7a：常量 == 模型 gorm:"size:N" tag ====================

// 从 gorm tag 里取 size:N。tag 形如 "size:255;not null" / "type:text"。
func gormSize(t *testing.T, typ reflect.Type, field string) (int, bool) {
	t.Helper()
	sf, ok := typ.FieldByName(field)
	require.True(t, ok, "%s 上不存在字段 %s", typ.Name(), field)

	for _, part := range strings.Split(sf.Tag.Get("gorm"), ";") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(part), "size:"); found {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			require.NoError(t, err, "%s.%s 的 size tag 无法解析：%q", typ.Name(), field, rest)
			return n, true
		}
	}
	return 0, false
}

// 常量是迁移 DDL 的冗余副本，而第三份副本（模型 tag）最容易漂 —— G-55 就是漂了几轮
// 没人发现。这条纯 Go 用例把「常量 vs 模型」钉住；「常量 vs 真列宽」由真 PG 的 U7b 兜。
func Test列宽常量与模型size_tag一致(t *testing.T) {
	cases := []struct {
		constName string
		got       int
		typ       reflect.Type
		field     string
	}{
		{"colAssetName", colAssetName, reflect.TypeOf(models.Asset{}), "Name"},
		{"colAssetBrand", colAssetBrand, reflect.TypeOf(models.Asset{}), "Brand"},
		{"colAssetModel", colAssetModel, reflect.TypeOf(models.Asset{}), "Model"},
		{"colAssetSN", colAssetSN, reflect.TypeOf(models.Asset{}), "SN"},
		{"colAssetSiteName", colAssetSiteName, reflect.TypeOf(models.Asset{}), "SiteName"},
		{"colAlertTriggerName", colAlertTriggerName, reflect.TypeOf(models.Alert{}), "TriggerName"},
		{"colAlertHostName", colAlertHostName, reflect.TypeOf(models.Alert{}), "HostName"},
		{"colTicketTitle", colTicketTitle, reflect.TypeOf(models.Ticket{}), "Title"},
		{"colMetricKey", colMetricKey, reflect.TypeOf(models.MetricSnapshot{}), "Key"},
		{"colAuditResource", colAuditResource, reflect.TypeOf(models.AuditLog{}), "Resource"},
	}
	for _, c := range cases {
		t.Run(c.constName, func(t *testing.T) {
			want, ok := gormSize(t, c.typ, c.field)
			require.True(t, ok, "%s.%s 没有 size tag，列宽常量失去比对基准", c.typ.Name(), c.field)
			assert.Equal(t, want, c.got,
				"%s 与 %s.%s 的 size tag 不一致（列宽常量必须跟着迁移 DDL 走）",
				c.constName, c.typ.Name(), c.field)
		})
	}
}
