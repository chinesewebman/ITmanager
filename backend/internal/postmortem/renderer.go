// Package postmortem 生成资产故障复盘 PDF 报告。
//
// 设计原则：
//   - 数据复用：直接调用 internal/service.DiagnosticService.GetTimeline，
//     不重复聚合 SQL（避免数据漂移）
//   - 中文支持：//go:embed 嵌入霞鹜文楷 TC (SIL OFL 1.1, ~15MB)，
//     零文件系统依赖，跨平台一致
//   - 流式输出：Renderer 接受 io.Writer，handler 直接写入 gin.Response，
//     避免大文件驻留内存
//   - 可测：Renderer 接收 io.Writer + 接口注入，方便单测
//
// 报告内容（参考 AWS/Google SRE postmortem 模板精简版）：
//  1. 报告头：资产名 / IP / 时间窗口 / 生成时间
//  2. 资产概要：状态、open alerts、open tickets
//  3. 可靠性指标：MTTR (平均恢复时间)、告警密度
//  4. 事件时间线：按时间倒序的事件流（alerts/tickets/status/link）
//  5. Top 5 严重告警：按 severity 排序的 top N
package postmortem

import (
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"network-monitor-platform/internal/models"
)

// 嵌入霞鹜文楷 TC (SIL OFL 1.1, ~15MB) — 思源宋体衍生手写字体。
// 详见 assets/FONT-LICENSE.txt。
//
//go:embed assets/LXGWWenKaiTC-Regular.ttf
var fontCN []byte

// fontName 是 fpdf 内部的字体 family 名（注册时使用，fpdf 内部存小写）
const fontName = "lxgwwenkaitc"

// ReportData 复盘报告输入（直接复用 DiagnosticTimeline）
type ReportData struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	WindowDays  int                        `json:"window_days"`
	IPAddress   string                     `json:"ip_address,omitempty"` // 从 assets 表单独取
	Timeline    *models.DiagnosticTimeline `json:"timeline"`
}

// Renderer 渲染器接口（便于 mock 单测）
type Renderer interface {
	Render(data *ReportData, w io.Writer) error
}

// FpdfRenderer 基于 fpdf 的标准渲染器
type FpdfRenderer struct{}

// NewFpdfRenderer 构造 FpdfRenderer
func NewFpdfRenderer() *FpdfRenderer {
	return &FpdfRenderer{}
}

// Render 流式生成 PDF 到 w。
// 中文支持：通过 //go:embed 嵌入思源黑体 CN Subset (SIL OFL 1.1)。
func (r *FpdfRenderer) Render(data *ReportData, w io.Writer) error {
	pdf := fpdf.New("P", "mm", "A4", "")

	// 注册中文字体（从 embed 字节加载，零文件系统访问）
	pdf.AddUTF8FontFromBytes(fontName, "", fontCN)

	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// 所有正文使用中文字体
	renderHeader(pdf, data)
	renderAssetSummary(pdf, data)
	renderMttrSummary(pdf, data)
	renderTimeline(pdf, data)
	renderTopAlerts(pdf, data)
	renderFooter(pdf, data)

	return pdf.Output(w)
}

func renderHeader(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont(fontName, "", 18)
	pdf.Cell(0, 10, "资产复盘报告 (Asset Postmortem)")
	pdf.Ln(12)

	pdf.SetFont(fontName, "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.Cell(0, 5, fmt.Sprintf("生成时间: %s", data.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC")))
	pdf.Ln(5)
	pdf.Cell(0, 5, fmt.Sprintf("时间窗口: 最近 %d 天", data.WindowDays))
	pdf.Ln(10)
	pdf.SetTextColor(0, 0, 0)
}

func renderAssetSummary(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont(fontName, "", 13)
	pdf.Cell(0, 8, "1. 资产概要 (Asset Summary)")
	pdf.Ln(9)

	if data.Timeline == nil || data.Timeline.Asset == nil {
		pdf.SetFont(fontName, "", 10)
		pdf.Cell(0, 5, "(无资产数据)")
		pdf.Ln(8)
		return
	}
	a := data.Timeline.Asset

	pdf.SetFont(fontName, "", 10)
	pdf.Cell(40, 6, "名称:")
	pdf.Cell(0, 6, safe(a.Name))
	pdf.Ln(6)

	pdf.Cell(40, 6, "类型:")
	pdf.Cell(0, 6, safe(a.AssetType))
	pdf.Ln(6)

	pdf.Cell(40, 6, "IP 地址:")
	ip := data.IPAddress
	if ip == "" {
		ip = "-"
	}
	pdf.Cell(0, 6, safe(ip))
	pdf.Ln(6)

	pdf.Cell(40, 6, "状态:")
	pdf.Cell(0, 6, safe(a.Status))
	pdf.Ln(6)

	if a.SiteName != "" {
		pdf.Cell(40, 6, "站点:")
		pdf.Cell(0, 6, safe(a.SiteName))
		pdf.Ln(6)
	}
	pdf.Ln(4)
}

