package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

// authUserTestSchema = 共享 users DDL（见 testschema_test.go）+ 本文件私有的 audit_logs
const authUserTestSchema = testUsersDDL + `
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    username TEXT,
    action TEXT,
    resource TEXT,
    resource_id TEXT,
    method TEXT,
    path TEXT,
    ip TEXT,
    user_agent TEXT,
    status INTEGER,
    error_msg TEXT,
    request_id TEXT,
    created_at DATETIME
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
	// C7: 跳过首次改密 — 测试路由 (注入 user_id via X-User-Id header)
	g.POST("/skip-password-change", func(c *gin.Context) {
		uid := c.GetHeader("X-User-Id")
		if uid == "" {
			c.Set("user_id", "")
		} else {
			c.Set("user_id", uid)
		}
		handlers.SkipPasswordChange(c)
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

// ==================== BUG FIX 回归测试（并发 race / 弱密码 / 改回旧密码） ====================

// TestLogin_并发失败_原子自增不会丢失计数 — BUG#1
//
//	之前 user.FailedLogin++ 在内存里递增后再 Save，5 并发失败实际只 +1
//	修复：gorm.Expr("failed_login + 1") 在 SQL 层原子自增
func TestLogin_并发失败_原子自增不会丢失计数(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "raceuser", "correct-pwd")

	// sqlite 的 `file::memory:?cache=shared` 是**表级**共享锁：多连接并发写同一张表会
	// 直接返回 SQLITE_LOCKED（不是 busy，重试无效），而 handler 对计数更新是 `_ = ...Error`
	// 静默吞错 → 计数随机少加（CI 上实测 10 并发只 +2，锁定断言随之 nil 解引用 panic）。
	// 把连接池压到 1 条：语句串行化后不再有锁冲突，但"读-改-写"仍是三条独立语句、
	// 并发交错窗口依旧存在 —— 足以证伪 in-memory++ 的丢更新（那才是本测试要抓的 bug）。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	r := gin.New()
	r.POST("/login", handlers.Login)

	// 模拟 10 个并发失败登录
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = doJSON(t, r, "POST", "/login", map[string]string{
				"username": "raceuser",
				"password": "wrong-pwd",
			})
		}()
	}
	wg.Wait()

	var updated models.User
	db.First(&updated, "id = ?", uid)
	// 关键断言：原子自增不能丢数（之前 in-memory++ 实际只 +1）。
	// 为什么是 >= 5 而不是 == 10：计数到 5 会写 locked_until，之后才读到该行的请求会
	// 在锁检查处提前返回、不再计数。反过来，计数 < 5 就意味着有更新丢了（只有发生 5 次
	// 自增才可能置锁），所以 >= 5 是确定的、能证伪 in-memory++ 的下界。
	assert.GreaterOrEqual(t, updated.FailedLogin, 5, "10 并发失败必须能原子加到阈值 5")
	assert.NotNil(t, updated.LockedUntil, "达到 5 次阈值必须锁定")
	assert.True(t, updated.LockedUntil.After(time.Now()), "锁定时间在未来")
}

// TestChangePassword_弱密码_返400 — BUG#2
func TestChangePassword_弱密码_返400(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "weakpwd", "real-pwd-123")

	r := newAuthTestRouter()

	tests := []struct {
		name string
		pwd  string
	}{
		{"太短1字符", "a"},
		{"太短7字符", "abc1234"},
		{"无数字", "abcdefghij"},
		{"无字母", "1234567890"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]string{
				"old_password": "real-pwd-123",
				"new_password": tc.pwd,
			})
			assert.Equal(t, http.StatusBadRequest, w.Code, "弱密码必须拒绝")
		})
	}
}

// TestChangePassword_改回旧密码_返400 — BUG#3
func TestChangePassword_改回旧密码_返400(t *testing.T) {
	setupAuthTestDB(t)
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "samepwd", "real-pwd-123")

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]string{
		"old_password": "real-pwd-123",
		"new_password": "real-pwd-123", // 跟旧密码一样
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "设回旧密码必须拒绝")

	// 验证 hash 没被改
	var updated models.User
	db.First(&updated, "id = ?", uid)
	assert.True(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("real-pwd-123")) == nil,
		"拒绝时 hash 必须保持原值")
}

// ==================== C7: 首次登录强改密 + 跳过 ====================

// TestLogin_返回MustChangePasswordFlag_True — seed 默认用户首次登录带 flag
func TestLogin_返回MustChangePasswordFlag_True(t *testing.T) {
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "freshuser", "old-pass-1")
	// 显式 must_change_password=1 (模拟 seed admin 默认状态)
	require.NoError(t, db.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ?`, uid).Error)

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]string{
		"username": "freshuser",
		"password": "old-pass-1",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Code int `json:"code"`
		Data struct {
			MustChangePassword bool `json:"must_change_password"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.True(t, body.Data.MustChangePassword, "首次登录用户必须返 must_change_password=true")
}

// TestLogin_返回MustChangePasswordFlag_False — 改过密后登录不带 flag
func TestLogin_返回MustChangePasswordFlag_False(t *testing.T) {
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "olduser", "old-pass-1")
	require.NoError(t, db.Exec(`UPDATE users SET must_change_password = 0 WHERE id = ?`, uid).Error)

	r := newAuthTestRouter()
	w := doJSON(t, r, "POST", "/auth/login", map[string]string{
		"username": "olduser",
		"password": "old-pass-1",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Data struct {
			MustChangePassword bool `json:"must_change_password"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Data.MustChangePassword, "改过密后不再返 must_change_password")
}

