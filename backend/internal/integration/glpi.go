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

// GLPIClient GLPI 客户端
type GLPIClient struct {
	baseURL   string
	appToken  string
	userToken string
	client    *http.Client
	session   string
}

// NewGLPIClient 创建 GLPI 客户端
func NewGLPIClient(cfg *config.GLPIConfig) *GLPIClient {
	return &GLPIClient{
		baseURL:   cfg.URL,
		appToken:  cfg.AppToken,
		userToken: cfg.UserToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// InitSession 初始化会话
func (g *GLPIClient) InitSession() error {
	reqBody := map[string]string{
		"app_token":  g.appToken,
		"user_token": g.userToken,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", g.baseURL+"/api/initSession", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("App-Token", g.appToken)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("GLPI 会话初始化失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GLPI 返回错误状态码: %d", resp.StatusCode)
	}

	var result struct {
		SessionToken string `json:"session_token"`
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析 GLPI 响应失败: %w", err)
	}

	g.session = result.SessionToken
	return nil
}

// GetTickets 获取工单列表
func (g *GLPIClient) GetTickets() ([]GLPITicket, error) {
	if g.session == "" {
		if err := g.InitSession(); err != nil {
			return nil, err
		}
	}

	httpReq, err := http.NewRequest("GET", g.baseURL+"/api/Ticket?range=0-100", nil)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Session-Token", g.session)
	httpReq.Header.Set("App-Token", g.appToken)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("获取工单失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GLPI 返回错误状态码: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tickets []GLPITicket
	if err := json.Unmarshal(respBody, &tickets); err != nil {
		return nil, fmt.Errorf("解析工单失败: %w", err)
	}

	return tickets, nil
}

// GLPITicket GLPI 工单
type GLPITicket struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Content         string `json:"content"`
	Status          int    `json:"status"`
	Priority        int    `json:"priority"`
	EntityID        int    `json:"entities_id"`
	Type            int    `json:"type"`
	RequestType     int    `json:"requesttypes_id"`
	SolutionType    int    `json:"solutiontypes_id"`
	Date            string `json:"date"`
	ClosedDate      string `json:"closedate"`
	SolvedDate      string `json:"solvedate"`
	TimeToResolve   string `json:"time_to_resolve"`
	TimeToOwn       string `json:"time_to_own"`
	UsersIDRequirer int    `json:"users_id_recipient"`
	UsersIDAssign   int    `json:"users_id_assign"`
	GroupsIDAssign  int    `json:"groups_id_assign"`
}

// GetStatusName 获取状态名称
func (t *GLPITicket) GetStatusName() string {
	statusMap := map[int]string{
		1: "新建",
		2: "处理中",
		3: "待定",
		4: "已解决",
		5: "已关闭",
		6: "待批准",
	}
	return statusMap[t.Status]
}

// GetPriorityName 获取优先级名称
func (t *GLPITicket) GetPriorityName() string {
	priorityMap := map[int]string{
		1: "非常低",
		2: "低",
		3: "中",
		4: "高",
		5: "非常高",
		6: "紧急",
	}
	return priorityMap[t.Priority]
}

// ConvertToTicket 转换为本地工单格式
func (t *GLPITicket) ConvertToTicket() *LocalTicket {
	statusMap := map[int]string{
		1: "open",
		2: "in_progress",
		3: "pending",
		4: "resolved",
		5: "closed",
	}

	priorityMap := map[int]string{
		1: "low",
		2: "low",
		3: "medium",
		4: "high",
		5: "critical",
		6: "critical",
	}

	return &LocalTicket{
		ExternalID:  fmt.Sprintf("%d", t.ID),
		Title:       t.Name,
		Description: t.Content,
		Status:      statusMap[t.Status],
		Priority:    priorityMap[t.Priority],
		TicketType:  "incident",
		Source:      "glpi",
		CreatedAt:   t.Date,
		ResolvedAt:  t.SolvedDate,
		ClosedAt:    t.ClosedDate,
	}
}

// LocalTicket 本地工单
type LocalTicket struct {
	ExternalID  string `json:"external_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	TicketType  string `json:"ticket_type"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	ResolvedAt  string `json:"resolved_at"`
	ClosedAt    string `json:"closed_at"`
}

// KillSession 终止会话
func (g *GLPIClient) KillSession() error {
	if g.session == "" {
		return nil
	}

	httpReq, err := http.NewRequest("GET", g.baseURL+"/api/killSession", nil)
	if err != nil {
		return err
	}

	httpReq.Header.Set("Session-Token", g.session)
	httpReq.Header.Set("App-Token", g.appToken)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	g.session = ""
	return nil
}
