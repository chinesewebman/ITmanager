package handlers

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// safeCSV 防 Excel 公式注入（C-F7）：对 = + - @ 	 \r 开头字段加前导单引号
func safeCSV(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '	', '\r':
		return "'" + s
	}
	return s
}

// maxBulkRetireIDs 单次批量退役的 id 上限（与 /alerts/bulk-* 同一防 DoS 口径）。
const maxBulkRetireIDs = 1000

// AssetHandler 资产相关 HTTP handler
type AssetHandler struct {
	svc service.AssetService
}

// NewAssetHandler 构造函数（依赖注入，便于测试时 mock service）
func NewAssetHandler(svc service.AssetService) *AssetHandler {
	return &AssetHandler{svc: svc}
}

// ListAssets 资产列表（分页+筛选）
func (h *AssetHandler) ListAssets(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	items, total, err := h.svc.List(c.Request.Context(), service.AssetFilter{
		Keyword:   c.Query("keyword"),
		Status:    c.Query("status"),
		AssetType: c.Query("type"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		apierr.Internal(c, "获取资产列表失败", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": items,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// GetAsset 资产详情（含网络接口）
func (h *AssetHandler) GetAsset(c *gin.Context) {
	asset, networks, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "获取资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"asset":    asset,
			"networks": networks,
		},
	})
}

// CreateAsset 创建资产
func (h *AssetHandler) CreateAsset(c *gin.Context) {
	var asset models.Asset
	if err := c.ShouldBindJSON(&asset); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	if err := h.svc.Create(c.Request.Context(), &asset); err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			apierr.Conflict(c, "资产已存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "资产名称不能为空")
			return
		}
		apierr.Internal(c, "创建资产失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": asset,
	})
}

// UpdateAsset 部分更新
func (h *AssetHandler) UpdateAsset(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	// M42: 规范化 jsonb 入参 (G-21, docs/FIX-PLAN-ASSET-JSONB.md §2.3 R-1).
	// service.Update 走 db.Updates(map) 不经过 BeforeSave 钩子,
	// 非法 jsonb 入参会在 PG 触发 22P02 (500) 或写 NULL 破坏回填不变量.
	if err := normalizeJSONBFields(updates); err != nil {
		apierr.BadRequest(c, err.Error())
		return
	}
	// M63 (T-76; brief 里写作 T-75 —— 那个号在本仓已被 M61 的全站白屏与 M62 的 IPv6 锚点各占用
	// 一次，见 M63-completion-report.md「trap 编号冲突」): 剥掉虚拟字段 `ip_address`。
	//
	// 它与 jsonb 那几列同源而不同因：`assets` 表里没有这一列，而 `db.Updates(map)`
	// **不丢弃**模型里不存在的键（GORM v1.30.0 的 callbacks/update.go：LookUpField 为 nil
	// 时照样 append Assignment）→ 生成 `SET "ip_address"=$n` → PG 42703 → 500。
	// 实测见 service 层 `TestM63_AssetService_Update_map含模型外列时GORM照发SET`；
	// 路由级见 `TestM63_UpdateAsset_带ip_address不产生该列的SQL_返200`。
	//
	// 为什么是「剥掉」而不是「写进 asset_networks」：本轮的 GET 投影让 `ip_address` 成为
	// **只读投影字段**（M63 只修读取侧）；写入侧（第一张网卡）是 G-Asset-NetworksPersist 的
	// 事，那一轮会把这里换成「取出来写网卡」。在那之前，前端若把表单里的 IP 回传上来，
	// 应当被忽略，而不是把请求打成 500。
	delete(updates, "ip_address")
	asset, err := h.svc.Update(c.Request.Context(), c.Param("id"), updates)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		apierr.Internal(c, "更新资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": asset,
	})
}

// DeleteAsset 删除
func (h *AssetHandler) DeleteAsset(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		apierr.Internal(c, "删除资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// B4: RetireAsset 软退役
// POST /api/assets/:id/retire
// body: { "reason": "设备下架" }
// - 把网卡 IP 移到 asset.last_known_ip*, 清空网卡 IP, status='retired'
// - 后续新设备可使用同一 IP 不冲突, 历史按 hostid 仍可查 (T-* future trap)
func (h *AssetHandler) RetireAsset(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}
	userID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		apierr.Unauthorized(c, "无效的用户凭证")
		return
	}
	asset, networks, err := h.svc.Retire(c.Request.Context(), c.Param("id"), req.Reason, userID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			// 重复退役 / 非 uuid id
			apierr.BadRequest(c, "无法退役（资产已退役或参数无效）")
			return
		}
		apierr.Internal(c, "退役资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "资产已软退役，IP 已释放",
		"data": gin.H{
			"asset":          asset,
			"networks":       networks,
			"released_ip4":   asset.LastKnownIP4,
			"released_ip6":   asset.LastKnownIP6,
			"retired_at":     asset.RetiredAt,
			"retired_reason": asset.RetiredReason,
		},
	})
}

