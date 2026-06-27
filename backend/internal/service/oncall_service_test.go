package service

import (
	"context"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newOncallTestDB 建 4 张表
func newOncallTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, s := range []string{
		`CREATE TABLE oncall_schedules (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
			timezone TEXT DEFAULT 'Asia/Shanghai', enabled INTEGER DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE oncall_shifts (
			id TEXT PRIMARY KEY, schedule_id TEXT NOT NULL, user_id TEXT NOT NULL,
			user_name TEXT, starts_at DATETIME NOT NULL, ends_at DATETIME NOT NULL,
			created_at DATETIME
		)`,
		`CREATE TABLE escalation_policies (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, enabled INTEGER DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE escalation_levels (
			id TEXT PRIMARY KEY, policy_id TEXT NOT NULL, level INTEGER NOT NULL,
			target_type TEXT, target_id TEXT, wait_minutes INTEGER DEFAULT 5,
			notify_methods TEXT
		)`,
	} {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

func seedSchedule(t *testing.T, db *gorm.DB, name string, enabled bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	en := 0
	if enabled {
		en = 1
	}
	require.NoError(t, db.Exec(`INSERT INTO oncall_schedules
		(id, name, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, en, now, now).Error)
	return id
}

func seedShift(t *testing.T, db *gorm.DB, scheduleID, userID uuid.UUID, userName string, start, end time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO oncall_shifts
		(id, schedule_id, user_id, user_name, starts_at, ends_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, scheduleID, userID, userName, start, end, time.Now().UTC()).Error)
	return id
}

func seedPolicy(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO escalation_policies
		(id, name, enabled, created_at, updated_at) VALUES (?, ?, 1, ?, ?)`,
		id, name, now, now).Error)
	return id
}

func seedLevel(t *testing.T, db *gorm.DB, policyID uuid.UUID, level int, targetType, targetID, methods string, waitMin int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO escalation_levels
		(id, policy_id, level, target_type, target_id, wait_minutes, notify_methods)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, policyID, level, targetType, targetID, waitMin, methods).Error)
	return id
}

// ==================== Schedule CRUD ====================

func TestOncallService_CreateSchedule_成功(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	sched := &models.OncallSchedule{Name: "dev-team"}
	err := svc.CreateSchedule(context.Background(), sched)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, sched.ID)
}

func TestOncallService_CreateSchedule_空name报错(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	err := svc.CreateSchedule(context.Background(), &models.OncallSchedule{})
	assert.Error(t, err)
}

func TestOncallService_DeleteSchedule_级联删shifts(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	seedShift(t, db, schedID, uuid.New(), "alice", time.Now(), time.Now().Add(time.Hour))

	err := svc.DeleteSchedule(context.Background(), schedID)
	require.NoError(t, err)

	var n int64
	db.Raw(`SELECT COUNT(*) FROM oncall_shifts WHERE schedule_id = ?`, schedID).Scan(&n)
	assert.Equal(t, int64(0), n, "级联删除应清空 shifts")
}

// TestOncallService_DeleteSchedule_事务原子性 (audit-P1 回归)
// 修前 bug: schedule 删了但 shifts 删除失败 → 孤儿数据
// 修后: 包事务, ErrNotFound 不触发级联删, 失败回滚 schedule
func TestOncallService_DeleteSchedule_事务原子性(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	seedShift(t, db, schedID, uuid.New(), "alice", time.Now(), time.Now().Add(time.Hour))

	// 删除 schedule
	err := svc.DeleteSchedule(context.Background(), schedID)
	require.NoError(t, err)

	// 验证 schedule 也删了 (事务提交)
	var schedCount int64
	db.Raw(`SELECT COUNT(*) FROM oncall_schedules WHERE id = ?`, schedID).Scan(&schedCount)
	assert.Equal(t, int64(0), schedCount, "事务提交后 schedule 应被删除")

	// 验证 shifts 也删了 (级联)
	var shiftCount int64
	db.Raw(`SELECT COUNT(*) FROM oncall_shifts WHERE schedule_id = ?`, schedID).Scan(&shiftCount)
	assert.Equal(t, int64(0), shiftCount, "事务提交后 shifts 应被级联删除")
}

// TestOncallService_DeleteSchedule_不存在时事务回滚 (audit-P1 回归)
// schedule 不存在 → ErrNotFound, 不应触发任何 shifts 删除 (空操作)
func TestOncallService_DeleteSchedule_不存在时事务回滚(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	shiftCount := int64(3)
	for i := 0; i < int(shiftCount); i++ {
		seedShift(t, db, schedID, uuid.New(), "alice", time.Now(), time.Now().Add(time.Hour))
	}

	// 不存在的 schedule ID
	err := svc.DeleteSchedule(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrNotFound)

	// 原 schedule 应仍在 (没被误删)
	var n int64
	db.Raw(`SELECT COUNT(*) FROM oncall_schedules WHERE id = ?`, schedID).Scan(&n)
	assert.Equal(t, int64(1), n, "ErrNotFound 时原 schedule 不应被误删")

	// 原 shifts 应仍在 (事务回滚)
	db.Raw(`SELECT COUNT(*) FROM oncall_shifts WHERE schedule_id = ?`, schedID).Scan(&n)
	assert.Equal(t, shiftCount, n, "ErrNotFound 时原 shifts 不应被误删")
}

