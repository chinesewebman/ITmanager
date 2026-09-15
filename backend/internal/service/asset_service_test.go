package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMockDB 拿一个 sqlmock 驱动的 *gorm.DB（C-F15 业务层测试基建）
// 跟生产用相同的 postgres dialect，让 SQL 语法校验通过
func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// ==================== Alert Service 测试 ====================

func TestAlertService_Get_存在返回alert(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	hostID := uuid.New()
	now := time.Now()
	zabbixID := "zb-123"

	rows := sqlmock.NewRows([]string{
		"id", "alert_id", "host_id", "host_name", "host_ip",
		"trigger_name", "trigger_id", "severity", "severity_name",
		"problem", "problem_start", "problem_end", "duration",
		"status", "ack_time", "ack_user", "resolve_time", "resolve_user",
		"ticket_id", "asset_id", "source", "repeat_count", "created_at", "updated_at",
	}).AddRow(
		id, zabbixID, hostID, "host-1", "10.0.0.1",
		"CPU 100%", "trig-1", 4, "High",
		"CPU usage over 90%", now, nil, 300,
		"problem", nil, "", nil, "",
		nil, nil, "zabbix", 0, now, now,
	)

	// GORM First(id) 走 SQL: SELECT * FROM "alerts" WHERE id = $1 ORDER BY "alerts"."id" LIMIT $2
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(context.Background(), id.String())
	require.NoError(t, err)
	assert.Equal(t, "problem", got.Status)
	assert.Equal(t, "CPU 100%", got.TriggerName)
	assert.Equal(t, "zabbix", got.Source)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	// GORM First() 找不到会扫一次 rows = empty
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"})) // 空

	_, err := svc.Get(context.Background(), id.String())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Acknowledge_成功更新状态(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	userID := uuid.New().String()
	now := time.Now()

	// 1) First(id) 拿 record
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "status", "ack_time", "ack_user", "updated_at",
		}).AddRow(id, "problem", nil, "", now))

	// 2) Save() 走 UPDATE
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Acknowledge(context.Background(), id.String(), userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Resolve_成功更新状态(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	userID := uuid.New().String()
	now := time.Now()

	// 1) First 拿 record
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "status", "resolve_time", "resolve_user", "updated_at",
		}).AddRow(id, "problem", nil, "", now))

	// 2) Save 走 UPDATE
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Resolve(context.Background(), id.String(), userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_DeleteRule_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "alert_rules"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.DeleteRule(context.Background(), id.String())
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_ListRules_空集合(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "alert_rules"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"})) // 空

	rules, err := svc.ListRules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rules)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== User Service 测试（最简业务） ====================

func TestUserService_List_空表返回空(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)

	// 🐛 BUG#26: List 现在走 Count + Find 两步
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "users"`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username"}))

	users, _, err := svc.List(context.Background(), 1, 20)
	require.NoError(t, err)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== ErrNotFound 边界测试 ====================

func TestErrNotFound_被业务方法正确返回(t *testing.T) {
	// 确保 ErrNotFound 是 sentinel error
	require.True(t, errors.Is(ErrNotFound, ErrNotFound))

	// sentinel 不应跟其他 err 相等
	require.False(t, errors.Is(ErrNotFound, errors.New("other")))
}

// 防止编译时 unused
var _ = sql.ErrNoRows

// ==================== Asset Service 测试 (Batch B 覆盖率) ====================

// 拿一个 asset 行（id/name/asset_tag/sn/status/asset_type + 6 个元数据字段，共 10 列）
func assetSampleRows(id string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "asset_tag", "sn", "status", "asset_type",
		"created_at", "updated_at",
	}).AddRow(id, "web-01", "AT-001", "SN-1", "active", "server", time.Now(), time.Now())
}

func TestAssetService_List_空表返回空(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	// 🐛 BUG#26: List 走 Count + Find
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "assets"`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "assets"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))

	items, total, err := svc.List(context.Background(), AssetFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_List_带keyword和status过滤(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New().String()
	// Count 走 filter (但 gorm 先 Count 再 Find, Count 也带 WHERE)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "assets" WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// Find 走 keyword ILIKE + status=
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE`).
		WithArgs("%web%", "%web%", "%web%", "active", 20).
		WillReturnRows(assetSampleRows(id))
	// M63: List 多一条 IN 查询投影主 IP（无网卡的资产不会出现在结果里）
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id IN`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "ipv4_address"}).
			AddRow(uuid.New(), id, "10.0.0.7"))

	items, total, err := svc.List(context.Background(), AssetFilter{
		Keyword: "web", Status: "active", Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, items, 1)
	assert.Equal(t, "web-01", items[0].Name)
	require.NotNil(t, items[0].IpAddress, "M63: 列表项必须带上投影出的主 IP")
	assert.Equal(t, "10.0.0.7", *items[0].IpAddress)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Get_成功返回asset和networks(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// 1) First(asset) — gorm 发 2 args (id, LIMIT 1)
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	// 2) Find(networks by asset_id)
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "asset_id", "ip_address", "ipv6_address", "mac", "interface_name", "speed_mbps",
		}).AddRow(uuid.New(), id, "10.0.0.1", nil, "00:11:22:33:44:55", "eth0", 1000))

	asset, networks, err := svc.Get(context.Background(), id.String())
	require.NoError(t, err)
	assert.Equal(t, "web-01", asset.Name)
	assert.Len(t, networks, 1)
	assert.Equal(t, "eth0", networks[0].InterfaceName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Create_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	// gorm Create 走 INSERT
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "assets"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	asset := &models.Asset{Name: "db-01", AssetTag: "AT-002"}
	err := svc.Create(context.Background(), asset, nil)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Create_空name返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewAssetService(gormDB)

	// 空 name 早返, 不打 DB
	err := svc.Create(context.Background(), &models.Asset{Name: "  "}, nil)
	assert.ErrorIs(t, err, ErrInvalidInput)

	err2 := svc.Create(context.Background(), nil, nil)
	assert.ErrorIs(t, err2, ErrInvalidInput)
}

