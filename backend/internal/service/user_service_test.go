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

func TestUserService_List_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "username", "email", "role"}).
		AddRow(uuid.NewString(), "admin", "admin@example.com", "admin").
		AddRow(uuid.NewString(), "user1", "user1@example.com", "user")

	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(rows)

	users, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 2)
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