// TestChangePassword_成功后清除Flag — C7 核心: 改密 → flag 自动清
func TestChangePassword_成功后清除Flag(t *testing.T) {
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "tochange", "old-pwd-123")
	// 初始 flag = true
	require.NoError(t, db.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ?`, uid).Error)

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/change-password", uid.String(), map[string]string{
		"old_password": "old-pwd-123",
		"new_password": "brand-new-456",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证: must_change_password=0 + password_set_at NOT NULL
	var updated models.User
	require.NoError(t, db.First(&updated, "id = ?", uid).Error)
	assert.False(t, updated.MustChangePassword, "改密成功后 must_change_password 必须变 false")
	assert.NotNil(t, updated.PasswordSetAt, "改密成功后 password_set_at 必须写入")
}

// TestSkipPasswordChange_强制态一律拒绝 — 判据是 DB flag, 不看客户端自报的 reason
// 主人 7/02 决策「首次登录强改密不可跳」。修复前只拒绝 reason=="first_login" 字面量,
// 空 body / reason=optional 会被放行并清 flag —— 同一账号状态换个字符串即可绕过 (S-1a)。
func TestSkipPasswordChange_强制态一律拒绝(t *testing.T) {
	bodies := map[string]any{
		"空body":              nil,
		"空对象":                map[string]string{},
		"reason=optional":    map[string]string{"reason": "optional"},
		"reason=first_login": map[string]string{"reason": "first_login"},
		"伪造未知reason":         map[string]string{"reason": "whatever"},
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			db := setupAuthTestDB(t)
			uid := seedActiveUser(t, db, "forcedskip", "any-pwd-123")
			require.NoError(t, db.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ?`, uid).Error)

			r := newAuthTestRouter()
			// 「空body」必须是真的不带 body（老 API 兼容承诺），不是 json.Marshal(nil) 的 "null"
			var w *httptest.ResponseRecorder
			if body == nil {
				w = doNoBodyWithUser(t, r, "POST", "/auth/skip-password-change", uid.String())
			} else {
				w = doJSONWithUser(t, r, "POST", "/auth/skip-password-change", uid.String(), body)
			}
			assert.Equal(t, http.StatusBadRequest, w.Code)

			// 拒绝路径必须不动 user 表
			var got models.User
			require.NoError(t, db.First(&got, "id = ?", uid).Error)
			assert.True(t, got.MustChangePassword, "拒绝路径必须保留 must_change_password flag")
			assert.Nil(t, got.PasswordSetAt, "拒绝路径不能写 password_set_at")

			var auditCount int64
			require.NoError(t, db.Raw(`SELECT COUNT(*) FROM audit_logs WHERE action = 'skip_password_change' AND user_id = ?`, uid.String()).Scan(&auditCount).Error)
			assert.Equal(t, int64(0), auditCount, "handler 内不再写 skip audit (改由 AuditLog 中间件按路由留痕)")
		})
	}
}

// TestSkipPasswordChange_无待办幂等且无副作用 — flag=false 时纯确认, 不写任何状态 (S-1c)
// 修复前会写 password_set_at = NOW()（用户并未改密）, 未来的密码过期策略会被这套假记录绕过。
func TestSkipPasswordChange_无待办幂等且无副作用(t *testing.T) {
	db := setupAuthTestDB(t)
	uid := seedActiveUser(t, db, "noskip", "any-pwd-123")
	// 测试 schema 的列默认值是 1（与生产 migration 000012 的 DEFAULT TRUE 一致），
	// 这里显式置 0 模拟「已改过密、无待办」状态
	require.NoError(t, db.Exec(`UPDATE users SET must_change_password = 0 WHERE id = ?`, uid).Error)

	r := newAuthTestRouter()
	w := doJSONWithUser(t, r, "POST", "/auth/skip-password-change", uid.String(), map[string]string{"reason": "optional"})
	assert.Equal(t, http.StatusOK, w.Code, "无待办时幂等返回 200")
	// 真·空 body 也必须 200（文档承诺「body 可空，老 API 兼容」）
	w = doNoBodyWithUser(t, r, "POST", "/auth/skip-password-change", uid.String())
	assert.Equal(t, http.StatusOK, w.Code, "空 body 也应 200: %s", w.Body.String())

	var got models.User
	require.NoError(t, db.First(&got, "id = ?", uid).Error)
	assert.False(t, got.MustChangePassword)
	assert.Nil(t, got.PasswordSetAt, "跳过改密不得写 password_set_at")

	var auditCount int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM audit_logs WHERE action = 'skip_password_change' AND user_id = ?`, uid.String()).Scan(&auditCount).Error)
	assert.Equal(t, int64(0), auditCount)
}

// ==================== 辅助 ====================

// doNoBodyWithUser 发一次**真正不带 body**的请求（区别于 doJSONWithUser(..., nil) 的 "null"）
func doNoBodyWithUser(t *testing.T, r *gin.Engine, method, path, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

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