func TestAssetService_Update_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// tx 包 First + UPDATE; ipAddress=nil → updateFirstNetworkIP 直接返 nil，不发任何 SQL
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	updates := map[string]interface{}{"status": "maintenance"}
	asset, err := svc.Update(context.Background(), id.String(), updates, nil)
	require.NoError(t, err)
	assert.NotNil(t, asset)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Update_空updates只Get不写DB(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// 空 map → 走 Get → First(asset) + Find(networks)
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	asset, err := svc.Update(context.Background(), id.String(), map[string]interface{}{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, asset)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Update_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// tx 包 First — 找不到返 ErrNotFound
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"})) // 空
	mock.ExpectRollback()

	asset, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"name": "x"}, nil)
	assert.Nil(t, asset)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// PATCH 撞 net_box_id 唯一索引（000015）必须映射成 409 而不是 500（审计 F-8）。
func TestAssetService_Update_唯一冲突返ErrAlreadyExists(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "idx_assets_net_box_id" (SQLSTATE 23505)`))
	mock.ExpectRollback()

	asset, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"net_box_id": 42}, nil)
	assert.Nil(t, asset)
	assert.ErrorIs(t, err, ErrAlreadyExists, "唯一冲突应映射成 409，不是原样 500")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Delete_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// gorm Delete 走事务
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "assets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Delete(context.Background(), id.String())
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_List_PageSizeClamp_500上限(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	// PageSize=999 应被 clamp 到 500
	mock.ExpectQuery(`SELECT count\(\*\) FROM "assets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// Find 走 ORDER BY + OFFSET + LIMIT(500)
	mock.ExpectQuery(`SELECT \* FROM "assets" ORDER BY created_at DESC`).
		WithArgs(500). // 1 arg: limit only (offset 0)
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))

	_, _, err := svc.List(context.Background(), AssetFilter{Page: 1, PageSize: 999})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== B4: Retire / Restore 测试 ====================

// retiredAssetSampleRows 返回 status=active 的 asset row (Retire 前置 First)
func activeAssetSampleRows(id string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "asset_tag", "sn", "status", "asset_type",
		"created_at", "updated_at",
	}).AddRow(id, "web-01", "AT-001", "SN-1", "active", "server", time.Now(), time.Now())
}

func TestAssetService_Retire_成功_IP转移到last_known(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	userID := uuid.New()
	netID := uuid.New()

	// M58: 事务边界从「只包写」扩到「读+写」—— 网卡快照与随后的清空 IP 必须在同一快照里
	// （否则并发改网卡会丢 last_known），且 Retire 与 BulkRetire 共用同一内核后只能有一种次序。
	mock.ExpectBegin()
	// 1) First 拿 asset (active) — First(id, ?) gorm 发 SELECT * WHERE id = $1 ORDER BY id LIMIT $2, 2 args (uid + 1)
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(activeAssetSampleRows(id.String()))
	// 2) Find networks (取 IPv4)
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "interface_name", "ipv4_address", "ipv6_address"}).
			AddRow(netID, id, "eth0", "192.168.3.50", ""))
	// 3a) UPDATE asset (Model.Updates 走 Exec, 走 Update 0 行也返 nil)
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// 3b) UPDATE asset_networks (清空 IP)
	mock.ExpectExec(`UPDATE "asset_networks" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// 4) 重读 networks (查最终态, 仍在事务内)
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ipv4_address", "ipv6_address"}).
			AddRow(netID, "", ""))
	mock.ExpectCommit()

	asset, networks, err := svc.Retire(context.Background(), id.String(), "设备下架", userID)
	require.NoError(t, err)
	assert.NotNil(t, asset)
	assert.Equal(t, "retired", asset.Status)
	require.NotNil(t, asset.LastKnownIP4)
	assert.Equal(t, "192.168.3.50", *asset.LastKnownIP4)
	assert.Nil(t, asset.LastKnownIP6)
	require.NotNil(t, asset.RetiredReason)
	assert.Equal(t, "设备下架", *asset.RetiredReason)
	require.NotNil(t, asset.RetiredBy)
	assert.Equal(t, userID, *asset.RetiredBy)
	assert.NotEmpty(t, networks)
	assert.Equal(t, "", networks[0].IPv4Address) // IP 已清空
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Retire_重复退役返ErrInvalidInput(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "name", "status", "asset_type", "created_at", "updated_at",
	}).AddRow(id, "web-01", "retired", "server", time.Now(), time.Now())
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(rows)
	mock.ExpectRollback()

	_, _, err := svc.Retire(context.Background(), id.String(), "再来一次", uuid.New())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Retire_不存在返ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectRollback()

	_, _, err := svc.Retire(context.Background(), id.String(), "x", uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 非法 uuid 在读表之前就返回 → 事务开了又立刻回滚，不产生任何业务 SQL。
func TestAssetService_Retire_非法UUID返ErrInvalidInput(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	mock.ExpectBegin()
	mock.ExpectRollback()

	_, _, err := svc.Retire(context.Background(), "not-a-uuid", "x", uuid.New())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Restore_成功_IP写回网卡(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	netID := uuid.New()
	lastIP4 := "192.168.3.50"

	// 1) First asset (retired with last_known_ip4) — 用 uuid.UUID 作为 First 参数 (Trap 9)
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "status", "asset_type", "last_known_ip4",
			"created_at", "updated_at",
		}).AddRow(id, "web-01", "retired", "server", lastIP4, time.Now(), time.Now()))
	// 2) Find networks (空 IP 待写回)
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "interface_name", "ipv4_address", "ipv6_address"}).
			AddRow(netID, id, "eth0", "", ""))
	// 3) Transaction Begin
	mock.ExpectBegin()
	// 3a) UPDATE asset_networks (写回 IPv4)
	mock.ExpectExec(`UPDATE "asset_networks" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// 3b) UPDATE asset (status=active, 清空 retired_*)
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// 4) 重读
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ipv4_address"}).
			AddRow(netID, lastIP4))
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow(id, "active"))

	asset, networks, err := svc.Restore(context.Background(), id.String())
	require.NoError(t, err)
	assert.NotNil(t, asset)
	assert.Equal(t, "active", asset.Status)
	assert.Nil(t, asset.RetiredAt)
	assert.Nil(t, asset.LastKnownIP4)
	require.NotEmpty(t, networks)
	assert.Equal(t, lastIP4, networks[0].IPv4Address)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// M23：多网卡资产的 Restore —— 只能写第一张网卡。
