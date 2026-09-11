package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"network-monitor-platform/internal/config"
)

// TestGLPIE2E_HappyPath 完整流程：InitSession → GetTickets → KillSession。
// 验证：Session-Token / App-Token header 注入 + session 复用。
func TestGLPIE2E_HappyPath(t *testing.T) {
	var calls int32
	var initCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			atomic.AddInt32(&initCount, 1)
			// 验证 app_token header
			if r.Header.Get("App-Token") != "app-tok-123" {
				t.Errorf("App-Token = %q, want app-tok-123", r.Header.Get("App-Token"))
			}
			// 验证 body 含 user_token
			body := make([]byte, 1024)
			n, _ := r.Body.Read(body)
			if !strings.Contains(string(body[:n]), "user-tok-456") {
				t.Errorf("initSession body missing user_token: %s", string(body[:n]))
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"session_token":"sess-abc-789"}`))

		case strings.Contains(r.URL.Path, "/Ticket"):
			// 验证 session + app token
			if r.Header.Get("Session-Token") != "sess-abc-789" {
				t.Errorf("Session-Token = %q", r.Header.Get("Session-Token"))
			}
			if r.Header.Get("App-Token") != "app-tok-123" {
				t.Errorf("App-Token = %q", r.Header.Get("App-Token"))
			}
			tickets := []map[string]any{
				{"id": 1, "name": "Disk full", "content": "Server disk 100%", "status": 1, "priority": 4, "date": "2026-06-15 10:00"},
				{"id": 2, "name": "Network down", "content": "Link down", "status": 2, "priority": 5, "date": "2026-06-15 11:00"},
			}
			b, _ := json.Marshal(tickets)
			w.WriteHeader(200)
			_, _ = w.Write(b)

		case strings.HasSuffix(r.URL.Path, "/killSession"):
			// 验证仍带 session header
			if r.Header.Get("Session-Token") != "sess-abc-789" {
				t.Errorf("killSession missing session token")
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.GLPIConfig{URL: srv.URL, AppToken: "app-tok-123", UserToken: "user-tok-456"}
	c := NewGLPIClient(cfg, rec)

	// 第一次：initSession + tickets
	tickets, err := c.GetTickets(context.Background())
	if err != nil {
		t.Fatalf("GetTickets: %v", err)
	}
	if len(tickets) != 2 {
		t.Errorf("got %d tickets, want 2", len(tickets))
	}
	if tickets[0].Name != "Disk full" {
		t.Errorf("first ticket = %q, want 'Disk full'", tickets[0].Name)
	}

	// 第二次：复用 session（不应再 init）
	_, err = c.GetTickets(context.Background())
	if err != nil {
		t.Fatalf("GetTickets #2: %v", err)
	}
	if atomic.LoadInt32(&initCount) != 1 {
		t.Errorf("expected 1 initSession, got %d (session not cached)", atomic.LoadInt32(&initCount))
	}

	// KillSession
	if err := c.KillSession(context.Background()); err != nil {
		t.Errorf("KillSession: %v", err)
	}
	// 总调用：init + 2*Ticket + kill = 4
	if calls := atomic.LoadInt32(&calls); calls != 4 {
		t.Errorf("expected 4 server calls, got %d", calls)
	}
}

// TestGLPIE2E_StatusMapping 验证状态/优先级 int→string 映射。
func TestGLPIE2E_StatusMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"session_token":"s1"}`))
		case strings.Contains(r.URL.Path, "/Ticket"):
			// 覆盖所有 status/priority 值
			tickets := []map[string]any{
				{"id": 1, "name": "t1", "status": 1, "priority": 1, "date": ""}, // open / low
				{"id": 2, "name": "t2", "status": 2, "priority": 3, "date": ""}, // in_progress / medium
				{"id": 3, "name": "t3", "status": 3, "priority": 4, "date": ""}, // pending / high
				{"id": 4, "name": "t4", "status": 5, "priority": 6, "date": ""}, // closed / critical
			}
			b, _ := json.Marshal(tickets)
			w.WriteHeader(200)
			_, _ = w.Write(b)
		}
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"}
	c := NewGLPIClient(cfg, rec)
	tickets, err := c.GetTickets(context.Background())
	if err != nil {
		t.Fatalf("GetTickets: %v", err)
	}
	if len(tickets) != 4 {
		t.Fatalf("got %d tickets", len(tickets))
	}
	checks := []struct {
		wantStatus, wantPrio string
	}{
		{"新建", "非常低"},
		{"处理中", "中"},
		{"待定", "高"},
		{"已关闭", "紧急"},
	}
	for i, c := range checks {
		if tickets[i].GetStatusName() != c.wantStatus {
			t.Errorf("ticket[%d].Status = %q, want %q", i, tickets[i].GetStatusName(), c.wantStatus)
		}
		if tickets[i].GetPriorityName() != c.wantPrio {
			t.Errorf("ticket[%d].Priority = %q, want %q", i, tickets[i].GetPriorityName(), c.wantPrio)
		}
	}
}

// TestGLPIE2E_ConvertToTicket 验证 ConvertToTicket 业务字段映射。
func TestGLPIE2E_ConvertToTicket(t *testing.T) {
	src := &GLPITicket{ID: 42, Name: "Test", Content: "Desc", Status: 1, Priority: 4, Date: "2026-06-15"}
	conv := src.ConvertToTicket()
	if conv.ExternalID != "42" {
		t.Errorf("ExternalID = %q, want 42", conv.ExternalID)
	}
	if conv.Title != "Test" {
		t.Errorf("Title = %q", conv.Title)
	}
	if conv.Status != "open" {
		t.Errorf("Status = %q", conv.Status)
	}
	if conv.Priority != "high" {
		t.Errorf("Priority = %q", conv.Priority)
	}
	if conv.Source != "glpi" {
		t.Errorf("Source = %q, want glpi", conv.Source)
	}
	if conv.TicketType != "incident" {
		t.Errorf("TicketType = %q, want incident", conv.TicketType)
	}
}

