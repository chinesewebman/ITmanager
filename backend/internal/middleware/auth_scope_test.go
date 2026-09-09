package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ==================== apiKeyAllows 单元测试（缺陷 D-7） ====================

func TestAPIKeyAllows(t *testing.T) {
	cases := []struct {
		name   string
		perms  models.StringList
		method string
		want   bool
	}{
		{"read+GET", models.StringList{"read"}, http.MethodGet, true},
		{"read+HEAD", models.StringList{"read"}, http.MethodHead, true},
		{"read+OPTIONS", models.StringList{"read"}, http.MethodOptions, true},
		{"read+POST", models.StringList{"read"}, http.MethodPost, false},
		{"read+PUT", models.StringList{"read"}, http.MethodPut, false},
		{"read+DELETE", models.StringList{"read"}, http.MethodDelete, false},
		{"write+GET", models.StringList{"write"}, http.MethodGet, true},
		{"write+POST", models.StringList{"write"}, http.MethodPost, true},
		{"write+DELETE", models.StringList{"write"}, http.MethodDelete, true},
		{"admin+GET", models.StringList{"admin"}, http.MethodGet, true},
		{"admin+POST", models.StringList{"admin"}, http.MethodPost, true},
		{"read+write+POST", models.StringList{"read", "write"}, http.MethodPost, true},
		{"大小写与空白容错", models.StringList{" READ ", " Write "}, http.MethodPost, true},
		// fail-safe：权限配置缺失/未知时按只读处理，绝不放行写
		{"空权限+GET", models.StringList{}, http.MethodGet, true},
		{"空权限+POST", models.StringList{}, http.MethodPost, false},
		{"nil权限+POST", nil, http.MethodPost, false},
		{"未知权限+POST", models.StringList{"superuser"}, http.MethodPost, false},
		{"未知权限+GET", models.StringList{"superuser"}, http.MethodGet, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, apiKeyAllows(c.perms, c.method))
		})
	}
}

// ==================== API Key scope 端到端拦截（缺陷 D-7） ====================

func newAPIKeyAuthDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// currentConfig 返回当前全局配置；未加载时返回 nil（config.Get 会 panic）。
func currentConfig() *config.Config {
	var c *config.Config
	func() {
		defer func() { _ = recover() }()
		c = config.Get()
	}()
	return c
}

// setupAPIKeyAuth 注入全局 DB/config，返回只读 Key 与写 Key 的明文。
func setupAPIKeyAuth(t *testing.T, db *gorm.DB) (readKey, writeKey string) {
	t.Helper()
	setupAuthEnv(t, db)
	return "read-key-plain", "write-key-plain"
}

// setupAuthEnv 注入全局 DB + 测试用 config（JWT secret + API key pepper），cleanup 恢复。
func setupAuthEnv(t *testing.T, db *gorm.DB) {
	t.Helper()
	oldDB := database.GetDB()
	oldCfg := currentConfig()
	if db != nil {
		database.SetDBForTest(db)
	}
	config.SetForTest(&config.Config{Auth: config.AuthConfig{
		JWT:          config.JWTConfig{Secret: "test-secret-32-bytes-for-hmac-sha256!", Expire: 3600},
		APIKeyPepper: "test-pepper",
	}})
	t.Cleanup(func() {
		database.SetDBForTest(oldDB)
		config.SetForTest(oldCfg)
	})
}

func apiKeyRow(perms string) *sqlmock.Rows {
	return apiKeyRowWith(perms, "[]", nil)
}

func apiKeyRowWith(perms, ipWhitelist string, expiresAt interface{}) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "user_id", "name", "key_hash", "prefix", "permissions",
		"ip_whitelist", "rate_limit", "expires_at", "status", "created_at", "updated_at",
	}).AddRow(
		uuid.NewString(), uuid.NewString(), "test-key", "hash", "read-k",
		perms, ipWhitelist, 1000, expiresAt, "active", nil, nil,
	)
}

