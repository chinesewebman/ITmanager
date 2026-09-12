// Package notification - Deduper 单测 (M38-B Round 9)
//
// 测试覆盖：
//   - 首次见 key → Allow=true (首次入窗)
//   - 60s 内同 key 第二次 → Allow=false (dedup 命中)
//   - 60s 后同 key → Allow=true (窗口过期重置)
//   - 不同 key 互不影响
//   - 并发：N 个 goroutine 拿同一 key 同时 Allow → 仅 1 个 true, 其余 false
//   - Allow 的总次数 = 首次 true + 60s 后的 true
//
// 时钟注入：
//   - 通过 newDeduperWithClock(now func() time.Time, life time.Duration)
//   - 测试不让 time.Now 跑, 直接推进时钟
//
// mutation inversion (Round 9 / 11 收口) 在 dedup_test_inversion_test.go 单独跑。

package notification

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixedClock 让 now 单调递增, 测试明确推进时间, 不依赖 sleep
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFixedClock() *fixedClock {
	return &fixedClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestDeduper_FirstHit_Allows(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	if !d.Allow("trig|1") {
		t.Fatal("first hit must Allow=true")
	}
	if d.Len() != 1 {
		t.Fatalf("Len=%d, want 1", d.Len())
	}
}

func TestDeduper_InWindow_Drops(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	if !d.Allow("trig|1") {
		t.Fatal("first hit must allow")
	}
	// 同 key, 30s 后 — 仍在 60s 窗口
	c.Advance(30 * time.Second)
	if d.Allow("trig|1") {
		t.Fatal("in-window hit must drop")
	}
	// 再 29s — 仍 59s < 60s
	c.Advance(29 * time.Second)
	if d.Allow("trig|1") {
		t.Fatal("still in-window, must drop")
	}
}

func TestDeduper_OutOfWindow_AllowsAgain(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	if !d.Allow("trig|1") {
		t.Fatal("first hit must allow")
	}
	// 60s 整 —— 在「< 60s」严格不等下允许通过 (now.Sub(prev) == 60s 不是 < 60s)
	// 但实现是 < life 判断 → 60s 整仍算窗口内；测试改 61s 更稳
	c.Advance(61 * time.Second)
	if !d.Allow("trig|1") {
		t.Fatal("after window, must allow again")
	}
}

func TestDeduper_DifferentKeys_Independent(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	if !d.Allow("trig|1") {
		t.Fatal("trig|1 first must allow")
	}
	if !d.Allow("trig|2") {
		t.Fatal("trig|2 first must allow")
	}
	if d.Allow("trig|1") {
		t.Fatal("trig|1 second must drop (still in window)")
	}
	if d.Allow("trig|2") {
		t.Fatal("trig|2 second must drop (still in window)")
	}
	if d.Len() != 2 {
		t.Fatalf("Len=%d, want 2", d.Len())
	}
}

func TestDeduper_FireKey_Format(t *testing.T) {
	// 显式锁定 key 拼接格式——一旦格式改了, 上下游要同步, 这里先 fail-fast
	cases := []struct {
		triggerID string
		ts        int64
		want      string
	}{
		{"trig-001", 1700000000, "trig-001|1700000000"},
		{"", 0, "|0"},
		{"zabbix:123", 1234567890, "zabbix:123|1234567890"},
	}
	for _, tc := range cases {
		got := FireKey(tc.triggerID, tc.ts)
		if got != tc.want {
			t.Errorf("FireKey(%q,%d)=%q, want %q", tc.triggerID, tc.ts, got, tc.want)
		}
	}
}

// TestDeduper_Concurrent_ExactlyOneFirstHit 是 mutation inversion 的前置验证:
// N 个 goroutine 同时拿同一 key Allow, 必须恰好 1 个 true, 其余全 false。
//
// 在「60s 窗口内」场景下做这个 — 第二次窗口外的写也算 true, 但只跑在第一波 N
// goroutine 同一时间窗内, 所以 1 个 true 是预期。
func TestDeduper_Concurrent_ExactlyOneFirstHit(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	const N = 200
	var wg sync.WaitGroup
	start := make(chan struct{})
	var allowed int64

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 同时起跑
			if d.Allow("trig|race") {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&allowed); got != 1 {
		t.Fatalf("concurrent first-hit: got %d allows, want exactly 1", got)
	}
}

// TestDeduper_Concurrent_TwoWindows_AllowTwice 模拟「首波并发只放一个,
// 60s 后第二轮并发也只放一个」——总计 2 个 allow。
func TestDeduper_Concurrent_TwoWindows_AllowTwice(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	run := func() int64 {
		const N = 50
		var wg sync.WaitGroup
		start := make(chan struct{})
		var allowed int64
		for i := 0; i < N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if d.Allow("trig|race2") {
					atomic.AddInt64(&allowed, 1)
				}
			}()
		}
		close(start)
		wg.Wait()
		return atomic.LoadInt64(&allowed)
	}

	if got := run(); got != 1 {
		t.Fatalf("window 1: got %d, want 1", got)
	}
	c.Advance(61 * time.Second)
	if got := run(); got != 1 {
		t.Fatalf("window 2: got %d, want 1", got)
	}
}

// TestDeduper_GC_RemovesExpired 验证 gcExpired 会清掉 60s 前的 key
func TestDeduper_GC_RemovesExpired(t *testing.T) {
	c := newFixedClock()
	d := newDeduperWithClock(c.Now, 60*time.Second)

	d.Allow("a|1")
	d.Allow("b|1")
	if d.Len() != 2 {
		t.Fatalf("Len=%d before advance", d.Len())
	}
	// 推 90s, 现在 90s 前的旧 key 都已 expired
	c.Advance(90 * time.Second)
	d.Allow("a|1") // 这一调用末尾会触发 gcExpired, 清掉 a|1 (90s old) 和 b|1 (90s old)
	// 90s 前 Allow 的 key 都 > 60s 前 → gc 应清掉
	// 但当前 a|1 已经被这次 Allow 写到了「现在」, 是新值 (实际 gc 走 Range 时,
	// 先看到 b|1 (老) → Delete; 再看到 a|1 — 但 a|1 在 Allow 内已被 Store 成 now,
	// 不算 expired; 我们手动 Range 一下验证)
	if got := d.Len(); got != 1 {
		t.Errorf("after gc: Len=%d, want 1 (a|1 kept, b|1 gc'd)", got)
	}
}

// 静音 unused 警告
var _ = strconv.Itoa
