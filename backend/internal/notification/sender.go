// Package notification 实现多渠道通知发送 (dingtalk/email/webhook) + 异步 worker。
//
// v1.4 落地: 把 v1.1 trigger 落的 pending notification_logs 真发出去。
//
// 架构:
//
//	alert.ack/resolve
//	  └─ writeNotificationTrigger 落 pending log
//	       └─ Worker.tick() 5s 拉 pending
//	           └─ Sender.Send() 调对应渠道 (dingtalk/email/webhook)
//	               └─ 更新 log.status = success/failed
package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/redact"
)

// urlErrCause 剥掉 *url.Error 的外壳。
//
// G-28：http.Client.Do 与 http.NewRequestWithContext 的失败都是 *url.Error，其 Error()
// 含**完整 URL**（钉钉的 access_token 在 query、飞书/Slack 的在 path、集成 URL 可能在
// userinfo），原样 return 会把凭据带进应用日志与 notification_logs.error_msg。
// 这里只取底层 cause（`dial tcp …`、`context deadline exceeded`），URL 由调用方用
// redact.URL 缩成 scheme://host。
// 重定向链可能嵌套，故递归剥；上限 4 层，内层为 nil 或超限一律返回固定文案 ——
// 宁可丢诊断信息，也不回传带 URL 的原串。
//
// 循环条件写在**解包前**：`for i := 0; i < 4` 只解 4 次却把第 5 层的判定留给兜底，
// 等于「4 层嵌套也丢 cause」（审计 MED-2，实测 depth=4 → 未知错误）。现在深度 4 能
// 拿到 cause，深度 ≥5 仍走安全兜底，且循环仍是有界 4 次解包（防御自引用链）。
func urlErrCause(err error) error {
	for i := 0; ; i++ {
		var ue *url.Error
		if !errors.As(err, &ue) {
			return err
		}
		if ue.Err == nil || i >= 4 {
			return errors.New("未知错误")
		}
		err = ue.Err
	}
}

// Sender 单一渠道发送器接口
type Sender interface {
	// Send 发送一条通知, 返回 error 即视为失败
	Send(ctx context.Context, recipient, content string) error
	// Type 返回渠道类型 (dingtalk/email/webhook), 用于工厂选择
	Type() string
}

// NewSender 按 channel.Type 工厂选 Sender
func NewSender(ch *models.NotificationChannel) (Sender, error) {
	if ch == nil {
		return nil, errors.New("channel is nil")
	}
	switch ch.Type {
	case "dingtalk":
		return NewDingTalkSender(ch)
	case "email":
		return NewEmailSender(ch)
	case "webhook":
		return NewWebhookSender(ch)
	default:
		// 不回显 ch.Type：该错误经 service 脱敏后回显到 400 body，而 redact.Text 只挡
		// URL / 键值形态，裸 token、JWT、percent 编码、无 scheme URL、多行文本都能穿过
		// （安全审计 M-1）。支持的类型是静态信息，写死即可。
		return nil, errors.New("unsupported channel type (支持: email/dingtalk/webhook)")
	}
}

// SenderRegistry 注册自定义 Sender (用于测试 mock)
var customSenders = map[string]Sender{}

// RegisterSender 注册一个渠道类型的 Sender (覆盖默认)
func RegisterSender(channelType string, s Sender) { customSenders[channelType] = s }

// Resolver 工厂选项: 优先返回注册的 mock, 否则默认实现
func Resolver(ch *models.NotificationChannel) (Sender, error) {
	if s, ok := customSenders[ch.Type]; ok {
		return s, nil
	}
	return NewSender(ch)
}

// channelConfig 通用配置 (从 NotificationChannel.Config JSON 解析)
type channelConfig struct {
	// webhook
	URL    string `json:"url"`
	Method string `json:"method,omitempty"` // 默认 POST
	Secret string `json:"secret,omitempty"`

	// dingtalk
	WebhookURL string `json:"webhook_url"`
	SignSecret string `json:"sign_secret,omitempty"`

	// email
	SMTPHost     string   `json:"smtp_host"`
	SMTPPort     int      `json:"smtp_port"`
	SMTPUser     string   `json:"smtp_user"`
	SMTPPassword string   `json:"smtp_password"`
	FromAddress  string   `json:"from"`
	FromName     string   `json:"from_name,omitempty"`
	ToAddresses  []string `json:"to"`
	UseTLS       bool     `json:"use_tls,omitempty"`
}