//
// 早先这里 IPv6 那支漏了「只有第一张」的守卫（IPv4 那支有 i == 0，IPv6 没有），
// N 张网卡的资产恢复后**每张卡都被写上同一个 IPv6** —— 一个地址同时挂在 N 个接口上。
//
// 证明手法：只设**一条** asset_networks UPDATE 期望，且 WithArgs 钉死目标是 net0。
// 若实现又去写 net1/net2，第二条 UPDATE 会撞上 sqlmock 的 unexpected call → 事务返回
// 错误 → 用例红。比「数 Exec 调用次数」更直接：不设期望就是不接受写入（同 M19 手法）。
func TestAssetService_Restore_多网卡时只写回第一张(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	net0, net1, net2 := uuid.New(), uuid.New(), uuid.New()
	lastIP4, lastIP6 := "192.168.3.50", "2001:db8::50"
	base := time.Now()

	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "status", "asset_type", "last_known_ip4", "last_known_ip6",
			"created_at", "updated_at",
		}).AddRow(id, "web-01", "retired", "server", lastIP4, lastIP6, base, base))

	// 三张网卡，Retire 已把它们（含 IPv6）全部清空。created_at 递增 → net0 是最早建的
	// 那张，也就是 Retire 取 last_known_ip* 时读的那张。
	netCols := []string{"id", "asset_id", "interface_name", "ipv4_address", "ipv6_address", "created_at"}
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows(netCols).
			AddRow(net0, id, "eth0", "", "", base).
			AddRow(net1, id, "eth1", "", "", base.Add(time.Second)).
			AddRow(net2, id, "eth2", "", "", base.Add(2*time.Second)))

	mock.ExpectBegin()
	// 唯一一条网卡写入：目标是 net0，且 IPv4/IPv6 各只出现一次
	// （GORM 对 map 形式的 Updates 按 key 排序，故 ipv4 在前；中间那个是自动带的 updated_at）
	mock.ExpectExec(`UPDATE "asset_networks" SET`).
		WithArgs(lastIP4, lastIP6, sqlmock.AnyArg(), net0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 重读：只有 net0 拿到地址，另两张仍是空的
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows(netCols).
			AddRow(net0, id, "eth0", lastIP4, lastIP6, base).
			AddRow(net1, id, "eth1", "", "", base.Add(time.Second)).
			AddRow(net2, id, "eth2", "", "", base.Add(2*time.Second)))
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(id, "active"))

	asset, networks, err := svc.Restore(context.Background(), id.String())
	require.NoError(t, err)
	require.Len(t, networks, 3)
	assert.Equal(t, "active", asset.Status)
	// 地址回到第一张卡
	assert.Equal(t, lastIP4, networks[0].IPv4Address)
	assert.Equal(t, lastIP6, networks[0].IPv6Address)
	// 关键断言：另两张卡没被写上同一个 IPv6
	assert.Empty(t, networks[1].IPv6Address, "第二张网卡不该拿到别人家的 IPv6")
	assert.Empty(t, networks[1].IPv4Address)
	assert.Empty(t, networks[2].IPv6Address, "第三张网卡不该拿到别人家的 IPv6")
	assert.Empty(t, networks[2].IPv4Address)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Restore_非退役状态返ErrInvalidInput(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(activeAssetSampleRows(id.String()))

	_, _, err := svc.Restore(context.Background(), id.String())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Restore_不存在返ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, _, err := svc.Restore(context.Background(), id.String())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAssetService_Retire_无网卡时last_known为空(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	userID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(activeAssetSampleRows(id.String()))
	// 0 张网卡
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`UPDATE "assets" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "asset_networks" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	asset, _, err := svc.Retire(context.Background(), id.String(), "无网卡", userID)
	require.NoError(t, err)
	assert.Equal(t, "retired", asset.Status)
	assert.Nil(t, asset.LastKnownIP4)
	assert.Nil(t, asset.LastKnownIP6)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== M58: BulkRetire ====================

// expectRetireCoreOnTx 铺一条「单个 id 在事务内核里退役成功」的 SQL 期望序列。
// 顺序 = retireCore 的读写序：SAVEPOINT → 读 asset → 读网卡 → 更新 asset → 清网卡 IP → 重读网卡。
//
// savepoint 名形如 `sp<random>`（gorm 用 maphash 生成，每个事务不同）→ 只能按前缀匹配。
func expectRetireCoreOnTx(mock sqlmock.Sqlmock, id, netID uuid.UUID, ip4 string) {
	mock.ExpectExec(`SAVEPOINT sp[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id, 1).
		WillReturnRows(activeAssetSampleRows(id.String()))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "interface_name", "ipv4_address", "ipv6_address"}).
			AddRow(netID, id, "eth0", ip4, ""))
	mock.ExpectExec(`UPDATE "assets" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "asset_networks" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1 ORDER BY created_at ASC, id ASC`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ipv4_address", "ipv6_address"}).AddRow(netID, "", ""))
}

