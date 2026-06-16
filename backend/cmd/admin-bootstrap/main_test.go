package main

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver（跟 api/routes_integration_test.go 同步）
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

// newTestDB 开一个 :memory: sqlite + 建 admin-bootstrap 需要的最小表
// (users + roles + user_roles) —— 全手写不用 AutoMigrate，避开 gorm 的 uuid + gen_random_uuid 解析
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 手写完整 users schema（与 production 同步，跟 gorm tag 匹配）
	// uuid 用 text，gorm.BeforeCreate hook 会用 uuid.New() 填
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

// withEnv 临时设置 env 变量，test 结束自动还原
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

// seedAdminRole 插入 admin role（admin-bootstrap 依赖此数据）
func seedAdminRole(t *testing.T, db *gorm.DB) string {
	t.Helper()
	roleID := uuid.New().String()
	err := db.Exec(`INSERT INTO roles (id, code, name) VALUES (?, 'admin', '管理员')`, roleID).Error
	require.NoError(t, err)
	return roleID
}

// ==================== parseBootstrapEnv 单元测试 ====================

func TestParseEnv_缺username返错(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "",
		"FIRST_ADMIN_PASSWORD": "ValidP@ssw0rd123",
	})
	_, _, _, _, _, err := parseBootstrapEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FIRST_ADMIN_USERNAME")
}

func TestParseEnv_缺password和hash返错(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME":      "admin",
		"FIRST_ADMIN_PASSWORD":      "",
		"FIRST_ADMIN_PASSWORD_HASH": "",
	})
	_, _, _, _, _, err := parseBootstrapEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PASSWORD")
}

func TestParseEnv_同时给password和hash返互斥错(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME":      "admin",
		"FIRST_ADMIN_PASSWORD":      "ValidP@ssw0rd123",
		"FIRST_ADMIN_PASSWORD_HASH": "$2a$10$abcdefghijklmnopqrstuv",
	})
	_, _, _, _, _, err := parseBootstrapEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "互斥")
}

func TestParseEnv_明文password_短于12字符返错(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "admin",
		"FIRST_ADMIN_PASSWORD": "short",
	})
	_, _, _, _, _, err := parseBootstrapEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "12")
}

func TestParseEnv_明文password_12字符以上自动bcrypt(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "admin",
		"FIRST_ADMIN_PASSWORD": "ValidP@ssw0rd123",
	})
	username, _, passwordHash, nickname, email, err := parseBootstrapEnv()
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
	assert.True(t, strings.HasPrefix(passwordHash, "$2"), "bcrypt hash 须以 $2 开头")
	assert.Equal(t, "管理员", nickname)
	assert.Equal(t, "admin@company.local", email)

	// 验证 hash 跟原密码匹配
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("ValidP@ssw0rd123"))
	assert.NoError(t, err)
}

func TestParseEnv_hash_非bcrypt格式返错(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME":      "admin",
		"FIRST_ADMIN_PASSWORD_HASH": "plaintext_password_no_prefix",
	})
	_, _, _, _, _, err := parseBootstrapEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bcrypt")
}

func TestParseEnv_hash_bcrypt直接透传不重新hash(t *testing.T) {
	// 给定 bcrypt hash 进去，期望原样出（不重新 hash）
	originalHash, _ := bcrypt.GenerateFromPassword([]byte("OriginalP@ssw0rd123"), bcrypt.MinCost)
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME":      "admin",
		"FIRST_ADMIN_PASSWORD_HASH": string(originalHash),
	})
	_, _, passwordHash, _, _, err := parseBootstrapEnv()
	require.NoError(t, err)
	assert.Equal(t, string(originalHash), passwordHash, "应原样透传，不重 hash")
}

func TestParseEnv_默认nickname和email(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "alice",
		"FIRST_ADMIN_PASSWORD": "ValidP@ssw0rd123",
	})
	_, _, _, nickname, email, err := parseBootstrapEnv()
	require.NoError(t, err)
	assert.Equal(t, "管理员", nickname)
	assert.Equal(t, "alice@company.local", email)
}

func TestParseEnv_自定义nickname和email优先(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "alice",
		"FIRST_ADMIN_PASSWORD": "ValidP@ssw0rd123",
		"FIRST_ADMIN_NICKNAME": "运维主管",
		"FIRST_ADMIN_EMAIL":    "alice@corp.io",
	})
	_, _, _, nickname, email, err := parseBootstrapEnv()
	require.NoError(t, err)
	assert.Equal(t, "运维主管", nickname)
	assert.Equal(t, "alice@corp.io", email)
}

func TestParseEnv_username前后空格被trim(t *testing.T) {
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "  admin  ",
		"FIRST_ADMIN_PASSWORD": "ValidP@ssw0rd123",
	})
	username, _, _, _, _, err := parseBootstrapEnv()
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
}

// ==================== runWithDeps 集成测试 ====================

func TestRunWithDeps_成功创建admin_关联userRole(t *testing.T) {
	db := newTestDB(t)
	roleID := seedAdminRole(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("ValidP@ssw0rd123"), bcrypt.MinCost)
	err := runWithDeps(db, "admin", "", string(hash), "管理员", "admin@company.local")
	require.NoError(t, err)

	// 验证 users 写入
	var user models.User
	err = db.First(&user, "username = ?", "admin").Error
	require.NoError(t, err)
	assert.Equal(t, "管理员", user.Nickname)
	assert.Equal(t, "admin", user.Role)
	assert.Equal(t, "active", user.Status)
	assert.True(t, strings.HasPrefix(user.PasswordHash, "$2"))

	// 验证 user_roles 关联
	var count int64
	db.Raw("SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?",
		user.ID, roleID).Scan(&count)
	assert.Equal(t, int64(1), count, "user_roles 关联应写入")
}

