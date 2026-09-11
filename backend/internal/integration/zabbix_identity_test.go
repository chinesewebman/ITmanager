package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/models"
)

// newZabbixFake 起一个**可参数化**的 Zabbix 假服务端：登录固定成功，trigger.get 原样
// 吐调用方给的 result 数组，并把每次 trigger.get 的 params 记下来供断言请求体。
//
// 为什么不复用 upsert_test.go 的 fakeZabbixServer：它把 triggerid / 单行 result /
// 单一 lastChange 全写死在构造期，表达不了「同一 trigger 两个不同 lastchange」；
// 而且用 r.Body.Read 只读一次 —— 请求体超过一次读到的字节数时 JSON 会被**静默截断**，
// 断言请求体（截断用例要查 params.limit）时正好是那种情形。这里统一 io.ReadAll。
type zabbixFake struct {
	*httptest.Server
	triggerParams []map[string]any
}

func newZabbixFake(t *testing.T, triggersJSON string) *zabbixFake {
	t.Helper()
	f := &zabbixFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 Zabbix 请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case bytes.Contains(raw, []byte(`"user.login"`)):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"tok-1","id":1}`))
		case bytes.Contains(raw, []byte(`"trigger.get"`)):
			var req struct {
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Errorf("解析 trigger.get 请求体失败: %v（原始 %s）", err, raw)
			}
			f.triggerParams = append(f.triggerParams, req.Params)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":` + triggersJSON + `,"id":2}`))
		default:
			t.Errorf("未预期的 Zabbix 请求: %s", raw)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *zabbixFake) service() *IntegrationService {
	return &IntegrationService{
		zabbix: NewZabbixClient(&config.ZabbixConfig{URL: f.URL, User: "admin", Password: "p"}, nil),
	}
}

// zabbixOneTrigger 渲染单条 trigger 的 result 数组。lastChange 为空时**整个字段省略**
// —— 空串与字段缺失在 parseUnixSeconds 里都是 timeAbsent，两种源侧形态都要能走降级。
func zabbixOneTrigger(triggerID, lastChange string) string {
	lc := ""
	if lastChange != "" {
		lc = `,"lastchange":"` + lastChange + `"`
	}
	return `[{"triggerid":"` + triggerID + `","description":"CPU > 90%","priority":5,` +
		`"hosts":[{"hostid":"1","host":"web-01"}],"value":"1"` + lc + `}]`
}

// M27/A 的两个夹具时刻：**同一 UTC 日、相差 60 秒**。
// 同日是刻意的 —— 若有人把身份键从 Unix 秒改成日粒度，第 4 行必须跟着变红，
// 而只有「同日不同秒」才让那个变异可证伪（IMPL §8 的 M3）。
const (
	zabbixOldSec = 1756728000 // 2025-09-01 12:00:00 UTC
	zabbixNewSec = 1756728060 // 2025-09-01 12:01:00 UTC
)

// seedZabbixAlert 预置一行 Zabbix 告警。
//
// 走 db.Create(&models.Alert{...}) 而**不是**裸 SQL：裸 SQL 存 "2026-09-01 12:00:00"、
// gorm 存带偏移的形态，两者字面不等 → 与待插入行根本不冲突，用例会**静默测不到东西**
// （M27 审查时第一版 RowsAffected 探针就踩了这个坑）。
func seedZabbixAlert(t *testing.T, db *gorm.DB, status string, start time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&models.Alert{
		TriggerID: "100", TriggerName: "CPU > 90%", HostName: "web-01",
		Status: status, Source: "zabbix", Severity: 5, ProblemStart: start,
	}).Error, "预置 %s 行失败", status)
}

