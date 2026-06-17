// Package service 错误定义 & 工具函数。
// 业务错误 (ErrNotFound / ErrAlreadyExists / ErrInvalidInput / ErrTooManyItems) 定义在 asset_service.go，
// 跨文件可见。本文件放工具函数。
package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// isUniqueViolation 判断 err 是否为唯一索引冲突 (PostgreSQL SQLSTATE 23505)。
// v1.1: 用于把 Create() 撞 unique 约束的通用 err 类型化，方便 handler 返 409。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// gorm.ErrDuplicatedKey (v2) 或 driver 底层 pgconn.PgError Code == "23505"
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	// 部分 gorm 版本不暴露 ErrDuplicatedKey，回退字符串匹配
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
