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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"network-monitor-platform/internal/models"
)

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
		return nil, fmt.Errorf("unsupported channel type: %s", ch.Type)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk http %d", resp.StatusCode)
	}
	return nil
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
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.Secret != "" {
		req.Header.Set("X-Webhook-Secret", w.cfg.Secret)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook http %d", resp.StatusCode)
	}
	return nil
}
