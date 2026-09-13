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
		// 缓冲给足：Publish 是**非阻塞**的，满了直接返 ErrBufferFull 丢事件。
		// 用 8 号小缓冲时，并发发布测试会丢事件 → wg.Wait() 永久阻塞（CI 10 分钟超时）。
		BufferSize:   1024,
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

	// 等「DB 写入完成」而不是等 stats：toDLQ 先加 stats.DLQ 再写库，
	// 等 stats 会在 Begin/Exec/Commit 还没跑完时就断言 ExpectationsWereMet（偶发失败）。
	require.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil },
		5*time.Second, 10*time.Millisecond, "无订阅者的事件应写入 DLQ")
	assert.GreaterOrEqual(t, bus.Stats().DLQ, uint64(1))
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

	// 等 DB 写入完成（DLQ 落库 = 3 次尝试都跑完了），再断言计数
	require.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil },
		5*time.Second, 10*time.Millisecond, "重试耗尽后应写入 DLQ")
	assert.Equal(t, int32(3), attempts.Load(), "应该 3 次尝试 (0+retry*2)")
	assert.GreaterOrEqual(t, bus.Stats().Retries, uint64(2))
}

func TestPublish_Buffer满返ErrBufferFull(t *testing.T) {
	mockDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})

	bus := New(gormDB, Config{BufferSize: 2, WorkerCount: 2})
	defer bus.Close() // 先注册 Close, 再注册 close(release): LIFO 下 worker 先解阻塞再等退出

	// 让 worker 全卡在 handler 上, chan(cap=2) 才可能真正满。
	// 旧写法用 WorkerCount: 0 想「不开 worker」, 但 New 把 <=0 归一成默认 4 ——
	// 第 3 个 Publish 是否满全看调度, CI 上偶发失败（eventbus_test.go:156）。
	release := make(chan struct{})
	defer close(release)
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		<-release
		return nil
	}))

	// 一直发到满: 2 个 worker 各占 1 个 + 缓冲 2 个之后, 再发必然返 ErrBufferFull（非阻塞）
	require.Eventually(t, func() bool {
		return errors.Is(bus.Publish(TopicAlertCreated, nil), ErrBufferFull)
	}, 2*time.Second, 5*time.Millisecond, "缓冲填满后 Publish 应返 ErrBufferFull")
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

// TestPublish_并发安全 race detector 守门 (M41): 100 个并发 Publish, 跑
// `go test -race` 必过. 任何 newID 的两步原子实现回归都会让这测试 FAIL.
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
	// 有界等待：万一将来又丢事件（Publish 满缓冲返 ErrBufferFull），
	// 5s 内给出明确失败，而不是 wg.Wait() 挂到 10 分钟包超时。
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("只收到 %d/100 个事件（Publish 缓冲满会丢事件，检查 BufferSize）", received.Load())
	}
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
	// 单 worker + sqlmock 顺序期望：DLQ 写入严格串行（两个 worker 并发写会
	// 交错成 Begin/Begin，命中 sqlmock 的 "Begin was not expected"）。
	// 单 worker 也更贴合本用例意图 —— 同一个 worker 必须能接着处理第二个事件。
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	bus := New(gormDB, Config{
		BufferSize:   8,
		MaxRetries:   2,
		RetryBackoff: 5 * time.Millisecond,
		WorkerCount:  1,
	})
	defer bus.Close()

	var calls atomic.Int32
	require.NoError(t, bus.Subscribe(TopicAlertCreated, func(ctx context.Context, e Event) error {
		calls.Add(1)
		panic("simulated panic")
	}))

	// 两次 DLQ 的期望都在 publish 前登记好
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "event_dlq"`).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	// 第一次 panic → DLQ；第二次 publish → worker 仍健在 (仍 panic → 仍 DLQ)
	require.NoError(t, bus.Publish(TopicAlertCreated, nil))
	require.NoError(t, bus.Publish(TopicAlertCreated, nil))

	// 等 DB 写入完成（不是等 stats：toDLQ 先加 stats.DLQ 再写库，等 stats 会抢跑）
	require.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil },
		10*time.Second, 10*time.Millisecond, "两次 panic 都应写入 DLQ")
	assert.GreaterOrEqual(t, bus.Stats().DLQ, uint64(2), "worker 健在, 两次 panic 都入 DLQ")
	assert.GreaterOrEqual(t, calls.Load(), int32(2), "handler 每次都应被调用")
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

	// 等 DB 写入完成（DLQ 落库 = retry 耗尽），再读 stats 断言
	require.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil },
		5*time.Second, 10*time.Millisecond, "retry 耗尽后应写入 DLQ")
	stats := bus.Stats()
	assert.Equal(t, uint64(3), stats.HandlerErrs, "3 次 handler 调用均返 err")
	assert.Equal(t, uint64(1), stats.HandlerFinalFails, "最终失败 1 次 (retry 耗尽后)")
	assert.GreaterOrEqual(t, stats.DLQ, uint64(1), "入 DLQ")
}
