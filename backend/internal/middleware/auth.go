package middleware

import (
	"strings"
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/apikey"
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

// verifyAPIKey Verify API key（C-F6：常量时间比较防时序侧信道）
func verifyAPIKey(key, keyHash string) bool {
	return apikey.Verify(key, config.Get().Auth.APIKeyPepper, keyHash)
}

// hashAPIKeyForMiddleware middleware 内调用的 HMAC 包装
func hashAPIKeyForMiddleware(key string) string {
	return apikey.Hash(key, config.Get().Auth.APIKeyPepper)
}

// AuthMiddleware JWT or API Key authentication middleware
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// C-F5: 优先从 Authorization header 读 Bearer token（CLI / API client），
		// 否则从 cookie 读（浏览器场景）
		authHeader := c.GetHeader("Authorization")
		var tokenString string
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "X-API-Key ") {
				apiKey := strings.TrimPrefix(authHeader, "X-API-Key ")
				if apiKey != "" {
					handleAPIKeyAuth(c, apiKey)
					return
				}
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			} else {
				apierr.Unauthorized(c, "Authorization 格式错误，支持 Bearer token 或 X-API-Key")
				c.Abort()
				return
			}
		} else {
			// fallback: 读 cookie（C-F5 httpOnly cookie 场景）
			if cookie, err := c.Cookie("auth_token"); err == nil && cookie != "" {
				tokenString = cookie
			}
		}

		if tokenString == "" {
			apierr.Unauthorized(c, "请求头缺少 Authorization 或 auth_token cookie")
			c.Abort()
			return
		}

		claims, err := VerifyToken(tokenString)
		if err != nil {
			apierr.Unauthorized(c, "Token 无效或已过期")
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
	keyHash := hashAPIKeyForMiddleware(apiKey)

	if err := database.DB.Where("key_hash = ? AND status = ?", keyHash, "active").First(&key).Error; err != nil {
		apierr.Unauthorized(c, "API Key 无效或已禁用")
		c.Abort()
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		apierr.Unauthorized(c, "API Key 已过期")
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
			apierr.Forbidden(c, "IP地址不在允许列表中")
			c.Abort()
			return
		}
	}

	// Update last used time (P1-审计: 异步批量写，避免每次 API key 调用都同步写 DB)
	// 写放大问题：高 QPS API key 调用会产生 N 次 UPDATE，拖慢主请求路径
	// 改用 in-memory buffer + background flush（30s 间隔或 100 条阈值）
	apiKeyTracker.Track(key.ID)

	// Get user info
	var user models.User
	if err := database.DB.First(&user, "id = ?", key.UserID).Error; err != nil {
		apierr.Unauthorized(c, "API Key 关联的用户不存在")
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

		apierr.Forbidden(c, "权限不足")
		c.Abort()
	}
}