func TestAssetService_BulkRetire_全部成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id1, id2 := uuid.New(), uuid.New()
	net1, net2 := uuid.New(), uuid.New()

	mock.ExpectBegin()
	expectRetireCoreOnTx(mock, id1, net1, "192.168.3.50")
	expectRetireCoreOnTx(mock, id2, net2, "192.168.3.51")
	mock.ExpectCommit()

	ok, failed, err := svc.BulkRetire(context.Background(),
		[]string{id1.String(), id2.String()}, "批量退役", uuid.New())

	require.NoError(t, err)
	assert.Equal(t, []string{id1.String(), id2.String()}, ok)
	assert.Empty(t, failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 部分失败：一条不存在 → 该条进 failed，其它照常提交。
//
// 这里同时钉住「失败只回滚自己那一格」：id2 的失败必须走 ROLLBACK TO SAVEPOINT（而不是
// 把整个事务打回），且外层仍是 COMMIT —— 没有 savepoint 时 PG 会让整个 tx 进入 aborted
// 态，后续每条语句都 25P02，即「一条不存在 ⇒ 全批失败」。
func TestAssetService_BulkRetire_部分失败_其它仍提交(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id1, id2 := uuid.New(), uuid.New()
	net1 := uuid.New()

	mock.ExpectBegin()
	expectRetireCoreOnTx(mock, id1, net1, "192.168.3.50")
	// id2: savepoint 后读 asset 就 not found → 回滚该 savepoint
	mock.ExpectExec(`SAVEPOINT sp[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id2, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectExec(`ROLLBACK TO SAVEPOINT sp[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	ok, failed, err := svc.BulkRetire(context.Background(),
		[]string{id1.String(), id2.String()}, "批量退役", uuid.New())

	require.NoError(t, err)
	assert.Equal(t, []string{id1.String()}, ok)
	assert.Equal(t, map[string]string{id2.String(): "资产不存在"}, failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 非法 uuid：不碰 DB（除 savepoint 骨架），错误进 failed 而非整体 500。
func TestAssetService_BulkRetire_非法UUID进failed(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	mock.ExpectBegin()
	mock.ExpectExec(`SAVEPOINT sp[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ROLLBACK TO SAVEPOINT sp[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	ok, failed, err := svc.BulkRetire(context.Background(), []string{"not-a-uuid"}, "x", uuid.New())

	require.NoError(t, err)
	assert.Empty(t, ok)
	assert.Equal(t, map[string]string{"not-a-uuid": "无法退役（资产已退役或参数无效）"}, failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== M63: 资产 IP 投影 (G-UI-AssetIpPersistence) ====================

// netCard 造一张网卡：只有 IPv4/IPv6 两列参与判据，其余字段与用例无关。
func netCard(v4, v6 string) models.AssetNetwork {
	return models.AssetNetwork{IPv4Address: v4, IPv6Address: v6}
}

// 指针入参用文件里已有的 strPtr（user_service_test.go:157）—— go.mod 还是 1.25.0，
// Go 1.26 的 `new(值)` 在本模块里用不了。

// M63: 主 IP 判据 —— 先第一个非空 IPv4，否则第一个非空 IPv6。
// 这是资产列表/详情 ip_address 与复盘报告头 fetchIP 共用的**唯一**一份实现。
func TestM63_PickPrimaryIP_判据(t *testing.T) {
	cases := []struct {
		name     string
		networks []models.AssetNetwork
		wantV4   *string
		wantV6   *string
	}{
		{"空 networks", nil, nil, nil},
		{"全 v4 取第一张", []models.AssetNetwork{netCard("10.0.0.1", ""), netCard("10.0.0.2", "")}, strPtr("10.0.0.1"), nil},
		{"全 v6 取第一张", []models.AssetNetwork{netCard("", "fe80::1"), netCard("", "fe80::2")}, nil, strPtr("fe80::1")},
		{"v4 与 v6 并存 → 各自第一张",
			[]models.AssetNetwork{netCard("10.0.0.1", "fe80::1")}, strPtr("10.0.0.1"), strPtr("fe80::1")},
		{"v6 在前 v4 在后 → 顺序无关",
			[]models.AssetNetwork{netCard("", "fe80::1"), netCard("10.0.0.1", "")}, strPtr("10.0.0.1"), strPtr("fe80::1")},
		{"第一张 v4 是空串 → 跳过它, v6 照样取到",
			[]models.AssetNetwork{netCard("", "fe80::1")}, nil, strPtr("fe80::1")},
		{"第一张 v6 是空串 → v4 照样取到",
			[]models.AssetNetwork{netCard("10.0.0.1", "")}, strPtr("10.0.0.1"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v4, v6 := pickPrimaryIP(c.networks)
			if c.wantV4 == nil {
				assert.Nil(t, v4)
			} else {
				require.NotNil(t, v4)
				assert.Equal(t, *c.wantV4, *v4)
			}
			if c.wantV6 == nil {
				assert.Nil(t, v6)
			} else {
				require.NotNil(t, v6)
				assert.Equal(t, *c.wantV6, *v6)
			}
		})
	}
}

// M63: 单值投影（列表/详情实际用的那个）：v4 优先，否则 v6，都没有 → nil。
func TestM63_PrimaryIP_单值投影(t *testing.T) {
	assert.Nil(t, primaryIP(nil), "无网卡 → null")
	assert.Nil(t, primaryIP([]models.AssetNetwork{netCard("", "")}), "网卡都在但没填 IP → null")

	v4v6 := []models.AssetNetwork{netCard("", "fe80::1"), netCard("10.0.0.1", "")}
	require.NotNil(t, primaryIP(v4v6))
	assert.Equal(t, "10.0.0.1", *primaryIP(v4v6), "有 v4 时不用 v6（哪怕 v4 在后面的卡上）")

	v6only := []models.AssetNetwork{netCard("", "fe80::1")}
	require.NotNil(t, primaryIP(v6only))
	assert.Equal(t, "fe80::1", *primaryIP(v6only))
}

// M63: List 投影 —— 3 条资产：v4+v6 取 v4 / 只有 v6 取 v6 / 无网卡为 null。
// 同时钉住「只有一条 IN 查询」（sqlmock 的期望是顺序消费的：多发一条就 "was not expected"）。
func TestM63_AssetService_List_投影ip_address(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	a1, a2, a3 := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "assets"`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "assets"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).
			AddRow(a1, "web-01").AddRow(a2, "web-02").AddRow(a3, "web-03"))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id IN \(\$1,\$2,\$3\) ORDER BY created_at ASC, id ASC`).
		WithArgs(a1, a2, a3).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "ipv4_address", "ipv6_address"}).
			AddRow(uuid.New(), a1, "10.0.0.1", "fe80::1"). // 同一张卡有 v4 与 v6 → v4
			AddRow(uuid.New(), a1, "10.0.0.2", "").        // 第二张卡不改变结论（第一张已有 v4）
			AddRow(uuid.New(), a2, "", "fe80::2"))         // 只有 v6 → v6

	items, total, err := svc.List(context.Background(), AssetFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, items, 3)

	require.NotNil(t, items[0].IpAddress)
	assert.Equal(t, "10.0.0.1", *items[0].IpAddress, "v4 优先，且是排序后的第一张卡")
	require.NotNil(t, items[1].IpAddress)
	assert.Equal(t, "fe80::2", *items[1].IpAddress, "没有 v4 时用 v6")
	assert.Nil(t, items[2].IpAddress, "无网卡的资产 → null（不是空串）")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// M63: Get 投影 —— 复用已经在手的 networks（不再多发一条查询）。
// v4 在**第二张**卡上：证明判据是「第一个非空 v4」而不是「第一张卡的 IP」。
func TestM63_AssetService_Get_投影ip_address(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "asset_networks" WHERE asset_id = $1 ORDER BY created_at ASC, id ASC`)).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id", "ipv4_address", "ipv6_address"}).
			AddRow(uuid.New(), id, "", "fe80::1").
			AddRow(uuid.New(), id, "10.0.0.1", ""))

	asset, networks, err := svc.Get(context.Background(), id.String())
	require.NoError(t, err)
	require.Len(t, networks, 2, "Get 仍返回全部网卡（详情页的网络接口表）")
	require.NotNil(t, asset.IpAddress)
	assert.Equal(t, "10.0.0.1", *asset.IpAddress, "v4 优先：v4 在第二张卡上也要选它")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// M63: 无网卡资产的 Get —— ip_address 为 nil（前端 `!record.ip_address` 禁用 Ping/Traceroute）。
func TestM63_AssetService_Get_无网卡时ip_address为nil(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "assets" WHERE id = \$1`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks" WHERE asset_id = \$1`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "asset_id"}))

	asset, networks, err := svc.Get(context.Background(), id.String())
	require.NoError(t, err)
	assert.Empty(t, networks)
	assert.Nil(t, asset.IpAddress)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// sqlCapture 记录驱动**实际收到**的 SQL。
