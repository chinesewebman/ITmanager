package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// G-33 M2：钉钉加签 + 回执校验的测试。
//
// 签名向量由**独立实现**算得（Python `hmac`/`hashlib`/`base64`，secret=SECtest123、
// ts=1700000000000、msg=ts+"\n"+secret），不是拿被测代码自证：
//
//	python3 -c "import hmac,hashlib,base64; s=b'SECtest123'; m=b'1700000000000\nSECtest123'; \
//	  print(base64.b64encode(hmac.new(s,m,hashlib.sha256).digest()).decode())"
const (
	signVectorSecret = "SECtest123"
	signVectorTS     = int64(1700000000000)
	signVectorSign   = "w3RMHXzixTMdzr8OHJUmVLS4IoPJVdu+Ut1LE48MePE="
	// key/msg 写反时的值（安全审计 M-4 的失败形态）：出现它说明公式写错了
	signVectorSwapped = "g422EgUWUUtq1vqcbsWy00w6OM8jnLYKr0K4GIfygTQ="
)

func TestDingTalkSignedURL_签名向量与query形态(t *testing.T) {
	t.Run("无query_url首参用问号", func(t *testing.T) {
		got, err := dingTalkSignedURL("https://oapi.dingtalk.com/robot/send", signVectorSecret, signVectorTS)
		require.NoError(t, err)

		u, err := url.Parse(got)
		require.NoError(t, err)
		q := u.Query()
		assert.Equal(t, "1700000000000", q.Get("timestamp"))
		assert.Equal(t, signVectorSign, q.Get("sign"), "签名必须等于独立向量")
		assert.NotEqual(t, signVectorSwapped, q.Get("sign"), "key/msg 顺序写反会得到另一个值")
		assert.True(t, strings.HasPrefix(got, "https://oapi.dingtalk.com/robot/send?"),
			"无 query 时首参必须是 ?（手拼 &timestamp= 会拼出非法 URL）：%s", got)
	})

	t.Run("已有query时保留原参数", func(t *testing.T) {
		got, err := dingTalkSignedURL("https://oapi.dingtalk.com/robot/send?access_token=tk", signVectorSecret, signVectorTS)
		require.NoError(t, err)

		q, err := url.ParseQuery(strings.SplitN(got, "?", 2)[1])
		require.NoError(t, err)
		assert.Equal(t, "tk", q.Get("access_token"), "access_token 必须保留")
		assert.Equal(t, signVectorSign, q.Get("sign"))
	})

	t.Run("非法URL返错", func(t *testing.T) {
		_, err := dingTalkSignedURL("://bad", signVectorSecret, signVectorTS)
		require.Error(t, err)
	})

	t.Run("base64的加号与等号被percent编码", func(t *testing.T) {
		got, err := dingTalkSignedURL("https://oapi.dingtalk.com/robot/send", signVectorSecret, signVectorTS)
		require.NoError(t, err)
		// 未编码时钉钉把 "+" 解成空格 → 恒定 310000
		assert.Contains(t, got, "%2B")
		assert.Contains(t, got, "%3D")
	})
}

func TestDingTalkSender_加签请求带timestamp与sign(t *testing.T) {
	type captured struct{ ts, sign, token string }
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got <- captured{ts: q.Get("timestamp"), sign: q.Get("sign"), token: q.Get("access_token")}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{ //nolint:exhaustruct
		Config: `{"webhook_url":"` + srv.URL + `/robot/send?access_token=tk","sign_secret":"` + signVectorSecret + `"}`,
	})
	require.NoError(t, err)
	require.NoError(t, s.Send(context.Background(), "", "告警"))

	c := <-got
	assert.Equal(t, "tk", c.token, "原有 query 参数必须保留")
	tsMillis, err := strconv.ParseInt(c.ts, 10, 64)
	require.NoError(t, err, "timestamp 必须是毫秒整数")
	assert.InDelta(t, time.Now().UnixMilli(), tsMillis, 60_000, "timestamp 应是当前毫秒时间戳")

	// 用回执里的 timestamp 独立重算（不依赖测试运行时刻）
	mac := hmac.New(sha256.New, []byte(signVectorSecret))
	mac.Write([]byte(c.ts + "\n" + signVectorSecret))
	assert.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), c.sign)
}

