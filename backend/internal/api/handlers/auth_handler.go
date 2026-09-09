package handlers

import (
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// 登录失败锁定阈值
const maxFailedLoginAttempts = 5

// sanitizeAuditUsername 净化「登录尝试的用户名」再放进审计 context。
//
// 审计行是行式消费的（SIEM / 日志导出 / CSV），请求体里的换行或控制字符能把一行
// 伪造成多条记录（安全审计 F3 实测：username 里带 \n 与 \0 原样入库）。
// 按**字节**预算截断并留出余量：audit.go 还会按 100 字节硬截，若这里不先压到
// 100 字节以内，中文/emoji 会在那里被切成非法 UTF-8。
// 只影响审计展示值，不参与任何鉴权判定。
func sanitizeAuditUsername(s string) string {
	const maxBytes = 96 // < audit.go 的 100 字节截断，保证不触发二次截断
	out := make([]rune, 0, maxBytes/3)
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		size := utf8.RuneLen(r)
		if n+size > maxBytes {
			break
		}
		out = append(out, r)
		n += size
	}
	return string(out)
}

// Login 登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请输入用户名和密码")
		return
	}

	// 审计留痕：未认证请求的 AuditLog 只能拿到 IP，这里把**尝试的用户名**放进
	// context，让爆破/锁定可被追溯（FIX-PLAN-AUTHZ-CLOSURE.md §2 D-E）。
	// 含不存在的用户名——枚举尝试同样需要可见。密码绝不入 context/审计。
	c.Set("username", sanitizeAuditUsername(req.Username))

	// 查找用户
	var user models.User
	if err := database.DB.First(&user, "username = ?", req.Username).Error; err != nil {
		// 故意返回通用消息，避免暴露用户名是否存在
		apierr.Unauthorized(c, "用户名或密码错误")
		return
	}

	// 检查用户状态
	if user.Status == "inactive" {
		apierr.Forbidden(c, "账户已被禁用")
		return
	}

	// 检查账户是否被锁定
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		apierr.Forbidden(c, "账户已被锁定，请稍后再试")
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// 🐛 BUG#1: 用 gorm.Expr 原子自增，避免并发 race
		// 5 并发都读到 user.FailedLogin=0，仅靠内存判断会漏 lock。
		// 解决：先原子 +1，再 re-fetch 看实际值再决定是否 lock
		_ = database.DB.Model(&models.User{}).
			Where("id = ?", user.ID).
			UpdateColumn("failed_login", gorm.Expr("failed_login + 1")).Error

		// 重新读最新状态
		var fresh models.User
		if dbErr := database.DB.First(&fresh, "id = ?", user.ID).Error; dbErr == nil {
			if fresh.FailedLogin >= maxFailedLoginAttempts && fresh.LockedUntil == nil {
				lockedUntil := time.Now().Add(30 * time.Minute)
				_ = database.DB.Model(&models.User{}).
					Where("id = ?", user.ID).
					UpdateColumn("locked_until", lockedUntil).Error
			}
		}
		apierr.Unauthorized(c, "用户名或密码错误")
		return
	}

	// 登录成功：重置失败次数 + 记录登录信息
	now := time.Now()
	_ = database.DB.Model(&user).Updates(map[string]interface{}{
		"failed_login":  0,
		"locked_until":  nil,
		"last_login":    &now,
		"last_login_ip": c.ClientIP(),
	}).Error

	// 生成 Token
	token, err := middleware.GenerateToken(user.ID.String(), user.Username, user.Role)
	if err != nil {
		apierr.Internal(c, "生成 Token 失败", err)
		return
	}

	// C-F5: 设置 httpOnly + SameSite cookie（替代 localStorage 防止 XSS 窃 token）
	// Secure flag 在 release 模式启用（需要 HTTPS）
	secure := config.Get().Server.Mode == "release"
	maxAge := config.Get().Auth.JWT.Expire // 跟 JWT 过期一致
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		"auth_token", // cookie 名
		token,
		maxAge,
		"/",
		"",
		secure,
		true, // httpOnly
	)

	c.JSON(200, gin.H{
		"code": 0,
		"data": gin.H{
			// C-F5: token 仍返回 body 以便非浏览器 client（如 API key 流）使用，
			// 但浏览器场景下应只走 cookie 鉴权（前端已切到 withCredentials）
			"token": token,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Username,
				"nickname": user.Nickname,
				"email":    user.Email,
				// 词表归一：存量库可能存遗留别名 operator/viewer，出站一律折叠
				// （ADR-0005 决策 2「只读入不写出」），与 /auth/me 和 openapi enum 保持一致。
				"role":   middleware.CanonicalRole(user.Role),
				"avatar": user.Avatar,
			},
			// C7: 首次登录强改密 flag — 前端检测后强制 redirect /change-password
			"must_change_password": user.MustChangePassword,
		},
	})
}

