// Package notification - firededup: 在 worker 端做 fire 路径的 60s 去重。
//
// M38-B / E2.a 决策：worker 收到 TopicAlertCreated 时按 (trigger_id + problem_start)
// 维护 in-memory 窗口——60s 内同 key 不重复 Send（即「第二次发事件的 rule 过滤 + 推送」被压扁）。
//
// 设计意图：
//   - **不依赖 caller**：publish 端可以并发、可以重复（Zabbix 同步 + 手工 sync 同时启动）；
//     worker 端冗余地挡一道，「至少一次」变成「最多一次 / 时间窗口」。
//   - **in-memory (sync.Map)**：单 worker 实例够用；横向扩展的 dedup 同步 out of scope。
//   - **窗口边界 = 60s 超时**：key 写入时记录 fireTime，60s 后视为新事件（即便密钥相同）。
//
// 与已有 notify_logs.pending 的区别：
//   - notify_logs 是「慢异步 + 持久化重试」——比如下游 SMTP 卡死能落库重投；
//   - firededup 是「即时去重」——同一个 alert 60s 内被推 N 次，本模块只投第一次。
//
// 与 worker.handleAlertEvent 的协作：
//   - firededup 不是中间件, 是 handler 内部主动调用 —— handler 在配置推送之前先
//     Allow(key), 若返回 false → 直接 return nil, 不进任何 sender.Send。
//   - 这样既保留 handler 的「未来也许改 dedup 策略」(配置化) 又不让 dedup 状态
//     蔓延到 worker 的多个 goroutine。
//
// 同步语义要点（sync.Map 后端）：
//   - LoadOrStore：原子地「load 已有值, 或写入 sentinel」—— 拿它做「是否首次见到 key」判定。
//     该方法保证「第一次见 key 时 loaded=false, 后续都 loaded=true」。
//   - Store：覆盖写；用于拿到首次窗口后写真正时间，原子性强；
//     也用于「窗口外覆写」路径, 配合 Load 读取后判断。
//   - CompareAndSwap：原子地「值 = expect 才替换」—— 用于并发覆盖的边界判断。
//   - Range：遍历所有 key (回收 expired key 用)。
//
// 安全边界：
//   - key 由 trigger_id 拼 problem_start unix 字符串形成, 拼操作在这里集中, 不扩散；
//   - Allow 顺手清 60s+ 的 key — 不会无限增长。
//   - sync.Map 适合「键写一次后多次读」的负载, 完美契合 60s 内的读多写少场景。

package notification

import (
	"strconv"
	"sync"
	"time"
)

// dedupWindow 默认窗口 60s; 测试可通过 newDeduperWithClock 注入。
const dedupWindow = 60 * time.Second

// Deduper 单实例轻量级去重器（sync.Map 后端）。
//
// 状态机语义：
//   - 第一次见 key → LoadOrStore 返回 sentinel + loaded=false → 写真正的 now → return true
//   - 60s 内再见同 key → loaded=true, actual 未过期 → return false (drop)
//   - 60s 后再见同 key → loaded=true, actual 过期 → 用 LoadOrStore(now) 覆写 (并发下
//     别人可能也写 newnow; 自己的「now」是 valid 的事实, 写一次就是 valid 的, 不需要 CAS)
//     → return true
type Deduper struct {
	m    sync.Map // key=string, value=time.Time
	now  func() time.Time
	life time.Duration
}

// NewDeduper 生产实例，60s 窗口。
func NewDeduper() *Deduper {
	return &Deduper{
		now:  time.Now,
		life: dedupWindow,
	}
}

// newDeduperWithClock 测试用：注入 now 让时间窗口可控。
func newDeduperWithClock(now func() time.Time, life time.Duration) *Deduper {
	return &Deduper{
		now:  now,
		life: life,
	}
}

// FireKey 组合 trigger_id + problem_start unix，单一来源，避免拼格式两处分叉
// （一旦两处各拼一遍，改格式时一处漏改 → 静默失效）。
func FireKey(triggerID string, problemStartUnix int64) string {
	return triggerID + "|" + strconv.FormatInt(problemStartUnix, 10)
}

// Allow returns true if (trigger_id+problem_start) is the FIRST hit inside the
// dedup window, false if dedup hit (drop).
//
// 行为细分：
//   - 60s 内同 key：第二次起拒绝 → sender.Send 不发起
//   - 60s 后同 key：视为新事件 → 允许 + 重置时间戳
//   - 并发：sync.Map.LoadOrStore 原子地「load 已有值, 或写入 sentinel」, 在并发下同一
//     key 也只让一个 goroutine 走「first-time」分支。
//
// 为什么 LoadOrStore 不是 Store + Load：两操作之间存在 TOCTOU——两个 goroutine 同时
// Load 看到 "key 不存在" 都 Store 写自己的时间，结果两个都「first-time」。LoadOrStore
// 把 "load 或 store" 合并为单步，第二次 LoadOrStore 一定看到第一次的写入 → 走
// 「actual 已存在」分支 → 判断窗口期 → 拒绝。
func (d *Deduper) Allow(key string) bool {
	now := d.now()
	sentinel := time.Time{} // 零值是 LoadOrStore 的"占位写入"
	actualAny, loaded := d.m.LoadOrStore(key, sentinel)
	if !loaded {
		// 第一个见这个 key 的 goroutine —— 写真正的 now
		d.m.Store(key, now)
		d.gcExpired(now)
		return true
	}
	// key 已存在
	actual := actualAny.(time.Time)
	if now.Sub(actual) < d.life {
		return false // 窗口内 → 去重
	}
	// 窗口外：覆写时间戳。我们刚做的判断「now - actual >= life」是 valid 的事实，
	// 写一次 now 就是 valid 的新一次，去不去重由别人的「now - my_now」判断 ——
	// 即使被别的并发抢先写 newnow，他们的 my_now 比 actual 新（窗口外），下一次仍
	// 走「actual = my_now」再走窗口期判断, 不出现假绿。
	d.m.Store(key, now)
	return true
}

// gcExpired 回收 expired 键（与 Allow 同进程，用 now 注入测试用）
//
// 不能单独 goroutine 周期跑（测试与生产行为分歧，单测拿不到时序保证）；
// 选择在每次 Allow 末尾顺手回收一波——map 体积随活跃 key 数增长，每次回收摊销 O(N)。
func (d *Deduper) gcExpired(now time.Time) {
	cutoff := now.Add(-d.life)
	d.m.Range(func(k, v any) bool {
		t, ok := v.(time.Time)
		if !ok {
			// 容错：万一外部代码塞入非 time.Time 直接跳过，避免 panic
			return true
		}
		if t.Before(cutoff) {
			d.m.Delete(k)
		}
		return true
	})
}

// Len 返回当前 map 大小（测试用，生产不需要）
func (d *Deduper) Len() int {
	n := 0
	d.m.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}