// ==================== Shift CRUD ====================

func TestOncallService_CreateShift_endsAt早于startsAt报错(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	shift := &models.OncallShift{
		ScheduleID: schedID,
		UserID:     uuid.New(),
		StartsAt:   time.Now().Add(time.Hour),
		EndsAt:     time.Now(),
	}
	err := svc.CreateShift(context.Background(), shift)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ends_at")
}

func TestOncallService_CreateShift_成功(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	shift := &models.OncallShift{
		ScheduleID: schedID,
		UserID:     uuid.New(),
		StartsAt:   time.Now(),
		EndsAt:     time.Now().Add(time.Hour),
	}
	err := svc.CreateShift(context.Background(), shift)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, shift.ID)
}

// ==================== GetCurrentOncall ====================

func TestOncallService_GetCurrentOncall_无班次返空(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	out, err := svc.GetCurrentOncall(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestOncallService_GetCurrentOncall_当前在班返回(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "dev-team", true)
	now := time.Now().UTC()
	seedShift(t, db, schedID, uuid.New(), "alice", now.Add(-time.Hour), now.Add(time.Hour))

	out, err := svc.GetCurrentOncall(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "alice", out[0].UserName)
}

func TestOncallService_GetCurrentOncall_过期班次不返(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "dev-team", true)
	now := time.Now().UTC()
	// 昨天 9-10 点的班次
	seedShift(t, db, schedID, uuid.New(), "alice", now.Add(-24*time.Hour), now.Add(-23*time.Hour))

	out, err := svc.GetCurrentOncall(context.Background(), now)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestOncallService_GetCurrentOncall_disabled_schedule不返(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "dev-team", false) // disabled
	now := time.Now().UTC()
	seedShift(t, db, schedID, uuid.New(), "alice", now.Add(-time.Hour), now.Add(time.Hour))

	out, err := svc.GetCurrentOncall(context.Background(), now)
	require.NoError(t, err)
	assert.Empty(t, out, "disabled schedule 不应被返回")
}

// ==================== Policy ====================

func TestOncallService_CreatePolicy_同步创建levels(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policy := &models.EscalationPolicy{
		Name: "p1",
		Levels: []models.EscalationLevel{
			{Level: 1, TargetType: "user", TargetID: "u1", WaitMinutes: 5, NotifyMethods: "email"},
			{Level: 2, TargetType: "channel", TargetID: "c1", WaitMinutes: 5, NotifyMethods: "sms"},
		},
	}
	err := svc.CreatePolicy(context.Background(), policy)
	require.NoError(t, err)

	// 验证 levels 都创建了
	var n int64
	db.Raw(`SELECT COUNT(*) FROM escalation_levels WHERE policy_id = ?`, policy.ID).Scan(&n)
	assert.Equal(t, int64(2), n)
}

func TestOncallService_ListPolicies_填充levels(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policyID := seedPolicy(t, db, "p1")
	seedLevel(t, db, policyID, 1, "user", "u1", "email", 5)
	seedLevel(t, db, policyID, 2, "user", "u2", "sms", 5)

	out, err := svc.ListPolicies(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "p1", out[0].Name)
	assert.Len(t, out[0].Levels, 2, "应自动 attach levels")
}

func TestOncallService_DeletePolicy_级联删levels(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policyID := seedPolicy(t, db, "p1")
	seedLevel(t, db, policyID, 1, "user", "u1", "email", 5)

	err := svc.DeletePolicy(context.Background(), policyID)
	require.NoError(t, err)
	var n int64
	db.Raw(`SELECT COUNT(*) FROM escalation_levels WHERE policy_id = ?`, policyID).Scan(&n)
	assert.Equal(t, int64(0), n, "级联删除应清空 levels")
}