func TestDingTalkSender_无sign_secret不加签(t *testing.T) {
	got := make(chan url.Values, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)
	require.NoError(t, s.Send(context.Background(), "", "x"))

	q := <-got
	assert.Empty(t, q.Get("sign"), "未配 sign_secret 不应加签")
	assert.Empty(t, q.Get("timestamp"))
}

func TestDingTalkSender_回执errcode非0返错(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"sign not match"}`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err, "HTTP 200 + errcode!=0 必须算失败（G-33 原始问题：被记成 success）")
	assert.Contains(t, err.Error(), "errcode=310000")
	assert.Contains(t, err.Error(), "sign not match")
}

func TestDingTalkSender_回执非JSON_fail_closed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err, "拿不到合法回执就无法确认送达 → fail-closed")
	assert.Contains(t, err.Error(), "回执不是合法 JSON")
}

// 安全审计 M-5：签名 URL 是限时凭据；L-1：errmsg 里的裸凭据要过脱敏。
func TestDingTalkSender_错误文本不含secret与签名URL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":1,"errmsg":"bad hook https://example.com/send?token=SECRETQUERY"}`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{ //nolint:exhaustruct
		Config: `{"webhook_url":"` + srv.URL + `?access_token=SECRETTOKEN","sign_secret":"` + signVectorSecret + `"}`,
	})
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), signVectorSecret, "sign_secret 不得进错误文本")
	assert.NotContains(t, err.Error(), "SECRETTOKEN", "webhook URL 的 query 凭据不得进错误文本")
	assert.NotContains(t, err.Error(), "SECRETQUERY", "errmsg 里的 URL 凭据必须被脱敏")
	assert.Contains(t, err.Error(), "example.com", "脱敏保留 host 便于定位")
}

func TestWebhookRespErr_best_effort(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"errcode非0", `{"errcode":1,"errmsg":"bad"}`, true},
		{"code非0", `{"code":40001,"msg":"invalid"}`, true},
		{"errcode0", `{"errcode":0,"errmsg":"ok"}`, false},
		{"code0", `{"code":0}`, false},
		{"非JSON纯文本", `ok`, false},
		{"非JSON_html", `<html></html>`, false},
		{"code是字符串", `{"code":"40001"}`, false},
		{"无errcode无code", `{"success":false}`, false},
		{"空体", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := webhookRespErr(tc.body)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "不假定第三方协议：形状不认识时维持「只看 HTTP 状态」")
			}
		})
	}
}

func TestWebhookSender_回执errcode非0返错(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	s, err := NewWebhookSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `"}`}) //nolint:exhaustruct
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "errcode=93000")
}

func TestSanitizeSnippet_按rune截断与脱敏(t *testing.T) {
	t.Run("截到200rune不切字符", func(t *testing.T) {
		got := sanitizeSnippet(strings.Repeat("告", 500))
		assert.Equal(t, 200, utf8.RuneCountInString(got), "上界按 rune 不是字节")
		assert.Equal(t, strings.Repeat("告", 200), got)
	})

	t.Run("URL凭据被脱敏", func(t *testing.T) {
		got := sanitizeSnippet("见 https://oapi.dingtalk.com/robot/send?access_token=SECRET 结束")
		assert.NotContains(t, got, "SECRET")
		assert.Contains(t, got, "oapi.dingtalk.com")
	})

	t.Run("非法UTF8被清理", func(t *testing.T) {
		got := sanitizeSnippet("bad\xff\xfe tail")
		assert.True(t, utf8.ValidString(got))
	})
}

func TestRespBody_限读4KiB(t *testing.T) {
	got := respBody(strings.NewReader(strings.Repeat("a", 10000)))
	assert.Len(t, got, 4<<10, "LimitReader 只限读取量（回执文本进错误前还要再过 sanitizeSnippet）")
}
