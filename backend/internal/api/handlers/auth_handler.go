package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// generateAPIKeyPrefix 生成 API key 的前缀
func generateAPIKeyPrefix() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// generateAPIKey 生成完整的 API key
func generateAPIKey() (string, string) {
	bytes := make([]byte, 24)
	rand.Read(bytes)
	key := hex.EncodeToString(bytes)
	prefix := generateAPIKeyPrefix()
	return prefix + "-" + key, prefix
}

// hashAPIKey Hash API key for storage
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
		ExpiresAt   *string  `json:"expires_at"` // Format: "2025-12-31"
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	userID := c.GetString("user_id")
	uid, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// Generate API key
	rawKey, prefix := generateAPIKey()
	keyHash := hashAPIKey(rawKey)

	// Parse expiration
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse("2006-01-02", *req.ExpiresAt)
		if err == nil {
			expiresAt = &t
		}
	}

	// Set defaults
	if req.RateLimit == 0 {
		req.RateLimit = 1000
	}
	if req.Permissions == nil {
		req.Permissions = []string{"read"}
	}
	if req.IPWhitelist == nil {
		req.IPWhitelist = []string{}
	}

	apiKey := models.APIKey{
		UserID:      uid,
		Name:        req.Name,
		KeyHash:     keyHash,
		Prefix:      prefix,
		Permissions: req.Permissions,
		IPWhitelist: req.IPWhitelist,
		RateLimit:   req.RateLimit,
		ExpiresAt:   expiresAt,
		Status:      "active",
	}

	if err := database.DB.Create(&apiKey).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建 API Key 失败",
		})
		return
	}

	// Return raw key only once - it cannot be retrieved again
	c.JSON(http.StatusCreated, gin.H{
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

// ListAPIKeys 获取用户的 API Key 列表
func ListAPIKeys(c *gin.Context) {
	userID := c.GetString("user_id")
	uid, _ := uuid.Parse(userID)

	var keys []models.APIKey
	if err := database.DB.Where("user_id = ?", uid).Order("created_at DESC").Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取 API Key 列表失败",
		})
		return
	}

	// Hide key hashes
	result := make([]gin.H, len(keys))
	for i, key := range keys {
		result[i] = gin.H{
			"id":          key.ID,
			"name":        key.Name,
			"prefix":      key.Prefix,
			"permissions": key.Permissions,
			"ip_whitelist": key.IPWhitelist,
			"rate_limit":  key.RateLimit,
			"status":      key.Status,
			"expires_at":  key.ExpiresAt,
			"last_used_at": key.LastUsedAt,
			"created_at":  key.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": result,
	})
}

// DeleteAPIKey 删除 API Key
func DeleteAPIKey(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	var key models.APIKey
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&key).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "API Key 不存在",
		})
		return
	}

	database.DB.Delete(&key)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// RevokeAPIKey 吊销 API Key
func RevokeAPIKey(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	var key models.APIKey
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&key).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "API Key 不存在",
		})
		return
	}

	key.Status = "revoked"
	database.DB.Save(&key)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "API Key 已吊销",
	})
}

// Login 登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请输入用户名和密码",
		})
		return
	}

	// 查找用户
	var user models.User
	if err := database.DB.First(&user, "username = ?", req.Username).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	// 检查用户状态
	if user.Status == "inactive" {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "账户已被禁用",
		})
		return
	}

	// 检查账户是否被锁定
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "账户已被锁定，请稍后再试",
		})
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// 记录失败次数
		user.FailedLogin++
		if user.FailedLogin >= 5 {
			lockedUntil := time.Now().Add(30 * time.Minute)
			user.LockedUntil = &lockedUntil
		}
		database.DB.Save(&user)

		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	// 重置失败次数
	user.FailedLogin = 0
	user.LockedUntil = nil

	// 记录登录时间
	now := time.Now()
	user.LastLogin = &now
	user.LastLoginIP = c.ClientIP()
	database.DB.Save(&user)

	// 生成 Token
	token, err := middleware.GenerateToken(user.ID.String(), user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成 Token 失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"token": token,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Username,
				"nickname": user.Nickname,
				"email":    user.Email,
				"role":     user.Role,
				"avatar":   user.Avatar,
			},
		},
	})
}

// Logout 登出
func Logout(c *gin.Context) {
	// JWT 无状态，登出直接返回成功
	// 如需实现黑名单，可以将 token 加入 Redis
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "登出成功",
	})
}

// GetCurrentUser 获取当前用户信息
func GetCurrentUser(c *gin.Context) {
	userID := c.GetString("user_id")

	var user models.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"nickname": user.Nickname,
			"email":    user.Email,
			"phone":    user.Phone,
			"avatar":   user.Avatar,
			"role":     user.Role,
			"status":   user.Status,
		},
	})
}

// ChangePassword 修改密码
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请输入旧密码和新密码",
		})
		return
	}

	userID := c.GetString("user_id")
	var user models.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "旧密码错误",
		})
		return
	}

	// 加密新密码
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	user.PasswordHash = string(hash)
	database.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "密码修改成功",
	})
}
