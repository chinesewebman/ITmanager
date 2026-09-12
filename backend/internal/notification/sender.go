// Package notification 实现多渠道通知发送 (dingtalk/email/wechat/webhook) + 异步 worker。
//
// v1.4 落地: 把 v1.1 trigger 落的 pending notification_logs 真发出去。
//
// 架构:
//
//	alert.ack/resolve
//	  └─ writeNotificationTrigger 落 pending log
//	       └─ Worker.tick() 5s 拉 pending
//	           └─ Sender.Send() 调对应渠道 (dingtalk/email/wechat/webhook)
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
	"sync"
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
	// Type 返回渠道类型 (dingtalk/email/wechat/webhook), 用于工厂选择
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
	case "wechat":
		return NewWeChatSender(ch)
	case "webhook":
		return NewWebhookSender(ch)
	default:
		// 不回显 ch.Type：该错误经 service 脱敏后回显到 400 body，而 redact.Text 只挡
		// URL / 键值形态，裸 token、JWT、percent 编码、无 scheme URL、多行文本都能穿过
		// （安全审计 M-1）。支持的类型是静态信息，写死即可。
		return nil, errors.New("unsupported channel type (支持: email/dingtalk/wechat/webhook)")
	}
}

// SenderRegistry 注册自定义 Sender (用于测试 mock)
//
// G-38：这两处访问必须走锁。map 的并发读写不是「数据不一致」而是 Go runtime 直接
// `fatal error: concurrent map read and map write`（**不可 recover**，整个进程挂掉）。
// 当前生产无写入者（RegisterSender 只在测试里调用），但它是「将来会被当插件点」的形状。
// 保护方式与仓库其余处一致（sync.RWMutex，见 middleware/rate_limit.go、metrics、eventbus）。
var (
	customSendersMu sync.RWMutex
	customSenders   = map[string]Sender{}
)

// RegisterSender 注册一个渠道类型的 Sender (覆盖默认)
func RegisterSender(channelType string, s Sender) {
	customSendersMu.Lock()
	defer customSendersMu.Unlock()
	customSenders[channelType] = s
}

// lookupCustomSender 读注册表；Resolver 之外不要直接访问 customSenders。
func lookupCustomSender(channelType string) (Sender, bool) {
	customSendersMu.RLock()
	defer customSendersMu.RUnlock()
	s, ok := customSenders[channelType]
	return s, ok
}

// Resolver 工厂选项: 优先返回注册的 mock, 否则默认实现
func Resolver(ch *models.NotificationChannel) (Sender, error) {
	if s, ok := lookupCustomSender(ch.Type); ok {
		return s, nil
	}
	return NewSender(ch)
}

