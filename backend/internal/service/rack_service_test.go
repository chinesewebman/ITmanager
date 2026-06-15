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

// ==================== Rack Service 测试 ====================

func TestRackService_ListRacks_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "name", "site_id", "total_u", "used_u"}).
		AddRow(uuid.NewString(), "rack-1", uuid.NewString(), 42, 20).
		AddRow(uuid.NewString(), "rack-2", uuid.NewString(), 42, 0)

	mock.ExpectQuery(`SELECT \* FROM "racks"`).
		WillReturnRows(rows)

	racks, err := svc.ListRacks(ctx, "")
	require.NoError(t, err)
	assert.Len(t, racks, 2)
	assert.Equal(t, "rack-1", racks[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRackService_GetRack_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "site_id", "total_u"}).
		AddRow(id, "rack-1", uuid.NewString(), 42)

	mock.ExpectQuery(`SELECT \* FROM "racks" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.GetRack(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRackService_GetRack_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "racks" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.GetRack(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRackService_GetRackDevices_空rackId返空切片(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	// 不存在的 rackID：SELECT assets WHERE rack_id=? 返空（不调 GetRack）
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE rack_id =`).
		WithArgs("empty-rack-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "rack_position", "asset_type", "status", "host_ip", "os", "rack_id", "serial_number", "manufacturer", "model", "purchase_date", "warranty_end", "location", "owner", "tags", "created_at", "updated_at", "deleted_at", "vendor", "rack_position_end", "parent_asset_id", "environment", "criticality"}))

	devices, err := svc.GetRackDevices(ctx, "empty-rack-id")
	require.NoError(t, err)
	assert.Empty(t, devices)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRackService_ListSites_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "name", "location"}).
		AddRow(uuid.NewString(), "site-1", "BJ-Yizhuang").
		AddRow(uuid.NewString(), "site-2", "BJ-Tongzhou")

	mock.ExpectQuery(`SELECT \* FROM "sites"`).
		WillReturnRows(rows)

	sites, err := svc.ListSites(ctx)
	require.NoError(t, err)
	assert.Len(t, sites, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}
