// Package eventbus 实现进程内 pub/sub 事件总线。
//
// 为什么 in-process?
//   - 单机部署 (主人 6/17 确认不需要分布式)
//   - chan 性能 > 100k events/s, 足够覆盖业务量
//   - 零外部依赖, 零运维
//
// 架构:
//
//	Publisher.Publish() ─┐
//	                     ▼
//	                 ┌──────────┐
//	                 │ chan Event│ (buffered, 1024)
//	                 └────┬─────┘
//	                      │ dispatcher goroutine
//	                      ▼
//	           ┌─────────────────────┐
//	           │ topic → []Handler  │ (subscriber 路由)
//	           └────────┬────────────┘
//	                    │ for each handler
//	                    ▼
//	              handler(ctx, e)
//	                    │ err → retry → DLQ
//	                    ▼
//	              event_dlq 表 (SQLite)
//
// 不做的事:
//   - 跨进程 pub/sub (用 NATS/Kafka, v3.0+ 考虑)
//   - 事件持久化主表 (audit_logs 已记录 HTTP 请求, 业务事件无需重复)
//   - 事件回放 (DLQ 用于人工补偿)
package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

// Topic 事件类型常量
const (
	TopicAlertCreated   = "alert.created"
	TopicAlertResolved  = "alert.resolved"
	TopicTicketCreated  = "ticket.created"
	TopicTicketResolved = "ticket.resolved"
	TopicUserLocked     = "user.locked"
)

// Event 事件结构
type Event struct {
	ID        string          `json:"id"` // uuid
	Topic     string          `json:"topic"`
	Payload   json.RawMessage `json:"payload"` // JSON 编码
	Timestamp time.Time       `json:"timestamp"`
	Attempts  int             `json:"attempts"` // 重试次数
}

// Handler 订阅者处理函数
// 返 nil = 成功, 返 err = 进入重试 (maxRetries 后入 DLQ)
type Handler func(ctx context.Context, e Event) error

// Bus 事件总线接口
type Bus interface {
	Publish(topic string, payload any) error
	Subscribe(topic string, h Handler) error
	Close() error
	Stats() Stats
}

// Stats 运行时统计
type Stats struct {
	Published         uint64 `json:"published"`           // 累计成功 Publish
	Dispatched        uint64 `json:"dispatched"`          // 累计成功 Dispatch (handler 返回 nil)
	DLQ               uint64 `json:"dlq"`                 // 累计入 DLQ (无订阅者 + retry 耗尽 + panic)
	Retries           uint64 `json:"retries"`             // 累计 retry 调用次数
	Pending           int    `json:"pending"`             // 当前 chan 排队
	Subscribers       int    `json:"subscribers"`         // 总订阅者数 (跨 topic 聚合)
	HandlerErrs       uint64 `json:"handler_errs"`        // handler 每次返 err (含 retry 中)
	HandlerFinalFails uint64 `json:"handler_final_fails"` // P2: handler 最终失败次数 (retry 耗尽, 进入 DLQ 前)
}

// Config 总线配置
type Config struct {
	BufferSize     int           // chan 缓冲, 默认 1024
	MaxRetries     int           // handler 重试次数, 默认 3
	RetryBackoff   time.Duration // 重试间隔基数, 默认 100ms (实际 = base << attempt 指数退避)
	WorkerCount    int           // dispatcher goroutine 数, 默认 4
	MaxPayloadSize int           // 单事件 payload 上限 (bytes), 默认 64KB; 超限 Publish 直接 err
	Logger         *slog.Logger  // 缺省 slog.Default()
}