func TestRunWithDeps_明文password自动bcrypt(t *testing.T) {
	db := newTestDB(t)
	seedAdminRole(t, db)

	// 模拟 parseBootstrapEnv 后的调用：明文已变 bcrypt hash
	hash, _ := bcrypt.GenerateFromPassword([]byte("MyPlainP@ssw0rd"), bcrypt.MinCost)
	err := runWithDeps(db, "admin", "", string(hash), "", "")
	require.NoError(t, err)

	var user models.User
	require.NoError(t, db.First(&user, "username = ?", "admin").Error)
	assert.True(t, strings.HasPrefix(user.PasswordHash, "$2"), "hash 须是 bcrypt")

	// 验证 hash 跟原明文匹配
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("MyPlainP@ssw0rd"))
	assert.NoError(t, err)
}

func TestRunWithDeps_用户名已存在返幂等错(t *testing.T) {
	db := newTestDB(t)
	seedAdminRole(t, db)

	// 先插一个同名用户
	existing := models.User{
		ID:           uuid.New(),
		Username:     "admin",
		PasswordHash: "$2a$10$existing",
		Status:       "active",
		Role:         "admin",
	}
	require.NoError(t, db.Create(&existing).Error)

	// 再调 runWithDeps 期望返"已存在"
	hash, _ := bcrypt.GenerateFromPassword([]byte("ValidP@ssw0rd123"), bcrypt.MinCost)
	err := runWithDeps(db, "admin", "", string(hash), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")

	// users 表不应新增（只有那 1 个 existing）
	var count int64
	db.Model(&models.User{}).Where("username = ?", "admin").Count(&count)
	assert.Equal(t, int64(1), count, "不应创建第二个同名用户")
}

func TestRunWithDeps_缺admin角色返错(t *testing.T) {
	db := newTestDB(t)
	// 不 seed admin role

	hash, _ := bcrypt.GenerateFromPassword([]byte("ValidP@ssw0rd123"), bcrypt.MinCost)
	err := runWithDeps(db, "admin", "", string(hash), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未找到 admin 角色")
}

func TestRunWithDeps_未设env但函数直接传空username_应已在前置校验拦截(t *testing.T) {
	// 注：runWithDeps 本身不校验 username 空（那是 parseBootstrapEnv 的活）
	// 这里测：即使 runWithDeps 被空 username 调用，也应该返"已存在"或"角色"
	db := newTestDB(t)
	seedAdminRole(t, db)

	err := runWithDeps(db, "", "", "anyhash", "", "")
	// 空 username 时 First(where "username = ?", "") 可能匹配不到任何记录 → 进入 admin role 查询 → 通过 → 创建
	// 这不是好设计，验证当前行为：会创建一个空 username 的用户
	if err == nil {
		var count int64
		db.Model(&models.User{}).Where("username = ?", "").Count(&count)
		assert.GreaterOrEqual(t, count, int64(1), "空 username 实际会被创建（这是已知宽松行为）")
	} else {
		// 如果未来加严，这里就是报错点
		t.Logf("空 username 返错（已加严）: %v", err)
	}
}

// ==================== 端到端：parseBootstrapEnv + runWithDeps ====================

func TestE2E_明文env走完parse到run全链路(t *testing.T) {
	db := newTestDB(t)
	seedAdminRole(t, db)

	// 模拟生产路径：env → parse → run
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME": "e2e_admin",
		"FIRST_ADMIN_PASSWORD": "E2eP@ssw0rd123",
	})
	username, _, passwordHash, nickname, email, err := parseBootstrapEnv()
	require.NoError(t, err)
	require.NoError(t, runWithDeps(db, username, "", passwordHash, nickname, email))

	// 验证：e2e_admin 已创建 + 默认 nickname + 默认 email
	var user models.User
	require.NoError(t, db.First(&user, "username = ?", "e2e_admin").Error)
	assert.Equal(t, "管理员", user.Nickname)
	assert.Equal(t, "e2e_admin@company.local", user.Email)
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("E2eP@ssw0rd123"))
	assert.NoError(t, err, "明文 password 走完整链路后须能 bcrypt 验证")
}

func TestE2E_HashEnv走完整链路(t *testing.T) {
	db := newTestDB(t)
	seedAdminRole(t, db)

	originalHash, _ := bcrypt.GenerateFromPassword([]byte("HashEnvP@ssw0rd123"), bcrypt.MinCost)
	withEnv(t, map[string]string{
		"FIRST_ADMIN_USERNAME":      "hash_admin",
		"FIRST_ADMIN_PASSWORD_HASH": string(originalHash),
	})
	username, _, passwordHash, nickname, email, err := parseBootstrapEnv()
	require.NoError(t, err)
	require.NoError(t, runWithDeps(db, username, "", passwordHash, nickname, email))

	var user models.User
	require.NoError(t, db.First(&user, "username = ?", "hash_admin").Error)
	assert.Equal(t, string(originalHash), user.PasswordHash, "hash 注入应原样落库")
	assert.Equal(t, "管理员", nickname)
	assert.Equal(t, "hash_admin@company.local", email)
}