// TestSyncFromZabbix_身份判据六行表 是 M27/A 的行为契约：去重键 = 「同一 trigger 的
// 同一次故障发生」，lastchange 不可用时退到「同 trigger 且未解决」。
//
// ⚠️ 各行的**证伪能力不同**，别把它们一律当成某条实现的守门人（T-47 的教训：
// 「断言没跑」和「没守住」是两件事）：
//   - 第 5/6 行守**降级判据 `open`**：删掉 open 查询，第 5 行会插进去 → n=1≠0，红。
//   - 第 4 行守**「不得按 trigger_id 一把抓」**：把去重退化成「trigger 命中即跳过」
//     → 真实的第二次故障被静默丢弃 → n=0≠1，红。它**不**守 exact 的键粒度
//     （那是 M3 变异：键改日粒度，靠第 4 行的两个时刻同日不同秒才可证伪）。
//   - 第 1/2/3 行守**整条链**（Go 判据 + 000027 的部分唯一索引 + ON CONFLICT）。
//     它们**不能**单独证明 exact 查询有用：把 exact 的查询那两行删掉，待插入行照旧
//     撞唯一索引被 ON CONFLICT 吞掉，n 仍是 0（假绿）。exact 的价值是「少一次注定
//     失败的往返 + 语义写在代码里」，真正的兜底是索引 —— 索引那条路由真 PG 用例守
//     （TestDBSmoke_ZabbixSyncOnConflict 的注入式混合批）。
func TestSyncFromZabbix_身份判据六行表(t *testing.T) {
	cases := []struct {
		name         string
		seededStatus string
		seededStart  time.Time
		lastChange   string
		wantSynced   int
		wantRows     int
	}{
		{"① 本地 problem 同行 → 跳过", "problem", time.Unix(zabbixOldSec, 0).UTC(), "1756728000", 0, 1},
		{"② 本地 acknowledged 同行 → 跳过（G-27 靶心）", "acknowledged", time.Unix(zabbixOldSec, 0).UTC(), "1756728000", 0, 1},
		{"③ 本地 resolved 同行 → 跳过", "resolved", time.Unix(zabbixOldSec, 0).UTC(), "1756728000", 0, 1},
		{"④ 同 trigger 但故障时刻不同 → 必须入库（反向用例）", "problem", time.Unix(zabbixOldSec, 0).UTC(), "1756728060", 1, 2},
		{"⑤ 无 lastchange + 本地 acknowledged → 降级跳过", "acknowledged", time.Time{}, "", 0, 1},
		{"⑥ 无 lastchange + 本地 resolved → 降级不算已存在", "resolved", time.Time{}, "", 1, 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newUpsertTestDB(t)
			seedZabbixAlert(t, db, c.seededStatus, c.seededStart)

			f := newZabbixFake(t, zabbixOneTrigger("100", c.lastChange))
			n, err := f.service().SyncFromZabbix(context.Background())
			require.NoError(t, err)
			assert.Equal(t, c.wantSynced, n, "synced 计数（= 同事务 COUNT 前后差）")

			var rows []models.Alert
			require.NoError(t, db.Where("trigger_id = ?", "100").Order("problem_start").Find(&rows).Error)
			require.Len(t, rows, c.wantRows, "库内行数")

			if c.wantRows > 1 {
				// 第 4/6 行的正向半边：新行必须落在**源侧给的**故障时刻上
				// —— 落成同步时刻（now）说明 lastchange 没被用上。
				newest := rows[len(rows)-1]
				assert.Equal(t, "problem", newest.Status, "新插入的行状态恒为 problem")
				if c.lastChange != "" {
					assert.Equal(t, int64(zabbixNewSec), newest.ProblemStart.Unix(),
						"新行的 problem_start 必须是源侧的 lastchange")
				} else {
					assert.False(t, newest.ProblemStart.IsZero(),
						"降级回落也要写 now，不能留零值（零值会让 SLA 窗口算不出来）")
				}
			}
		})
	}
}

// TestSyncFromZabbix_空结果与无主机行 覆盖两条前期 return 分支。
// 两条都是真实行为而非错误路径：源侧当前没有 firing 的告警（正常状态，每次轮询都会发生），
// 以及 trigger 没挂主机（没有宿主就没有告警对象，插进去会是一行没有 host 的孤儿）。
func TestSyncFromZabbix_空结果与无主机行(t *testing.T) {
	t.Run("源侧无告警 → 0 且不查库", func(t *testing.T) {
		db := newUpsertTestDB(t)
		f := newZabbixFake(t, `[]`)
		n, err := f.service().SyncFromZabbix(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, n)
		var cnt int64
		require.NoError(t, db.Model(&models.Alert{}).Count(&cnt).Error)
		assert.Zero(t, cnt)
	})

	t.Run("trigger 无主机 → 跳过，不入库", func(t *testing.T) {
		db := newUpsertTestDB(t)
		f := newZabbixFake(t, `[{"triggerid":"100","description":"孤儿 trigger",`+
			`"priority":5,"hosts":[],"value":"1","lastchange":"1756728000"}]`)
		n, err := f.service().SyncFromZabbix(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, n, "没有 host 的 trigger 不该产生告警行（ConvertToAlert 会读 Hosts[0]）")
		var cnt int64
		require.NoError(t, db.Model(&models.Alert{}).Count(&cnt).Error)
		assert.Zero(t, cnt)
	})
}

// TestSyncFromZabbix_预置行不是zabbix来源时不参与去重 守预过滤的 source 收窄。
//
// 000027 的索引谓词带 source='zabbix'，Go 侧预查也带了 —— 两边必须同判据。
// 若 Go 侧漏掉 source，一个 manual 行会被当成「已存在」把真实的 Zabbix 告警挡在门外
// （静默丢告警）；若索引漏掉 source，则将来第二个写 trigger_id 的来源会与本表相撞。
func TestSyncFromZabbix_预置行不是zabbix来源时不参与去重(t *testing.T) {
	db := newUpsertTestDB(t)
	require.NoError(t, db.Create(&models.Alert{
		TriggerID: "100", TriggerName: "同 trigger 的 manual 行", HostName: "web-01",
		Status: "problem", Source: "manual", Severity: 5,
	}).Error)

	f := newZabbixFake(t, zabbixOneTrigger("100", "1756728000"))
	n, err := f.service().SyncFromZabbix(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "manual 行不是「同一个告警」，不得把 Zabbix 告警挡在门外")

	var n2 int64
	require.NoError(t, db.Model(&models.Alert{}).Where("source = ?", "zabbix").Count(&n2).Error)
	assert.Equal(t, int64(1), n2)
}
