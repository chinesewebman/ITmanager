// Package middleware 提供 in-memory + 异步批量刷新的 API key 使用追踪器。
//
// 设计动机 (P1-审计):
//   - 旧实现：每次 API key 请求都同步 `UPDATE api_keys SET last_used_at = NOW()`
//   - 写放大：高 QPS 场景下 N 次请求 → N 次 UPDATE，拖慢主路径 + DB 压力
//   - 新实现：in-memory buffer 累积 (api_key_id → time)，background goroutine
//     每 30s 批量刷新一次（或 buffer 满 100 条立即 flush）
//
// 语义保证:
//   - 最多丢失 30s 的 last_used_at 精度（可接受：审计用，非强一致）
//   - 进程崩溃时丢失未 flush 数据（可接受：API key 状态由 expires_at 等字段控制）
//   - 单元可测：暴露 Stats() / FlushNow() 便于测试
package middleware

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// apiKeyTracker 全局单例（P1-审计 last_used_at 异步批量写）
var apiKeyTracker = newAPIKeyTracker()

// APIKeyTracker API key 使用追踪器
type APIKeyTracker struct {
	mu     sync.Mutex
	dirty  map[uuid.UUID]time.Time // 待 flush
	flushN int                     // 累计 flush 次数（测试用）
	stopCh chan struct{}
	db     *gorm.DB
}

var (
	trackerOnce sync.Once
	trackerRef  *APIKeyTracker
)

// newAPIKeyTracker 单例构造
func newAPIKeyTracker() *APIKeyTracker {
	trackerOnce.Do(func() {
		trackerRef = &APIKeyTracker{
			dirty:  make(map[uuid.UUID]time.Time),
			stopCh: make(chan struct{}),
		}
		go trackerRef.backgroundFlush()
	})
	return trackerRef
}

// Track 标记一个 API key 已被使用（非阻塞）
func (t *APIKeyTracker) Track(keyID uuid.UUID) {
	t.mu.Lock()
	t.dirty[keyID] = time.Now()
	size := len(t.dirty)
	t.mu.Unlock()

	// 满 100 条立即 flush（不阻塞调用方）
	if size >= 100 {
		go t.FlushNow()
	}
}

// FlushNow 立即批量写库（导出供测试 + 超大 buffer 触发）
func (t *APIKeyTracker) FlushNow() {
	t.mu.Lock()
	if len(t.dirty) == 0 {
		t.mu.Unlock()
		return
	}
	// 复制 dirty 快照，释放锁后写库
	pending := make(map[uuid.UUID]time.Time, len(t.dirty))
	for k, v := range t.dirty {
		pending[k] = v
	}
	t.dirty = make(map[uuid.UUID]time.Time)
	t.flushN++
	t.mu.Unlock()

	if t.db == nil {
		// 单测或未初始化 db：跳过（log 一次）
		log.Printf("apiKeyTracker: db 未初始化，跳过 %d 条 flush", len(pending))
		return
	}

	// 批量写：用 CASE WHEN 一次性 UPDATE 多行
	// GORM 不直接支持 IN-place batch update，用循环简单 UPDATE
	// 100 条上限 → 最坏 100 次 query，但 30s 间隔 → 实际 QPS 不高
	for id, ts := range pending {
		if err := t.db.Model(&struct{}{}). // 占位，用 Table("api_keys")
							Table("api_keys").
							Where("id = ?", id).
							Update("last_used_at", ts).Error; err != nil {
			log.Printf("apiKeyTracker flush 失败 id=%s: %v", id, err)
		}
	}
	log.Printf("apiKeyTracker: flushed %d keys", len(pending))
}

// backgroundFlush 每 30s 调一次 FlushNow
func (t *APIKeyTracker) backgroundFlush() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.FlushNow()
		case <-t.stopCh:
			t.FlushNow() // 退出前最后 flush 一次
			return
		}
	}
}

// SetDB 注入数据库连接（应在 main.go 启动后调用）
func (t *APIKeyTracker) SetDB(db *gorm.DB) {
	t.mu.Lock()
	t.db = db
	t.mu.Unlock()
}

// SetAPIKeyTrackerDB 包级便捷函数：注入 db 到全局单例 tracker
// main.go 在 database.Init 后调用
func SetAPIKeyTrackerDB(db *gorm.DB) {
	apiKeyTracker.SetDB(db)
}

// Stop 停止 background goroutine（测试用）
func (t *APIKeyTracker) Stop(ctx context.Context) {
	select {
	case <-t.stopCh:
		// 已停止
	default:
		close(t.stopCh)
	}
	// 等 background 退出
	<-ctx.Done()
}

// Stats 返回当前 buffer 状态（测试用）
func (t *APIKeyTracker) Stats() (pending int, flushCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.dirty), t.flushN
}
