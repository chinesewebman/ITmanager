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
	"gorm.io/gorm"
)

// ==================== Rack Service 测试 ====================

func TestRackService_ListRacks_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	rack1 := uuid.NewString()
	rack2 := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "site_id", "total_u", "used_u"}).
		AddRow(rack1, "rack-1", uuid.NewString(), 42, 20).
		AddRow(rack2, "rack-2", uuid.NewString(), 42, 0)

	mock.ExpectQuery(`SELECT \* FROM "racks"`).
		WillReturnRows(rows)

	// ListRacks 还会发 SELECT rack_id, COUNT(*) AS used FROM assets ... GROUP BY rack_id
	usedRows := sqlmock.NewRows([]string{"rack_id", "used"}).
		AddRow(rack1, 20).
		AddRow(rack2, 0)
	mock.ExpectQuery(`SELECT rack_id, COUNT\(\*\) AS used FROM "assets"`).
		WillReturnRows(usedRows)

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

	// toRackDTOs 会发 SELECT rack_id, COUNT(*) AS used GROUP BY rack_id
	usedRows := sqlmock.NewRows([]string{"rack_id", "used"}).AddRow(id, 5)
	mock.ExpectQuery(`SELECT rack_id, COUNT\(\*\) AS used FROM "assets"`).
		WillReturnRows(usedRows)

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

func TestRackService_GetSite_存在返回详情(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	// GetSite: First sites
	rows := sqlmock.NewRows([]string{"id", "name", "location", "contact"}).
		AddRow(id, "site-1", "BJ", "ops@example.com")
	mock.ExpectQuery(`SELECT \* FROM "sites" WHERE id =`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	mock.ExpectQuery(`SELECT count\(\*\) FROM "racks" WHERE site_id =`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectQuery(`SELECT count\(\*\) FROM "assets" WHERE site_id =`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	site, err := svc.GetSite(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, site)
	assert.Equal(t, id, site.Site.ID.String(), "Site.ID 是 uuid.UUID, 转 string 比对")
	assert.Equal(t, int64(5), site.RackCount)
	assert.Equal(t, int64(42), site.AssetCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRackService_GetSite_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "sites" WHERE id =`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	site, err := svc.GetSite(context.Background(), "nonexistent")
	assert.Nil(t, site)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRackService_GetRackDevices_有设备返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)
	ctx := context.Background()

	rackID := uuid.NewString()
	assetID := uuid.NewString()
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "rack_position", "asset_type", "status", "host_ip", "os", "rack_id", "serial_number", "manufacturer", "model", "purchase_date", "warranty_end", "location", "owner", "tags", "created_at", "updated_at", "deleted_at", "vendor", "rack_position_end", "parent_asset_id", "environment", "criticality"}).
		AddRow(assetID, "server-1", 1, "server", "online", "10.0.0.1", "linux", rackID, "SN-001", "Dell", "R750", now, now, "BJ", "ops", "[]", now, now, nil, "Dell", 2, nil, "prod", "high")
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE rack_id =`).
		WithArgs(rackID).
		WillReturnRows(rows)

	// GetRackDevices 用 GROUP BY 一次性拿所有 alert count
	alertRows := sqlmock.NewRows([]string{"asset_id", "cnt"}).
		AddRow(assetID, 3)
	mock.ExpectQuery(`SELECT asset_id, COUNT\(\*\) AS cnt FROM "alerts"`).
		WillReturnRows(alertRows)

	devices, err := svc.GetRackDevices(ctx, rackID)
	require.NoError(t, err)
	assert.Len(t, devices, 1)
	assert.Equal(t, "server-1", devices[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRackService_GetRackDevices_DB错误透传(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewRackService(gormDB)

	dbErr := errors.New("connection refused")
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE rack_id =`).
		WithArgs("rack-x").
		WillReturnError(dbErr)

	devices, err := svc.GetRackDevices(context.Background(), "rack-x")
	assert.Nil(t, devices)
	assert.ErrorIs(t, err, dbErr)
}
