package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/postmortem"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimelineFetcher 是 GetTimeline 的最小接口（解耦 PostmortemService → DiagnosticService，方便单测 mock）
type TimelineFetcher interface {
	GetTimeline(ctx context.Context, assetID uuid.UUID, filter DiagnosticFilter) (*models.DiagnosticTimeline, error)
}

// PostmortemService 资产复盘报告 service
//
// 职责：
//   - 复用 DiagnosticService.GetTimeline 拉取数据（避免重复 SQL 聚合）
//   - 单独查 assets 表取 IPv4/IPv6（DiagnosticAsset 不含 IP 字段）
//   - 委托 postmortem.Renderer 流式生成 PDF 到 io.Writer（不驻留内存）
//   - ctx 透传
type PostmortemService struct {
	db       *gorm.DB
	diag     TimelineFetcher
	renderer postmortem.Renderer
}

// NewPostmortemService 构造 PostmortemService
// diag 接受 *DiagnosticService 或 mock impl（实现 TimelineFetcher 即可）
func NewPostmortemService(db *gorm.DB, diag TimelineFetcher) *PostmortemService {
	return &PostmortemService{
		db:       db,
		diag:     diag,
		renderer: postmortem.NewFpdfRenderer(),
	}
}

// SetRenderer 注入自定义 renderer（用于单测 mock）
func (s *PostmortemService) SetRenderer(r postmortem.Renderer) {
	s.renderer = r
}

// GenerateReportParams 生成报告参数
type GenerateReportParams struct {
	// Days 时间窗口（天），0=默认 30，最大 365
	Days int
	// Limit 事件数上限，0=默认 200，最大 1000
	Limit int
}

// GenerateReport 流式生成资产复盘 PDF 报告到 w（handler 传 gin.Response）。
//
// 返回：
//   - ErrNotFound 当资产不存在
//   - 非 nil error 当生成失败
//   - 渲染失败时已写入 w 的部分可能不完整
func (s *PostmortemService) GenerateReport(ctx context.Context, w io.Writer, assetID uuid.UUID, params GenerateReportParams) (*postmortem.ReportData, error) {
	// 1) 拉时间线（复用 A-1 diagnostic）
	filter := DiagnosticFilter{Days: params.Days, Limit: params.Limit}
	timeline, err := s.diag.GetTimeline(ctx, assetID, filter)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: asset_id=%s", ErrNotFound, assetID)
		}
		return nil, fmt.Errorf("拉取时间线: %w", err)
	}

	// 2) 单独查 assets 表取 IP
	ipAddr, err := s.fetchIP(ctx, assetID)
	if err != nil {
		// IP 查不到不阻塞 — 留空即可
		ipAddr = ""
	}

	// 3) 组装 ReportData
	data := &postmortem.ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  effectiveDays(params.Days),
		IPAddress:   ipAddr,
		Timeline:    timeline,
	}

	// 4) 流式渲染 PDF
	if err := s.renderer.Render(data, w); err != nil {
		return data, fmt.Errorf("渲染 PDF: %w", err)
	}
	return data, nil
}

// fetchIP 从 asset_networks 表取 IPv4（优先）/IPv6
// 资产可能有多个网卡，取第一个非空的 IPv4，否则第一个 IPv6
func (s *PostmortemService) fetchIP(ctx context.Context, assetID uuid.UUID) (string, error) {
	type ipRow struct {
		IPv4Address string `gorm:"column:ipv4_address"`
		IPv6Address string `gorm:"column:ipv_address"` // 真实列名是 ipv_address (见 models.AssetNetwork)
	}
	var rows []ipRow
	if err := s.db.WithContext(ctx).
		Table("asset_networks").
		Select("ipv4_address, ipv_address").
		Where("asset_id = ?", assetID).
		Order("created_at ASC").
		Scan(&rows).Error; err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.IPv4Address != "" {
			return r.IPv4Address, nil
		}
	}
	for _, r := range rows {
		if r.IPv6Address != "" {
			return r.IPv6Address, nil
		}
	}
	return "", nil
}

func effectiveDays(d int) int {
	if d <= 0 {
		return 30
	}
	if d > 365 {
		return 365
	}
	return d
}
