package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ==================== 测试环境 ====================

// authUserTestDB 单例：auth handler 调 database.DB
var authUserTestDBOnce sync.Once
var authUserTestDB *gorm.DB

const authUserTestSchema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    nickname TEXT,
    email TEXT,
    phone TEXT,
    avatar TEXT,
    department_id TEXT,
    role TEXT DEFAULT 'user',
    status TEXT DEFAULT 'active',
    failed_login INTEGER DEFAULT 0,
    locked_until DATETIME,
    last_login DATETIME,
    last_login_ip TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME
);
`

func setupAuthTestDB(t *testing.T) *gorm.DB {
	authUserTestDBOnce.Do(func() {
		db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(authUserTestSchema).Error; err != nil {
			t.Fatal(err)
		}
		oldDB := database.DB
		database.SetDBForTest(db)
		_ = oldDB
		authUserTestDB = db
		// 注入 config（避免 handler 调 config.Get() 时 panic）
		cfg := &config.Config{
			Server: config.ServerConfig{Mode: "debug"},
			Auth: config.AuthConfig{
				JWT:          config.JWTConfig{Secret: "test-jwt-secret-32-bytes-valid", Expire: 3600},
				APIKeyPepper: "test-pepper-32-bytes-for-api-key-handler-test",
			},
		}
		config.SetForTest(cfg)
	})
	// 清表
	require.NoError(t, authUserTestDB.Exec("DELETE FROM users").Error)
	return authUserTestDB
}

// seedActiveUser 插入一个 active 状态的有效用户
func seedActiveUser(t *testing.T, db *gorm.DB, username, password string) uuid.UUID {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	uid := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, status, failed_login, created_at, updated_at)
		VALUES (?, ?, ?, 'active', 0, datetime('now'), datetime('now'))`,
		uid.String(), username, string(hash)).Error)
	return uid
}

func newAuthTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/auth")
	g.POST("/login", handlers.Login)
	g.POST("/logout", handlers.Logout)
	g.GET("/me", func(c *gin.Context) {
		// mock middleware: 解析 user_id header（测试用）
		uid := c.GetHeader("X-User-Id")
		if uid == "" {
			c.Set("user_id", "")
			handlers.GetCurrentUser(c)
			return
		}
		c.Set("user_id", uid)
		handlers.GetCurrentUser(c)
	})
	g.POST("/change-password", func(c *gin.Context) {
		uid := c.GetHeader("X-User-Id")
		c.Set("user_id", uid)
		handlers.ChangePassword(c)
	})
	return r
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var br *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		br = bytes.NewReader(b)
	} else {
		br = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, br)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ==================== Login ====================

func TestLogin_HappyPath_返Token和User(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "admin", "secret123")

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "admin",
		"password": "secret123",
	})
	assert.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	body := w.Body.String()
	// 关键：返 token + user
	assert.Contains(t, body, `"token"`)
	assert.Contains(t, body, `"user"`)
	assert.Contains(t, body, `"username":"admin"`)
	// user.id 应等于 seed 的 uid
	assert.Contains(t, body, uid.String())
	// C-F5: 应设 auth_token cookie
	cookies := w.Result().Cookies()
	var authCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "auth_token" {
			authCookie = c
		}
	}
	require.NotNil(t, authCookie, "必须设置 auth_token cookie (C-F5)")
	assert.True(t, authCookie.HttpOnly, "cookie 必须 HttpOnly 防 XSS")
	assert.Equal(t, "/", authCookie.Path)
}

func TestLogin_缺字段_返400(t *testing.T) {
	setupAuthTestDB(t)
	r := newAuthTestRouter()

	cases := []map[string]any{
		{},                                     // 全空
		{"username": "admin"},                  // 缺 password
		{"password": "secret"},                 // 缺 username
		{"username": "", "password": "secret"}, // username 空
	}
	for _, body := range cases {
		w := doJSON(t, r, "POST", "/auth/login", body)
		assert.Equal(t, http.StatusBadRequest, w.Code, "body=%v", body)
		assert.Contains(t, w.Body.String(), "请输入用户名和密码")
	}
}

func TestLogin_用户不存在_返401通用消息防枚举(t *testing.T) {
	setupAuthTestDB(t)
	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "nonexistent",
		"password": "any",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	// 关键：消息与密码错误**完全一致**（防 enumeration）
	assert.Contains(t, w.Body.String(), "用户名或密码错误")
}

func TestLogin_密码错误_返401通用消息(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	seedActiveUser(t, db, "admin", "correct-password")

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "admin",
		"password": "wrong-password",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "用户名或密码错误")
	// 与"用户不存在"消息完全相同（防 enumeration）
	// 这两个 test 都断言同一消息文本
}

func TestLogin_账号inactive_返403(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := uuid.New()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, status, failed_login, created_at, updated_at)
		VALUES (?, 'disabled', ?, 'inactive', 0, datetime('now'), datetime('now'))`,
		uid.String(), string(hash)).Error)

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "disabled",
		"password": "secret",
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "账户已被禁用")
}

func TestLogin_账号被锁定_返403(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := uuid.New()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	// locked_until 设到未来
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password_hash, status, failed_login, locked_until, created_at, updated_at)
		VALUES (?, 'locked', ?, 'active', 5, datetime('now', '+1 hour'), datetime('now'), datetime('now'))`,
		uid.String(), string(hash)).Error)

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "locked",
		"password": "secret",
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "账户已被锁定")
}

