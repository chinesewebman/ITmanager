package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"network-monitor-platform/internal/models"
)

// testActor 单测里的经手人。这些用例关心的是工单字段本身，归属用固定值即可 ——
// 「ctx 里的用户被正确解析成 Actor」由 handler 侧用例守（actor_test.go）。
func testActor() Actor { return Actor{Name: "tester"} }

// ==================== Ticket Service 测试 ====================

func TestTicketService_Get_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "title", "status", "priority"}).
		AddRow(id, "Test ticket", "open", "high")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Test ticket", got.Title)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Get(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTicketService_Create_空title返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: ""} //nolint:exhaustruct
	err := svc.Create(ctx, tk, testActor())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestTicketService_List_空filter返列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(uuid.NewString(), "t-1", "open").
		AddRow(uuid.NewString(), "t-2", "closed")

	// List 走 query + count 两条 SQL
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(`SELECT \* FROM "tickets"`).
		WillReturnRows(rows)

	list, total, err := svc.List(ctx, TicketFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, list, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_List_cursor模式不跑Count(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	cursorTS := time.Now()
	cursorID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(uuid.NewString(), "t-1", "open")

	// cursor 模式只跑一条 Find，不跑 count(*)（P19 修复点：Count 已挪到 offset 分支）。
	// 若 cursor 模式仍跑 Count，其 SQL `SELECT count(*) FROM "tickets"` 不匹配此期望，
	// sqlmock 会报 "not expected"，List 返回 err → 本用例红。
	mock.ExpectQuery(`SELECT \* FROM "tickets"`).
		WillReturnRows(rows)

	list, total, err := svc.List(ctx, TicketFilter{CursorTS: cursorTS, CursorID: cursorID})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total) // cursor 模式 total 恒 0（hasMore 检测在调用方）
	assert.Len(t, list, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_空updates返当前(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	// 空 updates 走 Get 返回当前（不调 Updates）
	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "unchanged", "open")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Update(ctx, id, map[string]interface{}{}, testActor())
	require.NoError(t, err)
	assert.Equal(t, "unchanged", got.Title)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// newTicketSQLiteDB 真 sqlite（手写 DDL，列必须与 models.Ticket 全字段一一对应，
// 少一列 gorm 的 INSERT 就会报 "no such column"）。
// 不用 AutoMigrate：models.Ticket.ID 带 `default:gen_random_uuid()`，sqlite 上没有
// 该函数，AutoMigrate 必炸（同 channel_service_test.go / cmd/seed/main_test.go 的既有做法）。
func newTicketSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE tickets (
		id TEXT PRIMARY KEY,
		ticket_number TEXT,
		title TEXT,
		description TEXT,
		ticket_type TEXT,
		priority TEXT,
		status TEXT,
		requester_id TEXT,
		requester_name TEXT,
		requester_email TEXT,
		assignee_id TEXT,
		assignee_name TEXT,
		category TEXT,
		tags TEXT,
		asset_id TEXT,
		asset_name TEXT,
		external_id TEXT,
		source TEXT,
		resolution TEXT,
		resolved_at DATETIME,
		closed_at DATETIME,
		due_date DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	createTicketHistoryTable(t, db)
	return db
}

// createTicketHistoryTable 建 ticket_history，与迁移 000025 同构（列名/可空性一致），
// 只去掉 PG 专有默认值：主键无 gen_random_uuid()（sqlite 没有），created_at 无
// DEFAULT NOW()（由 gorm 填）。少了这张表，留痕路径会以 "no such table" 全红 ——
// 那不是断言在守，是基座缺件。
//
// 抽成一处而不是每个夹具各写一份：任何写路径（Create / Update / CreateFromAlert）
// 都要往这张表里写，夹具少建一次，那组用例就整组假红/假绿。同一条 DDL 抄两遍迟早
// 只有一份被改（M25 步骤 5c 就是撞上 CreateFromAlert 那一组缺表）。
func createTicketHistoryTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE ticket_history (
		id TEXT PRIMARY KEY,
		ticket_id TEXT NOT NULL,
		batch_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		field_name TEXT,
		old_value TEXT,
		new_value TEXT,
		actor_id TEXT,
		actor_name TEXT,
		source TEXT,
		request_id TEXT,
		created_at DATETIME
	)`).Error)
}

// 安全审计 H-1 同款（channel_service.go 的先例）：handler 把请求体绑成 map 直接进
// gorm 的 Updates(map)，gorm 对每个键走 Schema.LookUpField —— 先列名、再 Go 字段名。
// 于是 {"id": …} 改主键（D-3 的 alerts.ticket_id 随即悬空）、{"TicketNumber": …}
// 走 Go 字段名那条路改工单号；{"CreatedAt": …} 让审计时间线失真。
func TestTicketService_Update_禁改列被拒(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T) (*gorm.DB, TicketService, uuid.UUID) {
		t.Helper()
		db := newTicketSQLiteDB(t)
		id := uuid.New()
		require.NoError(t, db.Create(&models.Ticket{ //nolint:exhaustruct
			ID: id, TicketNumber: "TICKET-20260101-A", Title: "原标题", Status: "open",
			CreatedAt: created, UpdatedAt: created,
		}).Error)
		return db, NewTicketService(db), id
	}

	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"小写主键id", map[string]interface{}{"id": uuid.New().String()}},
		{"Go字段名ID", map[string]interface{}{"ID": uuid.New().String()}},
		{"小写工单号", map[string]interface{}{"ticket_number": "TICKET-20260101-Z"}},
		{"Go字段名TicketNumber", map[string]interface{}{"TicketNumber": "TICKET-20260101-Z"}},
		{"小写created_at", map[string]interface{}{"created_at": "2020-01-01T00:00:00Z"}},
		{"Go字段名CreatedAt", map[string]interface{}{"CreatedAt": "2020-01-01T00:00:00Z"}},
		{"Go字段名UpdatedAt", map[string]interface{}{"UpdatedAt": "2020-01-01T00:00:00Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, svc, id := seed(t)
			_, err := svc.Update(context.Background(), id.String(), tc.updates, testActor())
			require.ErrorIs(t, err, ErrInvalidInput)

			var after models.Ticket
			require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
			assert.Equal(t, id, after.ID, "主键不得被改写")
			assert.Equal(t, "TICKET-20260101-A", after.TicketNumber, "工单号不得被改写")
			assert.Equal(t, created.UTC(), after.CreatedAt.UTC(), "created_at 不得被改写")
		})
	}

	t.Run("正控_业务列用Go字段名写法照常更新", func(t *testing.T) {
		db, svc, id := seed(t)
		_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{
			"Title": "新标题", "Status": "closed",
		}, testActor())
		require.NoError(t, err)

		var after models.Ticket
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Equal(t, "新标题", after.Title)
		assert.Equal(t, "closed", after.Status)
		// 归一化后 closed_at 判定也认 Go 字段名写法（改前 {"Status":"closed"} 不写 closed_at）
		assert.NotNil(t, after.ClosedAt, "关闭走 Go 字段名写法也必须写 closed_at")
	})
}

// ==================== M18：枚举列取值收口 ====================

// 与 openapi.yaml:2448-2453 的 Ticket.priority / Ticket.status enum 一一对应。
// 断言的是**集合相等**（不是「包含于」）：日后往词表里加一个拼法（'moderate' / '紧急'）
// 这里先红 —— M16 的 medium/normal 两套拼法就是这么来的。
func TestTicketEnumValues_与契约词表一致(t *testing.T) {
	assert.Equal(t, map[string]bool{
		"critical": true, "high": true, "normal": true, "low": true,
	}, ticketPriorityValues)
	assert.Equal(t, map[string]bool{
		"open": true, "in_progress": true, "pending": true, "resolved": true, "closed": true,
	}, ticketStatusValues)
}

// 词表外的值此前不报错、照样落库，然后从用户视野里**静默消失**：
// 工单页筛选器只有契约那几档选不中它，TicketStatsCards 的 `if (t.status in acc)`
// 不计数，TicketTable 的 PRIORITY_WEIGHT 查不到键 → 排序垫底。
func TestTicketService_Create_枚举列取值越界被拒(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*models.Ticket)
	}{
		{"priority 同义词 medium（M16 已归一，不得再引入）", func(tk *models.Ticket) { tk.Priority = "medium" }},
		{"priority 自由文本 urgent", func(tk *models.Ticket) { tk.Priority = "urgent" }},
		{"priority 大小写不宽容 HIGH", func(tk *models.Ticket) { tk.Priority = "HIGH" }},
		{"priority 中文 紧急", func(tk *models.Ticket) { tk.Priority = "紧急" }},
		{"status 自由文本 waiting", func(tk *models.Ticket) { tk.Status = "waiting" }},
		// 收了 "Closed" 就回到「已关闭但无关闭时间」：closed_at 判定是精确比较
		// status == "closed"（M17 修的就是这条裂缝的键侧，值侧同理）
		{"status 大小写不宽容 Closed", func(tk *models.Ticket) { tk.Status = "Closed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTicketSQLiteDB(t)
			svc := NewTicketService(db)
			tk := &models.Ticket{Title: "工单"} //nolint:exhaustruct
			tc.mut(tk)

			err := svc.Create(context.Background(), tk, testActor())
			require.ErrorIs(t, err, ErrInvalidInput)

			var n int64
			require.NoError(t, db.Model(&models.Ticket{}).Count(&n).Error)
			assert.Zero(t, n, "校验失败不得落库")
		})
	}
}

func TestTicketService_Create_枚举列契约值通过(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	tk := &models.Ticket{Title: "工单", Priority: "critical", Status: "in_progress"} //nolint:exhaustruct
	require.NoError(t, svc.Create(context.Background(), tk, testActor()))

	var got models.Ticket
	require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
	assert.Equal(t, "critical", got.Priority)
	assert.Equal(t, "in_progress", got.Status)
}

func TestTicketService_Update_枚举列取值越界被拒(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T) (*gorm.DB, TicketService, uuid.UUID) {
		t.Helper()
		db := newTicketSQLiteDB(t)
		id := uuid.New()
		require.NoError(t, db.Create(&models.Ticket{ //nolint:exhaustruct
			ID: id, TicketNumber: "TICKET-20260101-A", Title: "原标题",
			Status: "open", Priority: "normal",
			CreatedAt: created, UpdatedAt: created,
		}).Error)
		return db, NewTicketService(db), id
	}

	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"priority 小写键自由文本", map[string]interface{}{"priority": "urgent"}},
		{"priority Go字段名写法", map[string]interface{}{"Priority": "urgent"}},
		{"priority 同义词 medium", map[string]interface{}{"priority": "medium"}},
		{"status 小写键自由文本", map[string]interface{}{"status": "waiting"}},
		{"status Go字段名写法且大小写不符", map[string]interface{}{"Status": "Closed"}},
		// fail-closed（docs/TRAPS.md T-36）：`if s, ok := v.(string); ok { 校验 }` 这种
		// 写法会让下面这些形态静默放行 —— null 还会撞 status NOT NULL 变成 500。
		{"status 为 null", map[string]interface{}{"status": nil}},
		{"priority 为数字", map[string]interface{}{"priority": 3}},
		{"priority 为布尔", map[string]interface{}{"priority": true}},
		{"status 为对象", map[string]interface{}{"status": map[string]interface{}{"a": 1}}},
		{"status 为数组", map[string]interface{}{"status": []string{"open"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, svc, id := seed(t)
			_, err := svc.Update(context.Background(), id.String(), tc.updates, testActor())
			require.ErrorIs(t, err, ErrInvalidInput)

			var after models.Ticket
			require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
			assert.Equal(t, "open", after.Status, "状态不得被改写")
			assert.Equal(t, "normal", after.Priority, "优先级不得被改写")
		})
	}

	t.Run("正控_契约值照常更新", func(t *testing.T) {
		db, svc, id := seed(t)
		_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{
			"Priority": "high", "Status": "closed",
		}, testActor())
		require.NoError(t, err)

		var after models.Ticket
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Equal(t, "high", after.Priority)
		assert.Equal(t, "closed", after.Status)
		// 校验放在 closed_at 判定之前，但不能把正常路径挡住
		assert.NotNil(t, after.ClosedAt, "契约值 closed 仍须写 closed_at")
	})
}

// modelsTicket 测试辅助已用真 models.Ticket，hack helper 删

// ==================== Create 补全 ====================

func TestTicketService_Create_成功_默认值生效(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "新工单"} //nolint:exhaustruct

	// gorm Create 自动开事务, BeforeCreate 钩子在事务内 SELECT count(*) 生成 ticket number
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(`INSERT INTO "ticket_history"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "created", nil, nil, nil, nil, "tester", "", "", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	err := svc.Create(ctx, tk, testActor())
	require.NoError(t, err)
	assert.Equal(t, "open", tk.Status, "Status 默认 open")
	assert.Equal(t, "manual", tk.Source, "Source 默认 manual")
	assert.Equal(t, "[]", tk.Tags, "Tags 默认 []")
	// M16：不传 priority 的 POST /tickets 原来会落一行 priority=''（既筛不出也不显示，
	// 表格里优先级列空白）。handler 直接 bind 模型不校验，兜底只能落在这里。
	assert.Equal(t, "normal", tk.Priority, "Priority 默认 normal（契约词表内的值）")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Create_传值保留(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{
		Title:    "新工单",
		Status:   "in_progress",
		Source:   "alert",
		Tags:     `["p1"]`,
		Priority: "high",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(`INSERT INTO "ticket_history"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "created", nil, nil, nil, nil, "tester", "", "", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	err := svc.Create(ctx, tk, testActor())
	require.NoError(t, err)
	assert.Equal(t, "in_progress", tk.Status, "已传 Status 不覆盖")
	assert.Equal(t, "alert", tk.Source)
	assert.Equal(t, `["p1"]`, tk.Tags)
	assert.Equal(t, "high", tk.Priority, "已传 Priority 不得被默认值覆盖")
}

func TestTicketService_Create_nil指针返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewTicketService(gormDB)
	err := svc.Create(context.Background(), nil, testActor())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestTicketService_Create_唯一冲突后重试成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "retry"}
	// 第 1 次：工单号撞唯一索引 → 回滚
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnError(&pqUniqueError{msg: "duplicate key value violates unique constraint"})
	mock.ExpectRollback()
	// 第 2 次：重新生成工单号后成功
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(`INSERT INTO "ticket_history"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "created", nil, nil, nil, nil, "tester", "", "", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	require.NoError(t, svc.Create(ctx, tk, testActor()), "唯一冲突应重试而非直接失败（缺陷 D-2）")
	// 关键断言：第 2 次必须真的**重新生成**了号（count=1 → B）。只断言 NoError 是假绿 ——
	// 实现若忘了清空 TicketNumber，第 2 次 INSERT 会用同一个号，mock 照样返回成功。
	assert.Equal(t, "TICKET-"+time.Now().Format("20060102")+"-B", tk.TicketNumber,
		"重试时必须清空旧号并由 BeforeCreate 重新生成")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTicketService_Create_客户端自带工单号冲突不重试 保护外部对接语义：
// 调用方指定的号被占用时应返回 ErrAlreadyExists(409)，而不是悄悄换一个号返回成功。
func TestTicketService_Create_客户端自带工单号冲突不重试(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "ext", TicketNumber: "TICKET-20260101-A"}
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnError(&pqUniqueError{msg: "duplicate key value violates unique constraint"})
	mock.ExpectRollback()

	err := svc.Create(ctx, tk, testActor())
	assert.ErrorIs(t, err, ErrAlreadyExists, "客户端指定的号冲突应 409，不得静默换号")
	assert.Equal(t, "TICKET-20260101-A", tk.TicketNumber, "不得改写客户端传入的号")
	assert.NoError(t, mock.ExpectationsWereMet(), "客户端自带号不应进入重试路径")
}

func TestTicketService_Create_持续唯一冲突返回ErrAlreadyExists(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "dup"}
	// 连续 5 次都撞唯一索引 → 放弃并返回 ErrAlreadyExists（重试上限）
	const maxAttempts = 5
	for i := 0; i < maxAttempts; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`INSERT INTO "tickets"`).
			WillReturnError(&pqUniqueError{msg: "duplicate key value violates unique constraint"})
		mock.ExpectRollback()
	}

	err := svc.Create(ctx, tk, testActor())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)
	assert.NoError(t, mock.ExpectationsWereMet(), "应恰好重试 %d 次", maxAttempts)
}