// channelConfig 通用配置 (从 NotificationChannel.Config JSON 解析)
type channelConfig struct {
	// webhook / wechat（wechat 只用 url，忽略 secret —— 企微群机器人无需签名头）
	URL    string `json:"url"`
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

// stripControlChars 剥掉控制字符（含 NUL / CR / LF / DEL）。
//
// 口径同 handlers.sanitizeAuditUsername（`r < 0x20 || r == 0x7f`，drop 不替换）：
// NUL 让 PostgreSQL 直接拒收（22021 → 该行永远停在 pending 被无限重发），
// CR/LF 可把行式消费的日志与 error_msg 伪造成多条记录（M2 安全审计 MEDIUM-1：
// 本包新引入「第三方响应体进错误文本」这条通道，配套净化原先只做脱敏/UTF-8/截断）。
//
// M29：实现搬到 redact.StripControl（同一份判据在 redact / 本包 / audit 三处各写一遍
// 会漂移，T-52）。此处保留薄封装以免动调用点，语义不变。
// **调用顺序**：必须在本包的 redact.Text **之前**调用，理由见 redact.StripControl 注释。
func stripControlChars(s string) string {
	return redact.StripControl(s)
}

// sanitizeSnippet 把不可信文本（第三方响应体 / errmsg）规整成可安全嵌入错误文本的片段：
// 去首尾空白 → 剥控制字符 → 脱敏 → 清理非法 UTF-8（第三方可能回传非 UTF-8 字节）→
// 按 rune 截断（按字节截断会切断多字节字符，口径同 worker.markFailed）。
func sanitizeSnippet(s string) string {
	s = stripControlChars(strings.TrimSpace(s))
	s = redact.Text(s)
	s = strings.ToValidUTF8(s, "�")
	if utf8.RuneCountInString(s) > maxRespSnippet {
		s = string([]rune(s)[:maxRespSnippet])
	}
	return s
}

// maxRespBytes 回执读取量上界。只挡无限流：4KiB 会切断 JSON，把「真送达」判成
// 「回执不是合法 JSON」（正确性审计 LOW-2）；嵌入错误文本的长度由 sanitizeSnippet
// 的 200 rune 独立收敛，两者职责不同。
const maxRespBytes = 64 << 10

// credentialParams 是 URL query 里算凭据的参数名（大小写不敏感）。
// 与 redact 规则 3 的名字集合同口径，另加企微的 `key`。
var credentialParams = map[string]bool{
	"key": true, "access_token": true, "access_key": true, "token": true,
	"secret": true, "sign": true, "password": true, "passwd": true, "pwd": true, "apikey": true,
}

// credentialValues 取出 rawURL 里凭据参数的值，供 scrubSecrets 用。
//
// 直接切 RawQuery 而不用 u.Query()：后者只给**解码后**的值，而 `key=a%20b` 的原始
// 形态是 `a%20b`、解码形态是 `a b`、再编码形态又是 `a+b` —— 上游回显哪一种都可能，
// 两种都要收。同时 u.Query() 在畸形参数上会静默丢整段（同 M2 审计 LOW-4 的坑）。
func credentialValues(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(u.RawQuery, "&") {
		name, rawVal, _ := strings.Cut(seg, "=")
		if !credentialParams[strings.ToLower(name)] || rawVal == "" {
			continue
		}
		out = append(out, rawVal)
		if dec, err := url.QueryUnescape(rawVal); err == nil && dec != rawVal && dec != "" {
			out = append(out, dec)
		}
	}
	return out
}

// scrubSecrets 把文本里出现的凭据值替换成 ***。
//
// 为什么需要它（安全审计 M-1）：redact.Text 只覆盖「URL 形状」与固定参数名，
// 而企微凭据的参数名恰是 `key` —— 规则 3 刻意不收裸 `key=`（会误伤 "primary key="）。
// 于是网关/WAF/反代只回显 path+query（`/cgi-bin/webhook/send?key=…`，无 scheme://host）
// 或 `invalid key=…` 这类文本时，key 会经 errmsg 进 error_msg 与 stderr 日志。
// 值级替换比放宽全局规则窄得多：只动本渠道自己 URL 里的凭据值，零回归。
func scrubSecrets(s string, secrets []string) string {
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, "***")
	}
	return s
}

// respBody 读响应体（最多 64KiB）——上游响应体一律视为不可信（G-31 残余）。
func respBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxRespBytes))
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
	// 显式 ParseQuery 并检查 error：u.Query() 会吞掉畸形 query（`?access_token=%zz`）
	// 再被 Encode() 重写 → 凭据参数**无声消失**，钉钉只报 310000，排查会误判成签名问题
	// （正确性审计 LOW-4）。畸形一律 fail-closed。
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", err
	}
	q.Set("timestamp", ts)
	q.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// errcodeRespErr 校验「{"errcode":N,"errmsg":"…"}」形态的回执（钉钉与企业微信同口径）。
