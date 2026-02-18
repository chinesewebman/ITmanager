package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"network-monitor-platform/internal/config"
)

// ZabbixClient Zabbix 客户端
type ZabbixClient struct {
	baseURL  string
	user     string
	password string
	client   *http.Client
	auth     string
}

// NewZabbixClient 创建 Zabbix 客户端
func NewZabbixClient(cfg *config.ZabbixConfig) *ZabbixClient {
	return &ZabbixClient{
		baseURL:  cfg.URL,
		user:     cfg.User,
		password: cfg.Password,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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
	Error   *ZabbixError   `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	ID      int             `json:"id"`
}

// ZabbixError Zabbix 错误
type ZabbixError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// Login 登录 Zabbix
func (z *ZabbixClient) Login() error {
	req := ZabbixAPIRequest{
		JSONRPC: "2.0",
		Method:  "user.login",
		Params: map[string]string{
			"username": z.user,
			"password": z.password,
		},
		ID: 1,
	}

	resp, err := z.doRequest(req)
	if err != nil {
		return fmt.Errorf("Zabbix 登录失败: %w", err)
	}

	var result struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	z.auth = result.Result
	return nil
}

// GetTriggers 获取告警列表
func (z *ZabbixClient) GetTriggers() ([]Trigger, error) {
	if z.auth == "" {
		if err := z.Login(); err != nil {
			return nil, err
		}
	}

	req := ZabbixAPIRequest{
		JSONRPC: "2.0",
		Method:  "trigger.get",
		Params: map[string]interface{}{
			"only_true":     true,
			"skipDependent": true,
			"filter": map[string]interface{}{
				"value": 1, // PROBLEM
			},
			"selectHosts":       "extend",
			"selectItems":       "extend",
			"selectLastEvent":   "extend",
			"sortfield":         "lastchange",
			"sortorder":         "DESC",
			"limit":             100,
		},
		Auth: z.auth,
		ID:   2,
	}

	resp, err := z.doRequest(req)
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

// Trigger Zabbix 触发器
type Trigger struct {
	TriggerID   string   `json:"triggerid"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Value       string   `json:"value"`
	Priority    int      `json:"priority"`
	LastChange  string   `json:"lastchange"`
	Hosts       []Host  `json:"hosts"`
	Items       []Item  `json:"items"`
	LastEvent   Event   `json:"last_event"`
}

// Host 主机
type Host struct {
	HostID   string `json:"hostid"`
	Host     string `json:"host"`
	Name     string `json:"name"`
}

// Item 监控项
type Item struct {
	ItemID    string `json:"itemid"`
	Name      string `json:"name"`
	Key       string `json:"key_"`
	LastValue string `json:"lastvalue"`
}

// Event 事件
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

	// 转换严重级别
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
	HostName     string `json:"host_name"`
	HostIP       string `json:"host_ip"`
	TriggerName  string `json:"trigger_name"`
	TriggerID    string `json:"trigger_id"`
	Problem      string `json:"problem"`
	Severity     int    `json:"severity"`
	SeverityName string `json:"severity_name"`
	Status       string `json:"status"`
	Source       string `json:"source"`
}

func (z *ZabbixClient) doRequest(req ZabbixAPIRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", z.baseURL+"/api_jsonrpc.php", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := z.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Zabbix 返回错误状态码: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
