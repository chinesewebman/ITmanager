package postmortem

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"network-monitor-platform/internal/models"
)

func TestSafe(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "-"},
		{"hello", "hello"},
		{"a\nb", "a b"},  // \n → space
		{"中文测试", "中文测试"}, // 中文字符保留（不再替换为 ?）
		{"混合 mixed 123", "混合 mixed 123"},
		{strings.Repeat("a", 250), strings.Repeat("a", 200) + "..."},
	}
	for _, c := range cases {
		got := safe(c.in)
		if got != c.want {
			t.Errorf("safe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{30, "30s"},
		{60, "1m"},
		{3600, "1h0m"},
		{5400, "1h30m"},
	}
	for _, c := range cases {
		got := formatSeconds(c.in)
		if got != c.want {
			t.Errorf("formatSeconds(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFpdfRenderer_Render_Basic(t *testing.T) {
	r := NewFpdfRenderer()
	data := &ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  30,
		IPAddress:   "192.168.1.10",
		Timeline: &models.DiagnosticTimeline{
			Asset: &models.DiagnosticAsset{
				Name:      "web-server-01",
				AssetType: "server",
				Status:    "active",
				SiteName:  "北京-DC1",
			},
			Events: []models.TimelineEvent{
				{
					TS:    time.Date(2026, 6, 17, 10, 30, 0, 0, time.UTC),
					Kind:  models.TimelineEventAlert,
					Title: "CPU 高负载告警 (web-server-01)",
				},
			},
			Summary: &models.DiagnosticSummary{
				AlertCount:  5,
				OpenAlerts:  1,
				TicketCount: 2,
			},
		},
	}
	var buf bytes.Buffer
	if err := r.Render(data, &buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// PDF magic = %PDF
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Errorf("expected PDF magic, got %q", buf.String()[:20])
	}
	if buf.Len() < 1000 {
		t.Errorf("PDF too small: %d bytes", buf.Len())
	}
}

func TestFpdfRenderer_Render_NilTimeline(t *testing.T) {
	r := NewFpdfRenderer()
	data := &ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  7,
	}
	var buf bytes.Buffer
	if err := r.Render(data, &buf); err != nil {
		t.Fatalf("Render nil timeline: %v", err)
	}
	if buf.Len() < 1000 {
		t.Errorf("PDF too small: %d", buf.Len())
	}
}

func TestFpdfRenderer_Render_EmptyEvents(t *testing.T) {
	r := NewFpdfRenderer()
	data := &ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  7,
		Timeline: &models.DiagnosticTimeline{
			Asset:   &models.DiagnosticAsset{Name: "test"},
			Events:  []models.TimelineEvent{},
			Summary: &models.DiagnosticSummary{},
		},
	}
	var buf bytes.Buffer
	if err := r.Render(data, &buf); err != nil {
		t.Fatalf("Render empty events: %v", err)
	}
	if buf.Len() < 1000 {
		t.Errorf("PDF too small: %d", buf.Len())
	}
}

func TestFpdfRenderer_Render_ChineseContent(t *testing.T) {
	// 验证中文内容能完整生成 PDF（不依赖肉眼读 PDF，只验证不 panic、不空、>= 一定大小）
	r := NewFpdfRenderer()
	data := &ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  30,
		IPAddress:   "192.168.1.10",
		Timeline: &models.DiagnosticTimeline{
			Asset: &models.DiagnosticAsset{
				Name:      "数据库服务器-主库",
				AssetType: "server",
				Status:    "active",
				SiteName:  "上海数据中心-核心区",
			},
			Events: []models.TimelineEvent{
				{Kind: models.TimelineEventAlert, Title: "磁盘空间不足"},
				{Kind: models.TimelineEventAlert, Title: "MySQL 连接数过高 (QPS=5000)"},
				{Kind: models.TimelineEventTicket, Title: "工单 #1234：数据库慢查询"},
				{Kind: models.TimelineEventStatus, Title: "资产状态变更：active → maintenance"},
			},
			Summary: &models.DiagnosticSummary{
				AlertCount:  10,
				OpenAlerts:  2,
				TicketCount: 5,
			},
		},
	}
	var buf bytes.Buffer
	if err := r.Render(data, &buf); err != nil {
		t.Fatalf("Render Chinese: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Errorf("expected PDF magic")
	}
	// 嵌入字体后 PDF 会明显变大（正常 8MB 字体子集化后 + 内容 < 100KB）
	if buf.Len() < 10000 {
		t.Errorf("PDF too small for embedded font: %d bytes", buf.Len())
	}
}

// mockRenderer 验证 io.Writer 注入
type mockWriter struct {
	buf bytes.Buffer
}

func (m *mockWriter) Write(p []byte) (int, error) {
	return m.buf.Write(p)
}

func TestRenderer_AcceptsAnyWriter(t *testing.T) {
	// 验证 Renderer 接口接受任意 io.Writer（不只 *bytes.Buffer）
	r := NewFpdfRenderer()
	mw := &mockWriter{}
	err := r.Render(&ReportData{GeneratedAt: time.Now(), WindowDays: 7}, mw)
	if err != nil {
		t.Fatalf("Render to mockWriter: %v", err)
	}
	if mw.buf.Len() < 1000 {
		t.Errorf("mockWriter got too few bytes: %d", mw.buf.Len())
	}
}
