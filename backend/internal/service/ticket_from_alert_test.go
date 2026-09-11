package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ==================== Ticket.CreateFromAlert（TODO D-3 告警一键建单）====================
//
// 这组用例走**真 sqlite 库**（复用 diagnostic 用例建的 newDiagTestDB，含 alerts + tickets 表），
// 不匹配 SQL 文本 —— 本功能的要害是「认领 + 建票同事务」的原子性，sqlmock 只能验证发了哪条
// SQL，验证不了「插票失败时认领有没有跟着回滚」。

// seedAlertRow 插一行告警，返回补齐默认值后的实体（ID 由调用方从返回值里取）。
//
// 走裸 SQL 而不是 db.Create：newDiagTestDB 的 alerts 表是给 diagnostic 端点用的**裁剪版**，
// 没有 is_false_positive / marked_* 这几个模型字段，gorm 的 Create 会因为列不存在直接失败。
// ID 也必须显式给 —— sqlite 的 TEXT PRIMARY KEY 允许多个 NULL，靠 gen_random_uuid 默认值
// 会读回全零 UUID（G-24 轮踩过的同一个坑）。
func seedAlertRow(t *testing.T, db *gorm.DB, a models.Alert) models.Alert {
	t.Helper()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.SeverityName == "" {
		a.SeverityName = "Average"
	}
	if a.Status == "" {
		a.Status = "problem"
	}
	err := db.Exec(`INSERT INTO alerts
		(id, alert_id, asset_id, host_id, host_name, host_ip, trigger_name, trigger_id,
		 severity, severity_name, problem, problem_start, status, ticket_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AlertID, a.AssetID, a.HostID, a.HostName, a.HostIP, a.TriggerName, a.TriggerID,
		a.Severity, a.SeverityName, a.Problem, a.ProblemStart, a.Status, a.TicketID,
		time.Now().UTC(), time.Now().UTC()).Error
	require.NoError(t, err)
	return a
}

// alertTicketID 读回 alerts.ticket_id（NULL → uuid.Nil）。
func alertTicketID(t *testing.T, db *gorm.DB, alertID uuid.UUID) uuid.UUID {
	t.Helper()
	var got sql.NullString
	require.NoError(t, db.Raw(`SELECT ticket_id FROM alerts WHERE id = ?`, alertID).Scan(&got).Error)
	if !got.Valid || got.String == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(got.String)
	require.NoError(t, err, "ticket_id 应是合法 UUID")
	return id
}

func countTickets(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM tickets`).Scan(&n).Error)
	return n
}

func TestTicketService_CreateFromAlert_建单并回写关联(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	assetID := seedAsset(t, db, "core-sw-01")
	alert := seedAlertRow(t, db, models.Alert{
		AlertID:      "zbx-1001",
		AssetID:      &assetID,
		HostID:       &assetID,
		HostName:     "core-sw-01",
		HostIP:       "192.0.2.5",
		TriggerName:  "CPU 使用率超过 90%",
		TriggerID:    "trig-77",
		Severity:     3,
		SeverityName: "Average",
		Problem:      "CPU 持续 5 分钟高于 90%",
	})

	tk, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), Actor{Name: "yanru"})
	require.NoError(t, err)
	require.True(t, created, "首次建单 created 必须为 true")
	require.NotNil(t, tk)

	// 派生字段
	assert.Equal(t, "core-sw-01 CPU 使用率超过 90%", tk.Title)
	assert.Equal(t, "incident", tk.TicketType)
	assert.Equal(t, "normal", tk.Priority, "severity=3 应映射 normal（M16 归一到契约词表）")
	assert.Equal(t, "alert", tk.Source)
	assert.Equal(t, "yanru", tk.RequesterName)
	require.NotNil(t, tk.AssetID)
	assert.Equal(t, assetID, *tk.AssetID, "告警的资产应带到工单上")
	assert.Equal(t, "core-sw-01", tk.AssetName)
	assert.NotEmpty(t, tk.TicketNumber, "工单号由 BeforeCreate 生成")
	assert.Contains(t, tk.Description, "zbx-1001")
	assert.Contains(t, tk.Description, "192.0.2.5")

	// 落库 + 回写
	var stored models.Ticket
	require.NoError(t, db.First(&stored, "id = ?", tk.ID).Error)
	assert.Equal(t, tk.Title, stored.Title)
	assert.Equal(t, tk.ID, alertTicketID(t, db, alert.ID), "alerts.ticket_id 必须指回新工单")
}

