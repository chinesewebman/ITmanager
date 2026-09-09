package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// G-36 / M3：企业微信渠道端到端。
//
// 核心回归点是 **body 形状**：M1 只把 seed 的键名对齐成 url，body 仍走 WebhookSender
// 的 `{"content":…}`，企微一律回 400/93000 —— 键名对了照样发不出去。所以这里对请求体
// 做结构断言（不是「发了就行」）。

func TestWeChatSender_body形状与回执校验(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotCT = r.Method, r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		// 企微群机器人不需要签名头：WebhookSender 的 X-Webhook-Secret 不应被抄过来
		assert.Empty(t, r.Header.Get("X-Webhook-Secret"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.Equal(t, "wechat", s.Type())

	require.NoError(t, s.Send(context.Background(), "", "CPU 95%"))
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/json", gotCT)
	// 企微要 msgtype/text.content；`{"content":…}` 会被拒（93000 之类）
	assert.Equal(t, map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": "CPU 95%"},
	}, gotBody)
}

func TestWeChatSender_回执errcode非0判失败(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook key"}`))
	}))
	defer srv.Close()

	s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)
	err = s.Send(context.Background(), "", "x")
	require.Error(t, err, "HTTP 2xx 但 errcode != 0 必须判失败")
	assert.Contains(t, err.Error(), "wechat errcode=93000")
	assert.Contains(t, err.Error(), "invalid webhook key")
}

// fail-closed：与钉钉同口径（errcodeRespErr），企微恒返 errcode。
func TestWeChatSender_回执缺errcode_fail_closed(t *testing.T) {
	cases := []string{`{}`, `null`, `{"errmsg":"ok"}`, `{"errcode":null}`, `{"foo":1}`}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
			require.NoError(t, err)
			err = s.Send(context.Background(), "", "x")
			require.Error(t, err, "缺 errcode 的回执不能判成功")
			assert.Contains(t, err.Error(), "wechat 回执缺 errcode")
		})
	}
}

func TestWeChatSender_回执非JSON判失败(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()

	s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)
	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wechat 回执不是合法 JSON")
}

func TestWeChatSender_构造校验(t *testing.T) {
	_, err := NewWeChatSender(&models.NotificationChannel{Config: `{}`}) //nolint:exhaustruct
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")

	_, err = NewWeChatSender(&models.NotificationChannel{Config: `{not json`}) //nolint:exhaustruct
	assert.Error(t, err)
}

func TestWeChatSender_HTTP非2xx返错(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)
	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// G-28 口径：连接失败的错误文本不得带 URL 的 path/query（企微的 key 就在 query 里）。
func TestWeChatSender_发送失败不泄漏URL凭据(t *testing.T) {
	ln := newDeadListener(t)
	secretURL := "http://" + ln.Addr().String() + "/cgi-bin/webhook/send?key=SUPERSECRETKEY"
	s, err := NewWeChatSender(&models.NotificationChannel{Config: `{"url":"` + secretURL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = s.Send(ctx, "", "内容")

	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "应保留底层 ctx 超时: %v", err)
	require.Contains(t, err.Error(), "http://127.0.0.1:", "URL 必须塌缩成 scheme://host")
	assert.NotContains(t, err.Error(), "SUPERSECRETKEY")
	assert.NotContains(t, err.Error(), "key=")
	assert.NotContains(t, err.Error(), "/cgi-bin/webhook/send")
}

// 非法 URL 走的是 NewRequestWithContext（url.Parse）分支：这条路径没有 http.Client 的
// stripPassword，*url.Error 原样带 query → 只能回底层 cause（与钉钉同一条 G-28 口径）。
func TestWeChatSender_非法URL不泄漏原串(t *testing.T) {
	s, err := NewWeChatSender(&models.NotificationChannel{ //nolint:exhaustruct
		Config: `{"url":"http://[::1/cgi-bin/webhook/send?key=SECRETKEY"}`,
	})
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无效的 url")
	assert.NotContains(t, err.Error(), "SECRETKEY")
	assert.NotContains(t, err.Error(), "[::1")
}

// 安全审计 M-1：企微凭据的参数名是**裸 `key`**，redact.Text 规则 3 刻意不收它
// （收了会误伤 "primary key="），规则 1 只认 URL 形状 —— 于是网关/WAF/反代只回显
// path+query（无 scheme://host）或 `invalid key=…` 时，key 会经 errmsg 进
// error_msg 与应用日志。这里钉住值级抹除。
func TestWeChatSender_回执回显key被抹掉(t *testing.T) {
	const key = "SUPERSECRETKEYVALUE"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 两种真实回显形态：裸键值 + path/query（无 scheme://host → 规则 1 不命中）
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid key=` + key +
			` /cgi-bin/webhook/send?key=` + key + `"}`))
	}))
	defer srv.Close()

	s, err := NewWeChatSender(&models.NotificationChannel{ //nolint:exhaustruct
		Config: `{"url":"` + srv.URL + `/cgi-bin/webhook/send?key=` + key + `"}`,
	})
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), key, "凭据值不得进 error_msg")
	assert.Contains(t, err.Error(), "***", "应替换成占位符而不是丢掉整段")
	assert.Contains(t, err.Error(), "errcode=93000", "其余排障信息保留")
}

func TestCredentialValues_编码形态与键名过滤(t *testing.T) {
	got := credentialValues("https://qyapi.weixin.qq.com/send?key=a+b%2Fc&id=42&token=TK")
	assert.Contains(t, got, "a b/c", "解码后的值")
	assert.Contains(t, got, "a+b%2Fc", "原始形态（上游可能回显原样）")
	assert.Contains(t, got, "TK")
	assert.NotContains(t, got, "42", "非凭据参数不参与抹除，避免误伤文本")

	// 原始形态与「解码后再编码」形态**不相等**：`%20` 再编码会变 `+`。
	// 只收解码/再编码形态会漏掉上游原样回显的 `a%20b`（用 u.Query() 就会漏）。
	got = credentialValues("https://h/send?key=a%20b")
	assert.Contains(t, got, "a%20b", "原始 percent 形态")
	assert.Contains(t, got, "a b", "解码形态")

	assert.Empty(t, credentialValues("://bad"), "URL 不可解析时返回空而不是 panic")
	assert.Empty(t, credentialValues("https://h/send?key="), "空值必须跳过（ReplaceAll 空串会插满文本）")
	assert.Empty(t, credentialValues("https://h/send?id=42"), "无凭据参数")
}

func TestScrubSecrets_无凭据时原样返回(t *testing.T) {
	assert.Equal(t, "abc", scrubSecrets("abc", nil))
	assert.Equal(t, "***", scrubSecrets("SEC", []string{"SEC"}))
}

// 工厂分派 + 支持类型文案（后者会经 service 脱敏回显到 400 body）。
func TestNewSender_wechat分派(t *testing.T) {
	s, err := NewSender(&models.NotificationChannel{ //nolint:exhaustruct
		Type:   "wechat",
		Config: `{"url":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "wechat", s.Type())

	_, err = NewSender(&models.NotificationChannel{Type: "feishu", Config: `{}`}) //nolint:exhaustruct
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wechat", "支持类型清单必须含 wechat")
}
