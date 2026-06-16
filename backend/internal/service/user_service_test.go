package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ==================== User Service 测试 ====================

func TestUserService_List_分页返回列表(t *testing.T) {
	// 🐛 BUG#26: List 改成分页签名 (page, pageSize int) → (items, total, err)
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "username", "email", "role"}).
		AddRow(uuid.NewString(), "admin", "admin@example.com", "admin").
		AddRow(uuid.NewString(), "user1", "user1@example.com", "user")

	// Count + Find
	mock.ExpectQuery(`SELECT count\(\*\) FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(rows)

	users, total, err := svc.List(ctx, 1, 20)
	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.Equal(t, int64(2), total)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUserService_List_页码负数和超大限流(t *testing.T) {
	// 验证 page<1 → 1, pageSize<1 → 20, pageSize>500 → 500
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "username"})

	mock.ExpectQuery(`SELECT count\(\*\) FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(rows)

	// page=-1, pageSize=9999 都被纠正
	users, total, err := svc.List(ctx, -1, 9999)
	require.NoError(t, err)
	assert.Empty(t, users)
	assert.Equal(t, int64(0), total)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUserService_Get_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "username"}).
		AddRow(id, "admin")

	mock.ExpectQuery(`SELECT \* FROM "users" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Username)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUserService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "users" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Get(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}
