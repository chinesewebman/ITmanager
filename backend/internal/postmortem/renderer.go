// Package postmortem 生成资产故障复盘 PDF 报告。
//
// 设计原则：
//   - 数据复用：直接调用 internal/service.DiagnosticService.GetTimeline，
//     不重复聚合 SQL（避免数据漂移）
//   - 中文支持：使用 unicode 字体（需嵌入 .ttf），暂用 UTF-8 转 Latin-1 兼容 fallback
//   - 纯流式生成：handler 直接 io.Copy 到 gin.Response，避免大文件驻留内存
//   - 可测：Renderer 接收接口而非 *fpdf.Fpdf，方便单测
//
// 报告内容（参考 AWS/Google SRE postmortem 模板精简版）：
//  1. 报告头：资产名 / IP / 时间窗口 / 生成时间
//  2. 资产概要：状态、open alerts、open tickets
//  3. MTTR 摘要：平均恢复时间、告警密度
//  4. 事件时间线：按时间倒序的事件流（alerts/tickets/status/link）
//  5. Top 5 告警：按 severity 排序的 top N
package postmortem

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"network-monitor-platform/internal/models"
)

// ReportData 复盘报告输入（直接复用 DiagnosticTimeline）
type ReportData struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	WindowDays  int                        `json:"window_days"`
	IPAddress   string                     `json:"ip_address,omitempty"` // 从 assets 表单独取
	Timeline    *models.DiagnosticTimeline `json:"timeline"`
}

// Renderer 渲染器接口（便于 mock 单测）
type Renderer interface {
	Render(data *ReportData) (bytes.Buffer, error)
}

// FpdfRenderer 基于 fpdf 的标准渲染器
type FpdfRenderer struct{}

// NewFpdfRenderer 构造 FpdfRenderer
func NewFpdfRenderer() *FpdfRenderer {
	return &FpdfRenderer{}
}

// Render 生成 PDF bytes.Buffer
func (r *FpdfRenderer) Render(data *ReportData) (bytes.Buffer, error) {
	var buf bytes.Buffer

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)

	pdf.AddPage()

	// 标题（英文 + ASCII fallback，中文需嵌入 unicode 字体才完整支持）
	// fpdf 默认 Helvetica 只支持 Latin-1，Chinese 字符会显示为 #，但报告整体结构清晰
	// 这是 v1 的可接受权衡（不引入字体文件以保持包轻量）
	renderHeader(pdf, data)
	renderAssetSummary(pdf, data)
	renderMttrSummary(pdf, data)
	renderTimeline(pdf, data)
	renderTopAlerts(pdf, data)
	renderFooter(pdf, data)

	if err := pdf.Output(&buf); err != nil {
		return buf, fmt.Errorf("PDF output: %w", err)
	}
	return buf, nil
}

func renderHeader(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 10, "Asset Postmortem Report")
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.Cell(0, 5, fmt.Sprintf("Generated: %s", data.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC")))
	pdf.Ln(5)
	pdf.Cell(0, 5, fmt.Sprintf("Window: last %d days", data.WindowDays))
	pdf.Ln(10)
	pdf.SetTextColor(0, 0, 0)
}

func renderAssetSummary(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 13)
	pdf.Cell(0, 8, "1. Asset Summary")
	pdf.Ln(9)

	if data.Timeline == nil || data.Timeline.Asset == nil {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.Cell(0, 5, "(no asset data)")
		pdf.Ln(8)
		return
	}
	a := data.Timeline.Asset

	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(40, 6, "Name:")
	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, safe(a.Name))
	pdf.Ln(6)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(40, 6, "Type:")
	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, safe(a.AssetType))
	pdf.Ln(6)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(40, 6, "IP Address:")
	pdf.SetFont("Helvetica", "", 10)
	ip := data.IPAddress
	if ip == "" {
		ip = "-"
	}
	pdf.Cell(0, 6, safe(ip))
	pdf.Ln(6)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(40, 6, "Status:")
	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, safe(a.Status))
	pdf.Ln(6)

	if a.SiteName != "" {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.Cell(40, 6, "Site:")
		pdf.SetFont("Helvetica", "", 10)
		pdf.Cell(0, 6, safe(a.SiteName))
		pdf.Ln(6)
	}
	pdf.Ln(4)
}

