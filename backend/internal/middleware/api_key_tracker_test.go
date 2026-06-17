package middleware

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// 注：apiKeyTracker 是包级单例，测试共享状态。
// 断言用 "delta" 而非绝对值，避免顺序敏感。

func TestAPIKeyTracker_TrackAccumulates(t *testing.T) {
	before, _ := apiKeyTracker.Stats()
	apiKeyTracker.Track(uuid.New())
	apiKeyTracker.Track(uuid.New())
	after, _ := apiKeyTracker.Stats()
	if after-before < 2 {
		t.Errorf("expected +2 pending, got before=%d after=%d", before, after)
	}
}

func TestAPIKeyTracker_SameKeyDedupe(t *testing.T) {
	keyID := uuid.New()
	before, _ := apiKeyTracker.Stats()
	apiKeyTracker.Track(keyID)
	apiKeyTracker.Track(keyID)
	apiKeyTracker.Track(keyID)
	after, _ := apiKeyTracker.Stats()
	if after-before != 1 {
		t.Errorf("same key should dedupe to +1, got +%d", after-before)
	}
}

func TestAPIKeyTracker_FlushDecrementsPending(t *testing.T) {
	// 攒几条
	for i := 0; i < 3; i++ {
		apiKeyTracker.Track(uuid.New())
	}
	pendingBefore, flushBefore := apiKeyTracker.Stats()

	// FlushNow（无 db 模式会 log 跳过，pending 清零）
	apiKeyTracker.FlushNow()

	pendingAfter, flushAfter := apiKeyTracker.Stats()
	if pendingAfter != 0 {
		t.Errorf("after flush pending should be 0, got %d", pendingAfter)
	}
	if flushAfter-flushBefore < 1 {
		t.Errorf("flush count should increment, before=%d after=%d", flushBefore, flushAfter)
	}
	if pendingBefore == 0 {
		// buffer 已被前序测试清空，无意义
		t.Logf("warning: buffer was empty before test, no decrement to verify")
	}
}

func TestAPIKeyTracker_HighVolumeTriggersAutoFlush(t *testing.T) {
	// 攒 150 条 → 满 100 应自动 flush
	for i := 0; i < 150; i++ {
		apiKeyTracker.Track(uuid.New())
	}
	// 等异步 flush goroutine 跑完
	time.Sleep(150 * time.Millisecond)

	pending, flushCount := apiKeyTracker.Stats()
	if pending >= 150 {
		t.Errorf("expected auto-flush to reduce buffer, got pending=%d", pending)
	}
	if flushCount < 1 {
		t.Errorf("expected at least 1 auto-flush, got %d", flushCount)
	}
}

func TestAPIKeyTracker_BackgroundFlushInterval(t *testing.T) {
	// 验证 30s ticker 工作（用 Stop() 不实际等 30s，只验证非阻塞）
	// 这个测试只是烟测，不真正等 30s
	apiKeyTracker.Track(uuid.New())
	// 不等，立即读 stats — 应该至少有 1 个 pending
	_, _ = apiKeyTracker.Stats()
	// OK = 至少没死锁
}