// 重复点击（或两个运维同时点）只能有一张票 —— 本用例是 K-4「认领优先」的守门人：
// 变异去掉 `AND ticket_id IS NULL` 后，第二次调用会覆盖认领并多插一张票，此处变红。
func TestTicketService_CreateFromAlert_重复调用返回同一张票(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	alert := seedAlertRow(t, db, models.Alert{AlertID: "zbx-2", HostName: "web-01", TriggerName: "磁盘满", Severity: 4})

	first, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), Actor{Name: "yanru"})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), Actor{Name: "yanru"})
	require.NoError(t, err)
	assert.False(t, created, "第二次不应新建")
	assert.Equal(t, first.ID, second.ID, "重复调用必须拿到同一张票")
	assert.Equal(t, first.TicketNumber, second.TicketNumber)

	assert.Equal(t, int64(1), countTickets(t, db), "tickets 表只应有一行")
	assert.Equal(t, first.ID, alertTicketID(t, db, alert.ID), "关联不得被第二次调用改写")
}

func TestTicketService_CreateFromAlert_告警不存在返回ErrNotFound(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)

	tk, created, err := svc.CreateFromAlert(context.Background(), uuid.NewString(), Actor{Name: "yanru"})
	assert.Nil(t, tk)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, int64(0), countTickets(t, db), "失败路径不得留下工单")
}

// 查询失败不得被吞成「告警不存在」：404 会让运维以为告警没了，而实际只是这次查询挂了
// （表缺失 / 连接断 / 权限变更）。把 alerts 表删掉制造一次真实的读取失败（不 mock），
// 断言错误原样透传 —— 若有人把 `if err != nil` 一律写成 ErrNotFound，这条变红。
func TestTicketService_CreateFromAlert_查询失败不吞成NotFound(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	require.NoError(t, db.Exec(`DROP TABLE alerts`).Error)

	tk, created, err := svc.CreateFromAlert(context.Background(), uuid.NewString(), Actor{Name: "yanru"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound, "查询失败必须透传，不能伪装成 404")
	assert.Nil(t, tk)
	assert.False(t, created)
	assert.Equal(t, int64(0), countTickets(t, db), "失败路径不得留下工单")
}

// R-3：ticket_id 指向的工单已不存在（人工改库 / 外部写入）。本系统没有删除工单的端点，
// 属防御性分支 —— 断言报 404 而不是静默改写关联或留下半截状态。
func TestTicketService_CreateFromAlert_关联悬空返回ErrNotFound(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	dangling := uuid.New()
	alert := seedAlertRow(t, db, models.Alert{
		AlertID: "zbx-3", HostName: "db-01", TriggerName: "主从延迟", Severity: 5,
		TicketID: &dangling,
	})

	tk, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), Actor{Name: "yanru"})
	assert.Nil(t, tk)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, int64(0), countTickets(t, db))
	assert.Equal(t, dangling, alertTicketID(t, db, alert.ID), "悬空关联不得被悄悄改写")
}