func TestTicketService_Create_非唯一约束错误不重试(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "boom"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	err := svc.Create(ctx, tk, testActor())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAlreadyExists, "非唯一冲突应原样透传，不重试")
	assert.Contains(t, err.Error(), "connection reset")
	assert.NoError(t, mock.ExpectationsWereMet(), "不应发生第二次 INSERT")
}

// pqUniqueError 模拟 pq.Error (有 .Error() string)
type pqUniqueError struct{ msg string }

func (e *pqUniqueError) Error() string { return e.msg }

// ==================== Update 补全 ====================

func TestTicketService_Update_成功_非空updates(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	pre := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "原标题", "open")
	post := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "新标题", "open")

	// 事务序：pre（持锁）→ UPDATE → post → 插历史 → 重读响应体。
	// **顺序本身就是形态守卫**：pre 若被挪到 UPDATE 之后，第一条期望就对不上 —— 那时
	// pre 读到的是写后的值，diff 恒空、历史永远 0 行，而黑盒用例只看「修改生效了」是绿的。
	mock.ExpectBegin()
	// FOR UPDATE 必须真出现在 SQL 里：sqlite 基座不渲染它（driver 明说不支持行级锁），
	// 单测里唯一能钉住「加锁没被删掉」的地方就是这里（postgres dialector + sqlmock）。
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2 FOR UPDATE`).
		WithArgs(id, 1).WillReturnRows(pre)
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// 锚定结尾：不带 FOR UPDATE 的那条读法只能匹配到 post，不会替 pre 顶包。
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2\s*$`).
		WithArgs(id, 1).WillReturnRows(post)
	// PG 下批量插入带 RETURNING "id" → 走的是 Query 而不是 Exec
	mock.ExpectQuery(`INSERT INTO "ticket_history"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	// sqlmock.Rows 是一次性消费的：post 那批行在上面已被读走，这里必须新建一批，
	// 否则重读拿到 0 行 → record not found（不是代码错，是期望写错）
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "status"}).AddRow(id, "新标题", "open"))
	mock.ExpectCommit()

	got, err := svc.Update(ctx, id, map[string]interface{}{"title": "新标题"}, testActor())
	require.NoError(t, err)
	assert.Equal(t, "新标题", got.Title, "响应体取自重读的 struct")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_关闭工单_写closed_at(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	pre := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "工单", "in_progress")
	post := sqlmock.NewRows([]string{"id", "title", "status", "closed_at"}).
		AddRow(id, "工单", "closed", time.Now())

	// 不强校验 SQL, 只确保 Updates 跑过
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2 FOR UPDATE`).
		WithArgs(id, 1).WillReturnRows(pre)
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2\s*$`).
		WithArgs(id, 1).WillReturnRows(post)
	mock.ExpectQuery(`INSERT INTO "ticket_history"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(uuid.NewString()).AddRow(uuid.NewString()))
	// 注入的是 clause.Expr, gorm 不回写 struct → Update 末尾要重读一次拿真值。
	// rows 一次性消费，故新建一批（复用 post 会读到 0 行变成 record not found）
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "status", "closed_at"}).
			AddRow(id, "工单", "closed", time.Now()))
	mock.ExpectCommit()

	updates := map[string]interface{}{"status": "closed"}
	got, err := svc.Update(ctx, id, updates, testActor())
	require.NoError(t, err)
	// 返回值必须带上刚写进去的关闭时间：注入的是 clause.Expr，gorm 不会回写 struct，
	// 少了 Update 末尾那次重读，这里就是 nil（响应体与库不一致）。
	require.NotNil(t, got.ClosedAt, "返回给 handler 的 closed_at 不得是空")
	require.Contains(t, updates, "closed_at", "Update 内部应注入 closed_at")
	assert.NotNil(t, updates["closed_at"], "closed_at 必填")
	// 判据必须是 SQL 表达式，不能是 Go 侧读到的 t.Status 快照 —— 快照在并发跃迁下会判错
	// （见 Update 里 M24 注释）。形态在这里钉住，防回退；黑盒用例分辨不出这一点。
	assert.IsType(t, clause.Expr{}, updates["closed_at"], "跃迁判据要下推到 SQL 表达式")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	// pre 读不到 → 事务内返回 ErrNotFound → gorm 自动 Rollback（不留悬挂事务）
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2 FOR UPDATE`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectRollback()

	got, err := svc.Update(ctx, "nonexistent", map[string]interface{}{"title": "x"}, testActor())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTicketService_Update_DB错误_透传(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "t", "open")

	dbErr := errors.New("connection reset")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1 LIMIT \$2 FOR UPDATE`).
		WithArgs(id, 1).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := svc.Update(ctx, id, map[string]interface{}{"title": "x"}, testActor())
	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

