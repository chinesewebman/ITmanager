package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"network-monitor-platform/internal/postmortem"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostmortemService 资产复盘报告 service
//
// 职责：
//   - 复用 DiagnosticService.GetTimeline 拉取数据（避免重复 SQL 聚合）
//   - 单独查 assets 表取 IPv4/IPv6（DiagnosticAsset 不含 IP 字段）
//   - 委托 postmortem.Renderer 生成 PDF bytes.Buffer
//   - ctx 透传
type PostmortemService struct {
	db       *gorm.DB
	diag     *DiagnosticService
	renderer postmortem.Renderer
}

// NewPostmortemService 构造 PostmortemService
func NewPostmortemService(db *gorm.DB, diag *DiagnosticService) *PostmortemService {
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

// GenerateReport 生成资产复盘 PDF 报告
//
// 返回：
//   - ErrNotFound 当资产不存在
//   - 非 nil error 当生成失败
func (s *PostmortemService) GenerateReport(ctx context.Context, assetID uuid.UUID, params GenerateReportParams) (bytes.Buffer, *postmortem.ReportData, error) {
	var buf bytes.Buffer
	var data *postmortem.ReportData

	// 1) 拉时间线（复用 A-1 diagnostic）
	filter := DiagnosticFilter{Days: params.Days, Limit: params.Limit}
	timeline, err := s.diag.GetTimeline(ctx, assetID, filter)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return buf, nil, fmt.Errorf("%w: asset_id=%s", ErrNotFound, assetID)
		}
		return buf, nil, fmt.Errorf("拉取时间线: %w", err)
	}

	// 2) 单独查 assets 表取 IP
	ipAddr, err := s.fetchIP(ctx, assetID)
	if err != nil {
		// IP 查不到不阻塞 — 留空即可
		ipAddr = ""
	}

	// 3) 组装 ReportData
	data = &postmortem.ReportData{
		GeneratedAt: time.Now().UTC(),
		WindowDays:  effectiveDays(params.Days),
		IPAddress:   ipAddr,
		Timeline:    timeline,
	}

	// 4) 渲染 PDF
	buf, err = s.renderer.Render(data)
	if err != nil {
		return buf, data, fmt.Errorf("渲染 PDF: %w", err)
	}
	return buf, data, nil
}

// fetchIP 从 asset_networks 表取 IPv4（优先）/IPv6
// 资产可能有多个网卡，取第一个非空的 IPv4，否则第一个 IPv6
func (s *PostmortemService) fetchIP(ctx context.Context, assetID uuid.UUID) (string, error) {
	type ipRow struct {
		IPv4Address string
		IPv6Address string
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