//
// 为什么需要它：sqlmock 的期望匹配只能断言「某条 SQL 被发过」，无法断言「某列没被写」——
// 而 M63 要证明的恰恰是**没有** ip_address 出现在 INSERT/SET 里。期望仍要声明（sqlmock
// 按顺序消费），这里用 `.*` 全接受，把 actual SQL 抄下来供用例直接读。
type sqlCapture struct{ stmts []string }

func (c *sqlCapture) Match(_, actualSQL string) error {
	c.stmts = append(c.stmts, actualSQL)
	return nil
}

// has 是否有语句包含 sub（子串，非正则 —— 调用的都是带引号的列名）。
func (c *sqlCapture) has(sub string) bool {
	for _, s := range c.stmts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func newCapturingDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sqlCapture) {
	t.Helper()
	cap := &sqlCapture{}
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(cap))
	require.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock, cap
}

// M63/M64: `ip_address` 是虚拟投影字段 —— 它既不进 `assets` 的 INSERT（`gorm:"-"` 让它在
// schema 解析阶段就被排除在 Fields 之外），也**不是写入路径**：M64 起 IP 由 Create 的显式
// 入参 `ipAddress` 给出（handler 从同一个 JSON 键取，见 asset_handler.CreateAsset）。
//
// 本用例钉的正是这条分工：把值塞进模型上的虚拟字段（其余入参为 nil）**什么都不该发生**。
// 它是 M63 那个 bug（表单里的 IP 写完没影）的机制本身 —— 现在这个机制无害化了，
// 因为真写入走的是另一条显式通道。
func TestM63_AssetService_Create_虚拟字段不进INSERT也不建网卡(t *testing.T) {
	gormDB, mock, cap := newCapturingDB(t)
	svc := NewAssetService(gormDB)

	mock.ExpectBegin()
	mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := svc.Create(context.Background(), &models.Asset{Name: "web-01", IpAddress: strPtr("10.0.0.1")}, nil)
	require.NoError(t, err)

	require.True(t, cap.has(`INSERT INTO "assets"`), "Create 必须发出 INSERT：%v", cap.stmts)
	assert.False(t, cap.has("ip_address"), "assets 表没有 ip_address 列，INSERT 不得带上它：%v", cap.stmts)
	assert.False(t, cap.has(`INSERT INTO "asset_networks"`),
		"虚拟字段不是写入路径 —— 入参 ipAddress 为 nil 时不得建网卡：%v", cap.stmts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// M63 (T-76): Update 走的 `db.Updates(map)` **不丢弃**模型里没有的键 —— GORM v1.30.0 照样
// 生成 `SET "ip_address"=$n`（callbacks/update.go: LookUpField 为 nil 时仍 append Assignment）。
// 真 PG 上这是 42703 → 500。所以 handler 必须在入口剥掉这个键（asset_handler.UpdateAsset）。
//
// **M66 重写**：本轮不再走「handler 剥键 → Update 走 db.Updates(map)」这条路，
// 改成「handler 抽键 → 单独走 updateFirstNetworkIP 写网卡」。`updates` map 里不再含 `ip_address`。
// 本用例钉的是「updates map 不含 ip_address 即不发出 SET ip_address=」 —— 真 PG 上既不会
// 撞 42703（键没了），又会让网卡表收到 IP（走 tx 另一条路径）。
func TestM66_AssetService_Update_ipAddress_走独立参数不混进map(t *testing.T) {
	gormDB, mock, cap := newCapturingDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	// tx 包 First(asset) + UPDATE + 网卡查 + 网卡 INSERT
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// M68: v4 冲突守卫（在 updateFirstNetworkIP 内、UPDATE assets 之后、SELECT 网卡之前），
	// IP=10.0.0.1 没被占用，期望 ErrRecordNotFound（不是「有占用」）。
	mock.ExpectQuery(`SELECT id FROM asset_networks WHERE ipv4_address = \$1 AND asset_id <> \$2 AND ipv4_address <> ''`).
		WithArgs("10.0.0.1", id.String()).
		WillReturnError(gorm.ErrRecordNotFound)
	// 网卡先查再写
	mock.ExpectQuery(`SELECT \* FROM "asset_networks"`).
		WithArgs(id.String(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// GORM tx.Create(...) 在 PG 是 RETURNING Query —— sqlmock ExpectQuery 而不是 ExpectExec
	mock.ExpectQuery(`INSERT INTO "asset_networks"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	ip := "10.0.0.1"
	_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{
		"name": "web-02",
		// 故意不再带 ip_address —— M66 后它走独立参数
	}, &ip)
	require.NoError(t, err)
	assert.False(t, cap.has(`"ip_address"`),
		"updates map 不应再含 ip_address（否则 handler 没剥，仍会撞 42703）：%v", cap.stmts)
	assert.True(t, cap.has(`INSERT INTO "asset_networks"`),
		"ip_address 独立参数应触网卡表写入：%v", cap.stmts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== M68: 同 IP 多资产守卫 (G-Asset-IpConflictGuard) ====================

// TestM68_AssetService_ip被其他资产占用_返ErrIPConflict
// 真实业务场景：资产 B 的网卡已经持有 1.2.3.4，资产 A 想 POST/PUT 占同一 IP，必须 409。
//
// sqlmock 写法：guard SELECT 直接返一行（taken.ID != uuid.Nil）→ 立即 ErrIPConflict，
// 不进 SELECT 网卡、不进 UPDATE/INSERT 网卡。如果 guard 漏走，UPDATE 网卡仍然发生 →
// 测试通过反而是 bug。Mutation inversion 实证见 service 层 TestM68_Mutation_跳过guard_写成功。
func TestM68_AssetService_ip被其他资产占用_返ErrIPConflict(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	otherAssetID := uuid.New() // 占用同一 IP 的"别人"
	ip := "1.2.3.4"

	// tx 包：First(asset) → UPDATE assets → guard SELECT 命中 → Rollback。
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id FROM asset_networks WHERE ipv4_address = \$1 AND asset_id <> \$2 AND ipv4_address <> ''`).
		WithArgs(ip, id).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(otherAssetID))
	mock.ExpectRollback()

	_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"name": "web-02"}, &ip)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrIPConflict, "占用冲突应映 ErrIPConflict（不是 ErrAlreadyExists）")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestM68_AssetService_自己资产持同一IP_不冲突
// UPDATE 资产 A 的网卡到 A 现在已有的 IP（值不变）→ 不是冲突；guard 的 `asset_id <> ?`
// 必须**严格排除自己**，否则 UPDATE 同一资产会误报冲突。
func TestM68_AssetService_自己资产持同一IP_不冲突(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	ip := "1.2.3.4"

	// tx 包：First(asset) → UPDATE assets → guard SELECT 0 行 → SELECT 网卡 → UPDATE 网卡 → Commit。
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id FROM asset_networks WHERE ipv4_address = \$1 AND asset_id <> \$2 AND ipv4_address <> ''`).
		WithArgs(ip, id).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectQuery(`SELECT \* FROM "asset_networks"`).
		WithArgs(id.String(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec(`UPDATE "asset_networks"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"name": "web-02"}, &ip)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestM68_AssetService_v6_不参与v4校验
