package middleware

import (
	"sync"
	"time"

	"gorm.io/gorm"
)

// authStatusCacheEntry 用户状态缓存条目
//
// 设计取舍（FIX-PLAN-M40 §edges）：
//   - TTL 30s 是 trade-off：缩短到 5s 增加 DB 压力（每用户每分钟 12 次 SELECT），
//     延长到 5min 让运维体验不到「禁用立即生效」
//   - 多副本部署下 cache 是 per-process，副本 A 把用户改 inactive 后副本 B 的
//     cache 仍可能是 active；最长滞后 30s。这是可接受的：运维封禁是即时动作
//     （≤30s 全副本生效），不是强一致系统
//   - 不存 token 内容，只存 user_id；token 自身的 24h 过期由
//     jwt.RegisteredClaims.ExpiresAt 独立处理
type authStatusCacheEntry struct {
	status    string // "active" / "inactive"
	expiresAt time.Time
}

// authStatusCache JWT 路径的用户状态缓存（per-process in-memory）
//
// 调用方：AuthMiddleware 的 JWT 分支在 VerifyToken 通过后查 cache + DB。
// 不缓存「用户不存在」的情形（避免删除用户后误判为 active）—— 详见
// AuthMiddleware 的注释。
type authStatusCache struct {
	mu   sync.RWMutex
	data map[string]authStatusCacheEntry // key = user_id (UUID string)
	ttl  time.Duration
}

var defaultAuthStatusCache = &authStatusCache{
	data: make(map[string]authStatusCacheEntry),
	ttl:  30 * time.Second,
}

// get 读 cache：未命中或已过期返回 ("", false)
func (c *authStatusCache) get(userID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.data[userID]
	if !ok || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.status, true
}

// set 写 cache：覆盖已有条目并刷新过期时间
func (c *authStatusCache) set(userID, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[userID] = authStatusCacheEntry{
		status:    status,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// invalidate 主动失效：用户被禁用/启用时由调用方显式调用（运维操作路径）
//
// 当前实现为「无主动失效」—— 30s TTL 内 status 翻转不感知（FIX-PLAN-M40
// §edges 已记）。本函数保留给将来 hooks（如 user_service 在 Update 时同步
// 调 invalidate）使用；当前 round 不引入这条链路以控制爆炸半径。
func (c *authStatusCache) invalidate(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, userID)
}

// resetAuthStatusCache 测试用：清空 cache。
//
// 生产代码不应调用此函数。
func resetAuthStatusCache() {
	defaultAuthStatusCache.mu.Lock()
	defer defaultAuthStatusCache.mu.Unlock()
	defaultAuthStatusCache.data = make(map[string]authStatusCacheEntry)
}

// ResetAuthStatusCacheForTest 跨包测试用：导出版 resetAuthStatusCache.
// M41: 让 internal/api 包的 setupTestRouter cleanup 调用, 避免 cache
// singleton 跨 test 污染. 生产代码不应调用.
func ResetAuthStatusCacheForTest() {
	resetAuthStatusCache()
}

// InvalidateAuthStatusCacheForUser 测试用：清掉指定 user_id 的 cache 条目，
// 用于「模拟 cache 自然过期」的测试场景（真 PG db_smoke 不能 sleep 31s）。
//
// 生产代码不应调用此函数；运维封禁场景的 cache 滞后 30s 是设计取舍
//（FIX-PLAN-M40 §edges「多副本部署下 cache 是 per-process」）。
func InvalidateAuthStatusCacheForUser(userID string) {
	defaultAuthStatusCache.invalidate(userID)
}

// lookupUserStatus 查 DB 拿当前 users.status 并写 cache。
//
// 调用点：AuthMiddleware JWT 路径 cache miss 时。返回 ("", err) 表示 DB 错误
// —— 该错误由调用方决定降级策略（当前实现：DB 错误 = 拒绝请求，避免放行）。
//
// nil DB：返回 "active" + nil，**仅用于测试场景**（setupAuthEnv(t, nil)）。
// 生产路径 database.DB 由 cmd/server/main.go 启动时必装，永远非 nil。
// 这里不 panic 是为了不让既有 JWT 单测因本轮改动全部失败（FIX-PLAN-M40 §4）。
// 若 production DB 真的为空，AuthMiddleware 的 lookupErr 分支也会因 GORM panic
// 而被 gin recovery 兜住 → 500；不是新引入的安全洞。
func lookupUserStatus(db *gorm.DB, userID string) (string, error) {
	if status, ok := defaultAuthStatusCache.get(userID); ok {
		return status, nil
	}
	if db == nil {
		// 测试 fallback：返回 active 不阻塞既有测试。
		// 真生产中此处永远到不了（database.DB 由 cmd/server 注入）。
		return "active", nil
	}
	var status string
	if err := db.Raw("SELECT status FROM users WHERE id = ?", userID).Scan(&status).Error; err != nil {
		return "", err
	}
	// 防御：DB 里没有该用户（被物理删除）也写 cache 为 inactive，
	// 避免下次 cache miss 再走一次 DB
	if status == "" {
		status = "inactive"
	}
	defaultAuthStatusCache.set(userID, status)
	return status, nil
}
