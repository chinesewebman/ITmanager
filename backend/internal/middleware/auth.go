package middleware

import (
	"net/http"
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
	if !ipAllowedByWhitelist(key.IPWhitelist, c.ClientIP()) {
		apierr.Forbidden(c, "IP地址不在允许列表中")
		c.Abort()
		return
	}

	// 校验 API Key 自身的 scope（缺陷 D-7：原先只按关联用户的 role 放行，
	// key.Permissions 从不读取 → 只读 Key 也能写）。写操作端点不得靠 Key 越权。
	if !apiKeyAllows(key.Permissions, c.Request.Method) {
		apierr.Forbidden(c, "API Key 权限不足")
		c.Abort()
		return
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

	// 用户被禁用后，其 API Key 必须立即失效（否则 Key 只要没过期就能永久用，
	// 与登录路径的 inactive 拦截不一致 —— 见审计 M-5）。
	if user.Status == "inactive" {
		apierr.Unauthorized(c, "API Key 关联的用户已被禁用")
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

// ipAllowedByWhitelist 判断客户端 IP 是否命中 API Key 的 ip_whitelist。
//
// 条目可以是裸 IP（10.20.31.7）或 CIDR（10.20.0.0/16），解析与匹配复用 G-7 的
// parseTrustedNets/isTrustedPeer（trusted_proxy_warn.go），口径与 server.trusted_proxies
// 完全一致——包括裸 IP 等价 /32 或 /128、IPv4-mapped IPv6 归一化、脏条目跳过。
//
// 历史缺陷 G-11：这里原先用 `entry == clientIP` 精确比较字符串，而写入侧
// validateIPWhitelist 接受 CIDR 写法 → 填 CIDR 的白名单永不命中，表现为「莫名 403」。
//
// 空名单 = 不限制（返回 true），与调用点原先的 `len(...) > 0` 守卫语义等价。
// 注意 isTrustedPeer 对空表返回 false，与这里的期望相反，故必须先判空。
func ipAllowedByWhitelist(list []string, clientIP string) bool {
	if len(list) == 0 {
		return true
	}
	return isTrustedPeer(clientIP, parseTrustedNets(list))
}

// apiKeyAllows 按 API Key 的 permissions 判定某个 HTTP 方法是否放行。
//
// 语义：read 只读；write 可读写；admin 等同 write（保留位）。
// permissions 为空或全是未知值时按 read 处理（fail-safe：绝不因配置缺失而放行写）。
func apiKeyAllows(perms models.StringList, method string) bool {
	hasRead, hasWrite := false, false
	for _, p := range perms {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "read":
			hasRead = true
		case "write", "admin":
			hasWrite = true
		}
	}
	if !hasRead && !hasWrite {
		hasRead = true
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return hasRead || hasWrite
	default:
		return hasWrite
	}
}

// RejectAPIKeyAuth 拒绝以 API Key 身份访问本端点（凭据铸造 / 凭据变更类操作专用）。
//
// 长期凭据不得自我复制：能力矩阵按「关联用户的角色」放行（handleAPIKeyAuth 里
// c.Set("role", user.Role)），write scope 的 Key 挂在 admin 账号上就能铸造新 Key ——
// 旧 Key 被吊销后新 Key 依然有效，形成持久化后门（docs/FIX-PLAN-AUTHZ-LEFTOVER.md S-2）。
// 改密同理：泄露的 write Key 可直接改掉所属账号的密码，吊销 Key 撤销不了已改的密码。
// 这类操作必须用交互式登录会话（JWT / httpOnly cookie）。
//
// 依赖 AuthMiddleware 先运行（api_key_id 由 handleAPIKeyAuth 设置）；未挂 AuthMiddleware
// 的路由上该键为空 → 退化为放行，故只能挂在受保护路由上。
func RejectAPIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("api_key_id") != "" {
			apierr.Forbidden(c, "API Key 不能执行该操作,请使用登录会话")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireRole 角色权限中间件。
//
// Deprecated: 生产路由一律用 RequireCapability（能力矩阵，见 roles.go）。
// 本函数按角色字面量精确比较，遗留别名（operator/viewer）不会命中，也别指望它做词表归一；
// 仅为既有测试保留，新代码不得使用。
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
