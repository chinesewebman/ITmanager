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
		{"中文测试", "????"}, // 非 Latin-1 → ?（4 字符 → 4 个 ?）
		{strings.Repeat("a", 250), strings.Repeat("a", 200) + "..."}, // 长字符串截断
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
				SiteName:  "DC1",
			},
			Events: []models.TimelineEvent{
				{
					TS:    time.Date(2026, 6, 17, 10, 30, 0, 0, time.UTC),
					Kind:  models.TimelineEventAlert,
					Title: "High CPU on web-server-01",
				},
			},
			Summary: &models.DiagnosticSummary{
				AlertCount:  5,
				OpenAlerts:  1,
				TicketCount: 2,
			},
		},
	}
	buf, err := r.Render(data)
	if err != nil {
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
	buf, err := r.Render(data)
	if err != nil {
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
	buf, err := r.Render(data)
	if err != nil {
		t.Fatalf("Render empty events: %v", err)
	}
	if buf.Len() < 1000 {
		t.Errorf("PDF too small: %d", buf.Len())
	}
}

// asBool helper to avoid unused expression
func asBool() bool { return true }
