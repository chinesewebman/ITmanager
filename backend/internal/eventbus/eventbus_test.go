package eventbus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newTestBus(t *testing.T) (Bus, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	cfg := Config{
		BufferSize:   8,
		MaxRetries:   2,
		RetryBackoff: 5 * time.Millisecond,
		WorkerCount:  2,
	}
	return New(gormDB, cfg), mock
}

func TestPublish_Subscribe_单handler收事件(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	var got Event
	var mu sync.Mutex
	done := make(chan struct{})
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		mu.Lock()
		got = e
		mu.Unlock()
		close(done)
		return nil
	}))

	require.NoError(t, bus.Publish(TopicAlertCreated, map[string]string{"id": "a1"}))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called within 2s")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, TopicAlertCreated, got.Topic)
	assert.Contains(t, string(got.Payload), `"id":"a1"`)
}

func TestPublish_多个handler同topic都收到(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	var n1, n2 atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	require.NoError(t, bus.Subscribe(TopicAlertResolved, func(ctx context.Context, e Event) error {
		n1.Add(1)
		wg.Done()
		return nil
	}))
	require.NoError(t, bus.Subscribe(TopicAlertResolved, func(ctx context.Context, e Event) error {
		n2.Add(1)
		wg.Done()
		return nil
	}))

	require.NoError(t, bus.Publish(TopicAlertResolved, nil))
	wg.Wait()
	assert.Equal(t, int32(1), n1.Load())
	assert.Equal(t, int32(1), n2.Load())
}

func TestPublish_无订阅者入DLQ(t *testing.T) {
	bus, mock := newTestBus(t)
	defer bus.Close()

	// 期望: gorm Create 走事务 + INSERT (Postgres PreferSimpleProtocol 用 Exec)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, bus.Publish(TopicUserLocked, nil))

	// 等 dispatcher 处理
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stats := bus.Stats()
		if stats.DLQ > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, bus.Stats().DLQ, uint64(1))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublish_Handler返err进入重试(t *testing.T) {
	bus, mock := newTestBus(t)
	defer bus.Close()

	var attempts atomic.Int32
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		attempts.Add(1)
		return errors.New("simulated")
	}))

	// 期望: MaxRetries=2 → 3 次 handler 调用 + 1 次 DLQ
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, bus.Publish(TopicAlertCreated, nil))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bus.Stats().DLQ > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.Equal(t, int32(3), attempts.Load(), "应该 3 次尝试 (0+retry*2)")
	assert.GreaterOrEqual(t, bus.Stats().Retries, uint64(2))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublish_Buffer满返ErrBufferFull(t *testing.T) {
	// 不开 worker: 模拟 chan 满
	mockDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})

	// WorkerCount=0 意味着没人消费
	bus := New(gormDB, Config{BufferSize: 2, WorkerCount: 0})
	defer bus.Close()

	// 2 个能入, 第 3 个返 ErrBufferFull
	assert.NoError(t, bus.Publish(TopicAlertCreated, nil))
	assert.NoError(t, bus.Publish(TopicAlertCreated, nil))
	err = bus.Publish(TopicAlertCreated, nil)
	assert.ErrorIs(t, err, ErrBufferFull)
}

func TestClose_后Publish返ErrBusClosed(t *testing.T) {
	bus, _ := newTestBus(t)
	require.NoError(t, bus.Close())
	err := bus.Publish(TopicAlertCreated, nil)
	assert.ErrorIs(t, err, ErrBusClosed)
}

func TestClose_幂等(t *testing.T) {
	bus, _ := newTestBus(t)
	require.NoError(t, bus.Close())
	assert.NoError(t, bus.Close()) // 不应 panic
}

func TestStats_计数正确(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	var received atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		received.Add(1)
		wg.Done()
		return nil
	}))

	for i := 0; i < 3; i++ {
		require.NoError(t, bus.Publish(TopicAlertCreated, nil))
	}
	wg.Wait()

	stats := bus.Stats()
	assert.GreaterOrEqual(t, stats.Published, uint64(3))
	assert.GreaterOrEqual(t, stats.Dispatched, uint64(3))
	assert.Equal(t, 0, stats.Pending, "都消费完了, pending 应为 0")
}

func TestSubscribe_不同topic互不影响(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	var alertCount, ticketCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		alertCount.Add(1)
		wg.Done()
		return nil
	}))
	require.NoError(t, bus.Subscribe(TopicTicketCreated, func(ctx context.Context, e Event) error {
		ticketCount.Add(1)
		return nil
	}))

	require.NoError(t, bus.Publish(TopicAlertCreated, nil))
	wg.Wait()
	// 等 ticket 跑完 (虽然没人 publish TicketCreated, 但 stats 也应体现)
	assert.Equal(t, int32(1), alertCount.Load())
	assert.Equal(t, int32(0), ticketCount.Load())
}

