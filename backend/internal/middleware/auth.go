package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT 声明
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT Token
func GenerateToken(userID, username, role string) (string, error) {
	cfg := config.Get()
	expire := time.Now().Add(time.Duration(cfg.Auth.JWT.Expire) * time.Second)

	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expire),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "network-monitor-platform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Auth.JWT.Secret))
}

// VerifyToken 验证 JWT Token
func VerifyToken(tokenString string) (*Claims, error) {
	cfg := config.Get()

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.Auth.JWT.Secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// hashAPIKey Hash API key for storage
func hashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// verifyAPIKey Verify API key
func verifyAPIKey(key, keyHash string) bool {
	return hashAPIKey(key) == keyHash
}

// AuthMiddleware JWT or API Key authentication middleware
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "请求头缺少 Authorization",
			})
			c.Abort()
			return
		}

		// Check for API Key format: X-API-Key: <key>
		if strings.HasPrefix(authHeader, "X-API-Key ") {
			apiKey := strings.TrimPrefix(authHeader, "X-API-Key ")
			if apiKey != "" {
				handleAPIKeyAuth(c, apiKey)
				return
			}
		}

		// Check for Bearer token format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Authorization 格式错误，支持 Bearer token 或 X-API-Key",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims, err := VerifyToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Token 无效或已过期",
			})
			c.Abort()
			return
		}

		// 将用户信息存入上下文
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// handleAPIKeyAuth Handle API Key authentication
func handleAPIKeyAuth(c *gin.Context, apiKey string) {
	// Find API key in database
	var key models.APIKey
	keyHash := hashAPIKey(apiKey)

	if err := database.DB.Where("key_hash = ? AND status = ?", keyHash, "active").First(&key).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "API Key 无效或已禁用",
		})
		c.Abort()
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "API Key 已过期",
		})
		c.Abort()
		return
	}

	// Check IP whitelist if configured
	if len(key.IPWhitelist) > 0 {
		clientIP := c.ClientIP()
		ipAllowed := false
		for _, ip := range key.IPWhitelist {
			if ip == clientIP {
				ipAllowed = true
				break
			}
		}
		if !ipAllowed {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "IP地址不在允许列表中",
			})
			c.Abort()
			return
		}
	}

	// Update last used time
	database.DB.Model(&key).Update("last_used_at", time.Now())

	// Get user info
	var user models.User
	if err := database.DB.First(&user, "id = ?", key.UserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "API Key 关联的用户不存在",
		})
		c.Abort()
		return
	}

	// Set context values
	c.Set("user_id", key.UserID.String())
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set("api_key_id", key.ID.String())

	c.Next()
}

// RequireRole 角色权限中间件
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole := c.GetString("role")

		for _, role := range roles {
			if userRole == role {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "权限不足",
		})
		c.Abort()
	}
}
