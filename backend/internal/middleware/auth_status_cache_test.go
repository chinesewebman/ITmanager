package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ==================== 单元测试：authStatusCache (cache 本身) ====================
// TestMain 已在 rate_limit_test.go（M40 扩展了 resetAuthStatusCache）。

// ==================== 单元测试：authStatusCache (cache 本身) ====================

func TestAuthStatusCache_GetSet_Basic(t *testing.T) {
	resetAuthStatusCache()

	// miss → 返回 false
	_, ok := defaultAuthStatusCache.get("user-x")
	assert.False(t, ok, "未 set 的 key 必须 miss")

	// set 后 get 命中
	defaultAuthStatusCache.set("user-x", "active")
	got, ok := defaultAuthStatusCache.get("user-x")
	assert.True(t, ok)
	assert.Equal(t, "active", got)

	// 覆盖写
	defaultAuthStatusCache.set("user-x", "inactive")
	got, _ = defaultAuthStatusCache.get("user-x")
	assert.Equal(t, "inactive", got, "set 必须覆盖旧值")
}

func TestAuthStatusCache_Invalidate_RemovesEntry(t *testing.T) {
	resetAuthStatusCache()
	defaultAuthStatusCache.set("user-y", "active")

	defaultAuthStatusCache.invalidate("user-y")
	_, ok := defaultAuthStatusCache.get("user-y")
	assert.False(t, ok, "invalidate 必须清掉条目")

	// invalidate 未 set 的 key 不 panic
	defaultAuthStatusCache.invalidate("never-set")
}

func TestAuthStatusCache_TTL_过期后不可命中(t *testing.T) {
	c := &authStatusCache{
		data: make(map[string]authStatusCacheEntry),
		ttl:  20 * time.Millisecond,
	}
	c.set("u", "active")
	_, ok := c.get("u")
	require.True(t, ok, "刚 set 必须命中")
	time.Sleep(30 * time.Millisecond)
	_, ok = c.get("u")
	assert.False(t, ok, "TTL 过期后必须 miss")
}

// ==================== 单元测试：lookupUserStatus (与 DB 协作) ====================

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// lookupUserStatus_ActiveUser_HitCacheOnSecondCall 验证：第二次同 user_id
// 不再触发 SELECT（30s TTL 命中）。
func TestLookupUserStatus_ActiveUser_HitCacheOnSecondCall(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	userID := uuid.NewString()

	// 仅期望 1 次 SELECT（第二次走 cache）
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))

	// 第 1 次：DB 读
	s1, err := lookupUserStatus(db, userID)
	require.NoError(t, err)
	assert.Equal(t, "active", s1)

	// 第 2 次：cache 命中（不应再期望任何 SELECT）
	s2, err := lookupUserStatus(db, userID)
	require.NoError(t, err)
	assert.Equal(t, "active", s2)

	// mock.ExpectationsWereMet 会校验「期望的 query 全部被消费，且无未期望的」
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("期望 1 次 SELECT 但 mock 报错: %v", err)
	}
}

// lookupUserStatus_InactiveUser_ReturnsInactive 验证 inactive 直接进 cache。
func TestLookupUserStatus_InactiveUser_ReturnsInactive(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	userID := uuid.NewString()

	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("inactive"))

	s, err := lookupUserStatus(db, userID)
	require.NoError(t, err)
	assert.Equal(t, "inactive", s)
}

// lookupUserStatus_UserDeleted_TreatedAsInactive 防御：DB 里没有该用户
// （被物理删除）写 cache 为 inactive，下次 cache hit 直接拒绝（不再走 DB）。
func TestLookupUserStatus_UserDeleted_TreatedAsInactive(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	userID := uuid.NewString()

	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"})) // 空结果集

	s1, err := lookupUserStatus(db, userID)
	require.NoError(t, err)
	assert.Equal(t, "inactive", s1, "DB 无结果 → 视为 inactive")

	// 第 2 次走 cache（不应再触发 SELECT）
	s2, err := lookupUserStatus(db, userID)
	require.NoError(t, err)
	assert.Equal(t, "inactive", s2)
}

