package handlers

import (
	"time"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
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
		// 记录失败次数 + 自动锁定
		user.FailedLogin++
		if user.FailedLogin >= maxFailedLoginAttempts {
			lockedUntil := time.Now().Add(30 * time.Minute)
			user.LockedUntil = &lockedUntil
		}
		_ = database.DB.Save(&user).Error
		apierr.Unauthorized(c, "用户名或密码错误")
		return
	}

	// 登录成功：重置失败次数 + 记录登录信息
	user.FailedLogin = 0
	user.LockedUntil = nil
	now := time.Now()
	user.LastLogin = &now
	user.LastLoginIP = c.ClientIP()
	_ = database.DB.Save(&user).Error

	// 生成 Token
	token, err := middleware.GenerateToken(user.ID.String(), user.Username, user.Role)
	if err != nil {
		apierr.Internal(c, "生成 Token 失败", err)
		return
	}

	c.JSON(200, gin.H{
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

// Logout 登出 (JWT 无状态；如需黑名单可在此加 Redis 写入)
func Logout(c *gin.Context) {
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