func TestOncallService_DeletePolicy_不存在返ErrNotFound(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	err := svc.DeletePolicy(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== ProcessEscalation ====================

func TestOncallService_ProcessEscalation_返回所有level(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policyID := seedPolicy(t, db, "p1")
	seedLevel(t, db, policyID, 1, "user", "u1", "email,sms", 5)
	seedLevel(t, db, policyID, 2, "user", "u2", "webhook", 10)
	seedLevel(t, db, policyID, 3, "channel", "c1", "sms", 15)

	steps, err := svc.ProcessEscalation(context.Background(), policyID, time.Now().Add(-time.Hour), time.Now())
	require.NoError(t, err)
	require.Len(t, steps, 3)
	assert.Equal(t, 1, steps[0].Level)
	assert.Equal(t, 2, steps[1].Level)
	assert.Equal(t, []string{"email", "sms"}, steps[0].NotifyMethods)
	assert.Equal(t, 5, steps[0].WaitMinutes)
}

func TestOncallService_ProcessEscalation_策略禁用报错(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policyID := seedPolicy(t, db, "p1")
	require.NoError(t, db.Exec(`UPDATE escalation_policies SET enabled = 0 WHERE id = ?`, policyID).Error)

	_, err := svc.ProcessEscalation(context.Background(), policyID, time.Now(), time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "禁用")
}

func TestOncallService_ProcessEscalation_schedule类型解析name(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "production", true)
	policyID := seedPolicy(t, db, "p1")
	seedLevel(t, db, policyID, 1, "schedule", schedID.String(), "email", 5)

	steps, err := svc.ProcessEscalation(context.Background(), policyID, time.Now(), time.Now())
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "production", steps[0].TargetName, "schedule 类型应解析为 name")
}

// ==================== splitNonEmpty helper ====================

func TestSplitNonEmpty_基本逗号分隔(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, splitNonEmpty("a,b,c", ","))
}

func TestSplitNonEmpty_空字符串(t *testing.T) {
	assert.Equal(t, []string{}, splitNonEmpty("", ","))
}

func TestSplitNonEmpty_连续逗号跳过空(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitNonEmpty("a,,b", ","))
}

// ==================== ListSchedules / GetSchedule / DeleteSchedule 补全 ====================

func TestOncallService_ListSchedules_返回所有(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	seedSchedule(t, db, "team-a", true)
	seedSchedule(t, db, "team-b", false)
	seedSchedule(t, db, "team-c", true)

	out, err := svc.ListSchedules(context.Background())
	require.NoError(t, err)
	assert.Len(t, out, 3)
}

func TestOncallService_ListSchedules_空库返空切片(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)

	out, err := svc.ListSchedules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestOncallService_GetSchedule_存在返回(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)

	got, err := svc.GetSchedule(context.Background(), schedID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "team-a", got.Name)
	assert.True(t, got.Enabled)
}

func TestOncallService_GetSchedule_不存在返ErrNotFound(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	_, err := svc.GetSchedule(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestOncallService_DeleteSchedule_不存在返ErrNotFound(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	err := svc.DeleteSchedule(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== ListShifts / DeleteShift 补全 ====================

func TestOncallService_ListShifts_按startsAt升序(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	now := time.Now().UTC()
	seedShift(t, db, schedID, uuid.New(), "alice", now.Add(2*time.Hour), now.Add(3*time.Hour))
	seedShift(t, db, schedID, uuid.New(), "bob", now, now.Add(time.Hour))
	seedShift(t, db, schedID, uuid.New(), "carol", now.Add(time.Hour), now.Add(2*time.Hour))

	out, err := svc.ListShifts(context.Background(), schedID)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "bob", out[0].UserName, "应按 starts_at 升序")
	assert.Equal(t, "carol", out[1].UserName)
	assert.Equal(t, "alice", out[2].UserName)
}

func TestOncallService_ListShifts_其他schedule不混入(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedA := seedSchedule(t, db, "team-a", true)
	schedB := seedSchedule(t, db, "team-b", true)
	now := time.Now().UTC()
	seedShift(t, db, schedA, uuid.New(), "alice", now, now.Add(time.Hour))
	seedShift(t, db, schedB, uuid.New(), "bob", now, now.Add(time.Hour))

	out, err := svc.ListShifts(context.Background(), schedA)
	require.NoError(t, err)
	assert.Len(t, out, 1)
	assert.Equal(t, "alice", out[0].UserName)
}

func TestOncallService_DeleteShift_成功(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	schedID := seedSchedule(t, db, "team-a", true)
	shiftID := seedShift(t, db, schedID, uuid.New(), "alice", time.Now(), time.Now().Add(time.Hour))

	err := svc.DeleteShift(context.Background(), shiftID)
	require.NoError(t, err)

	var n int64
	db.Raw(`SELECT COUNT(*) FROM oncall_shifts WHERE id = ?`, shiftID).Scan(&n)
	assert.Equal(t, int64(0), n)
}

func TestOncallService_DeleteShift_不存在返ErrNotFound(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	err := svc.DeleteShift(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== GetPolicy 补全 ====================

func TestOncallService_GetPolicy_填充levels(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	policyID := seedPolicy(t, db, "p1")
	seedLevel(t, db, policyID, 1, "user", "u1", "email", 5)
	seedLevel(t, db, policyID, 2, "user", "u2", "sms", 10)

	got, err := svc.GetPolicy(context.Background(), policyID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "p1", got.Name)
	assert.Len(t, got.Levels, 2, "应自动 attach levels")
	assert.Equal(t, 1, got.Levels[0].Level)
}

func TestOncallService_GetPolicy_不存在返ErrNotFound(t *testing.T) {
	db := newOncallTestDB(t)
	svc := NewOncallService(db)
	_, err := svc.GetPolicy(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}