func doAPIKeyRequest(t *testing.T, db *gorm.DB, key, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(AuthMiddleware())
	r.Handle(method, path, func(c *gin.Context) { c.String(http.StatusOK, "reached") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "X-API-Key "+key)
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_APIKey只读Key写操作返403(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	// 只读 Key 打 POST：只应发生一次 api_keys 查询，随后被 403 拦下（不查 user）
	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["read"]`))

	r := gin.New()
	r.Use(AuthMiddleware())
	r.POST("/api/assets", func(c *gin.Context) { c.String(http.StatusOK, "reached") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.Header.Set("Authorization", "X-API-Key "+readKey)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "只读 API Key 不得写")
	assert.NotContains(t, w.Body.String(), "reached", "handler 不应被执行")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey写Key写操作放行(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	_, writeKey := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["write"]`))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role"}).
			AddRow(uuid.NewString(), "alice", "operator"))

	r := gin.New()
	r.Use(AuthMiddleware())
	r.POST("/api/assets", func(c *gin.Context) {
		c.String(http.StatusOK, "reached:"+c.GetString("username"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.Header.Set("Authorization", "X-API-Key "+writeKey)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "reached:alice")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey只读Key读操作放行(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["read"]`))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role"}).
			AddRow(uuid.NewString(), "bob", "user"))

	r := gin.New()
	r.Use(AuthMiddleware())
	r.GET("/api/assets", func(c *gin.Context) { c.String(http.StatusOK, "reached") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/assets", nil)
	req.Header.Set("Authorization", "X-API-Key "+readKey)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// 只断言 200 不够：中间件若写了自己的 200 空响应也会绿。断言 handler 真跑过。
	assert.Contains(t, w.Body.String(), "reached", "handler 必须被真正执行")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== RequireRole 单元测试 ====================

func TestRequireRole(t *testing.T) {
	cases := []struct {
		name     string
		role     string
		roles    []string
		wantCode int
	}{
		{"admin放行", "admin", []string{"admin"}, http.StatusOK},
		{"operator拒绝", "operator", []string{"admin"}, http.StatusForbidden},
		{"空角色拒绝", "", []string{"admin"}, http.StatusForbidden},
		{"多角色任一命中", "operator", []string{"admin", "operator"}, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(ctx *gin.Context) {
				ctx.Set("role", c.role)
				ctx.Next()
			})
			r.Use(RequireRole(c.roles...))
			r.GET("/x", func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			assert.Equal(t, c.wantCode, w.Code)
		})
	}
}

// ==================== AuthMiddleware JWT 路径 ====================

func doJWTRequest(t *testing.T, method, path, header, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(AuthMiddleware())
	r.Handle(method, path, func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString("username")+"|"+c.GetString("role"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: cookie})
	}
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_JWT_有效token注入上下文(t *testing.T) {
	setupAuthEnv(t, nil)
	tok, err := GenerateToken(uuid.NewString(), "alice", "admin")
	require.NoError(t, err)

	w := doJWTRequest(t, http.MethodGet, "/x", "Bearer "+tok, "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "alice|admin", w.Body.String())
}

func TestAuthMiddleware_JWT_无效token返401(t *testing.T) {
	setupAuthEnv(t, nil)
	w := doJWTRequest(t, http.MethodGet, "/x", "Bearer not-a-jwt", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_缺Authorization且无cookie返401(t *testing.T) {
	setupAuthEnv(t, nil)
	w := doJWTRequest(t, http.MethodGet, "/x", "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_Authorization格式错误返401(t *testing.T) {
	setupAuthEnv(t, nil)
	w := doJWTRequest(t, http.MethodGet, "/x", "Token abc", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_cookie回退_有效token放行(t *testing.T) {
	setupAuthEnv(t, nil)
	tok, err := GenerateToken(uuid.NewString(), "bob", "user")
	require.NoError(t, err)

	w := doJWTRequest(t, http.MethodGet, "/x", "", tok)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "bob|user", w.Body.String())
}

// ==================== AuthMiddleware API Key 失败路径 ====================

func TestAuthMiddleware_APIKey_无效key返401(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey_过期返401(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRowWith(`["read"]`, "[]", time.Now().Add(-time.Hour)))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey_IP不在白名单返403(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRowWith(`["read"]`, `["10.0.0.1"]`, nil))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey_关联用户不存在返401(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["read"]`))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthMiddleware_APIKey_IP在白名单内放行(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	// httptest.NewRequest 默认 RemoteAddr = 192.0.2.1:1234
	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRowWith(`["read"]`, `["192.0.2.1"]`, nil))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role"}).
			AddRow(uuid.NewString(), "carol", "operator"))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusOK, w.Code, "白名单内 IP 应放行")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuthMiddleware_JWT_伪造签名返401 用别的 secret 签的 token 必须被拒。
