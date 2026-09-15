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

// fetchIP 从 asset_networks 表取主 IP 给报告头：v4 优先, 否则 v6。
//
// M63: 判据不再在这里各写一份 —— 委托到 `primaryIP`（与资产列表/详情注入 `ip_address`
// 是同一份实现、同一批数据的同一个地址）。这里只保留「报告头要的是字符串、空值编码成 ""」
// 这一层差异（renderer 的 ReportData.IPAddress 是 string，没有 null）。
//
// 只 Select 两列（不拉整行）是刻意的：报告只要这个地址，网卡的外键/描述与本处无关。
// 注意列名由 **Go 字段名**推导（`IPv6Address` → `ipv6_address`），与 JSON tag 的
// `ipv_address` 无关 —— 后者只影响前端 payload。
func (s *PostmortemService) fetchIP(ctx context.Context, assetID uuid.UUID) (string, error) {
	var networks []models.AssetNetwork
	if err := s.db.WithContext(ctx).
		Table("asset_networks").
		Select("ipv4_address, ipv6_address").
		Where("asset_id = ?", assetID).
		Order("created_at ASC, id ASC").
		Scan(&networks).Error; err != nil {
		return "", err
	}
	if ip := primaryIP(networks); ip != nil {
		return *ip, nil
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