// K-3：插票失败时认领必须**一起回滚**，否则 alerts.ticket_id 会指向一张不存在的工单。
//
// 造一个真实存在的撞号：工单号按「当天已有条数」取（`models.generateTicketNumber`），
// 删掉当天一条后序号会被算重 —— 这正是 G-25 记录的那条残留边界，拿它当失败注入点。
func TestTicketService_CreateFromAlert_插票失败时认领回滚(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	// 真 schema 里 ticket_number 有唯一索引，sqlite 夹具没建 —— 不补上就根本撞不了号，
	// 这条用例会变成永远为真的假绿。
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX ux_tickets_number ON tickets(ticket_number)`).Error)

	prefix := "TICKET-" + time.Now().Format("20060102") + "-"
	insertTicket := func(number string) {
		require.NoError(t, db.Exec(`INSERT INTO tickets (id, ticket_number, title, status, created_at, updated_at)
			VALUES (?, ?, '占位', 'open', ?, ?)`,
			uuid.New(), number, time.Now().UTC(), time.Now().UTC()).Error)
	}
	insertTicket(prefix + "A")
	insertTicket(prefix + "B")
	require.NoError(t, db.Exec(`DELETE FROM tickets WHERE ticket_number = ?`, prefix+"A").Error)
	// 此刻当天条数 = 1 → 新号算成 -B → 与存量的 -B 撞唯一索引

	alert := seedAlertRow(t, db, models.Alert{AlertID: "zbx-9", HostName: "h-1", TriggerName: "网卡 down"})

	tk, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), Actor{Name: "yanru"})
	require.Error(t, err, "撞号应让建单失败（撞号重试不适用：重试也要新事务）")
	assert.Nil(t, tk)
	assert.False(t, created)
	assert.Equal(t, uuid.Nil, alertTicketID(t, db, alert.ID),
		"插票失败必须把认领一起回滚，否则 ticket_id 悬空指向不存在的工单")

	// 出生行**不在这里断言**：它与工单同事务，而「插票失败后出生行残留」这一场景
	// 在真库上被 ticket_history.ticket_id → tickets(id) 的外键直接堵死（孤儿出生行
	// 根本插不进去），sqlite 夹具又没建这个外键 —— 两个基座上该断言都恒真，是假绿。
	// 变异反证（V-6：把出生行改写到事务外）已确认它红不了，故删除。
	// 「工单在、出生行不在」那一半由 TestTicketService_CreateFromAlert_写出生历史行 守；
	// 外键本身由 TestDBSmoke_TicketHistory 守。
}

// R-4：触发器名最长 500，工单标题是 varchar(255)。按 rune 截断，且不能切出非法 UTF-8。
func TestTicketService_CreateFromAlert_超长标题按rune截断(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)

	alert := seedAlertRow(t, db, models.Alert{
		AlertID:     "zbx-4",
		HostName:    "host-1",
		TriggerName: strings.Repeat("中", 300),
		Severity:    1,
	})

	tk, created, err := svc.CreateFromAlert(context.Background(), alert.ID.String(), Actor{Name: "yanru"})
	require.NoError(t, err)
	require.True(t, created)

	assert.Equal(t, 255, len([]rune(tk.Title)), "按字符截到 255")
	assert.True(t, utf8.ValidString(tk.Title), "不得把汉字切一半留下非法 UTF-8")
	// 按 byte 截断会在这里露馅：7 字节前缀 + 248 字节汉字 = 82.67 个汉字 → 切在字中间，
	// 字节数虽然也是 255 以内，但 ValidString 为 false。
	assert.Greater(t, len(tk.Title), 255, "255 个汉字远超 255 字节，证明不是按 byte 截的")
}

// 空字段的退化路径：没有触发器名就用 problem，都没有就用 alert_id；工单标题仍要有内容。
func TestTicketService_CreateFromAlert_标题退化(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)

	cases := []struct {
		name  string
		alert models.Alert
		want  string
	}{
		{"无触发器名用 problem", models.Alert{AlertID: "z1", Problem: "接口 down"}, "接口 down"},
		{"都空用 alert_id", models.Alert{AlertID: "z2"}, "告警 z2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			alert := seedAlertRow(t, db, tc.alert)
			tk, _, err := svc.CreateFromAlert(context.Background(), alert.ID.String(), Actor{Name: "ops"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, tk.Title)
		})
	}
}

func TestPriorityFromSeverity_映射表(t *testing.T) {
	want := map[int]string{
		0: "low", 1: "low", // Not classified / Information
		2: "normal", 3: "normal", // Warning / Average
		4: "high", 5: "critical", // High / Disaster
		6: "critical", // 越界按最高级别兜底
	}
	for sev, exp := range want {
		assert.Equal(t, exp, priorityFromSeverity(sev), "severity=%d", sev)
	}
}

// M16：本函数的输出必须落在 openapi Ticket.priority 的词表内。
// 原先返回 medium —— 与契约的 normal 是同一个「普通」的两套拼法，后果是工单页按
// 「普通」筛选（WHERE priority='normal'）查不到这里建出来的票。这条断言把词表钉死：
// 日后有人再加一个拼法（'中' / 'moderate'），这里先红。
func TestPriorityFromSeverity_输出限定在契约词表内(t *testing.T) {
	// 与 openapi.yaml 的 Ticket.priority enum 一一对应，改动需同步契约
	vocab := map[string]bool{"low": true, "normal": true, "high": true, "critical": true}
	for sev := -1; sev <= 8; sev++ {
		got := priorityFromSeverity(sev)
		assert.True(t, vocab[got], "severity=%d 产出 %q，不在契约词表内", sev, got)
		assert.NotEqual(t, "medium", got,
			"severity=%d 产出了 medium —— 该拼法已由迁移 000023 归一为 normal，不得再引入", sev)
	}
}

// ==================== M25 步骤 5c：CreateFromAlert 的出生留痕 ====================

// historyRowsOf 读某张票的历史行（**含出生行**）。
//
// 不能用 ticket_service_test.go 的 historyOf：它断言每行 field_name 非空 —— 那是
// kind=updated 的形状。出生行的 field_name 按设计就是 NULL（那一次改的不是某个字段，
// 而是「这张票存在了」），套用那个 helper 会直接 require 失败。
func historyRowsOf(t *testing.T, db *gorm.DB, ticketID uuid.UUID) []models.TicketHistory {
	t.Helper()
	var rows []models.TicketHistory
	require.NoError(t, db.Where("ticket_id = ?", ticketID).Order("created_at, id").Find(&rows).Error)
	return rows
}

func countHistory(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM ticket_history`).Scan(&n).Error)
	return n
}

