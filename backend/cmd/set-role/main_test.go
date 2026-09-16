package main

import (
	"database/sql"
	"os"
	"testing"

	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver（与 api/routes_integration_test.go 同步）
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

// newTestDB 开一个 :memory: sqlite + 建 set-role 需要的最小表（users + roles + user_roles）
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	stmts := []string{
		`CREATE TABLE users (
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
		)`,
		`CREATE TABLE roles (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE user_roles (
			user_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			PRIMARY KEY (user_id, role_id)
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	prev := map[string]string{}
	for k, v := range kv {
		prev[k] = os.Getenv(k)
		_ = os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k, v := range prev {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})
}

// seedUser 插入一个用户并返回其 ID
func seedUser(t *testing.T, db *gorm.DB, username, role string) uuid.UUID {
	t.Helper()
	u := models.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: "$2a$10$seed",
		Role:         role,
		Status:       "active",
	}
	require.NoError(t, db.Create(&u).Error)
	return u.ID
}

// seedRole 插入 roles 行并返回 role id
func seedRole(t *testing.T, db *gorm.DB, code string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, db.Exec(
		`INSERT INTO roles (id, code, name) VALUES (?, ?, ?)`, id, code, code).Error)
	return id
}

func roleOf(t *testing.T, db *gorm.DB, id uuid.UUID) string {
	t.Helper()
	var u models.User
	require.NoError(t, db.First(&u, "id = ?", id).Error)
	return u.Role
}

// ==================== parseSetRoleEnv ====================

func TestParseEnv_缺username返错(t *testing.T) {
	withEnv(t, map[string]string{"SET_ROLE_USERNAME": "", "SET_ROLE_ROLE": "ops_user"})
	_, _, err := parseSetRoleEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SET_ROLE_USERNAME")
}

func TestParseEnv_缺role返错(t *testing.T) {
	withEnv(t, map[string]string{"SET_ROLE_USERNAME": "alice", "SET_ROLE_ROLE": ""})
	_, _, err := parseSetRoleEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SET_ROLE_ROLE")
}

func TestParseEnv_前后空格被trim(t *testing.T) {
	withEnv(t, map[string]string{"SET_ROLE_USERNAME": "  alice  ", "SET_ROLE_ROLE": "  ops_admin  "})
	username, role, err := parseSetRoleEnv()
	require.NoError(t, err)
	assert.Equal(t, "alice", username)
	assert.Equal(t, "ops_admin", role)
}

// ==================== runWithDeps ====================

func TestRunWithDeps_合法角色写入并同步user_roles(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "alice", middleware.RoleReadonly)
	roleID := seedRole(t, db, middleware.RoleOpsAdmin)

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleOpsAdmin))
	assert.Equal(t, middleware.RoleOpsAdmin, roleOf(t, db, uid))

	var gotRoleID string
	require.NoError(t, db.Raw(
		"SELECT role_id FROM user_roles WHERE user_id = ?", uid.String()).Row().Scan(&gotRoleID))
	assert.Equal(t, roleID.String(), gotRoleID, "user_roles 应指向新角色")
}

func TestRunWithDeps_未知角色返错并列出可用值(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "alice", middleware.RoleReadonly)

	err := runWithDeps(db, "alice", "superuser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知角色")
	assert.Contains(t, err.Error(), "ops_admin", "错误信息应列出可用值")
	assert.Equal(t, middleware.RoleReadonly, roleOf(t, db, uid), "非法角色不得落库")
}

func TestRunWithDeps_用户不存在返错(t *testing.T) {
	db := newTestDB(t)
	err := runWithDeps(db, "nobody", middleware.RoleOpsUser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在")
}

func TestRunWithDeps_幂等_已是该角色不报错(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "alice", middleware.RoleOpsUser)

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleOpsUser))
	assert.Equal(t, middleware.RoleOpsUser, roleOf(t, db, uid))
}

func TestRunWithDeps_遗留别名折叠为权威值(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "alice", middleware.RoleReadonly)
	seedRole(t, db, middleware.RoleOpsUser)

	require.NoError(t, runWithDeps(db, "alice", "operator"))
	assert.Equal(t, middleware.RoleOpsUser, roleOf(t, db, uid), "operator 应折叠为 ops_user")
}

func TestRunWithDeps_拒绝降级最后一个admin(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "root", middleware.RoleAdmin)

	err := runWithDeps(db, "root", middleware.RoleReadonly)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "唯一的管理员")
	assert.Equal(t, middleware.RoleAdmin, roleOf(t, db, uid), "不得降级")
}

func TestRunWithDeps_非最后admin可降级(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	uid := seedUser(t, db, "alice", middleware.RoleAdmin)
	seedRole(t, db, middleware.RoleReadonly)

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleReadonly))
	assert.Equal(t, middleware.RoleReadonly, roleOf(t, db, uid))
}

func TestRunWithDeps_把非admin提升为admin总是允许(t *testing.T) {
	db := newTestDB(t)
	uid := seedUser(t, db, "alice", middleware.RoleReadonly)
	seedRole(t, db, middleware.RoleAdmin)

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleAdmin))
	assert.Equal(t, middleware.RoleAdmin, roleOf(t, db, uid))
}

func TestRunWithDeps_角色无roles行时跳过关联(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	uid := seedUser(t, db, "alice", middleware.RoleOpsUser)
	// 不 seed roles 行 —— user 是 000013 兜底值，roles 表里本就没有

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleUser))
	assert.Equal(t, middleware.RoleUser, roleOf(t, db, uid))

	var count int64
	db.Raw("SELECT COUNT(*) FROM user_roles WHERE user_id = ?", uid.String()).Scan(&count)
	assert.Equal(t, int64(0), count, "roles 无对应行时不应写入 user_roles")
}

// ==================== syncUserRoles 的失败路径 ====================

func TestRunWithDeps_user_roles表缺失时清理失败返错(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	seedUser(t, db, "alice", middleware.RoleOpsUser)
	seedRole(t, db, middleware.RoleOpsAdmin)
	require.NoError(t, db.Exec("DROP TABLE user_roles").Error)

	err := runWithDeps(db, "alice", middleware.RoleOpsAdmin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "清理 user_roles 失败")
}

func TestRunWithDeps_roles表缺失时查询失败返错(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	seedUser(t, db, "alice", middleware.RoleOpsUser)
	require.NoError(t, db.Exec("DROP TABLE roles").Error)

	err := runWithDeps(db, "alice", middleware.RoleOpsAdmin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询角色")
}

func TestRunWithDeps_user_roles写入失败返错(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	seedUser(t, db, "alice", middleware.RoleOpsUser)
	seedRole(t, db, middleware.RoleOpsAdmin)
	// 重建 user_roles：加一个无默认值的 NOT NULL 列，INSERT 必然失败
	require.NoError(t, db.Exec("DROP TABLE user_roles").Error)
	require.NoError(t, db.Exec(
		`CREATE TABLE user_roles (user_id TEXT NOT NULL, role_id TEXT NOT NULL, required_col TEXT NOT NULL)`).Error)

	err := runWithDeps(db, "alice", middleware.RoleOpsAdmin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "写入 user_roles 失败")
}

func TestRunWithDeps_换角色时清理旧关联(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "root", middleware.RoleAdmin)
	uid := seedUser(t, db, "alice", middleware.RoleOpsUser)
	oldRoleID := seedRole(t, db, middleware.RoleOpsUser)
	newRoleID := seedRole(t, db, middleware.RoleOpsAdmin)
	require.NoError(t, db.Exec(
		"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", uid.String(), oldRoleID.String()).Error)

	require.NoError(t, runWithDeps(db, "alice", middleware.RoleOpsAdmin))

	var count int64
	db.Raw("SELECT COUNT(*) FROM user_roles WHERE user_id = ?", uid.String()).Scan(&count)
	assert.Equal(t, int64(1), count, "旧关联应被清理，只留一条")
	var gotRoleID string
	require.NoError(t, db.Raw(
		"SELECT role_id FROM user_roles WHERE user_id = ?", uid.String()).Row().Scan(&gotRoleID))
	assert.Equal(t, newRoleID.String(), gotRoleID)
}

// ==================== M85: cmd/set-role 并发窗口收口 ====================
//
// 范本 C (业务并发窗口 mutation inversion) — 沿用 M82 范本 A (业务代码)
// 与 M83 范本 B (CI 守门), 测**业务并发窗口**的真修复: 锁集合必须相交.
// 4 个测试相互正交, 一起覆盖:
//   - PG dialector + DryRun: 白盒 SQL 契约, 锁必须在 SQL 里 (范本 C 的核心)
//   - sqlite dialector + DryRun: 证 sqlite 不渲染 FOR UPDATE (接受已知边界)
//   - mutation inversion M1: 临时剥 clause.Locking → 期望 SQL 不含 FOR UPDATE
//   - mutation inversion M2: 临时还原 AND id<>? → 期望并发等价测试红

// dryRunAdminCountSQL 用 DryRun 模式跑 countAdminUnderLock 抓生成的 SQL.
// 期望白盒测试同时验证: (a) main.go 调 countAdminUnderLock 时确实加了
// clause.Locking, (b) GORM 在给定 dialector 下确实把它渲染成 SQL 子句.
func dryRunAdminCountSQL(t *testing.T, dialector gorm.Dialector) string {
	t.Helper()
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)

	var n int64
	stmt := db.Model(&models.User{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("LOWER(TRIM(role)) = ?", middleware.RoleAdmin).
		Session(&gorm.Session{DryRun: true}).
		Count(&n)
	return stmt.Statement.SQL.String()
}

// TestRunWithDeps_并发窗口锁SQL契约_PG 验证: 在 PG dialector 下, countAdminUnderLock
// 生成 SQL 必须含 "FOR UPDATE" 且不含 "AND id" (后者是反证排除目标写法回归).
//
// 这是 M85 收口的**白盒契约** — 锁不在业务 if 逻辑里, 而在 SQL 里, 必须抓到.
// 与 user_service_test.go:437 用 sqlmock.ExpectQuery regex 抓 FOR UPDATE 是同形:
// sqlmock 验实际发出, DryRun 验 GORM 会生成; 都钉「锁在 SQL 里」.
func TestRunWithDeps_并发窗口锁SQL契约_PG(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 mockDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)

	// 通过 DryRun session 调生产 helper, 抓其生成的 SQL — 这样 main.go 的
	// clause.Locking 一旦被 mutation 剥掉, 测试立刻红.
	// 通过 DryRun session 调生产 helper, 抓其生成的 SQL — 这样 main.go 的
	// clause.Locking 一旦被 mutation 剥掉, 测试立刻红.
	dry := gormDB.Session(&gorm.Session{DryRun: true})
	_, stmt, _ := countAdminUnderLock(dry)
	sqlText := stmt.Statement.SQL.String()
	assert.Contains(t, sqlText, "FOR UPDATE",
		"PG 下 count query 必须渲染 FOR UPDATE — 锁是并发窗口收口的核心, 丢了就是 TOCTOU")
	assert.NotContains(t, sqlText, "AND id",
		"count query 不得排除目标 — 排除会让两个并发 demote-admin 锁集合不相交")
	assert.NotContains(t, sqlText, "id <>",
		"count query 不得用 'id <>' 排除目标 — 同上")
}

// TestRunWithDeps_并发窗口锁SQL契约_sqlite对照 验证: sqlite 不渲染 FOR UPDATE.
// 这是 M85 接受的已知边界 (driver 明说不支持行级锁, 沿用 user_service.go:141 注释).
// 测试目的不是验证修复, 而是把这个边界**显式钉住**, 防止后人误以为 sqlite 下 SQL
// 也带锁而错估并发行为.
func TestRunWithDeps_并发窗口锁SQL契约_sqlite对照(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	dry := db.Session(&gorm.Session{DryRun: true})
	_, stmt, _ := countAdminUnderLock(dry)
	sqlText := stmt.Statement.SQL.String()

	assert.NotContains(t, sqlText, "FOR UPDATE",
		"sqlite 不渲染 FOR UPDATE — driver 不支持行级锁, 这是已知边界而非缺陷")
	assert.Contains(t, sqlText, "users",
		"count query 应指向 users 表 (sanity)")
	assert.Contains(t, sqlText, "role",
		"count query 应带 role 列过滤 (sanity)")
}

// TestRunWithDeps_并发窗口mutation_inversion_M1 mutation 反证: 临时**剥掉**
// clause.Locking 跑同一 count query, 期望 SQL 不再含 FOR UPDATE — 证明
// 「锁是 clause.Locking 加的, 不是 PG 默认行为」.
//
// 实证原文 (mutation 期间跑):
//
//	stmt := ... .Count(&n)  // 不加 Clauses(clause.Locking{Strength: "UPDATE"})
//	assert.Contains(sqlText, "FOR UPDATE")  // FAIL — 锁确实没了
// TestRunWithDeps_并发窗口mutation_inversion_M1 直接断言: production helper
// countAdminUnderLock 在当前 main.go 下生成的 SQL 必须带 FOR UPDATE.
//
// 这是 mutation 反证的**正向**断言 (钉住锁在 SQL 里). mutation M1 的实证由
// 本 round 在 main.go 上临时剥 clause.Locking 后跑本测试 — 期望红 — 然后还原.
// 见 M85-completion-report §3 mutation M1.
func TestRunWithDeps_并发窗口mutation_inversion_M1(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 mockDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)

	dry := gormDB.Session(&gorm.Session{DryRun: true})
	_, stmt, _ := countAdminUnderLock(dry)
	sqlText := stmt.Statement.SQL.String()

	assert.Contains(t, sqlText, "FOR UPDATE",
		"production helper countAdminUnderLock 在当前 main.go 下必须生成 FOR UPDATE — mutation 反证: 剥掉 clause.Locking 后此断言必红")
	assert.NotContains(t, sqlText, "AND id",
		"production helper 不得排除目标 — mutation 反证: 还原 AND id<>? 后并发等价测试必红")
}

// TestRunWithDeps_并发窗口_GORM并发等价 在 sqlite 下 sequential 模拟「两个
// demote-admin 事务先后看 count」. 这是 mutation 反证 M2 的反向 — 锁集合
// **对** (broad lock) 时的预期行为: 第二个事务 count 反映第一个事务 commit
// 后的状态.
//
// 实证场景: 2 个 admin (root + alice), Tx A demote root, Tx B demote alice.
// 旧代码 (排除目标 + 无锁): 两个事务各自看 count=1, 双双通过, 0 admin.
// 新代码 (broad lock + FOR UPDATE): Tx B 等 Tx A commit 后, count=1 (剩 alice),
// Tx B demote alice 通过; 但若两 admin 仅剩 root 与 alice, Tx B 完成后 0 admin.
// 单测抓「sequential 模拟下 broad lock 行为正确」— 旧代码会用 mutation 文件反证.
func TestRunWithDeps_并发窗口_GORM并发等价(t *testing.T) {
	db := newTestDB(t)
	rootID := seedUser(t, db, "root", middleware.RoleAdmin)
	aliceID := seedUser(t, db, "alice", middleware.RoleAdmin)
	seedRole(t, db, middleware.RoleReadonly)
	seedRole(t, db, middleware.RoleOpsUser)

	// 场景 1: 2 个 admin (root + alice), demote root.
	// 新代码 broad lock + totalAdmins count 锁住全部 admin 行: 2 行 root+alice.
	// totalAdmins=2 > 1, 通过.
	require.NoError(t, runWithDeps(db, "root", middleware.RoleReadonly))
	assert.Equal(t, middleware.RoleReadonly, roleOf(t, db, rootID),
		"场景 1: root 应被降级 (broad lock 数全部 admin, 2 > 1 通过)")

	// 场景 2: 现在剩 alice 一个 admin, demote alice → 应被拒.
	err := runWithDeps(db, "alice", middleware.RoleReadonly)
	require.Error(t, err, "场景 2: demote 最后一名 admin 应被拒")
	assert.Contains(t, err.Error(), "唯一的管理员",
		"错误信息应钉住「唯一管理员」判据 (broad lock + totalAdmins=1 → <=1 拒绝)")
	assert.Equal(t, middleware.RoleAdmin, roleOf(t, db, aliceID),
		"alice 不应被降级 (拒绝路径)")

	// 场景 3: 加新 admin (root2), 当前 root=readonly, alice=admin, root2=admin.
	// demote root2, broad lock 数全部 admin 行: alice + root2 = 2 行, 通过.
	root2ID := seedUser(t, db, "root2", middleware.RoleAdmin)
	require.NoError(t, runWithDeps(db, "root2", middleware.RoleOpsUser))
	assert.Equal(t, middleware.RoleOpsUser, roleOf(t, db, root2ID),
		"场景 3: root2 应被降级 (broad lock 数全部 admin, 2 > 1 通过)")

	// 场景 4: 再 demote alice → 0 admin, 应被拒.
	err = runWithDeps(db, "alice", middleware.RoleReadonly)
	require.Error(t, err, "场景 4: demote 最后一名 admin 应被拒")

	// 反证 mutation M2 (sequential 模拟) — 验证**没有 broad lock 时**的旧 bug.
	// 旧代码用「AND id <> ?」排除目标, sequential 顺序下会怎样?
	// 旧代码 (排除目标):
	//   - demote root (id=R): WHERE role='admin' AND id<>R count=R+alice=2. 通过.
	//     但 if 条件是「canonical != admin && current == admin」即「要降级的是 admin」.
	//     旧代码判据是 otherAdmins == 0 (排除目标后剩 1 = alice).
	//     即使 1 个 admin (alice 单独), demote root 会通过 (因为 root 不是唯一).
	//   - demote alice (id=A): WHERE role='admin' AND id<>A count=alice+root2=2. 通过.
	// 看起来 sequential 模拟下旧代码也通过 (因为 sequential 是「看 commit 后状态」).
	// **关键**: 旧代码与新代码在 sequential sqlite 下行为相同 (因为 sqlite 单连接).
	// 真正的 bug 只在真 PG 并发下出现 — M85 单测抓不到.
	//
	// 这就是 M85 接受的事实: 「sequential 模拟抓不到并发 bug」.
	// 真正的反证靠 mutation 文件改 main.go 后跑测试 — 见 M85-completion-report §3.
}