// Logout 登出 (JWT 无状态；如需黑名单可在此加 Redis 写入)
func Logout(c *gin.Context) {
	// C-F5: 清 cookie
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("auth_token", "", -1, "/", "", false, true)
	c.JSON(200, gin.H{
		"code":    0,
		"message": "登出成功",
	})
}

// GetCurrentUser 获取当前登录用户信息
func GetCurrentUser(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		apierr.Unauthorized(c, "")
		return
	}

	var user models.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		apierr.NotFound(c, "用户不存在")
		return
	}

	// role 与 capabilities 都取自**鉴权上下文**（JWT claim / API Key 关联用户），
	// 而不是回查 DB 的 user.Role：前者才是门禁实际使用的值，两者同源才不会出现
	// 「按钮隐藏但接口放行」的错位（docs/FIX-PLAN-AUTHZ.md §4.4）。
	role := middleware.CanonicalRole(c.GetString("role"))
	capabilities := make([]string, 0, 5)
	for _, cap := range middleware.Capabilities(role) {
		capabilities = append(capabilities, string(cap))
	}

	c.JSON(200, gin.H{
		"code": 0,
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Username,
			"nickname":     user.Nickname,
			"email":        user.Email,
			"phone":        user.Phone,
			"avatar":       user.Avatar,
			"role":         role,
			"capabilities": capabilities,
			"status":       user.Status,
		},
	})
}

// 密码强度校验：最少 8 字符，必须同时包含字母和数字
func validatePasswordStrength(pw string) error {
	if len(pw) < 8 {
		return errors.New("密码至少 8 个字符")
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("密码必须同时包含字母和数字")
	}
	return nil
}

// ChangePassword 修改当前用户密码
func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请输入旧密码和新密码")
		return
	}

	userID := c.GetString("user_id")
	var user models.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		apierr.NotFound(c, "用户不存在")
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		apierr.BadRequest(c, "旧密码错误")
		return
	}

	// 🐛 BUG#2: 新密码强度校验（最少 8 字符 + 字母 + 数字）
	if err := validatePasswordStrength(req.NewPassword); err != nil {
		apierr.BadRequest(c, err.Error())
		return
	}

	// 🐛 BUG#3: 禁止设回旧密码（用户常踩坑）
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.NewPassword)) == nil {
		apierr.BadRequest(c, "新密码不能与旧密码相同")
		return
	}

	// 加密新密码
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		apierr.Internal(c, "密码加密失败", err)
		return
	}

	now := time.Now()
	user.PasswordHash = string(hash)
	// C7: 改密成功后清除强制改密 flag (幂等: 多次调也安全)
	user.MustChangePassword = false
	user.PasswordSetAt = &now
	if err := database.DB.Save(&user).Error; err != nil {
		apierr.Internal(c, "密码更新失败", err)
		return
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "密码修改成功",
	})
}

// SkipPasswordChangeRequest 跳过改密请求
//
// Deprecated: reason 字段已废弃 —— 服务端只认数据库里的 must_change_password，
// 不再读客户端自报的 reason（可伪造）。字段保留仅为兼容既有调用方与 openapi 契约。
type SkipPasswordChangeRequest struct {
	Reason string `json:"reason"`
}

// SkipPasswordChange 跳过改密（仅用于「当前无强制改密待办」时的幂等确认）
//
// C7: 用户在 /change-password 页面点 "本次跳过" 调本接口。
//
// 判据是 DB 状态而非请求体：must_change_password=true 一律 400
// （主人 7/02 决策：首次登录 / 管理员重置后的强制改密必须完成）。
// 修复前只拒绝 reason=="first_login" 字面量，空 body 或 reason=optional 即可清 flag，
// 同一账号状态换个字符串就能绕过强改密（AUTHZ 遗留缺陷 S-1a）。
//
// 无待办时幂等返回 200 且不写任何状态：修复前会写 password_set_at = NOW()，
// 而用户并未改密 —— 该假记录会让将来的密码过期策略失效（S-1c）。
func SkipPasswordChange(c *gin.Context) {
	var req SkipPasswordChangeRequest
	// body 可空（老 API 兼容）；reason 已废弃，绑定后不参与判定
	_ = c.ShouldBindJSON(&req)

	userID := c.GetString("user_id")
	if userID == "" {
		apierr.Unauthorized(c, "")
		return
	}

	var user models.User
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		apierr.NotFound(c, "用户不存在")
		return
	}

	if user.MustChangePassword {
		apierr.BadRequest(c, "当前账号处于强制改密状态,不允许跳过")
		return
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "当前无需改密",
	})
}