// 告警一键建单必须留下出生行。这条路径是值班时**最常用**的建单方式（Zabbix 报警 → 点建单），
// 而它此前是全仓唯一一条「人点了、库里没有任何经手记录」的路径 —— 运营打开这张票看到的是
// 空时间线，读起来像「建了以后没人碰过」，而事实是系统刚从一条告警把它建出来。
func TestTicketService_CreateFromAlert_写出生历史行(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)

	alert := seedAlertRow(t, db, models.Alert{AlertID: "zbx-5c", HostName: "core-sw-02", TriggerName: "端口 err"})
	actorID := uuid.New()
	actor := Actor{ID: &actorID, Name: "yanru"}

	tk, created, err := svc.CreateFromAlert(context.Background(), alert.ID.String(), actor)
	require.NoError(t, err)
	require.True(t, created)

	rows := historyRowsOf(t, db, tk.ID)
	require.Len(t, rows, 1, "一次建单恰好一行出生记录")

	row := rows[0]
	assert.Equal(t, models.TicketHistoryKindCreated, row.Kind)
	assert.Equal(t, tk.ID, row.TicketID, "出生行必须挂在刚建的这张票上，不是零值 UUID")
	assert.NotEqual(t, uuid.Nil, row.BatchID, "出生批次要有真实 batch_id（UI 靠它分组）")
	// 出生改的不是某个字段，而是「这张票存在了」—— 三列皆 NULL。
	assert.Nil(t, row.FieldName, "出生行的 field_name 必须是 NULL")
	assert.Nil(t, row.OldValue)
	assert.Nil(t, row.NewValue)

	// actor 必须完整落库（步骤 5c 把签名从裸 userID 改成 Actor，就是为了这个 ID）。
	require.NotNil(t, row.ActorID, "actor_id 不能丢 —— 丢了就只剩名字，同名的人分不开")
	assert.Equal(t, actorID, *row.ActorID)
	assert.Equal(t, "yanru", row.ActorName)

	// D-6：历史行的 source 保持空串，来路由 tickets.source 承载。
	assert.Equal(t, "", row.Source, "历史行不填 source（D-6：来路读 tickets.source）")
	assert.Equal(t, "", row.RequestID, "request_id 当前无中间件可填（D-6）")
	// 而工单自己的 source 正是时间线要显示的「从告警来」。
	assert.Equal(t, "alert", tk.Source)
}

// 幂等路径不留第二行：重复点击（或两个运维同时点）拿到的是同一张既有票，
// 那次调用**没有建单**，因此不该产生任何历史。
func TestTicketService_CreateFromAlert_幂等路径不重复留痕(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	alert := seedAlertRow(t, db, models.Alert{AlertID: "zbx-5c-2", HostName: "web-02", TriggerName: "内存高"})
	actor := Actor{Name: "yanru"}

	first, created, err := svc.CreateFromAlert(ctx, alert.ID.String(), actor)
	require.NoError(t, err)
	require.True(t, created)

	_, created, err = svc.CreateFromAlert(ctx, alert.ID.String(), actor)
	require.NoError(t, err)
	assert.False(t, created, "第二次不应新建")

	rows := historyRowsOf(t, db, first.ID)
	assert.Len(t, rows, 1, "没建单的那次调用不得留下历史行")
	assert.Equal(t, int64(1), countHistory(t, db))
}
