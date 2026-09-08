package main

import (
	"database/sql"
	"os"
	"testing"

	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