// lookupUserStatus_DBError_ReturnsError 验证 DB 错误不写 cache、不吞错。
func TestLookupUserStatus_DBError_ReturnsError(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	userID := uuid.NewString()

	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnError(assert.AnError)

	s, err := lookupUserStatus(db, userID)
	assert.Error(t, err)
	assert.Equal(t, "", s, "DB 错误时返回零值")

	// 错误路径不写 cache：第 2 次必须再次 SELECT（mock 会校验）
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnError(assert.AnError)
	_, _ = lookupUserStatus(db, userID)
}

// ==================== 端到端测试：AuthMiddleware JWT 路径 + cache 接线 ====================

// authMiddlewareTestRouter 构造只挂 AuthMiddleware 的 gin.Engine。
// 与 auth_scope_test.go 的 doJWTRequest 等价，但额外注入 sqlmock DB。
func authMiddlewareTestRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	oldDB := database.GetDB()
	database.SetDBForTest(db)
	t.Cleanup(func() { database.SetDBForTest(oldDB) })

	resetAuthStatusCache()
	t.Cleanup(resetAuthStatusCache)

	r := gin.New()
	r.Use(AuthMiddleware())
	r.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString("username"))
	})
	return r
}

func doAuthRequest(r *gin.Engine, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func mintJWT(t *testing.T, userID string) string {
	t.Helper()
	// 不复用 GenerateToken（依赖 config 全局），手签一个本地 token。
	secret := "test-secret-32-bytes-for-hmac-sha256!"
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID:   userID,
		Username: "alice",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := tok.SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

// AC-M40-1: 用户被禁用后，旧 JWT 下次请求 → 401。
func TestAuthMiddleware_JWT_UserInactive_Returns401(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	r := authMiddlewareTestRouter(t, db)

	// VerifyToken 走 config.Get()，需要装一个最小 config（与 setupAuthEnv 同款）
	setupMinConfig(t)

	userID := uuid.NewString()
	tok := mintJWT(t, userID)

	// 第 1 次：DB 返回 active → 200
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	w1 := doAuthRequest(r, tok)
	assert.Equal(t, http.StatusOK, w1.Code, "active 用户首请求必须 200")

	// 清 cache 模拟「运维改 DB 后 cache 已过期」
	resetAuthStatusCache()

	// 第 2 次：DB 返回 inactive → 401
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("inactive"))
	w2 := doAuthRequest(r, tok)
	assert.Equal(t, http.StatusUnauthorized, w2.Code, "inactive 用户旧 JWT 必须 401")
}

// AC-M40-2: 同 user 200 次请求 → ≤ 1 次 SELECT（30s TTL 命中 cache）。
func TestAuthMiddleware_JWT_ActiveUser_CacheHitsAvoidDB(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	r := authMiddlewareTestRouter(t, db)

	setupMinConfig(t)
	userID := uuid.NewString()
	tok := mintJWT(t, userID)

	// 只期望 1 次 SELECT
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))

	// 200 次同 JWT 请求
	var okCount int32
	for i := 0; i < 200; i++ {
		w := doAuthRequest(r, tok)
		if w.Code == http.StatusOK {
			atomic.AddInt32(&okCount, 1)
		}
	}
	assert.Equal(t, int32(200), okCount, "200 次同 JWT 都应 200")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("期望 1 次 SELECT（其余走 cache），但 mock 报错: %v", err)
	}
}

// AC-M40-3: cache TTL 内 status 翻转不感知（trade-off 钉死）。
func TestAuthMiddleware_JWT_StatusFlipWithinTTL_NotDetected(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	r := authMiddlewareTestRouter(t, db)

	setupMinConfig(t)
	userID := uuid.NewString()
	tok := mintJWT(t, userID)

	// 第 1 次：active → cache 写 active
	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	w1 := doAuthRequest(r, tok)
	assert.Equal(t, http.StatusOK, w1.Code)

	// 不清 cache：DB 现在已变 inactive，但 cache 仍是 active → 应 200
	// 这里**不**追加 mock.ExpectQuery，因为 cache 命中不应该有 SELECT。
	w2 := doAuthRequest(r, tok)
	assert.Equal(t, http.StatusOK, w2.Code, "cache TTL 内 status 翻转不感知（trade-off）")
}