func renderMttrSummary(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont(fontName, "", 13)
	pdf.Cell(0, 8, "2. 可靠性指标 (Reliability Metrics)")
	pdf.Ln(9)

	if data.Timeline == nil || data.Timeline.Summary == nil {
		pdf.SetFont(fontName, "", 10)
		pdf.Cell(0, 5, "(无指标数据)")
		pdf.Ln(8)
		return
	}
	s := data.Timeline.Summary

	pdf.SetFont(fontName, "", 10)
	pdf.Cell(60, 6, fmt.Sprintf("总告警数:    %d", s.AlertCount))
	pdf.Cell(60, 6, fmt.Sprintf("未关告警:    %d", s.OpenAlerts))
	pdf.Ln(6)
	pdf.Cell(60, 6, fmt.Sprintf("总工单数:    %d", s.TicketCount))
	pdf.Cell(60, 6, fmt.Sprintf("未关工单:    %d", s.OpenTickets))
	pdf.Ln(6)
	pdf.Cell(60, 6, fmt.Sprintf("链路中断:    %d", s.LinkDownCount))
	if s.MTTRSeconds != nil {
		pdf.Cell(60, 6, fmt.Sprintf("MTTR:        %s", formatSeconds(*s.MTTRSeconds)))
	} else {
		pdf.Cell(60, 6, "MTTR:        n/a")
	}
	pdf.Ln(6)
	if s.LastOffline != nil {
		pdf.Cell(60, 6, fmt.Sprintf("最近离线:    %s", s.LastOffline.UTC().Format("2006-01-02 15:04 UTC")))
	}
	if s.LastOnline != nil {
		pdf.Cell(60, 6, fmt.Sprintf("最近恢复:    %s", s.LastOnline.UTC().Format("2006-01-02 15:04 UTC")))
	}
	pdf.Ln(10)
}

func renderTimeline(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont(fontName, "", 13)
	pdf.Cell(0, 8, "3. 事件时间线 (Event Timeline)")
	pdf.Ln(9)

	if data.Timeline == nil || len(data.Timeline.Events) == 0 {
		pdf.SetFont(fontName, "", 10)
		pdf.Cell(0, 5, "(窗口内无事件)")
		pdf.Ln(8)
		return
	}

	// 表头
	pdf.SetFont(fontName, "", 9)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(35, 6, "时间", "1", 0, "L", true, 0, "")
	pdf.CellFormat(25, 6, "类型", "1", 0, "L", true, 0, "")
	pdf.CellFormat(115, 6, "标题", "1", 1, "L", true, 0, "")

	pdf.SetFont(fontName, "", 8)
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
		pdf.SetFont(fontName, "", 9)
		pdf.Cell(0, 5, fmt.Sprintf("... 还有 %d 条事件（已截断）", len(data.Timeline.Events)-maxRows))
	}
	pdf.Ln(6)
}

func renderTopAlerts(pdf *fpdf.Fpdf, data *ReportData) {
	if data.Timeline == nil || len(data.Timeline.Events) == 0 {
		return
	}
	pdf.AddPage()
	pdf.SetFont(fontName, "", 13)
	pdf.Cell(0, 8, "4. Top 严重告警 (Top Severity Alerts)")
	pdf.Ln(9)

	// 收集 alert 事件并按 severity 排序（severity 越大越严重）
	alerts := make([]models.TimelineEvent, 0)
	for _, e := range data.Timeline.Events {
		if e.Kind == models.TimelineEventAlert {
			alerts = append(alerts, e)
		}
	}
	if len(alerts) == 0 {
		pdf.SetFont(fontName, "", 10)
		pdf.Cell(0, 5, "(窗口内无告警)")
		return
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return alerts[i].Severity > alerts[j].Severity
	})
	topN := 5
	if len(alerts) < topN {
		topN = len(alerts)
	}
	alerts = alerts[:topN]

	// 表头
	pdf.SetFont(fontName, "", 9)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(20, 6, "严重度", "1", 0, "C", true, 0, "")
	pdf.CellFormat(35, 6, "时间", "1", 0, "L", true, 0, "")
	pdf.CellFormat(20, 6, "子类型", "1", 0, "L", true, 0, "")
	pdf.CellFormat(100, 6, "标题", "1", 1, "L", true, 0, "")

	pdf.SetFont(fontName, "", 8)
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
	pdf.SetFont(fontName, "", 8)
	pdf.SetTextColor(150, 150, 150)
	pdf.Cell(0, 5, "本报告由系统自动生成（alerts / tickets / status history），结合工程团队评审进行根因分析。")
}

// safe 字符串清洗：去除控制字符（fpdf 不支持），保留所有可见字符。
// 中文支持由中文字体（fontName）保障。
func safe(s string) string {
	if s == "" {
		return "-"
	}
	var b strings.Builder
	for _, r := range s {
		if r < 32 {
			b.WriteByte(' ')
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