// v6 地址 (`fe80::1`) 不进 v4 守卫 → 守卫 SELECT 不应被调用。这条用例钉的是"v6 留 future"
// 这个 decision 的具体行为：守卫 SQL 含 ipv4_address 字段，v6 解析时 parsed.To4() == nil
// 直接跳过。
func TestM68_AssetService_v6_不参与v4校验(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAssetService(gormDB)

	id := uuid.New()
	ip := "fe80::1"

	// tx 包：First(asset) → UPDATE assets → 直接进 SELECT 网卡（v6 跳过 guard）→ INSERT 网卡 → Commit。
	// 没有 guard SELECT 期望 —— 如果守卫跑了，这个测试 fail。
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "assets"`).
		WithArgs(id.String(), 1).
		WillReturnRows(assetSampleRows(id.String()))
	mock.ExpectExec(`UPDATE "assets"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "asset_networks"`).
		WithArgs(id.String(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`INSERT INTO "asset_networks"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"name": "web-02"}, &ip)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== M64: Create 写 AssetNetwork (G-Asset-NetworksPersist) ====================

// newAssetSQLiteDB 真 sqlite + 手写 DDL（口径同 models/hooks_test.go 的 assetSchema）。
//
// 不用 AutoMigrate：models.Asset.ID 带 `default:gen_random_uuid()`，sqlite 上没有该函数。
//
// 为什么这一组用真库而不是 sqlmock：被测行为是「网卡表里**到底有没有那一行、值落在哪一列**」。
// sqlmock 只能断言「某条 SQL 被发过」，行内容得由测试作者手写进期望里 —— 而「写是写了、
// 写错了列/写错了行」正是这类改动最容易出的缺陷，恰恰是手写期望盖不住的地方。
func newAssetSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	stmts := []string{
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			asset_tag TEXT,
			sn TEXT,
			asset_type TEXT,
			brand TEXT,
			model TEXT,
			site_id TEXT,
			site_name TEXT,
			rack_id TEXT,
			rack_name TEXT,
			rack_position TEXT,
			purchase_date DATETIME,
			warranty_end DATETIME,
			vendor TEXT,
			vendor_contact TEXT,
			status TEXT DEFAULT 'active',
			online_time DATETIME,
			offline_time DATETIME,
			last_known_ip4 TEXT,
			last_known_ip6 TEXT,
			retired_at DATETIME,
			retired_reason TEXT,
			retired_by TEXT,
			business_unit TEXT,
			service_name TEXT,
			tags TEXT,
			custom_fields TEXT,
			net_box_id INTEGER,
			source TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			interface_name TEXT NOT NULL,
			interface_type TEXT,
			mac_address TEXT,
			ipv4_address TEXT,
			ipv4_netmask TEXT,
			ipv6_address TEXT,
			speed INTEGER,
			duplex TEXT,
			status TEXT,
			connected_to TEXT,
			connected_port TEXT,
			purpose TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// networksOf 回库读某资产的网卡（顺序对齐 listNetworks：created_at ASC, id ASC）。
func networksOf(t *testing.T, db *gorm.DB, assetID uuid.UUID) []models.AssetNetwork {
	t.Helper()
	var rows []models.AssetNetwork
	require.NoError(t, db.Where("asset_id = ?", assetID).
		Order("created_at ASC, id ASC").Find(&rows).Error)
	return rows
}

func assetRowCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.Asset{}).Count(&n).Error)
	return n
}

