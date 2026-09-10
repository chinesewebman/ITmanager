package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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

// TestGLPIE2E_ConvertToTicket_优先级限定契约词表 守 M16。
//
// 改 glpi.go 的 priorityMap 之前，这些分支在整套测试里**零覆盖**：
// glpi_e2e_test.go 只喂过 Priority 4（断言 high），其余档位写成什么都行、
// 没有用例会红。而 3 号档原先写的 medium 与契约的 normal 是同一个「普通」的
// 两套拼法，后果是工单页按「普通」筛选（WHERE priority='normal'）查不到这些票。
func TestGLPIE2E_ConvertToTicket_优先级限定契约词表(t *testing.T) {
	// 与 openapi.yaml 的 Ticket.priority enum 一一对应，改动需同步契约
	vocab := map[string]bool{"low": true, "normal": true, "high": true, "critical": true}
	want := map[int]string{1: "low", 2: "low", 3: "normal", 4: "high", 5: "critical", 6: "critical"}

	for p, exp := range want {
		got := (&GLPITicket{ID: 1, Name: "t", Status: 1, Priority: p}).ConvertToTicket().Priority
		if got != exp {
			t.Errorf("GLPI priority=%d → %q, want %q", p, got, exp)
		}
		if !vocab[got] {
			t.Errorf("GLPI priority=%d 产出 %q, 不在契约词表内", p, got)
		}
		if got == "medium" {
			t.Errorf("GLPI priority=%d 产出了 medium —— 该拼法已由迁移 000023 归一为 normal, 不得再引入", p)
		}
	}

	// 越界档位（真实 GLPI 只发 1~6）：记录当前行为是「原样落空串」，不是静默兜底成某一档。
	// 落空串会被 ticket_service.Create 的 M16 兜底接成 normal（HTTP 建单路径），
	// 但 GLPI 同步走的是 upsert，不经 Create —— 真出现越界值应当看得见。
	if got := (&GLPITicket{Priority: 0}).ConvertToTicket().Priority; got != "" {
		t.Errorf("越界 priority=0 → %q, 期望空串（当前行为）", got)
	}
}
