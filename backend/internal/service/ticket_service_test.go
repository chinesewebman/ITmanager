package service

import (
	"context"
	"errors"
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
	err := svc.Create(ctx, tk)
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
	return db
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

			err := svc.Create(context.Background(), tk)
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
	require.NoError(t, svc.Create(context.Background(), tk))

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
	mock.ExpectCommit()

	err := svc.Create(ctx, tk)
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
	mock.ExpectCommit()

	err := svc.Create(ctx, tk)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", tk.Status, "已传 Status 不覆盖")
	assert.Equal(t, "alert", tk.Source)
	assert.Equal(t, `["p1"]`, tk.Tags)
	assert.Equal(t, "high", tk.Priority, "已传 Priority 不得被默认值覆盖")
}

func TestTicketService_Create_nil指针返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewTicketService(gormDB)
	err := svc.Create(context.Background(), nil)
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
	mock.ExpectCommit()

	require.NoError(t, svc.Create(ctx, tk), "唯一冲突应重试而非直接失败（缺陷 D-2）")
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

	err := svc.Create(ctx, tk)
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

	err := svc.Create(ctx, tk)
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

	err := svc.Create(ctx, tk)
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
	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "原标题", "open")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.Update(ctx, id, map[string]interface{}{"title": "新标题"}, testActor())
	require.NoError(t, err)
	assert.Equal(t, "新标题", got.Title, "gorm Updates 后会刷到 struct")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_关闭工单_写closed_at(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "工单", "in_progress")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	// 不强校验 SQL, 只确保 Updates 跑过
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// 注入的是 clause.Expr, gorm 不回写 struct → Update 末尾要重读一次拿真值
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "status", "closed_at"}).
			AddRow(id, "工单", "closed", time.Now()))

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

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

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
	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	dbErr := errors.New("connection reset")
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tickets"`).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := svc.Update(ctx, id, map[string]interface{}{"title": "x"}, testActor())
	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
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
			require.NoError(t, svc.Create(context.Background(), tk))

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
		require.NoError(t, svc.Create(context.Background(), tk))

		var got models.Ticket
		require.NoError(t, db.First(&got, "id = ?", tk.ID.String()).Error)
		require.NotNil(t, got.ClosedAt)
		assert.WithinDuration(t, t1, *got.ClosedAt, time.Second)
	})
}