// 没给 IP：只有资产行。网卡表必须**空** —— 凭空造一张空网卡会让 GET 的 ip_address
// 投影与「这个资产有几个网口」的问题都失去答案（前端 `!record.ip_address` 判据还行，
// 但列表里会长出一张不存在的接口）。
func TestM64_AssetService_Create_无IP时只建资产行(t *testing.T) {
	db := newAssetSQLiteDB(t)
	svc := NewAssetService(db)

	asset := &models.Asset{Name: "web-01", AssetType: "server"}
	require.NoError(t, svc.Create(context.Background(), asset, nil))

	assert.EqualValues(t, 1, assetRowCount(t, db))
	assert.Empty(t, networksOf(t, db, asset.ID), "没给 IP 就不该建网卡")
}

// 空串（含纯空白）与「未提供」同义：表单里没填的 IP 会以 `""` 到达（AssetFormValues.ip_address
// 是 string 不是可选），把它当成一个 IP 去解析会得到 nil → 更不能拿 nil 去建网卡。
func TestM64_AssetService_Create_空IP视作未提供(t *testing.T) {
	for _, ip := range []string{"", "   "} {
		t.Run(ip, func(t *testing.T) {
			db := newAssetSQLiteDB(t)
			svc := NewAssetService(db)

			asset := &models.Asset{Name: "web-01", AssetType: "server"}
			require.NoError(t, svc.Create(context.Background(), asset, strPtr(ip)))

			assert.EqualValues(t, 1, assetRowCount(t, db))
			assert.Empty(t, networksOf(t, db, asset.ID), "空串不是 IP，不该建网卡")
		})
	}
}