// TestGLPIE2E_SessionError init session 5xx 失败应透传。
func TestGLPIE2E_SessionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	rec := newRecorderE2E()
	cfg := &config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"}
	c := NewGLPIClient(cfg, rec)

	_, err := c.GetTickets(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

// ticketEnum 从 openapi.yaml 读 Ticket.<field> 的 enum 字面量。
//
// 为什么不硬编码：契约是单一 source of truth（internal/api/swagger.go:20），
// 把 enum 抄进测试就等于抄了一份会各自漂移的副本 —— 有人改了契约而没改这里，
// 用例照样绿，正是本文件要防的那类假绿（T-42）。
func ticketEnum(t *testing.T, field string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../api/openapi.yaml")
	if err != nil {
		t.Fatalf("读 openapi.yaml 失败: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas struct {
				Ticket struct {
					Properties map[string]struct {
						Enum []string `yaml:"enum"`
					} `yaml:"properties"`
				} `yaml:"Ticket"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析 openapi.yaml 失败: %v", err)
	}
	prop, ok := doc.Components.Schemas.Ticket.Properties[field]
	if !ok {
		t.Fatalf("openapi.yaml 的 Ticket 里没有 %s 字段 —— 契约结构变了，本用例需同步", field)
	}
	if len(prop.Enum) == 0 {
		t.Fatalf("openapi.yaml 的 Ticket.%s 没有 enum —— 契约结构变了，本用例需同步", field)
	}
	set := make(map[string]bool, len(prop.Enum))
	for _, v := range prop.Enum {
		set[v] = true
	}
	return set
}

// TestGLPIE2E_ConvertToTicket_优先级per键对照 守 M16 + M26/D-1。
//
// **逐键对照**，不是值域对照：只检查「产出的值在 enum 内」抓不到缺键 ——
// 缺键会落空串，而空串会被同步循环当越界跳过（整类票静默不入库）。
// 必须为 GLPI 的每一个档位写死期望值，缺/错任何一键都红。
//
// 反证：删掉 priorityMap 的 `0:` 键 → 本条红。
func TestGLPIE2E_ConvertToTicket_优先级per键对照(t *testing.T) {
	vocab := ticketEnum(t, "priority")

	// GLPI 的 priority 取值域是 0..6（0 = 未指定）。
	// 期望值独立于被测的 priorityMap —— 这张表来自语义决策（D-1），不是从实现抄回来的。
	want := map[int]string{
		0: "normal", // M26/D-1：GLPI「未指定」→ 与手工建单表单默认值一致
		1: "low", 2: "low", 3: "normal", 4: "high", 5: "critical", 6: "critical",
	}

	for p, exp := range want {
		got := (&GLPITicket{ID: 1, Name: "t", Status: 1, Priority: p}).ConvertToTicket().Priority
		if got != exp {
			t.Errorf("GLPI priority=%d → %q, want %q", p, got, exp)
		}
		if !vocab[got] {
			t.Errorf("GLPI priority=%d 产出 %q, 不在契约 Ticket.priority 的 enum 内 (openapi.yaml:2503)", p, got)
		}
		if got == "medium" {
			t.Errorf("GLPI priority=%d 产出了 medium —— 该拼法已由迁移 000023 归一为 normal, 不得再引入", p)
		}
	}

	// 越界（∉ 0..6）：词表必须**留空串**，由调用方按 D-1 跳过并计数。
	// 在这里兜底成某一档会把「GLPI 发了没见过的值」这件事静默掩盖掉。
	for _, p := range []int{-1, 7, 99} {
		if got := (&GLPITicket{Priority: p}).ConvertToTicket().Priority; got != "" {
			t.Errorf("越界 priority=%d → %q, 期望留空串由调用方处置, 不得在此兜底", p, got)
		}
	}
}

// TestGLPIE2E_ConvertToTicket_状态per键对照 守 M26/D-1。
//
// 与优先级同法：**逐键对照**。缺 `6:` 键时 status=6 落空串 → 被当越界跳过，
// 「待批准」这一类票会整批静默不入库。
//
// 反证：删掉 statusMap 的 `6:` 键 → 本条红。
func TestGLPIE2E_ConvertToTicket_状态per键对照(t *testing.T) {
	vocab := ticketEnum(t, "status")

	// GLPI 的 status 取值域是 1..6。期望值独立于被测的 statusMap。
	want := map[int]string{
		1: "open", 2: "in_progress", 3: "pending", 4: "resolved",
		5: "closed",
		6: "pending", // M26/D-1：GLPI「待批准」无独立本地语义，归入 pending
	}

	for s, exp := range want {
		got := (&GLPITicket{ID: 1, Name: "t", Status: s, Priority: 3}).ConvertToTicket().Status
		if got != exp {
			t.Errorf("GLPI status=%d → %q, want %q", s, got, exp)
		}
		if !vocab[got] {
			t.Errorf("GLPI status=%d 产出 %q, 不在契约 Ticket.status 的 enum 内 (openapi.yaml:2506)", s, got)
		}
	}

	// 越界（∉ 1..6）留空串，同上。
	for _, s := range []int{0, 7, 99} {
		if got := (&GLPITicket{Status: s}).ConvertToTicket().Status; got != "" {
			t.Errorf("越界 status=%d → %q, 期望留空串由调用方处置, 不得在此兜底", s, got)
		}
	}
}
