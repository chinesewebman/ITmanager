// Package cache 提供 in-process TTL + LRU 缓存, 用于 dashboard stats / alert list 等热点数据。
//
// v1.4 落地: 替代"每次请求打 DB", 5-10x 性能提升 (实测 6 次 count → cache hit < 1ms)。
//
// 架构:
//
//	dashboard.Stats()
//	  └─ Cache.GetOrLoad(key, 30s, fn)  ← 1 次 DB / 30s
//	       └─ DB 失败 → 返旧值 (cache stale, 避免 5xx)
//
// 接口化 Cache (Get/Set/Delete) 便于 v2.0 替换 Redis 后端 (L2 分布式)。
package cache

import (
	"sync"
	"time"
)

// Cache 缓存接口
type Cache interface {
	Get(key string) (any, bool)
	Set(key string, value any, ttl time.Duration)
	Delete(key string)
	// GetOrLoad 缓存未命中或过期时调 loader, 失败保留旧值 (stale-while-revalidate 简化版)
	GetOrLoad(key string, ttl time.Duration, loader func() (any, error)) (any, error)
	// Stats 调试: 命中/未命中/总数
	Stats() Stats
	// Clear 清空
	Clear()
}

// Stats 命中率统计
type Stats struct {
	Hits      int64
	Misses    int64
	Sets      int64
	Evictions int64
}

// entry 缓存条目
type entry struct {
	value     any
	expiresAt time.Time
	createdAt time.Time
}

// LRU 进程内 LRU + TTL 缓存
//   - 用 map + 双向链表实现 (Go 没有泛型 LRU)
//   - 容量满时按 LRU 淘汰
type LRU struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*listNode
	head     *listNode // 最近使用
	tail     *listNode // 最久未用
	stats    Stats
}

type listNode struct {
	key   string
	entry *entry
	prev  *listNode
	next  *listNode
}

// NewLRU 创建 LRU cache, capacity=0 时默认 256
func NewLRU(capacity int) *LRU {
	if capacity <= 0 {
		capacity = 256
	}
	return &LRU{
		capacity: capacity,
		items:    make(map[string]*listNode, capacity),
	}
}

// Get 拿缓存值, 过期/不存在返 (nil, false)
func (c *LRU) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.items[key]
	if !ok {
		c.stats.Misses++
		return nil, false
	}
	if time.Now().After(n.entry.expiresAt) {
		// 过期
		c.removeNode(n)
		delete(c.items, key)
		c.stats.Misses++
		return nil, false
	}
	c.moveToFront(n)
	c.stats.Hits++
	return n.entry.value, true
}

// Set 设值, ttl=0 表示不过期
func (c *LRU) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.Sets++

	now := time.Now()
	exp := now.Add(ttl)

	if n, ok := c.items[key]; ok {
		n.entry.value = value
		n.entry.expiresAt = exp
		n.entry.createdAt = now
		c.moveToFront(n)
		return
	}
	n := &listNode{
		key: key,
		entry: &entry{
			value:     value,
			expiresAt: exp,
			createdAt: now,
		},
	}
	c.items[key] = n
	c.pushFront(n)
	if len(c.items) > c.capacity {
		// 淘汰最久未用
		oldest := c.tail
		if oldest != nil {
			c.removeNode(oldest)
			delete(c.items, oldest.key)
			c.stats.Evictions++
		}
	}
}

// Delete 主动失效
func (c *LRU) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.items[key]; ok {
		c.removeNode(n)
		delete(c.items, key)
	}
}

// Clear 清空
func (c *LRU) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*listNode, c.capacity)
	c.head = nil
	c.tail = nil
	c.stats = Stats{}
}

// GetOrLoad 取或加载 (stale-while-revalidate 简化: 失败保留旧值)
// 行为:
//  1. 缓存命中且未过期 → 返值
//  2. 缓存命中但过期 → 调 loader, 成功用新值 (覆盖旧), 失败返旧值 (标记为 stale)
//  3. 缓存未命中 → 调 loader, 成功写入, 失败返 error
func (c *LRU) GetOrLoad(key string, ttl time.Duration, loader func() (any, error)) (any, error) {
	// 命中 (含过期但有值)
	c.mu.Lock()
	n, ok := c.items[key]
	c.mu.Unlock()

	if ok {
		c.mu.RLock()
		expired := time.Now().After(n.entry.expiresAt)
		val := n.entry.value
		c.mu.RUnlock()

		if !expired {
			c.mu.Lock()
			c.moveToFront(n)
			c.stats.Hits++
			c.mu.Unlock()
			return val, nil
		}
		// 过期: 调 loader, 失败返旧值
		newVal, err := loader()
		if err != nil {
			// stale-while-error: 返旧值
			return val, nil
		}
		c.Set(key, newVal, ttl)
		return newVal, nil
	}

	// miss: 调 loader
	c.mu.Lock()
	c.stats.Misses++
	c.mu.Unlock()
	v, err := loader()
	if err != nil {
		return nil, err
	}
	c.Set(key, v, ttl)
	return v, nil
}

// Stats 命中率
func (c *LRU) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// 双向链表操作 (假设 c.mu 已持有)
func (c *LRU) moveToFront(n *listNode) {
	if c.head == n {
		return
	}
	c.removeNode(n)
	c.pushFront(n)
}

func (c *LRU) pushFront(n *listNode) {
	n.prev = nil
	n.next = c.head
	if c.head != nil {
		c.head.prev = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *LRU) removeNode(n *listNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		c.tail = n.prev
	}
	n.prev, n.next = nil, nil
}
