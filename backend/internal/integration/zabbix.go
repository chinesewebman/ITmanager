package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/httpx"
)

// ZabbixClient Zabbix 客户端（C-P7：走 httpx）。
type ZabbixClient struct {
	c        *httpx.Client
	user     string
	password string
	m        httpx.MetricsRecorder // v2.2: Reload 时复用，metrics 不断

	mu        sync.Mutex
	auth      string
	expiresAt time.Time // v1.1: auth 过期时间，过期前 60s 自动重登
}

// zabbixAuthTTL Zabbix session 有效期保守估计为 30 分钟（Zabbix 默认
// config 里是 1h30m，但加 session 闲置超时可能更短）。60s 提前重登。
const zabbixAuthTTL = 30 * time.Minute

// NewZabbixClient 创建 Zabbix 客户端。
func NewZabbixClient(cfg *config.ZabbixConfig, m httpx.MetricsRecorder) *ZabbixClient {
	hcfg := httpx.DefaultConfig(cfg.URL)
	hcfg.Timeout = 30 * time.Second
	return &ZabbixClient{
		c:        httpx.New(hcfg, "zabbix", m),
		user:     cfg.User,
		password: cfg.Password,
		m:        m,
	}
}

// Reload 运行时热更新配置（v2.2: UI 改 Zabbix URL/账号后免重启）。
// 清空缓存的 auth 让下次 GetTriggers 重新 Login；httpx 客户端 URL/timeout 重建，metrics 复用。
func (z *ZabbixClient) Reload(cfg *config.ZabbixConfig) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.user = cfg.User
	z.password = cfg.Password
	z.auth = ""
	z.expiresAt = time.Time{}
	hcfg := httpx.DefaultConfig(cfg.URL)
	hcfg.Timeout = 30 * time.Second
	z.c = httpx.New(hcfg, "zabbix", z.m)
}

// ZabbixAPIRequest Zabbix API 请求
type ZabbixAPIRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	Auth    string      `json:"auth,omitempty"`
	ID      int         `json:"id"`
}

// ZabbixAPIResponse Zabbix API 响应
type ZabbixAPIResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Error   *ZabbixError    `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	ID      int             `json:"id"`
}

// ZabbixError Zabbix 错误
type ZabbixError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// Login 登录 Zabbix（C-P7：ctx 透传）。
func (z *ZabbixClient) Login(ctx context.Context) error {
	req := ZabbixAPIRequest{
		JSONRPC: "2.0",
		Method:  "user.login",
		Params: map[string]string{
			"username": z.user,
			"password": z.password,
		},
		ID: 1,
	}
	resp, err := z.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("Zabbix 登录失败: %w", err)
	}
	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	z.mu.Lock()
	z.auth = result.Result
	z.expiresAt = time.Now().Add(zabbixAuthTTL)
	z.mu.Unlock()
	return nil
}

// GetTriggers 获取告警列表（C-P7：ctx 透传）。
func (z *ZabbixClient) GetTriggers(ctx context.Context) ([]Trigger, error) {
	z.mu.Lock()
	needLogin := z.auth == "" || time.Now().After(z.expiresAt.Add(-60*time.Second))
	z.mu.Unlock()
	if needLogin {
		if err := z.Login(ctx); err != nil {
			return nil, err
		}
	}

	req := ZabbixAPIRequest{
		JSONRPC: "2.0",
		Method:  "trigger.get",
		Params: map[string]interface{}{
			"only_true":     true,
			"skipDependent": true,
			"filter":        map[string]interface{}{"value": 1},
			"selectHosts":   "extend",
			"selectItems":   "extend",
			"sortfield":     "lastchange",
			"sortorder":     "DESC",
			"limit":         100,
		},
		Auth: z.auth,
		ID:   2,
	}
	resp, err := z.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("获取告警失败: %w", err)
	}
	var result struct {
		Result []Trigger `json:"result"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return result.Result, nil
}

// doRequest 走 httpx：自动 retry/熔断/metrics。
// v1.1: 检测 Zabbix "Session terminated, re-login" (code -10002) 错误，
// 触发自动重登一次后重试。
func (z *ZabbixClient) doRequest(ctx context.Context, req ZabbixAPIRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	respBody, _, err := z.c.DoWithHeaders(ctx, "POST", "/api_jsonrpc.php", bytes.NewReader(body),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return nil, err
	}
	// 业务错误（HTTP 200 但 result.error）
	var apiResp ZabbixAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Error != nil {
		// v1.1: session 过期 (-10002) → 重登一次再重试
		if apiResp.Error.Code == -10002 {
			z.mu.Lock()
			z.auth = ""
			z.expiresAt = time.Time{}
			z.mu.Unlock()
			if err := z.Login(ctx); err != nil {
				return nil, fmt.Errorf("Zabbix 自动重登失败: %w", err)
			}
			// 用新 auth 重试一次
			req.Auth = z.auth
			body2, _ := json.Marshal(req)
			respBody2, _, err := z.c.DoWithHeaders(ctx, "POST", "/api_jsonrpc.php", bytes.NewReader(body2),
				map[string]string{"Content-Type": "application/json"})
			if err != nil {
				return nil, err
			}
			var apiResp2 ZabbixAPIResponse
			if err := json.Unmarshal(respBody2, &apiResp2); err == nil && apiResp2.Error != nil {
				return nil, fmt.Errorf("Zabbix API 错 %d (重登后): %s", apiResp2.Error.Code, apiResp2.Error.Message)
			}
			return respBody2, nil
		}
		return nil, fmt.Errorf("Zabbix API 错 %d: %s", apiResp.Error.Code, apiResp.Error.Message)
	}
	return respBody, nil
}

// Trigger / Host / Item / Event 类型保持不变
type Trigger struct {
	TriggerID   string `json:"triggerid"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Value       string `json:"value"`
	Priority    int    `json:"priority"`
	LastChange  string `json:"lastchange"`
	Hosts       []Host `json:"hosts"`
	Items       []Item `json:"items"`
	LastEvent   Event  `json:"last_event"`
}
type Host struct {
	HostID string `json:"hostid"`
	Host   string `json:"host"`
	Name   string `json:"name"`
}
type Item struct {
	ItemID    string `json:"itemid"`
	Name      string `json:"name"`
	Key       string `json:"key_"`
	LastValue string `json:"lastvalue"`
}
type Event struct {
	EventID   string `json:"eventid"`
	Clock     string `json:"clock"`
	Value     string `json:"value"`
	AckStatus string `json:"acknowledged"`
}

// ConvertToAlert 转换为告警格式
func (t *Trigger) ConvertToAlert() *ZabbixAlert {
	alert := &ZabbixAlert{
		HostName:    t.Hosts[0].Host,
		HostIP:      "",
		TriggerName: t.Description,
		Problem:     t.Description,
		Status:      "problem",
		Severity:    t.Priority,
	}
	switch t.Priority {
	case 0:
		alert.SeverityName = "未分类"
	case 1:
		alert.SeverityName = "信息"
	case 2:
		alert.SeverityName = "警告"
	case 3:
		alert.SeverityName = "一般严重"
	case 4:
		alert.SeverityName = "严重"
	case 5:
		alert.SeverityName = "灾难"
	default:
		alert.SeverityName = "未知"
	}
	return alert
}

// ZabbixAlert Zabbix 告警
type ZabbixAlert struct {
	HostName     string
	HostIP       string
	TriggerName  string
	TriggerID    string
	Problem      string
	Severity     int
	SeverityName string
	Status       string
	Source       string
}