func renderMttrSummary(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 13)
	pdf.Cell(0, 8, "2. Reliability Metrics")
	pdf.Ln(9)

	if data.Timeline == nil || data.Timeline.Summary == nil {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.Cell(0, 5, "(no metrics)")
		pdf.Ln(8)
		return
	}
	s := data.Timeline.Summary

	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(60, 6, fmt.Sprintf("Total Alerts:    %d", s.AlertCount))
	pdf.Cell(60, 6, fmt.Sprintf("Open Alerts:     %d", s.OpenAlerts))
	pdf.Ln(6)
	pdf.Cell(60, 6, fmt.Sprintf("Total Tickets:   %d", s.TicketCount))
	pdf.Cell(60, 6, fmt.Sprintf("Open Tickets:    %d", s.OpenTickets))
	pdf.Ln(6)
	pdf.Cell(60, 6, fmt.Sprintf("Link Down:       %d", s.LinkDownCount))
	if s.MTTRSeconds != nil {
		pdf.Cell(60, 6, fmt.Sprintf("MTTR:            %s", formatSeconds(*s.MTTRSeconds)))
	} else {
		pdf.Cell(60, 6, "MTTR:            n/a")
	}
	pdf.Ln(6)
	if s.LastOffline != nil {
		pdf.Cell(60, 6, fmt.Sprintf("Last Offline:    %s", s.LastOffline.UTC().Format("2006-01-02 15:04 UTC")))
	}
	if s.LastOnline != nil {
		pdf.Cell(60, 6, fmt.Sprintf("Last Online:     %s", s.LastOnline.UTC().Format("2006-01-02 15:04 UTC")))
	}
	pdf.Ln(10)
}

func renderTimeline(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 13)
	pdf.Cell(0, 8, "3. Event Timeline")
	pdf.Ln(9)

	if data.Timeline == nil || len(data.Timeline.Events) == 0 {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.Cell(0, 5, "(no events in window)")
		pdf.Ln(8)
		return
	}

	// 表头
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(35, 6, "Time", "1", 0, "L", true, 0, "")
	pdf.CellFormat(25, 6, "Kind", "1", 0, "L", true, 0, "")
	pdf.CellFormat(115, 6, "Title", "1", 1, "L", true, 0, "")

	pdf.SetFont("Helvetica", "", 8)
	maxRows := 50
	events := data.Timeline.Events
	if len(events) > maxRows {
		events = events[:maxRows]
	}
	for _, e := range events {
		pdf.CellFormat(35, 5, e.TS.UTC().Format("01-02 15:04"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 5, string(e.Kind), "1", 0, "L", false, 0, "")
		title := e.Title
		if len(title) > 70 {
			title = title[:70] + "..."
		}
		pdf.CellFormat(115, 5, safe(title), "1", 1, "L", false, 0, "")
	}
	if len(data.Timeline.Events) > maxRows {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "I", 9)
		pdf.Cell(0, 5, fmt.Sprintf("... and %d more events (truncated)", len(data.Timeline.Events)-maxRows))
	}
	pdf.Ln(6)
}

func renderTopAlerts(pdf *fpdf.Fpdf, data *ReportData) {
	if data.Timeline == nil || len(data.Timeline.Events) == 0 {
		return
	}
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 13)
	pdf.Cell(0, 8, "4. Top Longest Alerts")
	pdf.Ln(9)

	// 收集 alert 事件并按 severity 排序（severity 越大越严重）
	alerts := make([]models.TimelineEvent, 0)
	for _, e := range data.Timeline.Events {
		if e.Kind == models.TimelineEventAlert {
			alerts = append(alerts, e)
		}
	}
	if len(alerts) == 0 {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.Cell(0, 5, "(no alerts in window)")
		return
	}
	// 按 severity desc 排序
	sort.SliceStable(alerts, func(i, j int) bool {
		return alerts[i].Severity > alerts[j].Severity
	})
	topN := 5
	if len(alerts) < topN {
		topN = len(alerts)
	}
	alerts = alerts[:topN]

	// 表头
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(20, 6, "Sev", "1", 0, "C", true, 0, "")
	pdf.CellFormat(35, 6, "Time", "1", 0, "L", true, 0, "")
	pdf.CellFormat(20, 6, "Kind", "1", 0, "L", true, 0, "")
	pdf.CellFormat(100, 6, "Title", "1", 1, "L", true, 0, "")

	pdf.SetFont("Helvetica", "", 8)
	for _, e := range alerts {
		pdf.CellFormat(20, 5, fmt.Sprintf("%d", e.Severity), "1", 0, "C", false, 0, "")
		pdf.CellFormat(35, 5, e.TS.UTC().Format("01-02 15:04"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(20, 5, safe(e.SubKind), "1", 0, "L", false, 0, "")
		title := e.Title
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		pdf.CellFormat(100, 5, safe(title), "1", 1, "L", false, 0, "")
	}
}

func renderFooter(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.Ln(10)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.SetTextColor(150, 150, 150)
	pdf.Cell(0, 5, "This report is auto-generated from alerts / tickets / status history. Review with engineering team for root cause analysis.")
}

// safe 字符串清洗：去除 fpdf 不支持的字符（控制字符、换行），避免 PDF 损坏
func safe(s string) string {
	if s == "" {
		return "-"
	}
	// fpdf 默认字体不支持 control char 和非 Latin-1；filter 到可打印 + 简单替换
	var b strings.Builder
	for _, r := range s {
		if r < 32 {
			b.WriteByte(' ')
			continue
		}
		// Latin-1 范围之外的字符替换为 ?（v1 权衡，后续可加 unicode 字体）
		if r > 255 {
			b.WriteByte('?')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > 200 {
		out = out[:200] + "..."
	}
	return out
}

// formatSeconds 把秒数格式化为 "1h23m" / "23m" / "45s"
func formatSeconds(s int64) string {
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%dh%dm", s/3600, (s%3600)/60)
}
