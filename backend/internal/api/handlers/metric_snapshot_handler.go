// metric_snapshots HTTP handler：批量插入 + 时序查询
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

// MetricSnapshotHandler 指标快照 HTTP 接口
type MetricSnapshotHandler struct {
	svc *service.MetricSnapshotService
}

func NewMetricSnapshotHandler(svc *service.MetricSnapshotService) *MetricSnapshotHandler {
	return &MetricSnapshotHandler{svc: svc}
}

// BulkInsert POST /api/metric-snapshots  body: [{asset_id,key,value,ts}, ...]
func (h *MetricSnapshotHandler) BulkInsert(c *gin.Context) {
	var snaps []models.MetricSnapshot
	if err := c.ShouldBindJSON(&snaps); err != nil {
		apierr.BadRequest(c, "请求体非法: "+err.Error())
		return
	}
	if err := h.svc.BulkInsert(c.Request.Context(), snaps); err != nil {
		if isInvalidInputErr(err) {
			apierr.BadRequest(c, err.Error())
			return
		}
		if isTooManyErr(err) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": gin.H{"count": len(snaps)}})
}

// Query GET /api/metric-snapshots?asset_id=&key=&from=&to=&limit=
func (h *MetricSnapshotHandler) Query(c *gin.Context) {
	filter := service.QueryFilter{
		AssetID: c.Query("asset_id"),
		Key:     c.Query("key"),
	}
	if s := c.Query("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			apierr.BadRequest(c, "from 时间格式非法")
			return
		}
		filter.From = t
	}
	if s := c.Query("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			apierr.BadRequest(c, "to 时间格式非法")
			return
		}
		filter.To = t
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			apierr.BadRequest(c, "limit 必须是整数")
			return
		}
		filter.Limit = n
	}

	items, err := h.svc.Query(c.Request.Context(), filter)
	if err != nil {
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// Latest GET /api/metric-snapshots/latest?asset_id=&key=&n=
func (h *MetricSnapshotHandler) Latest(c *gin.Context) {
	assetID := c.Query("asset_id")
	key := c.Query("key")
	if assetID == "" || key == "" {
		apierr.BadRequest(c, "asset_id 和 key 必填")
		return
	}
	n := 60
	if s := c.Query("n"); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil {
			apierr.BadRequest(c, "n 必须是整数")
			return
		}
		n = parsed
	}
	items, err := h.svc.LatestByAssetAndKey(c.Request.Context(), assetID, key, n)
	if err != nil {
		if isInvalidInputErr(err) {
			apierr.BadRequest(c, err.Error())
			return
		}
		apierr.Internal(c, "服务器内部错误", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// helper：service 包 sentinel error 识别
func isInvalidInputErr(err error) bool {
	return err != nil && (err == service.ErrInvalidInput || containsErr(err, service.ErrInvalidInput.Error()))
}

func isTooManyErr(err error) bool {
	return err != nil && (err == service.ErrTooManyItems || containsErr(err, service.ErrTooManyItems.Error()))
}

func containsErr(err error, sub string) bool {
	s := err.Error()
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