// ==================== M25 经手历史（真 sqlite 黑盒） ====================
//
// 这一组读的是 ticket_history 表本身 —— sqlmock 那组只能验「SQL 发出去了」，
// 验不了「落库的值对不对」。

// historyOf 读某张票的历史行，按 field_name 建索引。
// 只在「每个字段最多一行」的场景用 —— 同字段多行时后者会覆盖前者，
// 要全序请直接查（且别指望靠顺序分辨同秒的两行，T-45）。
func historyOf(t *testing.T, db *gorm.DB, ticketID uuid.UUID) map[string]models.TicketHistory {
	t.Helper()
	var rows []models.TicketHistory
	require.NoError(t, db.Where("ticket_id = ?", ticketID).Order("field_name").Find(&rows).Error)
	byField := make(map[string]models.TicketHistory, len(rows))
	for _, r := range rows {
		require.NotNil(t, r.FieldName, "kind=updated 的行必须带字段名")
		byField[*r.FieldName] = r
	}
	return byField
}

func seedHistoryTicket(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	require.NoError(t, db.Create(&models.Ticket{ //nolint:exhaustruct
		ID: id, TicketNumber: "TICKET-20260101-A", Title: "原标题", Status: "open",
		Priority: "normal", CreatedAt: created, UpdatedAt: created,
	}).Error)
	return id
}

