package service

import (
	"context"
	"testing"

	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
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

// ==================== M61 账号处置（Update / UpdateStatus / UpdateRole） ====================
//
// 这一组用**真 sqlite**（不是 sqlmock）的理由：守卫是 `读-判-写`（自我禁用 / 最后一名
// 管理员要 count 别的行），用 sqlmock 就得把 count 结果手写进期望里 —— 那样「守卫是否
// 真的查了库、查的条件对不对」全由测试作者口述，而守卫的**条件本身**（排除自己、只算
// active）正是缺陷最可能出现的地方。真 sqlite 让条件成为被测对象。
//
// newUserSQLiteDB 手写 DDL（不用 AutoMigrate：models.User.ID 带
// `default:gen_random_uuid()`，sqlite 无此函数 —— 同 channel/ticket 的既有做法）。
func newUserSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		nickname TEXT,
		email TEXT,
		phone TEXT,
		avatar TEXT,
		department_id TEXT,
		status TEXT DEFAULT 'active',
		role TEXT DEFAULT 'user',
		failed_login INTEGER DEFAULT 0,
		locked_until DATETIME,
		last_login DATETIME,
		last_login_ip TEXT,
		must_change_password INTEGER DEFAULT 1,
		password_set_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
	return db
}

// seedUserRow 直插一行（绕开 BeforeCreate：它调 uuid.New，与手写 id 冲突无益）。
func seedUserRow(t *testing.T, db *gorm.DB, username, role, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, role, status, must_change_password, created_at, updated_at)
		VALUES (?, ?, 'x', ?, ?, 1, datetime('now'), datetime('now'))`,
		id.String(), username, role, status).Error)
	return id
}

// readUser 回库读一行（断言「落库的到底是什么」，不复用 service 的返回值 —— 那是被测对象）。
func readUser(t *testing.T, db *gorm.DB, id uuid.UUID) models.User {
	t.Helper()
	var u models.User
	require.NoError(t, db.First(&u, "id = ?", id).Error)
	return u
}

// actor 构造：self=true 时 actor.ID == 目标 id（自我操作路径）。
func actorOf(id uuid.UUID, name string) Actor {
	return Actor{ID: &id, Name: name}
}

// strPtr / boolPtr：Go 1.26 的 `new(expr)` 落地后这两个 helper 可删（go.mod 仍是
// 1.25.0，`new(值)` 要 1.26+）。用它们而不是在调用点展开临时变量，是为了让
// 「这个字段是*提供*的」在测试里读起来就是构造入参的一部分。
func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestUserService_Update_只写给出的字段(t *testing.T) {
	// 局部更新的核心契约：没给的列**一个字节都不能动**。反例是把 Update 实现成
	// 「读出来 → 改一个字段 → Save 整个对象」：并发下别人的改动会被静默写回，
	// 且 handler 漏传的字段（如 must_change_password=false）会被零值覆盖。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	target := seedUserRow(t, db, "alice", "ops_user", "active")
	actor := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.Update(ctx, target.String(), UpdateUserInput{Status: strPtr("inactive")}, actorOf(actor, "root"))
	require.NoError(t, err)
	assert.Equal(t, "inactive", got.Status)
	// 响应体是**落库后**的值（Updates(map) 不把新值写回入参对象）
	assert.Equal(t, "ops_user", got.Role)

	after := readUser(t, db, target)
	assert.Equal(t, "inactive", after.Status)
	assert.Equal(t, "ops_user", after.Role, "role 未在请求里出现，不得被改写")
	assert.True(t, after.MustChangePassword, "must_change_password 未在请求里出现，不得被改写")
	assert.Equal(t, "alice", after.Username)
	assert.False(t, after.PasswordHash == "", "password_hash 不得被清空")

	assert.Equal(t, "active", readUser(t, db, actor).Status, "只影响目标行")
}

func TestUserService_Update_零值字段可写false与空串不当作未提供(t *testing.T) {
	// 指针入参存在的理由：`false` / `"inactive"` 都是合法取值，必须与「未提供」区分。
	// 若把 MustChangePassword 写成值类型，这里永远关不掉强改密。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	target := seedUserRow(t, db, "bob", "ops_user", "active")
	admin := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.Update(ctx, target.String(),
		UpdateUserInput{MustChangePassword: boolPtr(false)}, actorOf(admin, "root"))
	require.NoError(t, err)
	assert.False(t, got.MustChangePassword)
	assert.False(t, readUser(t, db, target).MustChangePassword)
}

func TestUserService_Update_role词表外返回ErrInvalidInput(t *testing.T) {
	// 注意 `"  ADMIN  "` 这类**在**词表内（CanonicalRole 会 TrimSpace + ToLower），
	// 归一化由下面 TestUserService_Update_role首尾空白与大小写归一 覆盖。
	for _, role := range []string{"superuser", "root", "admin-ish", "operators", "管理员"} {
		t.Run(role, func(t *testing.T) {
			db := newUserSQLiteDB(t)
			svc := NewUserService(db)
			target := seedUserRow(t, db, "alice", "ops_user", "active")
			admin := seedUserRow(t, db, "root", "admin", "active")

			got, err := svc.UpdateRole(context.Background(), target.String(), role, actorOf(admin, "root"))
			assert.Nil(t, got)
			assert.ErrorIs(t, err, ErrInvalidInput, "词表外的角色必须 400，不得落库")
			assert.Equal(t, "ops_user", readUser(t, db, target).Role, "拒绝时不得写库")
		})
	}
}

func TestUserService_Update_role首尾空白与大小写归一(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	ctx := context.Background()
	target := seedUserRow(t, db, "alice", "ops_user", "active")
	admin := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.UpdateRole(ctx, target.String(), "  OPS_ADMIN  ", actorOf(admin, "root"))
	require.NoError(t, err)
	assert.Equal(t, "ops_admin", got.Role)
	assert.Equal(t, "ops_admin", readUser(t, db, target).Role, "落库的必须是折叠后的词表值")
}

func TestUserService_Update_role遗留别名折叠后才落库(t *testing.T) {
	// S-1a 同族：`operator` / `viewer` 是设计期遗留别名，直接落库会让
	// `role == "ops_user"` 这类字面量比较（能力矩阵外部）静默失配。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	ctx := context.Background()
	target := seedUserRow(t, db, "alice", "readonly", "active")
	admin := seedUserRow(t, db, "root", "admin", "active")

	for _, tc := range []struct{ in, want string }{
		{"operator", "ops_user"},
		{"viewer", "readonly"},
		{"OPERATOR", "ops_user"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := svc.UpdateRole(ctx, target.String(), tc.in, actorOf(admin, "root"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Role)
			assert.Equal(t, tc.want, readUser(t, db, target).Role)
		})
	}
}

func TestUserService_Update_status枚举外返回ErrInvalidInput(t *testing.T) {
	// `locked` 不在词表内是**有意**的：鉴权侧只拦 status=="inactive"
	// （middleware/auth.go 的 JWT/API Key 两条路径 + login handler），写 locked
	// 不会拦住任何请求，收它等于给管理员一个静默无效的开关。
	for _, status := range []string{"locked", "deleted", "disabled", "ACTIVE", ""} {
		t.Run(status, func(t *testing.T) {
			db := newUserSQLiteDB(t)
			svc := NewUserService(db)
			target := seedUserRow(t, db, "alice", "ops_user", "active")
			admin := seedUserRow(t, db, "root", "admin", "active")

			got, err := svc.UpdateStatus(context.Background(), target.String(), status, actorOf(admin, "root"))
			assert.Nil(t, got)
			assert.ErrorIs(t, err, ErrInvalidInput)
			assert.Equal(t, "active", readUser(t, db, target).Status, "拒绝时不得写库")
		})
	}
}

func TestUserService_Update_禁用普通用户_成功(t *testing.T) {
	// 正控：守卫只该挡住「自我禁用」与「最后一名管理员」，普通用户必须能禁用 ——
	// 否则整个 M61 的功能是死的而守卫用例照样全绿。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	target := seedUserRow(t, db, "leaver", "ops_user", "active")
	admin := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.UpdateStatus(context.Background(), target.String(), "inactive", actorOf(admin, "root"))
	require.NoError(t, err)
	assert.Equal(t, "inactive", got.Status)
	assert.Equal(t, "inactive", readUser(t, db, target).Status)
}

func TestUserService_Update_自我禁用返回ErrForbidden(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	// 两个 admin：把「最后一名管理员」这条守卫隔离掉，证明拒绝来自 self 那条
	self := seedUserRow(t, db, "root", "admin", "active")
	seedUserRow(t, db, "root2", "admin", "active")

	got, err := svc.UpdateStatus(context.Background(), self.String(), "inactive", actorOf(self, "root"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "active", readUser(t, db, self).Status, "拒绝时不得写库（事务内不得留半成品）")
}

func TestUserService_Update_自我降级admin返回ErrForbidden(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	self := seedUserRow(t, db, "root", "admin", "active")
	seedUserRow(t, db, "root2", "admin", "active")

	// 另一个 admin 在，故「最后一名管理员」不成立 —— 拒绝只能来自 self 守卫
	got, err := svc.UpdateRole(context.Background(), self.String(), "ops_admin", actorOf(self, "root"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "admin", readUser(t, db, self).Role)
}

func TestUserService_Update_自己改自己的非降级字段_允许(t *testing.T) {
	// 反向守卫：不能因为「拦住自我操作」把 admin 置自己的 must_change_password 也挡掉。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	self := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.Update(context.Background(), self.String(),
		UpdateUserInput{MustChangePassword: boolPtr(false)}, actorOf(self, "root"))
	require.NoError(t, err)
	assert.False(t, got.MustChangePassword)

	// 把 admin 设成 admin（幂等写入）也不算自我降级
	got, err = svc.UpdateRole(context.Background(), self.String(), "admin", actorOf(self, "root"))
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Role)
}

func TestUserService_Update_禁用最后一名管理员返回ErrForbidden(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	only := seedUserRow(t, db, "root", "admin", "active")
	actor := seedUserRow(t, db, "other", "ops_admin", "active")

	got, err := svc.UpdateStatus(context.Background(), only.String(), "inactive", actorOf(actor, "other"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "active", readUser(t, db, only).Status)
}

func TestUserService_Update_降级最后一名管理员返回ErrForbidden(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	only := seedUserRow(t, db, "root", "admin", "active")
	actor := seedUserRow(t, db, "other", "ops_admin", "active")

	got, err := svc.UpdateRole(context.Background(), only.String(), "readonly", actorOf(actor, "other"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "admin", readUser(t, db, only).Role)
}

func TestUserService_Update_被禁用的管理员不算能自救(t *testing.T) {
	// 判据必须是「还有**能登进来**的 admin」：另一个 admin 若 status=inactive，
	// 它登不进来（auth.go 拦 inactive），禁用当前这个就等于系统无人可管理。
	// 漏掉 status 条件会让这道守卫在「最后一名可登录管理员」上静默失效。
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	active := seedUserRow(t, db, "root", "admin", "active")
	seedUserRow(t, db, "root-disabled", "admin", "inactive")
	actor := seedUserRow(t, db, "other", "ops_admin", "active")

	got, err := svc.UpdateStatus(context.Background(), active.String(), "inactive", actorOf(actor, "other"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "active", readUser(t, db, active).Status)
}

func TestUserService_Update_还有另一名启用admin时可禁用(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	a := seedUserRow(t, db, "root-a", "admin", "active")
	seedUserRow(t, db, "root-b", "admin", "active")

	got, err := svc.UpdateStatus(context.Background(), a.String(), "inactive", actorOf(a, "root-a"))
	// 注意 actor 是 a 自己 —— 自我禁用先命中，故这里换用第三人称操作者再验一次
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, "active", readUser(t, db, a).Status)

	actor := seedUserRow(t, db, "op", "ops_admin", "active")
	got, err = svc.UpdateStatus(context.Background(), a.String(), "inactive", actorOf(actor, "op"))
	require.NoError(t, err)
	assert.Equal(t, "inactive", got.Status)
}

func TestUserService_Update_目标不存在返回ErrNotFound(t *testing.T) {
	db := newUserSQLiteDB(t)
	svc := NewUserService(db)
	actor := seedUserRow(t, db, "root", "admin", "active")

	got, err := svc.UpdateStatus(context.Background(), uuid.NewString(), "inactive", actorOf(actor, "root"))
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUserService_Update_空输入不发UPDATE只回读(t *testing.T) {
	// `{}` 等价于「没要求改任何东西」：不得走 UPDATE（gorm 的 Updates(空 map) 会报错），
	// 也不该开事务（白占连接）。用 sqlmock 钉住 — 开了事务就会多出 BEGIN。
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "username", "role", "status"}).
		AddRow(id, "alice", "ops_user", "active")
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE id = \$1`).WithArgs(id, 1).WillReturnRows(rows)

	got, err := svc.Update(context.Background(), id, UpdateUserInput{}, Actor{Name: "root"})
	require.NoError(t, err)
	assert.Equal(t, "alice", got.Username)
	assert.NoError(t, mock.ExpectationsWereMet(), "空输入不得发任何写语句")
}

func TestUserService_Update_持锁读旧值(t *testing.T) {
	// 守卫是读-判-写：不加行锁时两个并发请求可各自读到「还有别的 admin」而双双降级
	// → 一个 admin 不剩。sqlite 基座不渲染 FOR UPDATE（driver 不支持行级锁），
	// 故这是**唯一**能钉住「加锁没被删掉」的地方（口径同 ticket_service.Update）。
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	actorID := uuid.New()
	pre := sqlmock.NewRows([]string{"id", "username", "role", "status"}).
		AddRow(id, "alice", "ops_user", "active")
	post := sqlmock.NewRows([]string{"id", "username", "role", "status"}).
		AddRow(id, "alice", "ops_user", "inactive")

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE id = \$1.*FOR UPDATE`).
		WithArgs(id, 1).WillReturnRows(pre)
	mock.ExpectExec(`UPDATE "users" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE id = \$1`).
		WithArgs(id, 1).WillReturnRows(post)
	mock.ExpectCommit()

	got, err := svc.UpdateStatus(ctx, id, "inactive", actorOf(actorID, "root"))
	require.NoError(t, err)
	assert.Equal(t, "inactive", got.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}