func parseConfig(s string) (channelConfig, error) {
	var c channelConfig
	if s == "" {
		return c, nil
	}
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, fmt.Errorf("invalid channel config json: %w", err)
	}
	return c, nil
}

// maxRespSnippet 第三方回执进错误文本前的 rune 上界。LimitReader 只限**读取量**，
// 不限嵌入错误文本的长度（安全审计 M-3），故读取后再截断。
const maxRespSnippet = 200

// sanitizeSnippet 把不可信文本（第三方响应体 / errmsg）规整成可安全嵌入错误文本的片段：
// 脱敏 → 清理非法 UTF-8（第三方可能回传非 UTF-8 字节）→ 按 rune 截断（按字节截断会
// 切断多字节字符，口径同 worker.markFailed）。
func sanitizeSnippet(s string) string {
	s = redact.Text(strings.TrimSpace(s))
	s = strings.ToValidUTF8(s, "�")
	if utf8.RuneCountInString(s) > maxRespSnippet {
		s = string([]rune(s)[:maxRespSnippet])
	}
	return s
}

// respBody 读响应体（最多 4KiB）——上游响应体一律视为不可信（G-31 残余）。
func respBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4<<10))
	return string(b)
}

// dingTalkSignedURL 返回带 timestamp/sign 的钉钉 webhook URL。
//
// 消歧义写法（安全审计 M-4）：key = sign_secret，msg = timestamp + "\n" + sign_secret，
// sign = base64(HMAC-SHA256(key, msg))。写成 HMAC(key=msg, msg=secret) 这种顺序写反的
// 记法会得到恒定 310000（钉钉只报「签名校验失败」，不告诉你是顺序问题）。
//
// 追加 query 用 url.Values.Encode()（与钉钉的 quote_plus 等价），不手拼 "&timestamp="
// ——webhook_url 没有 query 时首个参数必须是 "?"。
//
// 返回值是**限时凭据**：调用方只能放局部变量，不得回写 cfg、不得进错误文本/日志。
func dingTalkSignedURL(webhookURL, signSecret string, tsMillis int64) (string, error) {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return "", err
	}
	ts := strconv.FormatInt(tsMillis, 10)
	mac := hmac.New(sha256.New, []byte(signSecret))
	mac.Write([]byte(ts + "\n" + signSecret))
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// dingRespErr 校验钉钉回执：HTTP 2xx 也可能 errcode != 0（310000 加签错误、关键词不匹配、
// 频率限制）——只看状态码会把「根本没发出去」记成 success，正是 G-33 的原始问题。
// 回执不可解析一律按失败处理（fail-closed）：钉钉恒返 JSON，拿到非 JSON 说明中间有代理/网关，
// 此时无法确认送达。
func dingRespErr(raw string) error {
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return fmt.Errorf("dingtalk 回执不是合法 JSON: %s", sanitizeSnippet(raw))
	}
	if r.ErrCode != 0 {
		return fmt.Errorf("dingtalk errcode=%d: %s", r.ErrCode, sanitizeSnippet(r.ErrMsg))
	}
	return nil
}

// webhookRespErr 通用 webhook 的 **best-effort** 回执校验：仅当响应体是合法 JSON 且含
// 数值型 errcode/code 且非 0 时视为失败；其余形状维持「只看 HTTP 状态」——不假定第三方协议。
func webhookRespErr(raw string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	for _, k := range []string{"errcode", "code"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			continue // 非数值型：不判定
		}
		if n != 0 {
			return fmt.Errorf("webhook 回执 %s=%d: %s", k, n, sanitizeSnippet(raw))
		}
		return nil
	}
	return nil
}

// ==================== DingTalk Sender ====================

type DingTalkSender struct {
	cfg    channelConfig
	client *http.Client
}