// 字段级 diff + actor 快照 + 同批次 + 系统列排除。
// 其中 old_value 必须是**写入前**的值：这是「pre 在 UPDATE 之前读」的唯一黑盒判据 ——
// 若 pre 被挪到 UPDATE 之后，old 会等于 new，历史就成了「改了但不知道从什么改成什么」。
func TestTicketService_Update_留痕_字段级diff与actor快照(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)
	actorID := uuid.New()

	_, err := svc.Update(context.Background(), id.String(),
		map[string]interface{}{"title": "新标题", "priority": "high"},
		Actor{ID: &actorID, Name: "燕如"})
	require.NoError(t, err)

	got := historyOf(t, db, id)
	require.Len(t, got, 2, "两个字段变更 → 两行")

	title := got["title"]
	require.NotNil(t, title.OldValue)
	require.NotNil(t, title.NewValue)
	assert.Equal(t, "原标题", *title.OldValue, "old 必须是写入前的值")
	assert.Equal(t, "新标题", *title.NewValue)
	assert.Equal(t, models.TicketHistoryKindUpdated, title.Kind)
	assert.Equal(t, "燕如", title.ActorName, "actor_name 是快照，不是外键引用")
	require.NotNil(t, title.ActorID)
	assert.Equal(t, actorID, *title.ActorID)

	assert.Equal(t, "normal", *got["priority"].OldValue)
	assert.Equal(t, "high", *got["priority"].NewValue)

	assert.Equal(t, title.BatchID, got["priority"].BatchID, "同一次 PUT 的多行共享 batch_id")
	assert.False(t, title.CreatedAt.IsZero(), "created_at 必须落库")

	// 系统列：updated_at 每次 PUT 必被 gorm 补，记它等于每行都带一条噪声，
	// 把「谁改了什么」淹掉；id / created_at / ticket_number 同理。
	for _, sys := range []string{"updated_at", "created_at", "id", "ticket_number"} {
		_, ok := got[sys]
		assert.False(t, ok, "%s 是系统列，不得进历史", sys)
	}
}

// 传了键但没有实质变化 → 0 行历史。
// 判据必须落在**按列比**上：gorm 对 map 更新无条件补 updated_at，这一行一定发生，
// 拿「有没有 UPDATE」当判据会永远为真。
func TestTicketService_Update_留痕_无实质变化不写行(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)

	_, err := svc.Update(context.Background(), id.String(),
		map[string]interface{}{"title": "原标题"}, testActor()) // 值与原值相同
	require.NoError(t, err)

	assert.Empty(t, historyOf(t, db, id), "值没变不留痕")
}

// 模型外列（tickets 表里有、models.Ticket 里没有）改了也必须留痕 ——
// `{"alert_id": X}` 走 gorm 的裸列兜底**真的写库**，用 struct 比会整个漏掉。
func TestTicketService_Update_留痕_模型外列也留痕(t *testing.T) {
	db := newTicketSQLiteDB(t)
	require.NoError(t, db.Exec("ALTER TABLE tickets ADD COLUMN alert_id TEXT").Error)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)

	_, err := svc.Update(context.Background(), id.String(),
		map[string]interface{}{"alert_id": "a-1"}, testActor())
	require.NoError(t, err)

	got := historyOf(t, db, id)
	require.Contains(t, got, "alert_id", "模型外列的改动同样要留痕")
	assert.Nil(t, got["alert_id"].OldValue, "原本没有值 → old 是 NULL（不是空串）")
	require.NotNil(t, got["alert_id"].NewValue)
	assert.Equal(t, "a-1", *got["alert_id"].NewValue)
}

// 两次操作必须是两个批次：否则 UI 会把先后两次改动并成一坨。
// 断言刻意落在**批次集合**上而不是「第几行」：同字段的两行 created_at 可能同秒，
// 靠顺序区分就是 T-45（没有 ORDER BY 的顺序没有定义）。
func TestTicketService_Update_留痕_两次操作批次不同(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)
	ctx := context.Background()

	_, err := svc.Update(ctx, id.String(), map[string]interface{}{"title": "第一次"}, testActor())
	require.NoError(t, err)
	_, err = svc.Update(ctx, id.String(), map[string]interface{}{"title": "第二次"}, testActor())
	require.NoError(t, err)

	var rows []models.TicketHistory
	require.NoError(t, db.Where("ticket_id = ?", id).Find(&rows).Error)
	require.Len(t, rows, 2, "两次 PUT 各留一行")
	batches := map[uuid.UUID]struct{}{}
	for _, r := range rows {
		batches[r.BatchID] = struct{}{}
	}
	assert.Len(t, batches, 2, "两次 PUT 是两个批次，不能被并成一坨")
}

