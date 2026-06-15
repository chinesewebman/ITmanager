package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// API Key 默认配置
const (
	defaultAPIKeyRateLimit = 1000
	defaultAPIPermission   = "read"
)

// generateAPIKeyPrefix 生成 8 字符前缀（用于 UI 列表展示）
func generateAPIKeyPrefix() string {
	bytes := make([]byte, 4)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// generateAPIKey 返回 (完整 key, 前缀)。完整 key 仅在创建时返回一次。
func generateAPIKey() (string, string) {
	bytes := make([]byte, 24)
	_, _ = rand.Read(bytes)
	key := hex.EncodeToString(bytes)
	prefix := generateAPIKeyPrefix()
	return prefix + "-" + key, prefix
}

// hashAPIKey 对 key 做 SHA-256 哈希（用于存储）
func hashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// CreateAPIKey 创建 API Key
func CreateAPIKey(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Permissions []string `json:"permissions"`
		IPWhitelist []string `json:"ip_whitelist"`
		RateLimit   int      `json:"rate_limit"`
		ExpiresAt   *string  `json:"expires_at"` // 格式: "2026-12-31"
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请求参数错误")
		return
	}

	userID := c.GetString("user_id")
	uid, err := uuid.Parse(userID)
	if err != nil {
		apierr.BadRequest(c, "无效的用户ID")
		return
	}

	// 解析过期时间
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse("2006-01-02", *req.ExpiresAt)
		if err != nil {
			apierr.BadRequest(c, "expires_at 格式错误，应为 YYYY-MM-DD")
			return
		}
		expiresAt = &t
	}

	// 默认值
	rateLimit := req.RateLimit
	if rateLimit == 0 {
		rateLimit = defaultAPIKeyRateLimit
	}
	permissions := req.Permissions
	if permissions == nil {
		permissions = []string{defaultAPIPermission}
	}
	ipWhitelist := req.IPWhitelist
	if ipWhitelist == nil {
		ipWhitelist = []string{}
	}

	rawKey, prefix := generateAPIKey()
	apiKey := models.APIKey{
		UserID:      uid,
		Name:        req.Name,
		KeyHash:     hashAPIKey(rawKey),
		Prefix:      prefix,
		Permissions: permissions,
		IPWhitelist: ipWhitelist,
		RateLimit:   rateLimit,
		ExpiresAt:   expiresAt,
		Status:      "active",
	}

	if err := database.DB.Create(&apiKey).Error; err != nil {
		apierr.Internal(c, "创建 API Key 失败", err)
		return
	}

	// rawKey 仅此一次返回给客户端
	c.JSON(201, gin.H{
		"code": 0,
		"data": gin.H{
			"id":          apiKey.ID,
			"name":        apiKey.Name,
			"api_key":     rawKey,
			"prefix":      prefix,
			"permissions": apiKey.Permissions,
			"rate_limit":  apiKey.RateLimit,
			"expires_at":  apiKey.ExpiresAt,
			"created_at":  apiKey.CreatedAt,
		},
		"message": "API Key 已创建，请妥善保管，只显示一次",
	})
}

// ListAPIKeys 获取当前用户的 API Key 列表（隐藏哈希）
func ListAPIKeys(c *gin.Context) {
	userID := c.GetString("user_id")
	uid, _ := uuid.Parse(userID)

	var keys []models.APIKey
	if err := database.DB.Where("user_id = ?", uid).Order("created_at DESC").Find(&keys).Error; err != nil {
		apierr.Internal(c, "获取 API Key 列表失败", err)
		return
	}

	result := make([]gin.H, len(keys))
	for i, key := range keys {
		result[i] = gin.H{
			"id":           key.ID,
			"name":         key.Name,
			"prefix":       key.Prefix,
			"permissions":  key.Permissions,
			"ip_whitelist": key.IPWhitelist,
			"rate_limit":   key.RateLimit,
			"status":       key.Status,
			"expires_at":   key.ExpiresAt,
			"last_used_at": key.LastUsedAt,
			"created_at":   key.CreatedAt,
		}
	}

	c.JSON(200, gin.H{
		"code": 0,
		"data": result,
	})
}

// DeleteAPIKey 物理删除 API Key
func DeleteAPIKey(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	var key models.APIKey
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&key).Error; err != nil {
		apierr.NotFound(c, "API Key 不存在")
		return
	}

	if err := database.DB.Delete(&key).Error; err != nil {
		apierr.Internal(c, "删除 API Key 失败", err)
		return
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// RevokeAPIKey 吊销（不删除，置 status=revoked）
func RevokeAPIKey(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	var key models.APIKey
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&key).Error; err != nil {
		apierr.NotFound(c, "API Key 不存在")
		return
	}

	key.Status = "revoked"
	if err := database.DB.Save(&key).Error; err != nil {
		apierr.Internal(c, "吊销 API Key 失败", err)
		return
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "API Key 已吊销",
	})
}
