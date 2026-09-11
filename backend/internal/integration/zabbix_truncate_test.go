package integration

import (
	"bytes"
	"context"
	"log"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"network-monitor-platform/internal/models"
)

// zabbixManyTriggers 渲染 n 条 trigger 的 result 数组。
//
// triggerid **必须互不相同**：降级分支下所有行的 problem_start 都是循环外同一个 now，
// 撞上 000027 的部分唯一索引后 ON CONFLICT DO NOTHING 会静默吞行，
// 「入库行数 == 上限」那条断言就会假红（是夹具撞车，不是代码错）。
func zabbixManyTriggers(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"triggerid":"trg-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","description":"CPU > 90%","priority":5,`)
		b.WriteString(`"hosts":[{"hostid":"1","host":"web-01"}],"value":"1","lastchange":"1756728000"}`)
	}
	b.WriteByte(']')
	return b.String()
}

// TestSyncFromZabbix_截断可见 守 M27/B：源侧超过上限时**必须留下可发现的痕迹**。
//
// 旧行为（limit=100）是静默丢弃：没有日志、没有计数、返回的 synced 还是被截断后的数，
// 运维从 UI 上完全看不出少了告警 —— 这正是 B 要消除的失败模式。
//
// ② 是边界对照：恰好多要 1 条（limit = 上限+1）才能区分「正好这么多」与「被截断」，
// 去掉那个 +1，② 会因「收到 == 上限」被误判成截断而红。
func TestSyncFromZabbix_截断可见(t *testing.T) {
	t.Run("① 超过上限 → truncated=1 且只入库上限条数", func(t *testing.T) {
		db := newUpsertTestDB(t)
		var buf bytes.Buffer
		oldOut := log.Default().Writer()
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(oldOut) })

		f := newZabbixFake(t, zabbixManyTriggers(zabbixTriggerLimit+1))
		n, truncated, err := f.service().SyncFromZabbix(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, truncated, "超过上限必须置截断标志")
		assert.Equal(t, zabbixTriggerLimit, n, "入库条数必须是上限（多要的那 1 条只用于判定）")

		var cnt int64
		require.NoError(t, db.Model(&models.Alert{}).Count(&cnt).Error)
		assert.Equal(t, int64(zabbixTriggerLimit), cnt, "库里也必须是上限条")

		assert.Contains(t, buf.String(), "超过上限",
			"截断必须打日志；静默丢弃正是 B 要消除的失败模式。实际日志: %q", buf.String())
	})

	t.Run("② 恰好等于上限 → truncated=0（边界）", func(t *testing.T) {
		db := newUpsertTestDB(t)

		f := newZabbixFake(t, zabbixManyTriggers(zabbixTriggerLimit))
		n, truncated, err := f.service().SyncFromZabbix(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, truncated, "正好这么多不是截断 —— 判据是「> 上限」")
		assert.Equal(t, zabbixTriggerLimit, n)

		var cnt int64
		require.NoError(t, db.Model(&models.Alert{}).Count(&cnt).Error)
		assert.Equal(t, int64(zabbixTriggerLimit), cnt)
	})
}

// TestGetTriggers_请求上限与不再拉items 守请求体本身。
//
// 断言请求体而不是响应，是因为这两件事**只在请求侧可见**：多要 1 条是为了让截断可判定，
// 去掉 selectItems 是为了不再白拉一份没有任何读取点的 items。
func TestGetTriggers_请求上限与不再拉items(t *testing.T) {
	f := newZabbixFake(t, `[]`)
	_, _, err := f.service().SyncFromZabbix(context.Background())
	require.NoError(t, err)

	require.Len(t, f.triggerParams, 1, "应当只发一次 trigger.get")
	params := f.triggerParams[0]

	// json 解进 map[string]any 后数字是 float64 —— 用 EqualValues 而不是 Equal，
	// 否则红在类型上而不是值上。
	assert.EqualValues(t, zabbixTriggerLimit+1, params["limit"],
		"请求上限必须是「上限+1」：源侧正好返回 limit 条时与「被截断」不可区分")

	_, hasItems := params["selectItems"]
	assert.False(t, hasItems,
		"不得再请求 selectItems（Trigger.Items 全包无读取点）—— 断言「键不存在」而不是「值为空」："+
			"空数组仍会占响应体积")
}