// 历史插入失败 → 整个 Update 回滚（同事务）。
// 制造失败的方式：把 ticket_history 表删掉，插入必报 no such table。
// 这条守的是「失败即整单回滚」的承诺：宁可改不了，也不留一条无痕的修改。
func TestTicketService_Update_留痕_插历史失败整单回滚(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)

	require.NoError(t, db.Exec("DROP TABLE ticket_history").Error)

	_, err := svc.Update(context.Background(), id.String(),
		map[string]interface{}{"title": "不该留下"}, testActor())
	require.Error(t, err, "历史写不进去时 Update 必须失败，而不是默默改掉")

	var after models.Ticket
	require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
	assert.Equal(t, "原标题", after.Title, "整单回滚：字段改动不得留下")
}

func TestTicketService_Get_DB错误_透传(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	dbErr := errors.New("connection refused")
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs("x", 1).
		WillReturnError(dbErr)

	_, err := svc.Get(ctx, "x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, err, dbErr)
}

// ==================== List 补全 ====================

func TestTicketService_List_带Status筛选(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets" WHERE status = \$1`).
		WithArgs("open").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(uuid.NewString(), "open-ticket", "open")
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE status = \$1`).
		WithArgs("open", 20). // status + limit (gorm Postgres Offset+Limit 走 2 args)
		WillReturnRows(rows)

	list, total, err := svc.List(ctx, TicketFilter{Status: "open"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)
	assert.Equal(t, "open-ticket", list[0].Title)
}

func TestTicketService_List_带Priority筛选(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets" WHERE priority = \$1`).
		WithArgs("high").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE priority = \$1`).
		WithArgs("high", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "status"}))

	list, total, err := svc.List(ctx, TicketFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, list)
}

func TestTicketService_List_PageSize_默认20_最大500(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// PageSize=0 应默认 20, Page=0 应默认 1
	mock.ExpectQuery(`SELECT \* FROM "tickets"`).
		WithArgs(20). // gorm Postgres Offset+Limit 走 1 arg (limit)
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "status"}))

	_, _, err := svc.List(ctx, TicketFilter{})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== M24：closed_at 与 status 的一致性 ====================

// 不变式：`status == "closed"` ⟺ `closed_at IS NOT NULL`。这条链路上它有两个入口 ——
// Update 的**跃迁**与 Create 的**出生即关闭**，所以两侧各有用例。
//
// 为什么值得钉死：消费者是资产时间线与 SLA 统计，两边都只看时间戳、不看 status。
// diagnostic_service 只认 `closed_at IS NOT NULL` 就发「工单关闭」事件 —— 残留的关闭时间
// = 一条永久假事件；dashboard 的 SLA 口径是 `status='closed' AND closed_at >= ?` ——
// 缺失的关闭时间 = 一张已关闭却在统计里不存在的工单。
func TestTicketService_Update_关闭时间随状态跃迁(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, status string, closedAt *time.Time) (*gorm.DB, TicketService, uuid.UUID) {
		t.Helper()
		db := newTicketSQLiteDB(t)
		id := uuid.New()
		require.NoError(t, db.Create(&models.Ticket{ //nolint:exhaustruct
			ID: id, TicketNumber: "TICKET-20260101-A", Title: "原标题", Status: status,
			ClosedAt: closedAt, CreatedAt: t0, UpdatedAt: t0,
		}).Error)
		return db, NewTicketService(db), id
	}
	load := func(t *testing.T, db *gorm.DB, id uuid.UUID) models.Ticket {
		t.Helper()
		var after models.Ticket
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		return after
	}

	t.Run("重开工单_清空closed_at", func(t *testing.T) {
		db, svc, id := seed(t, "closed", &t0)
		got, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"status": "open"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		assert.Equal(t, "open", after.Status)
		assert.Nil(t, after.ClosedAt,
			"重开后 closed_at 必须清空，否则资产时间线上永久挂着一条「工单关闭」")
		// 返回值同样要反映清空：注入的是 clause.Expr，gorm 不回写 struct，
		// 少了 Update 末尾的重读，响应体里会是一个库中已不存在的旧关闭时间。
		require.NotNil(t, got)
		assert.Nil(t, got.ClosedAt, "响应体里的 closed_at 不得落后于库")
	})

	t.Run("已关闭工单再PATCH_不重置关闭时间", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			updates map[string]interface{}
		}{
			{"重复置closed", map[string]interface{}{"status": "closed", "description": "改个描述"}},
			{"只改其它列", map[string]interface{}{"description": "改个描述"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				db, svc, id := seed(t, "closed", &t0)
				_, err := svc.Update(context.Background(), id.String(), tc.updates, testActor())
				require.NoError(t, err)

				after := load(t, db, id)
				require.NotNil(t, after.ClosedAt)
				assert.WithinDuration(t, t0, *after.ClosedAt, time.Second,
					"一次无关编辑不得把 SLA 的关闭耗时重置成 now")
			})
		}
	})

	t.Run("关闭时显式给closed_at_尊重调用方", func(t *testing.T) {
		db, svc, id := seed(t, "open", nil)
		_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{
			"status": "closed", "closed_at": t1,
		}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ClosedAt)
		assert.WithinDuration(t, t1, *after.ClosedAt, time.Second,
			"对接回灌的原始关闭时间不得被 now 覆盖")
	})

	t.Run("正控_真正关到closed仍写now", func(t *testing.T) {
		db, svc, id := seed(t, "in_progress", nil)
		got, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"status": "closed"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ClosedAt, "进入 closed 必须记关闭时间")
		assert.WithinDuration(t, time.Now(), *after.ClosedAt, time.Minute)
		// 响应体必须带上刚写的这个时间（而不是 First 读到的 nil）
		require.NotNil(t, got)
		require.NotNil(t, got.ClosedAt, "响应体里的 closed_at 不得为空")
		assert.WithinDuration(t, *after.ClosedAt, *got.ClosedAt, time.Second,
			"响应体里的 closed_at 必须与库中一致")
	})
}

func TestTicketService_Create_建单即关闭_写closed_at(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		wantSet bool
	}{
		{"建单即已关闭", "closed", true},
		{"缺省open不写", "", false},
		{"中间态不写", "in_progress", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTicketSQLiteDB(t)
			svc := NewTicketService(db)

			tk := &models.Ticket{Title: "补录工单", Status: tc.status} //nolint:exhaustruct
			require.NoError(t, svc.Create(context.Background(), tk, testActor()))

			var got models.Ticket
			require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
			if tc.wantSet {
				assert.NotNil(t, got.ClosedAt,
					"建单即关闭必须落下 closed_at，否则 SLA 统计与资产时间线都看不见它")
			} else {
				assert.Nil(t, got.ClosedAt)
			}
		})
	}

	t.Run("显式给closed_at_不覆盖", func(t *testing.T) {
		db := newTicketSQLiteDB(t)
		svc := NewTicketService(db)
		t1 := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

		tk := &models.Ticket{Title: "回灌工单", Status: "closed", ClosedAt: &t1} //nolint:exhaustruct
		require.NoError(t, svc.Create(context.Background(), tk, testActor()))

		var got models.Ticket
		require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
		require.NotNil(t, got.ClosedAt)
		assert.WithinDuration(t, t1, *got.ClosedAt, time.Second)
	})
}

// ==================== M25 出生事件（真 sqlite 黑盒） ====================
//
// 出生行是「这张票存在了」的唯一留痕。它与工单的 INSERT **同事务** —— 这一组
// 用真 sqlite 钉住三件事：内容对（kind/actor/批次）、失败不留孤儿行、历史写不进去
// 时工单也不该存在。

// 出生行的形状：kind=created、field_name/old/new 三列皆 NULL（出生改的不是某个字段，
// 而是「这张票存在了」）、actor 两列是**快照**、batch_id 非零值（UI 靠它分组）。
func TestTicketService_Create_留痕_出生行与actor快照(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	actorID := uuid.New()
	tk := &models.Ticket{Title: "新工单"} //nolint:exhaustruct
	require.NoError(t, svc.Create(context.Background(), tk, Actor{ID: &actorID, Name: "燕如"}))

	var rows []models.TicketHistory
	require.NoError(t, db.Where("ticket_id = ?", tk.ID).Find(&rows).Error)
	require.Len(t, rows, 1, "建单恰好一行出生记录（一次请求建一张票，故独占一个批次）")

	row := rows[0]
	assert.Equal(t, models.TicketHistoryKindCreated, row.Kind)
	assert.Nil(t, row.FieldName, "出生行不带字段名")
	assert.Nil(t, row.OldValue)
	assert.Nil(t, row.NewValue)
	assert.NotEqual(t, uuid.Nil, row.BatchID, "batch_id 缺失会让 UI 无法把同批次的行分到一组")
	require.NotNil(t, row.ActorID, "传了 actor 就必须落 id —— 只有姓名的话按人查历史查不到")
	assert.Equal(t, actorID, *row.ActorID)
	assert.Equal(t, "燕如", row.ActorName, "姓名是快照：用户改名后仍要看得见当时是谁")
}

// 内部调用（seed / 定时任务 / GLPI 同步）拿不到 user id，只留姓名也必须是**合法状态**
// 而不是错误：宁可有名无 id，也不要把「谁经手」整个丢掉（同 D-5）。
func TestTicketService_Create_留痕_无用户id时留姓名(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	tk := &models.Ticket{Title: "系统建单"} //nolint:exhaustruct
	require.NoError(t, svc.Create(context.Background(), tk, testActor()))

	var row models.TicketHistory
	require.NoError(t, db.First(&row, "ticket_id = ?", tk.ID).Error)
	assert.Nil(t, row.ActorID)
	assert.Equal(t, "tester", row.ActorName)
}

// 每次尝试各开一个事务：撞号失败的那几次必须**整体回滚**（工单行与出生行都不留）。
//
// 这里让每次尝试都撞同一个号（占位行的号恰好等于按当天条数算出来的号），
// 5 次尝试全部失败 → ErrAlreadyExists。若实现把循环包进一个长事务、或把出生行写在
// 事务之外，这张表里就会多出 1 张票或若干行孤儿历史。
func TestTicketService_Create_留痕_撞号重试失败不留任何行(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	// 基座的 tickets DDL 是手写的，**没有 ticket_number 唯一索引**（生产有）——
	// 不补上的话撞号路径在 sqlite 上根本不可达，这条用例会假绿。补一个只属于本用例的索引，
	// 让基座在这一列上与生产同构。（同族登记：模型外列在基座是宽松 TEXT、真 PG 带类型与外键。）
	require.NoError(t, db.Exec(
		"CREATE UNIQUE INDEX idx_test_tickets_number ON tickets(ticket_number)").Error)

	// 占位行直接进库（不走 service，故不产生历史）。当天条数=1 → 生成的号是 B，
	// 与占位行同号 → 每次尝试都撞唯一索引。
	occupant := &models.Ticket{ //nolint:exhaustruct
		ID: uuid.New(), TicketNumber: "TICKET-" + time.Now().Format("20060102") + "-B", Title: "占位",
		Status: "open", Priority: "normal",
	}
	require.NoError(t, db.Create(occupant).Error)

	tk := &models.Ticket{Title: "撞号工单"} //nolint:exhaustruct
	require.ErrorIs(t, svc.Create(context.Background(), tk, testActor()), ErrAlreadyExists)

	var tickets int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&tickets).Error)
	assert.EqualValues(t, 1, tickets, "失败的尝试必须整体回滚，只留占位那一行")

	var history int64
	require.NoError(t, db.Model(&models.TicketHistory{}).Count(&history).Error)
	assert.Zero(t, history, "工单没建成就不该有出生记录 —— 出生行与 INSERT 同事务")
}

// 出生行**写不进去**时，工单也不该存在（失败即整单回滚）。
//
// 这是「出生行与工单同事务」的直接判据：若把 insertTicketBirth 挪到事务外（或用 s.db），
// 这条会红成「工单建成了、出生记录没有」—— 那正是这张表最该避免的孤儿状态。
// 与 Update 侧的同款用例（DROP 表后改单回滚）成对。
func TestTicketService_Create_留痕_插历史失败整单回滚(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	require.NoError(t, db.Exec("DROP TABLE ticket_history").Error)

	tk := &models.Ticket{Title: "不该存在的工单"} //nolint:exhaustruct
	require.Error(t, svc.Create(context.Background(), tk, testActor()),
		"留痕写不进去就必须整单回滚 —— 记不上历史的建单没有意义")

	var tickets int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&tickets).Error)
	assert.Zero(t, tickets, "回滚必须连工单行一起撤销")
}

// ==================== M25：resolved_at 与 status 的一致性 ====================
//
// 语义与 closed_at（M24）同族但**转移表不同**：closed_at 在任何非 closed 状态下都必须为
// NULL；resolved_at 在 **closed 下必须存活** —— 否则 resolved→closed 之后 MTTR 归零。
// 「重开清空」是燕如 2026-09-11 的拍板（当前状态语义），配套约定是**清掉的值进历史**，
// 所以这里既钉库里的值，也钉历史里那行 old_value。
func TestTicketService_Update_解决时间随状态跃迁(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, status string, resolvedAt *time.Time) (*gorm.DB, TicketService, uuid.UUID) {
		t.Helper()
		db := newTicketSQLiteDB(t)
		id := uuid.New()
		require.NoError(t, db.Create(&models.Ticket{ //nolint:exhaustruct
			ID: id, TicketNumber: "TICKET-20260101-A", Title: "原标题", Status: status,
			ResolvedAt: resolvedAt, CreatedAt: t0, UpdatedAt: t0,
		}).Error)
		return db, NewTicketService(db), id
	}
	load := func(t *testing.T, db *gorm.DB, id uuid.UUID) models.Ticket {
		t.Helper()
		var after models.Ticket
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		return after
	}

	t.Run("进入resolved_写now", func(t *testing.T) {
		db, svc, id := seed(t, "in_progress", nil)
		got, err := svc.Update(context.Background(), id.String(),
			map[string]interface{}{"status": "resolved"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ResolvedAt, "进入 resolved 必须记解决时刻，否则 MTTR 与时间线都看不见它")
		assert.WithinDuration(t, time.Now(), *after.ResolvedAt, time.Minute)
		require.NotNil(t, got)
		require.NotNil(t, got.ResolvedAt, "响应体里的 resolved_at 不得为空")
		assert.WithinDuration(t, *after.ResolvedAt, *got.ResolvedAt, time.Second,
			"响应体里的 resolved_at 必须与库中一致")
	})

	// 本轮工作流的起因：重开必须清掉 resolved_at，否则「当前状态」是假的。
	// 但**清掉不等于丢掉** —— 同事务的字段级 diff 会把被清掉的时间留在历史里。
	t.Run("重开工单_清空resolved_at且被清的值进历史", func(t *testing.T) {
		db, svc, id := seed(t, "resolved", &t0)
		got, err := svc.Update(context.Background(), id.String(),
			map[string]interface{}{"status": "open"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		assert.Equal(t, "open", after.Status)
		assert.Nil(t, after.ResolvedAt, "重开后 resolved_at 必须清空（当前状态语义）")
		require.NotNil(t, got)
		assert.Nil(t, got.ResolvedAt, "响应体里的 resolved_at 不得落后于库")

		row, ok := historyOf(t, db, id)["resolved_at"]
		require.True(t, ok, "清空 resolved_at 必须留痕 —— 否则「谁在何时解决的」永久消失")
		require.NotNil(t, row.OldValue, "被清掉的时间要留在 old_value 里")
		assert.Contains(t, *row.OldValue, "2026-01-01", "old_value 必须是原解决时刻")
		assert.Nil(t, row.NewValue, "新值为 NULL（不是空串）—— 读端要能分辨「没有值」")
	})

	// resolved→closed 是本轮最关键的**反面用例**：closed_at 那套写法的直觉会把它一起清掉。
	t.Run("resolved到closed_保留解决时间", func(t *testing.T) {
		db, svc, id := seed(t, "resolved", &t0)
		_, err := svc.Update(context.Background(), id.String(),
			map[string]interface{}{"status": "closed"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ResolvedAt,
			"resolved→closed 必须保留解决时刻，否则已关闭的票 MTTR 归零")
		assert.WithinDuration(t, t0, *after.ResolvedAt, time.Second)
		require.NotNil(t, after.ClosedAt, "同时要落下关闭时刻")
	})

	t.Run("已解决工单再PATCH_不重置解决时间", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			updates map[string]interface{}
		}{
			{"重复置resolved", map[string]interface{}{"status": "resolved", "description": "改个描述"}},
			{"只改其它列", map[string]interface{}{"description": "改个描述"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				db, svc, id := seed(t, "resolved", &t0)
				_, err := svc.Update(context.Background(), id.String(), tc.updates, testActor())
				require.NoError(t, err)

				after := load(t, db, id)
				require.NotNil(t, after.ResolvedAt)
				assert.WithinDuration(t, t0, *after.ResolvedAt, time.Second,
					"一次无关编辑不得把 MTTR 重置成 now")
			})
		}
	})

	// 已知边界（按 §2.5 规范写、未加 extra CASE）：closed→resolved 直接跳转时，
	// 若这张票当初是 resolved→closed 过来的，它带着真实的解决时刻 —— 回到 resolved
	// 应当保留它，而不是当成一次新的解决。这条钉住的是「不加额外分支」这个决定本身，
	// 免得日后被当成 bug「修」成 now（那会把 MTTR 与时间线一起改掉）。
	t.Run("closed回到resolved_保留既有解决时刻", func(t *testing.T) {
		db, svc, id := seed(t, "closed", &t0)
		_, err := svc.Update(context.Background(), id.String(),
			map[string]interface{}{"status": "resolved"}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ResolvedAt)
		assert.WithinDuration(t, t0, *after.ResolvedAt, time.Second)
		assert.Nil(t, after.ClosedAt, "回到 resolved 已经离开 closed，关闭时刻必须清掉")
	})

	t.Run("关闭时显式给resolved_at_尊重调用方", func(t *testing.T) {
		db, svc, id := seed(t, "open", nil)
		_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{
			"status": "closed", "resolved_at": t1,
		}, testActor())
		require.NoError(t, err)

		after := load(t, db, id)
		require.NotNil(t, after.ResolvedAt,
			"调用方显式给的 resolved_at 不得被「closed 保留原值」这条规则悄悄丢掉")
		assert.WithinDuration(t, t1, *after.ResolvedAt, time.Second)
	})
}

// 同一个不变式的第二个入口：出生即 resolved 也要落 resolved_at。
func TestTicketService_Create_出生即解决_写resolved_at(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	t.Run("status为resolved_落now", func(t *testing.T) {
		tk := &models.Ticket{Title: "回灌工单", Status: "resolved"} //nolint:exhaustruct
		require.NoError(t, svc.Create(context.Background(), tk, testActor()))

		var got models.Ticket
		require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
		require.NotNil(t, got.ResolvedAt,
			"建单即解决必须落下 resolved_at，否则 MTTR 与资产时间线都看不见它")
		assert.WithinDuration(t, time.Now(), *got.ResolvedAt, time.Minute)
	})

	t.Run("显式给resolved_at_不覆盖", func(t *testing.T) {
		t1 := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
		tk := &models.Ticket{Title: "回灌工单", Status: "resolved", ResolvedAt: &t1} //nolint:exhaustruct
		require.NoError(t, svc.Create(context.Background(), tk, testActor()))

		var got models.Ticket
		require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
		require.NotNil(t, got.ResolvedAt)
		assert.WithinDuration(t, t1, *got.ResolvedAt, time.Second,
			"对接回灌的原始解决时间不得被 now 覆盖")
	})

	// status=closed 出生时**不补** resolved_at：那会凭空发明一个从未发生过的解决时刻。
	// 这条是**反面**用例 —— 把 closed 也一并补上是很自然的「顺手」，必须被挡住。
	t.Run("status为closed_不发明解决时刻", func(t *testing.T) {
		tk := &models.Ticket{Title: "直接关闭的工单", Status: "closed"} //nolint:exhaustruct
		require.NoError(t, svc.Create(context.Background(), tk, testActor()))

		var got models.Ticket
		require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
		require.NotNil(t, got.ClosedAt)
		assert.Nil(t, got.ResolvedAt,
			"closed 的语义是「保留已有的解决时间」，出生时本来就没有 —— 不能发明一个")
	})
}

// ==================== M25：读端点 ListHistory ====================
//
// 读路径的判据只有三条，但每条都能把「看起来对」的实现打回去：
// 全序（否则翻页重复/漏行）、clamp（否则可拖库）、不存在即 404（否则前端会
// 渲染一个并不存在的工单详情页）。
func TestTicketService_ListHistory_全序与分页(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)
	batch := uuid.New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 五行的 created_at 刻意只落在两个时刻上（后两个时刻各有一对完全相同），
	// 逼出 id DESC 这个次序键 —— 只按时间排的话，同一时刻的两行次序未定义（T-45），
	// 翻页会在两页之间重复或漏掉它们。id 用可比较的固定值，让期望顺序是写死的。
	mk := func(n int, at time.Time) models.TicketHistory {
		field := "f" + strconv.Itoa(n)
		old := "old"
		return models.TicketHistory{ //nolint:exhaustruct
			ID:        uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)),
			TicketID:  id,
			BatchID:   batch,
			Kind:      models.TicketHistoryKindUpdated,
			FieldName: &field,
			OldValue:  &old,
			ActorName: "燕如",
			CreatedAt: at,
		}
	}
	rows := []models.TicketHistory{
		mk(1, base), mk(2, base),
		mk(3, base.Add(time.Minute)), mk(4, base.Add(time.Minute)),
		mk(5, base.Add(2*time.Minute)),
	}
	require.NoError(t, db.Create(&rows).Error)

	ids := func(items []models.TicketHistory) []string {
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it.ID.String())
		}
		return out
	}
	// 期望顺序：时间倒序；时间相同则 id 倒序。写成字面量而不是「按同样规则再排一遍」，
	// 免得实现和用例用同一个错误假设互相印证。
	wantOrder := []string{
		"00000000-0000-0000-0000-000000000005",
		"00000000-0000-0000-0000-000000000004",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000001",
	}

	t.Run("默认分页_最新在前", func(t *testing.T) {
		items, total, err := svc.ListHistory(context.Background(), id.String(), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		assert.Equal(t, wantOrder, ids(items), "page/pageSize 传 0 时按 1/20 兜底，且必须是全序")
	})

	t.Run("翻页不重不漏", func(t *testing.T) {
		var seen []string
		for page := 1; page <= 3; page++ {
			items, total, err := svc.ListHistory(context.Background(), id.String(), page, 2)
			require.NoError(t, err)
			assert.Equal(t, int64(5), total, "total 是全量，不随页变化")
			seen = append(seen, ids(items)...)
		}
		assert.Equal(t, wantOrder, seen, "逐页拼起来必须恰好是全序序列 —— 重复或漏行都会在这里露出来")
	})

	t.Run("越界页返回空而不是报错", func(t *testing.T) {
		items, total, err := svc.ListHistory(context.Background(), id.String(), 99, 20)
		require.NoError(t, err)
		assert.Empty(t, items)
		assert.Equal(t, int64(5), total)
	})
}

// 页大小上限 500：契约层 page_size 没有 maximum（openapi.yaml），不 clamp 就是
// 一句 `page_size=100000` 拖库。要观测 clamp 必须有超过上限的行数 —— 五行数据下
// `LIMIT 10000` 与 `LIMIT 500` 结果完全一样，那条用例会假绿。
func TestTicketService_ListHistory_页大小上限500(t *testing.T) {
	db := newTicketSQLiteDB(t)
	id := seedHistoryTicket(t, db)
	svc := NewTicketService(db)

	rows := make([]models.TicketHistory, 0, 501)
	for i := 0; i < 501; i++ {
		field := "f" + strconv.Itoa(i)
		rows = append(rows, models.TicketHistory{ //nolint:exhaustruct
			ID:        uuid.New(),
			TicketID:  id,
			BatchID:   uuid.New(),
			Kind:      models.TicketHistoryKindUpdated,
			FieldName: &field,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, i, time.UTC),
		})
	}
	require.NoError(t, db.CreateInBatches(&rows, 200).Error)

	items, total, err := svc.ListHistory(context.Background(), id.String(), 1, 10000)
	require.NoError(t, err)
	assert.Equal(t, int64(501), total)
	assert.Len(t, items, 500, "page_size 超过上限必须被夹到 500")
}

// 不存在的工单要 404，不能是空列表：空列表会让调用方把「这张票不存在」读成
// 「这张票还没被改过」，前端据此渲染出一个并不存在的工单详情页。
func TestTicketService_ListHistory_工单不存在(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)

	items, _, err := svc.ListHistory(context.Background(), uuid.New().String(), 1, 20)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, items, "报错时不得同时返回一个空列表给调用方「将就用」")
}

// 黑盒：读端点拿到的必须是 Update 真正写下的东西（含出生行），不是另一套形状。
func TestTicketService_ListHistory_读到Update写入的历史(t *testing.T) {
	db := newTicketSQLiteDB(t)
	svc := NewTicketService(db)
	ctx := context.Background()

	tk := &models.Ticket{Title: "读端点黑盒", Status: "open", Priority: "normal"} //nolint:exhaustruct
	require.NoError(t, svc.Create(ctx, tk, Actor{Name: "燕如"}))
	_, err := svc.Update(ctx, tk.ID.String(), map[string]interface{}{"title": "改过的标题"}, Actor{Name: "燕如"})
	require.NoError(t, err)

	items, total, err := svc.ListHistory(ctx, tk.ID.String(), 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total, "出生行 + 一次字段变更 = 两行")
	require.Len(t, items, 2)

	newest := items[0]
	assert.Equal(t, models.TicketHistoryKindUpdated, newest.Kind)
	require.NotNil(t, newest.FieldName)
	assert.Equal(t, "title", *newest.FieldName)
	assert.Equal(t, "燕如", newest.ActorName, "读端点要能看到是谁改的")
	require.NotNil(t, newest.NewValue)
	assert.Equal(t, "改过的标题", *newest.NewValue)

	oldest := items[1]
	assert.Equal(t, models.TicketHistoryKindCreated, oldest.Kind)
	assert.Nil(t, oldest.FieldName, "出生行改的不是某个字段，而是「这张票存在了」")
}
