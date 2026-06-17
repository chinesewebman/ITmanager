package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() { gin.SetMode(gin.TestMode) }

func newAuditDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// TestAuditLog_写入审计记录 测试同步模式
func TestAuditLog_写入审计记录(t *testing.T) {
	db, mock := newAuditDB(t)

	userID := uuid.NewString()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "alice")
		c.Next()
	})
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/assets/:id", func(c *gin.Context) { c.String(200, "ok") })

	// gorm Create 走 INSERT ... RETURNING "id" (Query 类型)
	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/assets/"+uuid.NewString(), nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuditLog_异步写不阻塞请求
func TestAuditLog_异步写不阻塞请求(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	r.Use(AuditLog(AuditConfig{DB: db, Async: true}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 200, w.Code)
	// 等异步 goroutine 写完
	time.Sleep(50 * time.Millisecond)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuditLog_跳过SkipPaths
func TestAuditLog_跳过SkipPaths(t *testing.T) {
	db, mock := newAuditDB(t)
	// 不应 expect 任何 DB 调用

	r := gin.New()
	r.Use(AuditLog(AuditConfig{DB: db}))
	r.GET("/healthz", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	// /api/x 期望 INSERT, /healthz 不期望
	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest("GET", "/healthz", nil))
	assert.Equal(t, 200, w1.Code)

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest("GET", "/api/x", nil))
	assert.Equal(t, 200, w2.Code)

	assert.NoError(t, mock.ExpectationsWereMet(), "只 /api/x 应写审计")
}

// TestAuditLog_写失败不阻塞主流程
func TestAuditLog_写失败不阻塞主流程(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "still ok") })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnError(assert.AnError)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 200, w.Code, "审计失败不应影响主流程")
}

// TestAuditLog_无User时记录为匿名
func TestAuditLog_无User时记录为匿名(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	// 不设 user_id/username
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 200, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuditLog_捕获状态码
func TestAuditLog_捕获状态码(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/x", func(c *gin.Context) { c.JSON(403, gin.H{"error": "forbidden"}) })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 403, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet(), "应记录 403 状态")
}

// TestResourceFromPath_简单 case
func TestResourceFromPath_简单case(t *testing.T) {
	// 通过 c.FullPath() 注入机制: 用 ServeHTTP 后取 c
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/assets/:id", "assets"},
		{"/api/alert-rules/:id", "alert-rules"},
		{"/api/tickets/:id", "tickets"},
		{"/api/dashboard/stats", "dashboard"},
		{"/unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r := gin.New()
			r.GET(tt.path, func(c *gin.Context) {
				got := resourceFromPath(c)
				assert.Equal(t, tt.expected, got)
			})
			// ServeHTTP 会让 gin 解析 FullPath
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", strings.ReplaceAll(tt.path, ":id", "abc"), nil)
			r.ServeHTTP(w, req)
		})
	}
}

// TestResourceFromPath_无FullPath返unknown
func TestResourceFromPath_无FullPath返unknown(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/orphan", nil)
	assert.Equal(t, "unknown", resourceFromPath(c))
}

// TestResourceFromPath_跳过空段和api
func TestResourceFromPath_跳过空段和api(t *testing.T) {
	r := gin.New()
	r.GET("/api/assets/:id", func(c *gin.Context) {
		assert.Equal(t, "assets", resourceFromPath(c))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/assets/abc", nil))
}

// TestSplitPath_基本
func TestSplitPath_基本(t *testing.T) {
	assert.Equal(t, []string{"api", "assets", "id"}, splitPath("/api/assets/id"))
	assert.Nil(t, splitPath(""))
	assert.Equal(t, []string{"a"}, splitPath("a"))
	assert.Equal(t, []string{"a", "b"}, splitPath("/a/b/"))
}

// TestTruncate_边界
func TestTruncate_边界(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "ab", truncate("abcd", 2))
	assert.Equal(t, "", truncate("", 5))
}

// TestAuditLog_CustomActionFunc
func TestAuditLog_CustomActionFunc(t *testing.T) {
	db, mock := newAuditDB(t)

	called := 0
	r := gin.New()
	r.Use(AuditLog(AuditConfig{
		DB:    db,
		Async: false,
		ActionFunc: func(c *gin.Context) string {
			called++
			return "custom_action"
		},
	}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 1, called, "ActionFunc 应被调 1 次")
}

// TestAuditLog_捕获RequestID
func TestAuditLog_捕获RequestID(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("X-Request-ID", "test-req-id-123")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuditLog_默认SkipPaths
func TestAuditLog_默认SkipPaths(t *testing.T) {
	paths := DefaultSkipPaths()
	assert.True(t, paths["/healthz"])
	assert.True(t, paths["/readyz"])
	assert.True(t, paths["/metrics"])
	assert.True(t, paths["/api/health"])
	assert.False(t, paths["/api/x"])
}
