package handlers

import (
	"errors"
	"net/http"
	"time"

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

// Login 登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "请输入用户名和密码")
		return
	}

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
				"role":     user.Role,
				"avatar":   user.Avatar,
			},
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

	c.JSON(200, gin.H{
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

	user.PasswordHash = string(hash)
	if err := database.DB.Save(&user).Error; err != nil {
		apierr.Internal(c, "密码更新失败", err)
		return
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "密码修改成功",
	})
}
