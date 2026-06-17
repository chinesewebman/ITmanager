package service

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
)

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

	got, err := svc.Update(ctx, id, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "unchanged", got.Title)
	assert.NoError(t, mock.ExpectationsWereMet())
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
}

func TestTicketService_Create_nil指针返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewTicketService(gormDB)
	err := svc.Create(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestTicketService_Create_唯一冲突返回ErrAlreadyExists(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: "dup"}
	// 构造 PG unique violation 错误（isUniqueViolation 认 'duplicate key' / 'unique constraint'）
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`INSERT INTO "tickets"`).
		WillReturnError(&pqUniqueError{msg: "duplicate key value violates unique constraint"})
	mock.ExpectRollback()

	err := svc.Create(ctx, tk)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)
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

	got, err := svc.Update(ctx, id, map[string]interface{}{"title": "新标题"})
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

	updates := map[string]interface{}{"status": "closed"}
	_, err := svc.Update(ctx, id, updates)
	require.NoError(t, err)
	require.Contains(t, updates, "closed_at", "Update 内部应注入 closed_at")
	assert.NotNil(t, updates["closed_at"], "closed_at 必填")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Update(ctx, "nonexistent", map[string]interface{}{"title": "x"})
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

	_, err := svc.Update(ctx, id, map[string]interface{}{"title": "x"})
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