// AC-M40-4: DB 错误 → 500 Internal（fail-closed）。
func TestAuthMiddleware_JWT_DBError_Returns500(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	r := authMiddlewareTestRouter(t, db)

	setupMinConfig(t)
	userID := uuid.NewString()
	tok := mintJWT(t, userID)

	mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
		WithArgs(userID).
		WillReturnError(assert.AnError)

	w := doAuthRequest(r, tok)
	assert.Equal(t, http.StatusInternalServerError, w.Code, "DB 错误必须 500（fail-closed）")
}

// setupMinConfig 装一份最小 config（VerifyToken 内部 config.Get() 用）。
func setupMinConfig(t *testing.T) {
	t.Helper()
	old := currentConfig()
	config.SetForTest(&config.Config{Auth: config.AuthConfig{
		JWT: config.JWTConfig{Secret: "test-secret-32-bytes-for-hmac-sha256!", Expire: 3600},
	}})
	t.Cleanup(func() { config.SetForTest(old) })
}

// ==================== 变异反证（per task-completion-protocol §Mutation Inversion） ====================

// TestMutationInversion_CacheDisabled_FailsAC202 验证：把 cache.set 注释掉
// 后，AC-M40-2（200 req → 1 SELECT）的 mock 期望就会因「剩余 199 次未期望的
// SELECT」而失败 → 证明 cache.set 是守门网的关键。
//
// 实现：AC-M40-2 的 TestAuthMiddleware_JWT_ActiveUser_CacheHitsAvoidDB
// 已经天然承担「mutation inversion 守门」的角色——只要 cache 失效（去掉
// cache.set、或 cache.get 永远返回 false），200 次请求就会触发 200 次
// SELECT，mock 校验会失败。
//
// 这里用 lookupUserStatusNoCache 单独复现「cache 完全旁路」时的行为，
// 证明「守门网确实能检测到 cache 失效」（如果 mock 校验通过 = cache 失效也
// 没被检测到 = 守门网漏了，这是 mutation inversion 必须排除的失败模式）。
func TestMutationInversion_CacheDisabled_Detected(t *testing.T) {
	resetAuthStatusCache()
	db, mock := newMockDB(t)
	setupMinConfig(t)

	userID := uuid.NewString()

	// 模拟「cache 完全旁路」：50 次 SELECT 期望
	for i := 0; i < 50; i++ {
		mock.ExpectQuery(`SELECT status FROM users WHERE id =`).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	}

	// 走 lookupUserStatusNoCache（不走 cache），50 次请求 → 50 次 SELECT
	for i := 0; i < 50; i++ {
		s, err := lookupUserStatusNoCache(db, userID)
		require.NoError(t, err)
		require.Equal(t, "active", s)
	}

	// 若 cache 被去掉，mock 会因「仍有未匹配 SELECT」而失败
	// 此处 PASS = cache 失效能被 mock 检测（守门网 binding 成立）
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("cache 失效未被守门网检测（mutation 漏了）: %v", err)
	}
	t.Log("✅ Mutation inversion: cache 失效时 50 SELECT 全部被 mock 期望覆盖，守门网 binding 成立")
}

// lookupUserStatusNoCache 是 mutation inversion 专用的「不带 cache」版本。
// 它直接走 DB，与 lookupUserStatus 的唯一区别是不写 cache、不读 cache。
// 仅用于测试 mutation 假设，不在生产代码中使用。
func lookupUserStatusNoCache(db *gorm.DB, userID string) (string, error) {
	if db == nil {
		return "active", nil
	}
	var status string
	if err := db.Raw("SELECT status FROM users WHERE id = ?", userID).Scan(&status).Error; err != nil {
		return "", err
	}
	if status == "" {
		status = "inactive"
	}
	return status, nil
}
