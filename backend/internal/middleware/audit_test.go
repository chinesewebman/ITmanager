package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// TestResourceFromPath_动态段在前/中间 (audit-P1 回归)
// 修前 bug: /api/:tenant/users 返 "" (因 :tenant 后直接 return)
// 修后: 跳过动态段继续找第一个静态段 → "users"
func TestResourceFromPath_动态段在前返第一个静态段(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/:tenant/users", "users"},
		{"/api/users/:id/posts", "users"},
		{"/api/:id", "unknown"},      // 全是动态段 → unknown
		{"/:org/api/repos", "repos"}, // 跳 :org, 跳 "api", 取 "repos"
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r := gin.New()
			r.GET(tt.path, func(c *gin.Context) {
				got := resourceFromPath(c)
				assert.Equal(t, tt.expected, got)
			})
			// 把所有动态段 (:id/:tenant/:org) 替换成 "x" 让 gin 能 match
			concrete := tt.path
			for _, dyn := range []string{":id", ":tenant", ":org"} {
				concrete = strings.ReplaceAll(concrete, dyn, "x")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", concrete, nil))
		})
	}
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

// TestAuditLog_InvalidUUIDLogWarn (P2)
// audit user_id 非合法 uuid 时应 log warn 而非静默丢弃
func TestAuditLog_InvalidUUIDLogWarn(t *testing.T) {
	db, mock := newAuditDB(t)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "not-a-uuid") // 故意放非法值
		c.Set("username", "tester")
		c.Next()
	})
	r.Use(AuditLog(AuditConfig{DB: db, Async: false}))
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })

	// INSERT 仍应执行 (P2: parse 失败仅 log warn, 不影响主路径)
	mock.ExpectQuery(`INSERT INTO "audit_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/x", nil))

	assert.Equal(t, 200, w.Code, "请求仍成功 (uuid parse 失败不阻塞)")
	assert.NoError(t, mock.ExpectationsWereMet(), "INSERT 应执行 (即使 user_id 非法)")
}

// ==================== G-44：审计字段净化 + 按字符截断 ====================

// 尺子必须是 rune：varchar(n) 按字符计数，按字节截断会把多字节字符切成非法 UTF-8，
// PG 拒收（22021）→ 审计行照样丢，只是从「超长」换成「编码非法」。
func TestSanitizeField_按字符截断且输出合法UTF8(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		max       int
		wantRunes int
		want      string // 非空则断言精确值
	}{
		{"多字节超长按字符截断", strings.Repeat("中", 600), 500, 500, ""},
		{"ASCII 超长按字符截断", strings.Repeat("a", 507), 500, 500, ""},
		{"恰好等于上限不动", strings.Repeat("中", 500), 500, 500, ""},
		{"未超限原样返回", "资产/中文/名", 100, 7, "资产/中文/名"},
		{"CRLF 并进同一行", "a\r\nb", 500, 2, "ab"},
		{"NUL 与 DEL 被删", "a\x00b\x7fc", 500, 3, "abc"},
		{"空串", "", 500, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := sanitizeField(c.in, c.max)
			assert.True(t, utf8.ValidString(out), "输出必须是合法 UTF-8，实际 %q", out)
			assert.Equal(t, c.wantRunes, utf8.RuneCountInString(out))
			assert.NotContains(t, out, "\r")
			assert.NotContains(t, out, "\n")
			if c.want != "" {
				assert.Equal(t, c.want, out)
			}
		})
	}
}

// 走真实路由拿 context（resourceFromPath 依赖 c.FullPath()），再直接断言 buildAuditEntry
// 的产物 —— 比隔着 sqlmock 的 SQL 参数更容易读，且测的正是本次改的代码。
func TestBuildAuditEntry_长路径与超长RequestID被净化(t *testing.T) {
	var captured *gin.Context
	r := gin.New()
	r.GET("/api/assets/:id", func(c *gin.Context) {
		captured = c
		c.Status(200)
	})

	req := httptest.NewRequest("GET",
		"/api/assets/"+strings.Repeat("中", 600)+"%0d%0a[FAKE]", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("r", 60))
	req.Header.Set("User-Agent", "UA/1.0 "+strings.Repeat("中", 600)+"\r\nfake-ua")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.NotNil(t, captured, "handler 必须被命中")

	entry := buildAuditEntry(captured, AuditConfig{
		ActionFunc: func(c *gin.Context) string { return c.Request.Method },
	})

	// path：截到 varchar(500) 且是合法 UTF-8（按字节截断这里会留下半个汉字）
	assert.Equal(t, 500, utf8.RuneCountInString(entry.Path), "path 必须截到列宽 500 字符")
	assert.True(t, utf8.ValidString(entry.Path), "path 必须是合法 UTF-8，实际 %q", entry.Path)
	assert.NotContains(t, entry.Path, "\r")
	assert.NotContains(t, entry.Path, "\n")

	// request_id：列宽只有 50，此前完全不截断 —— 超长即 22001 丢整行
	assert.Equal(t, 50, utf8.RuneCountInString(entry.RequestID),
		"request_id 必须截到列宽 50 字符")

	// user_agent：同样 500，且多字节不被切坏
	assert.Equal(t, 500, utf8.RuneCountInString(entry.UserAgent))
	assert.True(t, utf8.ValidString(entry.UserAgent))

	// 正常值不受影响：resource 取自路由模式，短且无需截断
	assert.Equal(t, "assets", entry.Resource)
}

// 未超限的普通请求必须逐字不变（防止净化误伤正常审计内容）。
func TestBuildAuditEntry_正常请求字段不变(t *testing.T) {
	var captured *gin.Context
	r := gin.New()
	r.GET("/api/assets/:id", func(c *gin.Context) { captured = c; c.Status(200) })

	req := httptest.NewRequest("GET", "/api/assets/abc", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("User-Agent", "curl/8.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	entry := buildAuditEntry(captured, AuditConfig{
		ActionFunc: func(c *gin.Context) string { return c.Request.Method },
	})
	assert.Equal(t, "/api/assets/abc", entry.Path)
	assert.Equal(t, "req-123", entry.RequestID)
	assert.Equal(t, "curl/8.0", entry.UserAgent)
	assert.Equal(t, "assets", entry.Resource)
	assert.Equal(t, "GET", entry.Method)
}
