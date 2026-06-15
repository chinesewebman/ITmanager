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

// GLPIClient GLPI 客户端（C-P7：走 httpx）。
type GLPIClient struct {
	c         *httpx.Client
	appToken  string
	userToken string

	mu      sync.Mutex
	session string
}

// NewGLPIClient 创建 GLPI 客户端。
func NewGLPIClient(cfg *config.GLPIConfig, m httpx.MetricsRecorder) *GLPIClient {
	hcfg := httpx.DefaultConfig(cfg.URL)
	hcfg.Timeout = 30 * time.Second
	return &GLPIClient{
		c:         httpx.New(hcfg, "glpi", m),
		appToken:  cfg.AppToken,
		userToken: cfg.UserToken,
	}
}

// InitSession 初始化会话（C-P7：ctx 透传）。
func (g *GLPIClient) InitSession(ctx context.Context) error {
	reqBody := map[string]string{
		"app_token":  g.appToken,
		"user_token": g.userToken,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	respBody, _, err := g.c.DoWithHeaders(ctx, "POST", "/api/initSession", bytes.NewReader(body),
		map[string]string{"App-Token": g.appToken})
	if err != nil {
		return fmt.Errorf("GLPI 会话初始化失败: %w", err)
	}
	var result struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析 GLPI 响应失败: %w", err)
	}
	g.mu.Lock()
	g.session = result.SessionToken
	g.mu.Unlock()
	return nil
}

// GetTickets 获取工单列表（C-P7：ctx 透传）。
func (g *GLPIClient) GetTickets(ctx context.Context) ([]GLPITicket, error) {
	g.mu.Lock()
	needLogin := g.session == ""
	g.mu.Unlock()
	if needLogin {
		if err := g.InitSession(ctx); err != nil {
			return nil, err
		}
	}

	g.mu.Lock()
	sess := g.session
	g.mu.Unlock()

	respBody, _, err := g.c.DoWithHeaders(ctx, "GET", "/api/Ticket?range=0-100", nil,
		map[string]string{
			"Session-Token": sess,
			"App-Token":     g.appToken,
		})
	if err != nil {
		return nil, fmt.Errorf("获取工单失败: %w", err)
	}
	var tickets []GLPITicket
	if err := json.Unmarshal(respBody, &tickets); err != nil {
		return nil, fmt.Errorf("解析工单失败: %w", err)
	}
	return tickets, nil
}

// KillSession 终止会话（C-P7：ctx 透传）。
func (g *GLPIClient) KillSession(ctx context.Context) error {
	g.mu.Lock()
	sess := g.session
	g.session = ""
	g.mu.Unlock()
	if sess == "" {
		return nil
	}
	_, _, _ = g.c.DoWithHeaders(ctx, "GET", "/api/killSession", nil,
		map[string]string{
			"Session-Token": sess,
			"App-Token":     g.appToken,
		})
	return nil
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

// GetStatusName / GetPriorityName / ConvertToTicket / LocalTicket 保持不变
func (t *GLPITicket) GetStatusName() string {
	statusMap := map[int]string{1: "新建", 2: "处理中", 3: "待定", 4: "已解决", 5: "已关闭", 6: "待批准"}
	return statusMap[t.Status]
}
func (t *GLPITicket) GetPriorityName() string {
	priorityMap := map[int]string{1: "非常低", 2: "低", 3: "中", 4: "高", 5: "非常高", 6: "紧急"}
	return priorityMap[t.Priority]
}

func (t *GLPITicket) ConvertToTicket() *LocalTicket {
	statusMap := map[int]string{1: "open", 2: "in_progress", 3: "pending", 4: "resolved", 5: "closed"}
	priorityMap := map[int]string{1: "low", 2: "low", 3: "medium", 4: "high", 5: "critical", 6: "critical"}
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

type LocalTicket struct {
	ExternalID  string
	Title       string
	Description string
	Status      string
	Priority    string
	TicketType  string
	Source      string
	CreatedAt   string
	ResolvedAt  string
	ClosedAt    string
}
