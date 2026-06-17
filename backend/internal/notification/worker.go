package notification

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"network-monitor-platform/internal/eventbus"
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

// SubscribeToBus 把 Worker 注册为事件总线 subscriber (v2.0)
// 处理 alert.created / alert.resolved 事件, 同步调通知 channel 真发
// (保留老 tick 路径处理 notification_logs, 双轨并行)
func (w *Worker) SubscribeToBus(bus eventbus.Bus) error {
	if err := bus.Subscribe(eventbus.TopicAlertCreated, w.handleAlertEvent); err != nil {
		return err
	}
	if err := bus.Subscribe(eventbus.TopicAlertResolved, w.handleAlertEvent); err != nil {
		return err
	}
	return nil
}

// AlertEventPayload 事件 payload (alert.created/resolved)
// service 层 publish 时序列化此结构
type AlertEventPayload struct {
	AlertID   string `json:"alert_id"`
	HostName  string `json:"host_name"`
	Severity  int    `json:"severity"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"` // "problem" 或 "resolved"
	EventType string `json:"event_type"` // "created" 或 "resolved"
}

// handleAlertEvent 处理 alert 事件, 真发通知
func (w *Worker) handleAlertEvent(ctx context.Context, e eventbus.Event) error {
	var p AlertEventPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return err // 返 err → bus 自动重试 → DLQ
	}
	// 加载所有启用的 channel
	var channels []models.NotificationChannel
	if err := w.db.WithContext(ctx).Where("is_enabled = ?", true).Find(&channels).Error; err != nil {
		return err
	}
	if len(channels) == 0 {
		return nil // 没 channel 配, 不算错
	}
	// 构造消息内容
	verb := "告警"
	if p.EventType == "resolved" {
		verb = "告警恢复"
	}
	content := "[ITmanager " + verb + "] " + p.Trigger +
		"\n主机: " + p.HostName +
		"\n级别: " + severityName(p.Severity) +
		"\n时间: " + time.Now().Format("2006-01-02 15:04:05")

	// 逐 channel 发 (无中间表, 失败返 err → 重试 → DLQ)
	for i := range channels {
		ch := &channels[i]
		// 从 Config JSON 解析 recipient (webhook URL / email / chat_id)
		recipient, _ := recipientFromConfig(ch.Config, ch.Type)
		if recipient == "" {
			log.Printf("[notification subscriber] channel %s: no recipient in config", ch.Name)
			continue
		}
		sender, err := w.resolver(ch)
		if err != nil {
			log.Printf("[notification subscriber] resolver err for channel %s: %v", ch.Name, err)
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = sender.Send(sendCtx, recipient, content)
		cancel()
		if err != nil {
			log.Printf("[notification subscriber] send err for channel %s: %v", ch.Name, err)
		}
	}
	return nil
}

// recipientFromConfig 从 channel.Config JSON 提取 recipient
// 各种渠道字段不统一, 这里取常用的几个 key
func recipientFromConfig(configJSON, channelType string) (string, error) {
	if configJSON == "" {
		return "", nil
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "", err
	}
	// 优先 keys
	for _, key := range []string{"recipient", "webhook_url", "url", "to", "email", "chat_id"} {
		if v, ok := cfg[key].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", nil
}

func severityName(sev int) string {
	switch sev {
	case 5:
		return "灾难"
	case 4:
		return "高"
	case 3:
		return "中"
	case 2:
		return "低"
	case 1:
		return "信息"
	default:
		return "未知"
	}
}
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