func TestAuthMiddleware_JWT_伪造签名返401(t *testing.T) {
	setupAuthEnv(t, nil)

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: uuid.NewString(),
		Role:   "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := forged.SignedString([]byte("wrong-secret"))
	require.NoError(t, err)

	w := doJWTRequest(t, http.MethodGet, "/x", "Bearer "+signed, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAuthMiddleware_APIKey_关联用户被禁用返401 审计 M-5：
// 用户 inactive 后，其 API Key 必须立即失效（Key 本身仍 active 且未过期）。
func TestAuthMiddleware_APIKey_关联用户被禁用返401(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["read"]`))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "status"}).
			AddRow(uuid.NewString(), "disabled-user", "admin", "inactive"))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusUnauthorized, w.Code, "被禁用用户的 API Key 应失效")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuthMiddleware_APIKey_关联用户active放行 对照组：status=active 不受影响。
func TestAuthMiddleware_APIKey_关联用户active放行(t *testing.T) {
	db, mock := newAPIKeyAuthDB(t)
	readKey, _ := setupAPIKeyAuth(t, db)

	mock.ExpectQuery(`SELECT \* FROM "api_keys"`).
		WillReturnRows(apiKeyRow(`["read"]`))
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "status"}).
			AddRow(uuid.NewString(), "active-user", "admin", "active"))

	w := doAPIKeyRequest(t, db, readKey, http.MethodGet, "/api/assets")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== RejectAPIKeyAuth（AUTHZ 遗留缺陷 S-2） ====================

// TestRejectAPIKeyAuth 单测中间件本体：
//   - API Key 身份（api_key_id 已设置）→ 403 且 handler 不执行
//   - 会话身份（JWT/cookie，api_key_id 为空）→ 放行
//
// 直接构造 context，不依赖 AuthMiddleware 的 DB 查询。
func TestRejectAPIKeyAuth(t *testing.T) {
	cases := []struct {
		name     string
		apiKeyID string
		want     int
	}{
		{"会话身份放行", "", http.StatusOK},
		{"API Key 身份拒绝", uuid.NewString(), http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			// 模拟 AuthMiddleware 已运行：API Key 路径设置 api_key_id，JWT 路径不设置
			r.Use(func(ctx *gin.Context) {
				if c.apiKeyID != "" {
					ctx.Set("api_key_id", c.apiKeyID)
				}
				ctx.Next()
			})
			reached := false
			r.POST("/api/auth/api-keys", RejectAPIKeyAuth(), func(ctx *gin.Context) {
				reached = true
				ctx.String(http.StatusOK, "reached")
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/api-keys", nil))

			assert.Equal(t, c.want, w.Code)
			assert.Equal(t, c.want == http.StatusOK, reached, "handler 执行与否必须与状态码一致")
			if c.want == http.StatusForbidden {
				assert.Contains(t, w.Body.String(), "登录会话")
			}
		})
	}
}
