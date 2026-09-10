package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/DATA-DOG/go-sqlmock"
)

// ==================== M19：告警状态迁移收口 ====================
//
// 合法迁移表来自前端 getAlertActions（problem 才有「确认」、problem/acknowledged 才有
// 「解决」）。这组用例把服务端钉成同一张表的权威。
//
// 断言里"没有 UPDATE"不是靠数调用次数，而是**不设 UPDATE 期望**：sqlmock 对未期望的
// 调用会返回错误，若实现偷偷写了库，Updates 的 res.Error 非 nil，下面的 ErrorIs /
// NoError 立刻红。

const alertFirstSQL = `SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`

// alertRow 造一条 Get() 能解出的最小 alerts 行。
func alertRow(id uuid.UUID, status string, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "status", "ack_time", "ack_user", "updated_at"}).
		AddRow(id, status, nil, "", now)
}

// expectAlertGet 期望一次 s.Get 并返回指定状态的行。
func expectAlertGet(mock sqlmock.Sqlmock, id uuid.UUID, status string, now time.Time) {
	mock.ExpectQuery(alertFirstSQL).WithArgs(id, 1).WillReturnRows(alertRow(id, status, now))
}

func TestAlertService_Acknowledge_已解决被拒(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	expectAlertGet(mock, id, "resolved", time.Now())

	// 关键：不设 UPDATE 期望 —— 拒绝必须是「没写库」，不是「写了再报错」
	err := svc.Acknowledge(context.Background(), id.String(), "u1")

	require.ErrorIs(t, err, ErrInvalidState)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Acknowledge_已确认是幂等成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	expectAlertGet(mock, id, "acknowledged", time.Now())

	// 幂等：不重写 ack_time/ack_user（重写会把认领人改成后点的人，并把 MTTD 拉长），
	// 也不重发通知 —— 所以同样不设 UPDATE 期望
	require.NoError(t, svc.Acknowledge(context.Background(), id.String(), "u2"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Resolve_已解决是幂等成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	expectAlertGet(mock, id, "resolved", time.Now())

	// 重复解决若不挡，resolve_time 会被推到 now → dashboard 的 MTTR（AVG(resolve_time -
	// problem_start)）随之虚高。幂等 = 不写库。
	require.NoError(t, svc.Resolve(context.Background(), id.String(), "u2"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Resolve_已确认可解决(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	expectAlertGet(mock, id, "acknowledged", time.Now())

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.Resolve(context.Background(), id.String(), "u1"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 并发：读到 problem（守卫放行）与写之间被别人改成了 resolved。
// 条件 UPDATE 的 WHERE 命中 0 行 —— 这正是「读-判-写」挡不住、必须靠 WHERE 的场景。
func TestAlertService_Acknowledge_并发下0行_复读已解决则拒(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	now := time.Now()
	expectAlertGet(mock, id, "problem", now)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	// classifyAlertNoRows 的复读
	mock.ExpectQuery(`SELECT "status" FROM "alerts"`).WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("resolved"))

	require.ErrorIs(t, svc.Acknowledge(context.Background(), id.String(), "u1"), ErrInvalidState)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Resolve_并发下0行_复读已解决仍幂等(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	now := time.Now()
	expectAlertGet(mock, id, "problem", now)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT "status" FROM "alerts"`).WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("resolved"))

	// 别人抢先解决 = 目标状态已达成 → 幂等成功，而不是报错
	require.NoError(t, svc.Resolve(context.Background(), id.String(), "u1"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Acknowledge_0行且记录没了_返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	expectAlertGet(mock, id, "problem", time.Now())

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	// 复读时记录已被删除 —— 不能用 ErrInvalidState 冒充（调用方会去刷新一个不存在的对象）
	mock.ExpectQuery(`SELECT "status" FROM "alerts"`).WithArgs(id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	require.ErrorIs(t, svc.Acknowledge(context.Background(), id.String(), "u1"), ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 批量路径的守卫只在 SQL 里可见，故直接钉 WHERE 子句。
// 语义（终态行被跳过、affected 说真话）由 tests/db_smoke_test.go 的真 PG 用例证明 ——
// 那里才是 WHERE 真正生效的地方。
func TestAlertService_Bulk_两条路都带源状态守卫(t *testing.T) {
	t.Run("BulkAcknowledge 只动 problem", func(t *testing.T) {
		gormDB, mock := newMockDB(t)
		svc := NewAlertService(gormDB)
		ids := []string{uuid.New().String(), uuid.New().String()}

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "alerts" SET .*WHERE id IN \([^)]*\) AND status IN \([^)]*\)`).
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectCommit()

		n, err := svc.BulkAcknowledge(context.Background(), ids, "u1")
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("BulkResolve 只动 problem/acknowledged", func(t *testing.T) {
		gormDB, mock := newMockDB(t)
		svc := NewAlertService(gormDB)
		ids := []string{uuid.New().String()}

		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id IN \([^)]*\) AND status IN \([^)]*\)`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectExec(`UPDATE "alerts" SET .*WHERE id IN \([^)]*\) AND status IN \([^)]*\)`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		n, err := svc.BulkResolve(context.Background(), ids, "u1")
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
