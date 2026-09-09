package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ==================== URL ====================

func TestURL_只留scheme和host(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"钉钉 query token", "https://oapi.dingtalk.com/robot/send?access_token=SECRET", "https://oapi.dingtalk.com"},
		{"Slack path token", "https://hooks.slack.com/services/T000/B000/SECRETPATH", "https://hooks.slack.com"},
		{"飞书 hook token", "https://open.feishu.cn/open-apis/bot/v2/hook/abcdef-1234", "https://open.feishu.cn"},
		{"userinfo", "https://user:pw@example.com/api?token=x", "https://example.com"},
		{"带端口", "http://127.0.0.1:8080/hook", "http://127.0.0.1:8080"},
		{"IPv6 带端口", "http://[::1]:8080/hook", "http://[::1]:8080"},
		{"空串", "", invalidURL},
		{"解析失败", "http://[::1", invalidURL},
		{"无 host", "mailto:someone@example.com", invalidURL},
		// 审计 M-2：配置漏写 scheme 的 URL，Go 会把原串塞进 *url.Error → 也要塌缩。
		{"scheme-relative", "//example.com/path", "//example.com"},
		{"scheme-relative 带 token 路径", "//127.0.0.1:1/hooks/T/B/SECRET", "//127.0.0.1:1"},
		// 审计 M-3：userinfo 没有密码时 net/url 把它并进 Host，原样返回等于回显凭据。
		{"userinfo 无密码", "https://user:", invalidURL},
		{"host 尾冒号", "http://example.com:/api", invalidURL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, URL(c.in))
		})
	}
}

// ==================== Text ====================

func TestText_替换凭据值(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"URL path token（飞书/Slack 形态）",
			`Post "https://hooks.slack.com/services/T000/B000/SECRETPATH": dial tcp: refused`,
			`Post "https://hooks.slack.com": dial tcp: refused`,
		},
		{
			"URL query token（钉钉形态）",
			`Post "https://oapi.dingtalk.com/robot/send?access_token=SECRET": EOF`,
			`Post "https://oapi.dingtalk.com": EOF`,
		},
		{
			"URL userinfo",
			"zabbix: http://user:pw@example.com/api_jsonrpc.php",
			"zabbix: http://example.com",
		},
		{
			"Authorization 头",
			"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.PAYLOAD",
			"Authorization: Bearer ***",
		},
		{
			"Authorization Basic 头",
			"authorization: basic dXNlcjpwYXNz",
			"authorization: basic ***",
		},
		{
			"query 键值（保留非敏感参数）",
			"channel config: access_token=SECRET&page=2",
			"channel config: access_token=***&page=2",
		},
		{
			"JSON 形态",
			`{"smtp_password":"SECRET","host":"x"}`,
			`{"smtp_password":"***","host":"x"}`,
		},
		{
			"冒号形态",
			"api_key: SECRET",
			"api_key: ***",
		},
		{
			"大小写混合",
			"Access_Token=SECRET",
			"Access_Token=***",
		},
		{
			"值含 $ 不破坏替换",
			"token=ab$1cd",
			"token=***",
		},
		{
			"x-webhook-secret",
			"X-Webhook-Secret: SECRET",
			"X-Webhook-Secret: ***",
		},
		{
			"带前缀/后缀的组合名",
			"secret_key=SECRET&csrf_token=T2&access_key_id=AKIA",
			"secret_key=***&csrf_token=***&access_key_id=***",
		},
		{
			"scheme-relative URL（审计 M-2）",
			`Post "//127.0.0.1:1/hooks/T/B/SECRETPATH": dial tcp: refused`,
			`Post "//127.0.0.1:1": dial tcp: refused`,
		},
		{
			// M-3：旧值类把 ' 当终点 → `AB` 之后全部留在文本里。现在整串一起塌缩。
			"单引号不再是值终点（审计 M-3）",
			`Post "http://127.0.0.1:1/api?auth=AB'CDEF": dial tcp: refused`,
			`Post "http://127.0.0.1:1": dial tcp: refused`,
		},
		{
			// 审计 P1：旧值类把 } ] < > 当终点 → JSON 回显只剩 `***}c"}`。
			"值含右花括号（审计 P1）",
			`{"password":"ab}c"}`,
			`{"password":"***"}`,
		},
		{
			// 审计 P1 最坏形态：值**以**分隔符开头时旧版整条不匹配 = 完全不脱敏。
			"值以右花括号开头（审计 P1）",
			"password=}SUPERSECRET",
			"password=***",
		},
		{
			"值被尖括号包裹（审计 P1）",
			"token=<SECRET>",
			"token=***",
		},
		{
			"Bearer 值含逗号（审计 P1）",
			"Authorization: Bearer abc,def",
			"Authorization: Bearer ***",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_不误伤正常文本 — R-1 的守门用例：脱敏必须只动凭据值。
func TestText_不误伤正常文本(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"SQL 约束名", `pq: duplicate key value violates unique constraint "users_username_key"`},
		{"非敏感 query", "GET /api/assets?page=2&limit=10"},
		{"design= 不是 sign=", "design=flat"},
		{"无 = 无 : 的普通错误", "context deadline exceeded"},
		{"空串", ""},
		{"monkey= 不是 key=", "monkey=banana"},
		{"primary key= 不脱敏", "pq: duplicate key value violates unique constraint (primary key=id)"},
		// 双斜杠路径不是 URL：authority 必须是「带点域名 / localhost / [IPv6]」。
		{"双斜杠路径", "open //data/memory.db: no such file"},
		{"注释里的双斜杠", "// 这里记录重试次数"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.in, Text(c.in), "非敏感文本必须原样返回")
		})
	}
}
