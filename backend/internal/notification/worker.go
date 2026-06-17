package notification

import (
	"context"
	"log"
	"sync"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Worker 异步消费 pending notification_logs
type Worker struct {
	db       *gorm.DB
	tick     time.Duration
	maxBatch int
	resolver func(*models.NotificationChannel) (Sender, error)
	stop     chan struct{}
	wg       sync.WaitGroup
}

// WorkerConfig 配置
type WorkerConfig struct {
	// Tick 拉取间隔 (默认 5s)
	Tick time.Duration
	// MaxBatch 一次最多处理多少条 (默认 50)
	MaxBatch int
	// Resolver Sender 工厂 (默认 notification.Resolver)
	Resolver func(*models.NotificationChannel) (Sender, error)
}

// NewWorker 构造 worker (不启动)
func NewWorker(db *gorm.DB, cfg WorkerConfig) *Worker {
	if cfg.Tick == 0 {
		cfg.Tick = 5 * time.Second
	}
	if cfg.MaxBatch == 0 {
		cfg.MaxBatch = 50
	}
	if cfg.Resolver == nil {
		cfg.Resolver = Resolver
	}
	return &Worker{
		db:       db,
		tick:     cfg.Tick,
		maxBatch: cfg.MaxBatch,
		resolver: cfg.Resolver,
		stop:     make(chan struct{}),
	}
}

// Start 启动后台 goroutine, Start 多次调用是 no-op
func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
}

func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.tickOnce(ctx); err != nil {
				log.Printf("[notification worker] tick error: %v", err)
			}
		}
	}
}

// Stop 停 worker, 阻塞等 goroutine 退出
func (w *Worker) Stop() {
	close(w.stop)
	w.wg.Wait()
}

// tickOnce 单次拉 pending, 逐条发送
func (w *Worker) tickOnce(ctx context.Context) error {
	var logs []models.NotificationLog
	if err := w.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("sent_at ASC").
		Limit(w.maxBatch).
		Find(&logs).Error; err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}
	// 一次性加载所有 channel (避免 N+1)
	channelIDs := make([]uuid.UUID, 0, len(logs))
	seen := make(map[uuid.UUID]struct{})
	for _, l := range logs {
		if _, ok := seen[l.ChannelID]; !ok {
			seen[l.ChannelID] = struct{}{}
			channelIDs = append(channelIDs, l.ChannelID)
		}
	}
	var channels []models.NotificationChannel
	if len(channelIDs) > 0 {
		if err := w.db.WithContext(ctx).
			Where("id IN ?", channelIDs).
			Find(&channels).Error; err != nil {
			return err
		}
	}
	channelMap := make(map[uuid.UUID]models.NotificationChannel, len(channels))
	for _, c := range channels {
		channelMap[c.ID] = c
	}

	for _, logEntry := range logs {
		w.sendOne(ctx, logEntry, channelMap)
	}
	return nil
}

func (w *Worker) sendOne(ctx context.Context, entry models.NotificationLog, channelMap map[uuid.UUID]models.NotificationChannel) {
	ch, ok := channelMap[entry.ChannelID]
	if !ok {
		w.markFailed(ctx, entry.ID, "channel not found: "+entry.ChannelID.String())
		return
	}
	if !ch.IsEnabled {
		w.markSkipped(ctx, entry.ID)
		return
	}
	sender, err := w.resolver(&ch)
	if err != nil {
		w.markFailed(ctx, entry.ID, err.Error())
		return
	}
	// 30s 超时, 防某个 channel 卡死整批
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := sender.Send(sendCtx, entry.Recipient, entry.Content); err != nil {
		w.markFailed(ctx, entry.ID, err.Error())
		return
	}
	w.markSuccess(ctx, entry.ID)
}

func (w *Worker) markSuccess(ctx context.Context, id uuid.UUID) {
	now := time.Now()
	w.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":    "success",
			"sent_at":   now,
			"error_msg": "",
		})
}

func (w *Worker) markFailed(ctx context.Context, id uuid.UUID, errMsg string) {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	w.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":    "failed",
			"error_msg": errMsg,
		})
}

// markSkipped channel 禁用, 标记 success 不重试
func (w *Worker) markSkipped(ctx context.Context, id uuid.UUID) {
	w.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":    "success",
			"error_msg": "channel disabled, skipped",
		})
}