func TestPublish_并发安全(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	var received atomic.Int32
	var wg sync.WaitGroup
	wg.Add(100)
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		received.Add(1)
		wg.Done()
		return nil
	}))

	var pubWG sync.WaitGroup
	for i := 0; i < 100; i++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			_ = bus.Publish(TopicAlertCreated, nil)
		}()
	}
	pubWG.Wait()
	wg.Wait()
	assert.Equal(t, int32(100), received.Load())
}

func TestSubscribe_nilHandler返err(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()
	assert.Error(t, bus.Subscribe(TopicAlertCreated, nil))
}

// TestSubscribe_Subscribers跨topic聚合 (audit-P1 回归)
// 修前 bug: 只统计最后一个 Subscribe 的 topic 的 handler 数
// 修后: 聚合所有 topic 的 handler 总数
func TestSubscribe_Subscribers跨topic聚合(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()

	h := func(ctx context.Context, e Event) error { return nil }

	// topic A: 2 个 handler
	require.NoError(t, bus.Subscribe(TopicAlertCreated, h))
	require.NoError(t, bus.Subscribe(TopicAlertCreated, h))
	assert.Equal(t, 2, bus.Stats().Subscribers, "topic A 2 handler")

	// topic B: 3 个 handler → 总 5
	require.NoError(t, bus.Subscribe(TopicAlertResolved, h))
	require.NoError(t, bus.Subscribe(TopicAlertResolved, h))
	require.NoError(t, bus.Subscribe(TopicAlertResolved, h))
	assert.Equal(t, 5, bus.Stats().Subscribers, "跨 topic 聚合应为 5")
}

// TestPublish_HandlerPanic不挂worker (audit-P1 回归)
// 修前 bug: handler panic 直接挂 worker goroutine, 后继事件堆积
// 修后: dispatch 层 defer recover, 事件入 DLQ, worker 健在
func TestPublish_HandlerPanic不挂worker(t *testing.T) {
	bus, mock := newTestBus(t)
	defer bus.Close()

	var calls atomic.Int32
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		calls.Add(1)
		panic("simulated panic")
	}))

	// 第一次 panic → DLQ
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, bus.Publish(TopicAlertCreated, nil))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bus.Stats().DLQ >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, calls.Load(), int32(1), "handler 至少被调一次")
	assert.GreaterOrEqual(t, bus.Stats().DLQ, uint64(1), "panic 后事件入 DLQ")

	// 第二次 publish → worker 仍健在 (仍 panic → 仍 DLQ)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	require.NoError(t, bus.Publish(TopicAlertCreated, nil))

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bus.Stats().DLQ >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, bus.Stats().DLQ, uint64(2), "worker 健在, 第二次 panic 也入 DLQ")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublish_payload无法序列化返err(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Close()
	// channel 不可 json.Marshal
	err := bus.Publish(TopicAlertCreated, make(chan int))
	assert.Error(t, err)
}

// TestPublish_超大payload返ErrPayloadTooLarge (P2)
func TestPublish_超大payload返ErrPayloadTooLarge(t *testing.T) {
	mockDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})

	bus := New(gormDB, Config{MaxPayloadSize: 100, WorkerCount: 0})
	defer bus.Close()

	// payload ~50 bytes 应通过
	require.NoError(t, bus.Publish(TopicAlertCreated, map[string]string{"id": "abc"}))

	// payload >100 bytes 应返 ErrPayloadTooLarge
	big := strings.Repeat("x", 200)
	err = bus.Publish(TopicAlertCreated, map[string]string{"data": big})
	assert.ErrorIs(t, err, ErrPayloadTooLarge)
}

// TestPublish_HandlerErrs与HandlerFinalFails区分 (P2)
// handler 返 err 后 retry, 最终入 DLQ; HandlerErrs 含 retry 次数, HandlerFinalFails 仅 1
func TestPublish_HandlerErrs与HandlerFinalFails区分(t *testing.T) {
	bus, mock := newTestBus(t)
	defer bus.Close()

	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		return errors.New("simulated")
	}))

	// MaxRetries=2 → 3 次 handler 调用 → 1 次 DLQ
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, bus.Publish(TopicAlertCreated, nil))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bus.Stats().DLQ > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stats := bus.Stats()
	assert.Equal(t, uint64(3), stats.HandlerErrs, "3 次 handler 调用均返 err")
	assert.Equal(t, uint64(1), stats.HandlerFinalFails, "最终失败 1 次 (retry 耗尽后)")
	assert.GreaterOrEqual(t, stats.DLQ, uint64(1), "入 DLQ")
	assert.NoError(t, mock.ExpectationsWereMet())
}
