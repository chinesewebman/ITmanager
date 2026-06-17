package cache

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_SetGet_基本读写(t *testing.T) {
	c := NewLRU(10)
	c.Set("k1", "v1", time.Minute)
	v, ok := c.Get("k1")
	assert.True(t, ok)
	assert.Equal(t, "v1", v)
}

func TestLRU_Get不存在返miss(t *testing.T) {
	c := NewLRU(10)
	_, ok := c.Get("nope")
	assert.False(t, ok)
	stats := c.Stats()
	assert.Equal(t, int64(1), stats.Misses)
}

func TestLRU_TTL过期返miss(t *testing.T) {
	c := NewLRU(10)
	c.Set("k1", "v1", 30*time.Millisecond)
	v, ok := c.Get("k1")
	assert.True(t, ok)
	assert.Equal(t, "v1", v)
	time.Sleep(50 * time.Millisecond)
	_, ok = c.Get("k1")
	assert.False(t, ok, "TTL 过期后应 miss")
}

func TestLRU_容量满LRU淘汰(t *testing.T) {
	c := NewLRU(3)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("c", 3, time.Minute)
	// 触 a (变最近)
	_, _ = c.Get("a")
	// 加 d, 应淘汰 b
	c.Set("d", 4, time.Minute)
	_, hasA := c.Get("a")
	_, hasB := c.Get("b")
	_, hasC := c.Get("c")
	_, hasD := c.Get("d")
	assert.True(t, hasA)
	assert.False(t, hasB, "b 应被 LRU 淘汰")
	assert.True(t, hasC)
	assert.True(t, hasD)
	assert.Equal(t, int64(1), c.Stats().Evictions)
}

func TestLRU_Delete主动失效(t *testing.T) {
	c := NewLRU(10)
	c.Set("k1", "v1", time.Minute)
	c.Delete("k1")
	_, ok := c.Get("k1")
	assert.False(t, ok)
}

func TestLRU_Clear清空(t *testing.T) {
	c := NewLRU(10)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Clear()
	stats := c.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	_, ok := c.Get("a")
	assert.False(t, ok)
}

func TestLRU_Update现有key不增容量(t *testing.T) {
	c := NewLRU(2)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("a", 100, time.Minute) // update, 不应淘汰 b
	_, hasB := c.Get("b")
	assert.True(t, hasB)
}

func TestLRU_GetOrLoad_缓存命中不调loader(t *testing.T) {
	c := NewLRU(10)
	loaderCalled := 0
	loader := func() (any, error) {
		loaderCalled++
		return "v", nil
	}
	v, err := c.GetOrLoad("k", time.Minute, loader)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, 1, loaderCalled)

	// 第二次命中
	v2, _ := c.GetOrLoad("k", time.Minute, loader)
	assert.Equal(t, "v", v2)
	assert.Equal(t, 1, loaderCalled, "命中不应调 loader")
}

func TestLRU_GetOrLoad_缓存未命中调loader(t *testing.T) {
	c := NewLRU(10)
	loader := func() (any, error) { return 42, nil }
	v, err := c.GetOrLoad("k", time.Minute, loader)
	require.NoError(t, err)
	assert.Equal(t, 42, v)
	stats := c.Stats()
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(1), stats.Sets)
}

func TestLRU_GetOrLoad_Loader失败返error(t *testing.T) {
	c := NewLRU(10)
	loader := func() (any, error) { return nil, errors.New("boom") }
	_, err := c.GetOrLoad("k", time.Minute, loader)
	assert.Error(t, err)
	stats := c.Stats()
	assert.Equal(t, int64(1), stats.Misses, "失败也算 miss")
	assert.Equal(t, int64(0), stats.Sets, "失败不写缓存")
}

func TestLRU_GetOrLoad_过期后loader失败返旧值(t *testing.T) {
	c := NewLRU(10)
	// 第一次: 写旧值
	_, err := c.GetOrLoad("k", 20*time.Millisecond, func() (any, error) { return "old", nil })
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)

	// 第二次: 过期 + loader 失败 → 返旧值 (stale-while-error)
	v, err := c.GetOrLoad("k", 20*time.Millisecond, func() (any, error) {
		return nil, errors.New("db down")
	})
	assert.NoError(t, err, "stale 模式下 loader 失败应返 nil error")
	assert.Equal(t, "old", v, "应返旧值")
}

func TestLRU_GetOrLoad_过期后loader成功刷新(t *testing.T) {
	c := NewLRU(10)
	_, err := c.GetOrLoad("k", 20*time.Millisecond, func() (any, error) { return "old", nil })
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)

	v, err := c.GetOrLoad("k", time.Minute, func() (any, error) { return "new", nil })
	require.NoError(t, err)
	assert.Equal(t, "new", v)
}

func TestLRU_Stats_命中率(t *testing.T) {
	c := NewLRU(10)
	c.Set("a", 1, time.Minute)
	for i := 0; i < 5; i++ {
		c.Get("a")    // 5 hits
		c.Get("nope") // 5 misses
	}
	stats := c.Stats()
	assert.Equal(t, int64(5), stats.Hits)
	assert.Equal(t, int64(5), stats.Misses)
}

func TestLRU_并发安全(t *testing.T) {
	c := NewLRU(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			c.Set(string(rune('a'+i%26)), i, time.Minute)
		}(i)
		go func(i int) {
			defer wg.Done()
			c.Get(string(rune('a' + i%26)))
		}(i)
	}
	wg.Wait()
	// 不 panic 即通过
	stats := c.Stats()
	assert.Greater(t, stats.Hits+stats.Misses, int64(0))
}

func TestLRU_零容量用默认256(t *testing.T) {
	c := NewLRU(0)
	assert.Equal(t, 256, c.capacity)
}

func TestLRU_不同TTL相互独立(t *testing.T) {
	c := NewLRU(10)
	c.Set("short", 1, 30*time.Millisecond)
	c.Set("long", 2, time.Minute)
	time.Sleep(50 * time.Millisecond)
	_, hasShort := c.Get("short")
	_, hasLong := c.Get("long")
	assert.False(t, hasShort)
	assert.True(t, hasLong, "长 TTL 不应被短 TTL 影响")
}