//
// HTTP 2xx 也可能 errcode != 0（钉钉 310000 加签错误/关键词不匹配/频率限制；企微 93000
// 等）——只看状态码会把「根本没发出去」记成 success，正是 G-33 的原始问题。
// 回执不可解析一律按失败处理（fail-closed）：两家恒返 JSON，拿到非 JSON 说明中间有代理/
// 网关，此时无法确认送达。
//
// errcode **必须存在且为整数**（用 *int 判空）：`{}`、`null`、`{"errmsg":"ok"}` 这类合法
// JSON 旧版会因零值 0 被判成功——与「无法确认送达即失败」自相矛盾（安全审计 MEDIUM-1、
// 正确性审计 MEDIUM-1，两路独立命中）。两家恒返 errcode，此约束对真实端点零影响。
//
// label 只用于错误文案前缀（"dingtalk"/"wechat"），是静态字面量、不含配置值。
func errcodeRespErr(raw, label string) error {
	var r struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		// errcode 类型不符（字符串/浮点）时 JSON 本身合法，报「不是合法 JSON」会把人
		// 带偏到「网关改写了响应」（正确性审计 LOW-3）
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return fmt.Errorf("%s 回执 errcode 类型不是整数: %s", label, sanitizeSnippet(raw))
		}
		return fmt.Errorf("%s 回执不是合法 JSON: %s", label, sanitizeSnippet(raw))
	}
	if r.ErrCode == nil {
		return fmt.Errorf("%s 回执缺 errcode: %s", label, sanitizeSnippet(raw))
	}
	if *r.ErrCode != 0 {
		return fmt.Errorf("%s errcode=%d: %s", label, *r.ErrCode, sanitizeSnippet(r.ErrMsg))
	}
	return nil
}

// webhookRespErr 通用 webhook 的 **best-effort** 回执校验：仅当响应体是合法 JSON 且含
// 数值型 errcode/code 且非 0 时视为失败；其余形状维持「只看 HTTP 状态」——不假定第三方协议。
//
// 两个键都看完再决定：先命中就 return 会让 `{"errcode":0,"code":1}` 判成功、而
// `{"code":0,"errcode":1}` 判失败——同一语义两种结果（正确性审计 LOW-1）。
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
	return errcodeRespErr(respBody(resp.Body), "dingtalk")
}

// ==================== WeChat Work Sender ====================

// WeChatSender 企业微信群机器人。
//
// 与 WebhookSender 的差别只在 **body 形状**：企微要 `{"msgtype":"text","text":{"content":…}}`，
// 而通用 webhook 发 `{"content":…}`（G-36 的原始缺陷：键名对齐了也发不出去）。
// 回执同为 errcode 形态，复用 errcodeRespErr（钉钉/企微同口径）。
// 配置键是 `url`（webhook key 在 query 里），无签名头——企微群机器人不需要 secret。
type WeChatSender struct {
	cfg    channelConfig
	client *http.Client
	// secrets 是 cfg.URL 里的凭据值，用于抹掉上游回显（安全审计 M-1）
	secrets []string
}

func NewWeChatSender(ch *models.NotificationChannel) (*WeChatSender, error) {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("wechat: url is required")
	}
	return &WeChatSender{
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
		secrets: credentialValues(cfg.URL),
	}, nil
}

func (w *WeChatSender) Type() string { return "wechat" }

// Send 发企业微信 text 消息
func (w *WeChatSender) Send(ctx context.Context, _, content string) error {
	body, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": content},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		// 不包原 err：url.Parse 的失败文本含完整 URL（webhook key 就在 query 里）
		return fmt.Errorf("wechat: 无效的 url: %w", urlErrCause(err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		// G-28：只留 scheme://host + 底层 cause，不带 URL 的 path/query
		return fmt.Errorf("wechat: POST %s: %w", redact.URL(w.cfg.URL), urlErrCause(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("wechat http %d", resp.StatusCode)
	}
	// 先抹凭据再校验：redact.Text 认不出裸 `key=`（见 scrubSecrets 注释）
	return errcodeRespErr(scrubSecrets(respBody(resp.Body), w.secrets), "wechat")
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
	return &WebhookSender{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (w *WebhookSender) Type() string { return "webhook" }

// Send 发自定义 webhook, body 是 JSON { content: "..." }。
// 只支持 POST：`channelConfig` 曾有一个 `method` 字段（`json:"method,omitempty"`），
// 但 `Send` 一直硬编码 POST、构造器算出的局部变量也被丢弃 → 配了不生效的死键
// （契约样本/前端表单/OpenAPI 都没有它）。已删除，避免"配了以为生效"的陷阱；
// 将来要支持别的动词，需同时加进跨语言契约样本与前端表单。

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


// (M37-A 真 PG 测试直接走 WorkerConfig.Resolver, 不再用 RegisterSender, 所以 GetCustomSendersForTest 已删)