// M58: BulkRetireAssets 批量软退役
// POST /api/assets/bulk-retire
// body: { "ids": ["<uuid>", ...], "reason": "..." }
//   - 单请求替代前端原本的 N 次串行 POST /assets/:id/retire（100 项 100 RTT → 1 RTT）
//   - 部分成功语义：succeeded 为成功 id 列表，failed 为 id → 失败文案；两者都是 200 正常响应
//   - 审计由 AuditLog 中间件按**请求**落一行（Path=/api/assets/bulk-retire 即 bulk 动作标记），
//     不会按 id 拆成 N 行 —— 这正是批量端点相对 N 次单条调用在审计侧的意义。
func (h *AssetHandler) BulkRetireAssets(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		apierr.BadRequest(c, "ids 不能为空")
		return
	}
	// 上限对齐 /alerts/bulk-*：单次批量防 DoS。
	if len(req.IDs) > maxBulkRetireIDs {
		apierr.BadRequest(c, "单次批量最多 1000 条")
		return
	}
	userID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		apierr.Unauthorized(c, "无效的用户凭证")
		return
	}
	succeeded, failed, err := h.svc.BulkRetire(c.Request.Context(), req.IDs, req.Reason, userID)
	if err != nil {
		apierr.Internal(c, "批量退役失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"succeeded": succeeded,
			"failed":    failed,
		},
	})
}

// B4: RestoreAsset 恢复软退役
// POST /api/assets/:id/restore
// - 把 last_known_ip* 写回网卡, 清空 retired_*, status='active'
func (h *AssetHandler) RestoreAsset(c *gin.Context) {
	asset, networks, err := h.svc.Restore(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "资产不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			apierr.BadRequest(c, "无法恢复（资产未处于退役状态）")
			return
		}
		apierr.Internal(c, "恢复资产失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "资产已恢复，IP 已写回",
		"data": gin.H{
			"asset":    asset,
			"networks": networks,
		},
	})
}

// ExportAssets 导出 CSV/JSON
func (h *AssetHandler) ExportAssets(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")

	// 导出走专用全量查询（不分页、不计数）。M32：原先是 List(Page:1, PageSize:500)，
	// 被 List 的 500 硬顶截断且总数被丢弃 → 静默产出不完整的对账文件。
	items, err := h.svc.ListAll(c.Request.Context())
	if err != nil {
		apierr.Internal(c, "导出资产失败", err)
		return
	}

	// X-Total-Count = 本次导出的数据行数（不含 CSV 表头）。
	// 取 len(items) 而不是另发一次 COUNT(*)：头值恒等于 body 行数，不存在
	// COUNT 与 SELECT 之间的竞态窗口。调用方用它自校验完整性。
	c.Header("X-Total-Count", strconv.Itoa(len(items)))

	if format == "csv" {
		// C-F7: 用 encoding/csv 正确转义（含逗号/换行/双引号）
		// 并对 = + - @ 	 \r 开头字段加前导单引号防止 Excel 公式注入（DDE）
		c.Header("Content-Disposition", `attachment; filename=assets.csv`)
		c.Header("X-Content-Type-Options", "nosniff")

		// 先写满内存缓冲再一次性发出：要么完整 200、要么 500 无 body。
		// 直接 csv.NewWriter(c.Writer) 是边写边发，中途失败会产出「半截 CSV + 已 200」，
		// 且原实现 _ = w.Write / defer w.Flush() 把错误全丢了 —— 那本身就是静默不完整。
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"ID", "Name", "Type", "Status"})
		for _, a := range items {
			row := []string{
				a.ID.String(),
				safeCSV(a.Name),
				safeCSV(a.AssetType),
				safeCSV(a.Status),
			}
			_ = w.Write(row)
		}
		w.Flush()
		if err := w.Error(); err != nil {
			apierr.Internal(c, "导出资产失败", err)
			return
		}
		// 显式设 CL：body 超过 net/http 的 2KB 缓冲时不会自动带 CL，会退化成 chunked，
		// 客户端就无法校验「收全了没有」。此处长度与即将写出的字节数恒等（同一 buf）。
		c.Header("Content-Length", strconv.Itoa(buf.Len()))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": items,
	})
}