func NewDingTalkSender(ch *models.NotificationChannel) (*DingTalkSender, error) {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return nil, err
	}
	if cfg.WebhookURL == "" {
		return nil, errors.New("dingtalk: webhook_url is required")
	}
	return &DingTalkSender{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (d *DingTalkSender) Type() string { return "dingtalk" }

// Send 发钉钉 markdown 消息
func (d *DingTalkSender) Send(ctx context.Context, _, content string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": "网络监控告警",
			"text":  content,
		},
	}
	body, _ := json.Marshal(payload)

	// 加签：target 是限时凭据，只放局部变量 —— 不回写 cfg、不进错误文本/日志/recipient。
	target := d.cfg.WebhookURL
	if d.cfg.SignSecret != "" {
		signed, err := dingTalkSignedURL(target, d.cfg.SignSecret, time.Now().UnixMilli())
		if err != nil {
			return fmt.Errorf("dingtalk: 无效的 webhook_url: %w", urlErrCause(err))
		}
		target = signed
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		// 不包原 err：url.Parse 的失败文本含完整 URL（access_token 就在 query 里）
		return fmt.Errorf("dingtalk: 无效的 webhook_url: %w", urlErrCause(err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		// G-28：只留 scheme://host + 底层 cause，不带 URL 的 path/query
		return fmt.Errorf("dingtalk: POST %s: %w", redact.URL(d.cfg.WebhookURL), urlErrCause(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk http %d", resp.StatusCode)
	}
	return dingRespErr(respBody(resp.Body))
}

// ==================== Email Sender ====================

type EmailSender struct {
	cfg channelConfig
}

func NewEmailSender(ch *models.NotificationChannel) (*EmailSender, error) {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return nil, err
	}
	if cfg.SMTPHost == "" || cfg.SMTPPort == 0 || cfg.SMTPUser == "" || cfg.FromAddress == "" {
		return nil, errors.New("email: smtp_host/port/user/from are required")
	}
	if len(cfg.ToAddresses) == 0 {
		return nil, errors.New("email: at least one to address required")
	}
	return &EmailSender{cfg: cfg}, nil
}

func (e *EmailSender) Type() string { return "email" }

// Send 发 SMTP 邮件
func (e *EmailSender) Send(ctx context.Context, _, content string) error {
	addr := net.JoinHostPort(e.cfg.SMTPHost, fmt.Sprintf("%d", e.cfg.SMTPPort))
	from := e.cfg.FromAddress
	if e.cfg.FromName != "" {
		from = e.cfg.FromName + " <" + e.cfg.FromAddress + ">"
	}
	msg := []byte("From: " + from + "\r\n" +
		"To: " + strings.Join(e.cfg.ToAddresses, ", ") + "\r\n" +
		"Subject: Network Monitor Alert\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		content)

	var auth smtp.Auth
	if e.cfg.SMTPPassword != "" {
		auth = smtp.PlainAuth("", e.cfg.SMTPUser, e.cfg.SMTPPassword, e.cfg.SMTPHost)
	}

	// ctx 取消感知: 用 goroutine + ctx done
	done := make(chan error, 1)
	go func() {
		if e.cfg.UseTLS {
			done <- sendMailTLS(addr, e.cfg.SMTPHost, auth, e.cfg.FromAddress, e.cfg.ToAddresses, msg)
		} else {
			done <- smtp.SendMail(addr, auth, e.cfg.FromAddress, e.cfg.ToAddresses, msg)
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendMailTLS 走 TLS 的 SMTP (端口 465 等)
func sendMailTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ==================== Webhook Sender ====================

type WebhookSender struct {
	cfg    channelConfig
	client *http.Client
}

func NewWebhookSender(ch *models.NotificationChannel) (*WebhookSender, error) {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("webhook: url is required")
	}
	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}
	return &WebhookSender{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (w *WebhookSender) Type() string { return "webhook" }

// Send 发自定义 webhook, body 是 JSON { content: "..." }
func (w *WebhookSender) Send(ctx context.Context, _, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		// 不包原 err：url.Parse 的失败文本含完整 URL（飞书/Slack 的 token 在 path 里）
		return fmt.Errorf("webhook: 无效的 url: %w", urlErrCause(err))
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.Secret != "" {
		req.Header.Set("X-Webhook-Secret", w.cfg.Secret)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		// G-28：只留 scheme://host + 底层 cause，不带 URL 的 path/query
		return fmt.Errorf("webhook: POST %s: %w", redact.URL(w.cfg.URL), urlErrCause(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook http %d", resp.StatusCode)
	}
	return webhookRespErr(respBody(resp.Body))
}
