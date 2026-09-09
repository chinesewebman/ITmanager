// G-33 M1：通知渠道「配置契约」的 handler 级验证。
//
// 这里刻意用**真 service + 真 sqlite**（不是 mock）：400 body 的脱敏声明只有在
// 「真构造器 → service 侧 redact.Text → handler 回显」整条链上才有意义，
// 用 mock service 返回预制错误只能验证 handler 的转发（FIX-PLAN-NOTIFY-CHANNEL §4 V-7）。
package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/api/handlers"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

// newChannelRouterWithRealSvc 真 ChannelService + 手写 DDL 的 sqlite。
// 不用 AutoMigrate：NotificationChannel.ID 带 default:gen_random_uuid()，sqlite 无此函数。
func newChannelRouterWithRealSvc(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE notification_channels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		config TEXT,
		is_enabled INTEGER DEFAULT 1,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)

	h := handlers.NewChannelHandler(service.NewChannelService(db))
	r := gin.New()
	r.POST("/channels", h.CreateChannel)
	r.PUT("/channels/:id", h.UpdateChannel)
	return r, db
}

func postJSON(t *testing.T, r *gin.Engine, method, path string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// V-7 Create 的 400 body 不得回显调用方可控字符串。
//
// sender.go 的 default 分支不再回显 ch.Type（安全审计 M-1）：该文本会经 handler 原样进
// 400 body，而 redact.Text 只挡 URL / 键值形态 —— 裸 token、JWT、percent 编码、
// 无 scheme URL、多行文本都能穿过。六种形态逐个钉住。
func TestChannelHandler_Create400_不回显类型值(t *testing.T) {
	for _, tc := range []struct {
		name, leaky, fragment string
	}{
		{"URL形态", "https://hooks.slack.com/services/T000/B000/SECRETPATH", "SECRETPATH"},
		{"裸token", "ghp-ABCDEF1234567890SECRET", "ghp-ABCDEF"},
		{"JWT", "eyJhbGciOiJIUzI1NiJ9.SECRETPAYLOAD.sig", "eyJhbGciOiJIUzI1NiJ9"},
		{"percent编码", "https%3A%2F%2Fhooks.slack.com%2Fservices%2FSECRETPATH", "SECRETPATH"},
		{"无scheme", "hooks.slack.com/services/SECRETPATH", "SECRETPATH"},
		{"多行", "line1\nPASSWORD=hunter2", "hunter2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newChannelRouterWithRealSvc(t)
			w := postJSON(t, r, http.MethodPost, "/channels", map[string]interface{}{
				"name": "x", "type": tc.leaky, "config": `{}`,
			})

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := w.Body.String()
			assert.Contains(t, body, "unsupported channel type", "仍要能定位到「类型不支持」")
			assert.NotContains(t, body, tc.fragment, "400 body 不得回显调用方字符串")
		})
	}
}

// V-7 Update 路径同上：只改 type 也走构造器，同样不得回显
func TestChannelHandler_Update400_不回显类型值(t *testing.T) {
	r, db := newChannelRouterWithRealSvc(t)

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationChannel{
		ID: id, Name: "钩子", Type: "webhook", Config: `{"url":"https://example.com/hook"}`,
	}).Error)

	const leaky = "ghp-ABCDEF1234567890SECRET"
	w := postJSON(t, r, http.MethodPut, "/channels/"+id.String(), map[string]interface{}{"type": leaky})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "unsupported channel type")
	assert.NotContains(t, body, "ghp-ABCDEF")
}

// H-1 HTTP 层：Go 字段名键 / 主键键 / 未知键一律 400 且 DB 不变。
// 修复前 {"Config": …} / {"Type": …} 在真 PG 上返回 200 并落库（安全审计 H-1）。
func TestChannelHandler_Update400_键白名单(t *testing.T) {
	const original = `{"url":"https://example.com/hook"}`

	seed := func(t *testing.T, db *gorm.DB) uuid.UUID {
		t.Helper()
		id := uuid.New()
		require.NoError(t, db.Create(&models.NotificationChannel{
			ID: id, Name: "钩子", Type: "webhook", Config: original,
		}).Error)
		return id
	}

	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"Go字段名Config_非法JSON", map[string]interface{}{"Config": "{}"}},
		{"Go字段名Config_数字", map[string]interface{}{"Config": 12345}},
		{"Go字段名Type_坏组合", map[string]interface{}{"Type": "dingtalk"}},
		{"小写主键id", map[string]interface{}{"id": uuid.New().String()}},
		{"未知键", map[string]interface{}{"foo": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, db := newChannelRouterWithRealSvc(t)
			id := seed(t, db)

			w := postJSON(t, r, http.MethodPut, "/channels/"+id.String(), tc.updates)
			require.Equal(t, http.StatusBadRequest, w.Code, "键绕过修复前这里返 200")

			var after models.NotificationChannel
			require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
			assert.Equal(t, id, after.ID, "主键不得被改写")
			assert.Equal(t, "webhook", after.Type)
			assert.Equal(t, original, after.Config)
		})
	}

	t.Run("正控_大小写变体归一化后合法即通过", func(t *testing.T) {
		r, db := newChannelRouterWithRealSvc(t)
		id := seed(t, db)

		w := postJSON(t, r, http.MethodPut, "/channels/"+id.String(), map[string]interface{}{
			"Config": `{"url":"https://example.com/hook2"}`,
		})
		require.Equal(t, http.StatusOK, w.Code)

		var after models.NotificationChannel
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Contains(t, after.Config, "hook2")
	})
}

// V-8 400 文案：名称空 / 配置坏，都必须带具体原因（原实现把 Create 的原因硬编码成名称空）
func TestChannelHandler_Create400_文案带具体原因(t *testing.T) {
	t.Run("名称空", func(t *testing.T) {
		r, _ := newChannelRouterWithRealSvc(t)
		w := postJSON(t, r, http.MethodPost, "/channels", map[string]interface{}{
			"name": "", "type": "webhook", "config": `{"url":"https://example.com/hook"}`,
		})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "渠道名称不能为空")
	})

	t.Run("缺url", func(t *testing.T) {
		r, _ := newChannelRouterWithRealSvc(t)
		w := postJSON(t, r, http.MethodPost, "/channels", map[string]interface{}{
			"name": "x", "type": "webhook", "config": `{}`,
		})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "webhook: url is required")
	})
}

// Update 的 ErrInvalidInput 必须转 400（原实现没有该分支 → 会落到 Internal 500）
func TestChannelHandler_Update400_坏config返400(t *testing.T) {
	r, db := newChannelRouterWithRealSvc(t)

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationChannel{
		ID: id, Name: "钩子", Type: "webhook", Config: `{"url":"https://example.com/hook"}`,
	}).Error)

	w := postJSON(t, r, http.MethodPut, "/channels/"+id.String(), map[string]interface{}{"config": `{}`})
	require.Equal(t, http.StatusBadRequest, w.Code, "校验失败应是 400 而不是 500")
	assert.Contains(t, w.Body.String(), "webhook: url is required")
}
