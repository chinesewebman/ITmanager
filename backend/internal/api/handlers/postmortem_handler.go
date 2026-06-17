package handlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PostmortemHandler 资产复盘 PDF HTTP 处理器
type PostmortemHandler struct {
	svc *service.PostmortemService
}

// NewPostmortemHandler 构造 PostmortemHandler
func NewPostmortemHandler(svc *service.PostmortemService) *PostmortemHandler {
	return &PostmortemHandler{svc: svc}
}

// DownloadReport godoc
// @Summary      下载资产复盘 PDF 报告
// @Description  聚合 alerts / tickets / status 历史生成 1-2 页 PDF 复盘报告
// @Description  days 默认 30，上限 365
// @Tags         postmortem
// @Produce      application/pdf
// @Security     BearerAuth
// @Param        id    path      string  true   "资产 UUID"
// @Param        days  query     int     false  "查询窗口（天）默认 30，最大 365"
// @Param        limit query     int     false  "事件数上限，默认 200，最大 1000"
// @Success      200   {file}    file    "application/pdf"
// @Failure      400   {object}  apierr.ErrorResponse
// @Failure      401   {object}  apierr.ErrorResponse
// @Failure      404   {object}  apierr.ErrorResponse
// @Failure      500   {object}  apierr.ErrorResponse
// @Router       /postmortem/assets/{id}/report [get]
func (h *PostmortemHandler) DownloadReport(c *gin.Context) {
	idStr := c.Param("id")
	assetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.BadRequest(c, "资产 ID 格式错误")
		return
	}

	params := service.GenerateReportParams{}
	if v := c.Query("days"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 0 {
			apierr.BadRequest(c, "days 参数必须是正整数")
			return
		}
		params.Days = d
	}
	if v := c.Query("limit"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 0 {
			apierr.BadRequest(c, "limit 参数必须是正整数")
			return
		}
		params.Limit = l
	}

	// 30s 超时（PDF 生成是 CPU + DB IO）
	ctx, cancel := contextWithTimeout(c.Request.Context(), 30)
	defer cancel()

	data, err := h.svc.GenerateReport(ctx, c.Writer, assetID, params)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "生成复盘报告失败", err)
		return
	}

	// 文件名格式: postmortem_<asset>_<YYYYMMDD>.pdf
	filename := fmt.Sprintf("postmortem_%s_%s.pdf",
		sanitizeFilename(data.Timeline.Asset.Name),
		time.Now().UTC().Format("20060102"),
	)

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("X-Content-Type-Options", "nosniff")
	// 注意：PDF 已通过 c.Writer 流式写入，无需再 c.Data()
	// gin 会在 handler return 时自动 flush
}

// contextWithTimeout 给 ctx 加超时（秒）。若 ctx 已有 Deadline 则不覆盖。
func contextWithTimeout(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if _, has := parent.Deadline(); has {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

// sanitizeFilename 文件名清洗：去除路径分隔符和特殊字符
func sanitizeFilename(s string) string {
	if s == "" {
		return "asset"
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		} else if r == ' ' {
			out = append(out, '_')
		}
	}
	res := string(out)
	if len(res) > 50 {
		res = res[:50]
	}
	if res == "" {
		return "asset"
	}
	return res
}