func TestLogin_5次失败后自动锁定30分钟(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	seedActiveUser(t, db, "victim", "right-pwd")

	r := newAuthTestRouter()
	// 连续 5 次错密码
	for i := 0; i < 5; i++ {
		w := doJSON(t, r, "POST", "/auth/login", map[string]any{
			"username": "victim",
			"password": "wrong",
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code, "第 %d 次错密码应 401", i+1)
	}

	// 第 6 次 — 即使密码正确也应 403（被锁）
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "victim",
		"password": "right-pwd",
	})
	assert.Equal(t, http.StatusForbidden, w.Code, "锁定后即使密码对也应 403")
	assert.Contains(t, w.Body.String(), "账户已被锁定")

	// 验证 DB 状态
	var user models.User
	require.NoError(t, db.First(&user, "username = ?", "victim").Error)
	assert.GreaterOrEqual(t, user.FailedLogin, 5, "FailedLogin 应 >= 5")
	assert.NotNil(t, user.LockedUntil, "LockedUntil 应被设")
}

func TestLogin_成功后清零失败次数(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "user", "secret")
	// 模拟已有 3 次失败
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", uid).Update("failed_login", 3).Error)

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]any{
		"username": "user",
		"password": "secret",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证 DB
	var user models.User
	require.NoError(t, db.First(&user, "id = ?", uid).Error)
	assert.Equal(t, 0, user.FailedLogin, "登录成功后 FailedLogin 应清零")
	assert.Nil(t, user.LockedUntil, "登录成功后 LockedUntil 应清空")
	assert.NotNil(t, user.LastLogin, "LastLogin 应被设")
}

// ==================== Logout ====================

func TestLogout_清Cookie并返200(t *testing.T) {
	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/logout", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	// 验证 cookie 被清
	cookies := w.Result().Cookies()
	var authCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "auth_token" {
			authCookie = c
		}
	}
	if authCookie != nil {
		assert.Equal(t, "", authCookie.Value, "logout 后 cookie value 应空")
		assert.Less(t, authCookie.MaxAge, 0, "MaxAge 负数 = 立即过期")
	}
}

// ==================== GetCurrentUser ====================

func TestGetCurrentUser_无userID_返401(t *testing.T) {
	setupAuthTestDB(t)
	r := newAuthTestRouter()
	// 不带 X-User-Id header
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetCurrentUser_用户不存在_返404(t *testing.T) {
	setupAuthTestDB(t)
	r := newAuthTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("X-User-Id", "99999999-9999-9999-9999-999999999999")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetCurrentUser_正常返用户信息(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "alice", "secret")

	r := newAuthTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("X-User-Id", uid.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"username":"alice"`)
	assert.Contains(t, body, uid.String())
	// 关键：不应返回 password_hash
	assert.NotContains(t, body, "password_hash")
	assert.NotContains(t, body, "PasswordHash")
}

// ==================== ChangePassword ====================

func TestChangePassword_HappyPath_成功(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "alice", "old-pwd")

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]any{
		"old_password": "old-pwd",
		"new_password": "new-pwd-123",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "密码修改成功")

	// 验证 DB hash 实际变了
	var user models.User
	require.NoError(t, db.First(&user, "id = ?", uid).Error)
	assert.True(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("new-pwd-123")) == nil,
		"新密码应能 hash 校验")
}

func TestChangePassword_缺字段_返400(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "alice", "old")

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]any{
		"old_password": "old",
		// 缺 new_password
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChangePassword_旧密码错_返400(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "alice", "real-pwd")

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]any{
		"old_password": "wrong-pwd",
		"new_password": "new-pwd",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "旧密码错误")

	// 验证 hash 没变
	var user models.User
	require.NoError(t, db.First(&user, "id = ?", uid).Error)
	assert.True(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("real-pwd")) == nil,
		"旧密码错时 hash 应保持原值")
}

// ==================== 已知 Issue 文档化 ====================
//
// 审查发现 issue #1 (FailedLogin++ 并发 race)：
//   当前实现：每次失败 user.FailedLogin++ 然后 Save
//   并发场景：5 个并发失败请求，每个都 +1 后 Save，最后 saved.FailedLogin = 1
//   实际攻击者 1 秒发 100 个失败请求，FailedLogin 只 +1，远达不到 5 次阈值
//   修复：GORM `UpdateColumn("failed_login", gorm.Expr("failed_login + 1"))`
//   或在 SQL 层加 SELECT ... FOR UPDATE
//
// 审查发现 issue #2 (改密码无强度校验)：
//   当前实现：NewPassword 任意字符都通过
//   修复建议：min 8 chars + 数字 + 字母
//   修复后，加 expect 400 测试
//
// 审查发现 issue #3 (改密码可设回旧密码)：
//   当前实现：旧密码校验通过后立即设新 hash，不防回设
//   修复建议：先 CompareHashAndPassword(newPassword) 失败才允许
//   修复后，加 expect 400 测试
//
// ==================== 辅助 ====================

func doJSONWithUser(t *testing.T, r *gin.Engine, method, path, userID string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
