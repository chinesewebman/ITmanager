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
//   - LoadOrStore(now)：原子地「load 已有值, 或写入 now」—— 拿到「是否首次见 key」
//     的同时, 首次见直接写入 now, 不用二次 Store。这把 first-hit 的「load-then-store
//     之间存在 TOCTOU」问题彻底消掉。
//   - CompareAndSwap：原子地「actual = expect 才 swap 成 now」—— 用于窗口外的覆写,
//     避免多个并发在窗口外互相覆盖时让两条 hit 都「first-time」。
//   - Range：遍历所有 key (回收 expired key 用)。
//
// 安全边界：
//   - key 由 trigger_id 拼 problem_start unix 字符串形成, 拼操作在这里集中, 不扩散；
//   - Allow 顺手清 60s+ 的 key — 不会无限增长。

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
//   - 第一次见 key → LoadOrStore(now) 返回 (now, loaded=false) → return true
//   - 60s 内再见同 key → LoadOrStore(now) 返回 (oldTime, loaded=true), oldTime 未过期 → return false
//   - 60s 后再见同 key → LoadOrStore(now) 返回 (oldTime, loaded=true), oldTime 过期 →
//     CompareAndSwap(oldTime, now) 覆写 → return true；CAS 失败重试一次
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
// dedup window, false if dedup hit (drop)。
//
// 行为细分：
//   - 60s 内同 key：第二次起拒绝 → sender.Send 不发起
//   - 60s 后同 key：视为新事件 → 允许 + 重置时间戳
//   - 并发：sync.Map.LoadOrStore 原子地「load 已有值, 或写入 now」, 在并发下同一
//     key 也只让一个 goroutine 走「first-time」分支。
//
// 为什么不先 Load 再 Store：两操作之间存在 TOCTOU——两个 goroutine 同时 Load 看到
// "key 不存在" 都 Store 写自己的时间，结果两个都「first-time」。LoadOrStore 把
// "load 或 store" 合并为单步，第二次 LoadOrStore 一定看到第一次的写入 → 走
// 「actual 已存在」分支 → 判断窗口期。
//
// 窗口外为什么不直接 Store：跟 first-time 同样的原因——窗口外两个并发同时
// 判断 "actual 过期", 都 Store(now) 会让两个 hit 都算 first。改成 CompareAndSwap
// 把「actual 还是旧值我才覆盖」原子化，第一个 CAS 成功 → 唯一 first。
func (d *Deduper) Allow(key string) bool {
	now := d.now()
	// first-time 路径：LoadOrStore 直接写 now, 拿到 (now, false) 一定是首次
	actualAny, loaded := d.m.LoadOrStore(key, now)
	if !loaded {
		d.gcExpired(now)
		return true
	}
	// key 已存在 — actualAny 是当前 stored 值
	actual := actualAny.(time.Time)
	if now.Sub(actual) < d.life {
		return false // 窗口内 → 去重
	}
	// 窗口外：CAS 覆写, 把"actual 还是旧值"作为原子条件
	if d.m.CompareAndSwap(key, actual, now) {
		// 我们这次是窗口外首次命中, 同时清一波过期键
		d.gcExpired(now)
		return true
	}
	// CAS 失败 — 别的 goroutine 已覆写 actual 到 newActual;
	// 它的覆盖基于它的 now, 它的窗口期判断与本 goroutine 独立 (它的「actual - oldActual」
	// 在它眼里也是「窗口外」), 所以我们这次不算 first, 应该 drop
	return false
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