// IPv4：落 ipv4_address，ipv6_address 必须留空。
// 两列并存正是为区分族 —— 都塞一列会让 pickPrimaryIP 的判据（先第一个非空 v4）读出错误结果。
func TestM64_AssetService_Create_IPv4落ipv4列(t *testing.T) {
	db := newAssetSQLiteDB(t)
	svc := NewAssetService(db)

	asset := &models.Asset{Name: "web-01", AssetType: "server"}
	require.NoError(t, svc.Create(context.Background(), asset, strPtr("10.0.0.5")))

	nets := networksOf(t, db, asset.ID)
	require.Len(t, nets, 1, "给了 IP 必须恰好建一张网卡")
	assert.Equal(t, asset.ID, nets[0].AssetID, "网卡必须挂在这张资产下")
	assert.Equal(t, "eth0", nets[0].InterfaceName, "接口名是这一轮的产品决定（多网卡另立 G-Asset-MultiNetwork）")
	assert.Equal(t, "10.0.0.5", nets[0].IPv4Address)
	assert.Empty(t, nets[0].IPv6Address, "v4 不得落到 v6 列")
}

// IPv6：落 ipv6_address，ipv4_address 必须留空（反向的同一件事）。
// 第二条子用例钉「落库的是解析后的规范形式」：`net.ParseIP().String()` 会把 2001:0db8:0:0::1
// 归一成 2001:db8::1 —— 同一个地址的两种写法若原样入库，按字符串比对的去重/检索就会各算一条。
func TestM64_AssetService_Create_IPv6落ipv6列(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2001:db8::1", "2001:db8::1"},
		{"2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			db := newAssetSQLiteDB(t)
			svc := NewAssetService(db)

			asset := &models.Asset{Name: "web-01", AssetType: "server"}
			require.NoError(t, svc.Create(context.Background(), asset, strPtr(tc.in)))

			nets := networksOf(t, db, asset.ID)
			require.Len(t, nets, 1)
			assert.Equal(t, tc.want, nets[0].IPv6Address)
			assert.Empty(t, nets[0].IPv4Address, "v6 不得落到 v4 列（pickPrimaryIP 会把它当 v4 读出）")
		})
	}
}

// 非法 IP：service 直接拒绝，且**一张资产行都不落**。
// 早返必须发生在事务之前：先建资产再发现 IP 坏了，会留下半落状态（资产在、IP 没了），
// 而调用方（handler 已挡在入口，这里是给 db_smoke/导入器那类直接调用方的兜底）会当整条失败。
func TestM64_AssetService_Create_非法IP不落库(t *testing.T) {
	db := newAssetSQLiteDB(t)
	svc := NewAssetService(db)

	err := svc.Create(context.Background(), &models.Asset{Name: "web-01", AssetType: "server"}, strPtr("not-an-ip"))
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Zero(t, assetRowCount(t, db), "非法 IP 不得留下任何行")
}

// 网卡那一步失败时资产行必须一起回滚。
//
// 手法：把 asset_networks 整张表删掉，让第二条 INSERT 撞上**真的 DB 错误**（"no such table"），
// 而不是靠 mock 编排一个假错误 —— 要证的正是真驱动上事务边界的实际行为。
// 若 Create 不包事务（先 Create(asset) 再 Create(network)），资产行会留下而接口返回 500。
func TestM64_AssetService_Create_网卡写失败时资产行一并回滚(t *testing.T) {
	db := newAssetSQLiteDB(t)
	require.NoError(t, db.Exec(`DROP TABLE asset_networks`).Error)
	svc := NewAssetService(db)

	err := svc.Create(context.Background(), &models.Asset{Name: "web-01", AssetType: "server"}, strPtr("10.0.0.5"))
	require.Error(t, err, "网卡写不进去就必须报错，不得谎报成功")
	assert.Zero(t, assetRowCount(t, db),
		"资产行必须随网卡行一起回滚 —— 半落状态会让调用方重试时撞 name 唯一约束")
}