// EventDLQ 死信队列行 (SQLite 表)
type EventDLQ struct {
	ID            string    `gorm:"primaryKey;type:text" json:"id"`
	Topic         string    `gorm:"index;not null" json:"topic"`
	Payload       []byte    `gorm:"type:blob" json:"payload"`
	ErrorMsg      string    `gorm:"type:text" json:"error_msg"`
	Attempts      int       `json:"attempts"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// TableName DLQ 表名
func (EventDLQ) TableName() string { return "event_dlq" }

type bus struct {
	cfg    Config
	ch     chan Event
	db     *gorm.DB
	mu     sync.RWMutex
	subs   map[string][]Handler
	stop   chan struct{}
	wg     sync.WaitGroup
	stats  Stats
	logger *slog.Logger
}

// New 创建事件总线, 自动启动 dispatcher.
// 需在数据库中先 AutoMigrate(&EventDLQ{}) 否则 DLQ 写入会失败.
func New(db *gorm.DB, cfg Config) Bus {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 1024
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 100 * time.Millisecond
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.MaxPayloadSize <= 0 {
		cfg.MaxPayloadSize = 64 * 1024 // 64KB 默认
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	b := &bus{
		cfg:    cfg,
		ch:     make(chan Event, cfg.BufferSize),
		db:     db,
		subs:   make(map[string][]Handler),
		stop:   make(chan struct{}),
		logger: cfg.Logger,
	}
	for i := 0; i < cfg.WorkerCount; i++ {
		b.wg.Add(1)
		go b.worker()
	}
	return b
}

// Publish 同步发布事件: payload 序列化为 JSON 后入 chan.
// 返 ErrBusClosed 如果总线已 Close; 返 ErrBufferFull 如果 chan 满 (非阻塞).
// P2: 返 ErrPayloadTooLarge 如果 payload 超 MaxPayloadSize (默认 64KB).
func (b *bus) Publish(topic string, payload any) error {
	select {
	case <-b.stop:
		return ErrBusClosed
	default:
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	// P2: payload 大小检查 — 防大 payload 撑爆 chan / DB DLQ
	if len(raw) > b.cfg.MaxPayloadSize {
		return fmt.Errorf("%w: %d > %d bytes", ErrPayloadTooLarge, len(raw), b.cfg.MaxPayloadSize)
	}
	e := Event{
		ID:        newID(),
		Topic:     topic,
		Payload:   raw,
		Timestamp: time.Now().UTC(),
	}
	select {
	case b.ch <- e:
		atomic.AddUint64(&b.stats.Published, 1)
		return nil
	case <-b.stop:
		return ErrBusClosed
	default:
		// 缓冲满: 返错而不是阻塞调用方
		return ErrBufferFull
	}
}

// Subscribe 注册订阅者. 同一 topic 可注册多个 handler (并发派发).
func (b *bus) Subscribe(topic string, h Handler) error {
	if h == nil {
		return errors.New("nil handler")
	}
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], h)
	// audit-P1: 聚合所有 topic 的 handler 数 (旧版只算最后一个 topic, 监控失真)
	b.stats.Subscribers = 0
	for _, handlers := range b.subs {
		b.stats.Subscribers += len(handlers)
	}
	b.mu.Unlock()
	return nil
}

// Close 优雅关闭: 停止接收新事件, 等待已入 chan 的事件处理完.
func (b *bus) Close() error {
	select {
	case <-b.stop:
		return nil // 已关闭
	default:
	}
	close(b.stop)
	b.wg.Wait()
	return nil
}

// Stats 返运行时统计快照
func (b *bus) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return Stats{
		Published:         atomic.LoadUint64(&b.stats.Published),
		Dispatched:        atomic.LoadUint64(&b.stats.Dispatched),
		DLQ:               atomic.LoadUint64(&b.stats.DLQ),
		Retries:           atomic.LoadUint64(&b.stats.Retries),
		Pending:           len(b.ch),
		Subscribers:       b.stats.Subscribers,
		HandlerErrs:       atomic.LoadUint64(&b.stats.HandlerErrs),
		HandlerFinalFails: atomic.LoadUint64(&b.stats.HandlerFinalFails),
	}
}

// worker dispatcher 主循环
func (b *bus) worker() {
	defer b.wg.Done()
	for {
		select {
		case e := <-b.ch:
			b.dispatch(e)
		case <-b.stop:
			// drain remaining
			for {
				select {
				case e := <-b.ch:
					b.dispatch(e)
				default:
					return
				}
			}
		}
	}
}

func (b *bus) dispatch(e Event) {
	// audit-P1: handler panic recover — 防止 worker goroutine 死亡导致 chan 堆积
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("eventbus: handler panic recovered",
				slog.String("event_id", e.ID),
				slog.String("topic", e.Topic),
				slog.Any("panic", r),
			)
			// 入 DLQ 防止事件丢失 (人工可补)
			b.toDLQ(e, fmt.Sprintf("panic: %v", r))
		}
	}()

	b.mu.RLock()
	handlers := b.subs[e.Topic]
	b.mu.RUnlock()

	if len(handlers) == 0 {
		// 无订阅者, 直接入 DLQ (避免事件丢失, 人工可补)
		b.toDLQ(e, "no subscribers")
		return
	}

	for _, h := range handlers {
		b.invokeWithRetry(e, h)
	}
	atomic.AddUint64(&b.stats.Dispatched, 1)
}

func (b *bus) invokeWithRetry(e Event, h Handler) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for attempt := 0; attempt <= b.cfg.MaxRetries; attempt++ {
		err := h(ctx, e)
		if err == nil {
			return
		}
		atomic.AddUint64(&b.stats.HandlerErrs, 1)
		e.Attempts = attempt + 1
		if attempt < b.cfg.MaxRetries {
			atomic.AddUint64(&b.stats.Retries, 1)
			// P2: 指数退避 base << attempt (100ms, 200ms, 400ms...), 替代线性 base*(attempt+1)
			backoff := b.cfg.RetryBackoff << attempt
			time.Sleep(backoff)
			continue
		}
		// P2: 超过 maxRetries, 计入最终失败, 入 DLQ
		atomic.AddUint64(&b.stats.HandlerFinalFails, 1)
		b.toDLQ(e, err.Error())
		return
	}
}

func (b *bus) toDLQ(e Event, reason string) {
	atomic.AddUint64(&b.stats.DLQ, 1)
	if b.db == nil {
		return
	}
	row := &EventDLQ{
		ID:            e.ID,
		Topic:         e.Topic,
		Payload:       e.Payload,
		ErrorMsg:      reason,
		Attempts:      e.Attempts,
		LastAttemptAt: time.Now().UTC(),
		CreatedAt:     e.Timestamp,
	}
	if err := b.db.Create(row).Error; err != nil {
		b.logger.Error("eventbus: DLQ write failed",
			slog.String("event_id", e.ID),
			slog.String("topic", e.Topic),
			slog.String("err", err.Error()),
		)
	}
}

// ==================== 错误 ====================

// ErrBusClosed 总线已关闭
var ErrBusClosed = errors.New("event bus closed")

// ErrBufferFull chan 缓冲满 (非阻塞模式)
var ErrBufferFull = errors.New("event bus buffer full")

// ErrPayloadTooLarge payload 超 MaxPayloadSize (默认 64KB)
var ErrPayloadTooLarge = errors.New("event payload too large")

// newID 生成本事件 ID (基于 timestamp + atomic counter 避免 uuid 依赖)
var idCounter uint64

func newID() string {
	atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), idCounter)
}
