package notification

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/redact"

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
	// M38-B Round 7: fire 路径 in-memory 60s dedup
	// nil 时跳过（与历史语义兼容 —— 即同一个 alert 60s 内多 publish 全部推）
	deduper *Deduper
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
	// M38-B Round 7: 默认启动期注入 deduper (60s 窗口)
	// 注入点放在这里而不是 NewWorker: 让通知单元测试可不构造 deduper 直接驱动 worker,
	// 而生产 (main.go 调用 Start 后) 一定走 dedup。
	if w.deduper == nil {
		w.deduper = NewDeduper()
	}
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
	Status    string `json:"status"`     // "problem" 或 "resolved"
	EventType string `json:"event_type"` // "created" 或 "resolved"

	// M37-A：让 worker 能按 AlertRule.NotifyChannels 过滤推送，避免二次 DB 读 rule
	// RuleID 空 → worker fallback 推全启用 channels（与改动前一致）
	RuleID           string   `json:"rule_id,omitempty"`
	NotifyChannelIDs []string `json:"notify_channel_ids,omitempty"` // 解析后的 UUID 字符串数组；空 = "推全启用"

	// M38-B Round 6：worker 增加按 NotifyUsers 推送（与 NotifyChannels 平级）。
	// 空 = "不带 NotifyUsers" (与改动前一致——不漏 channel 通知, 仅 user 通知跳过)。
	NotifyUserIDs []string `json:"notify_user_ids,omitempty"`

	// M38-B Round 7: fire-path dedup key (trigger + problem_start unix)
	// 老事件 payload 没这两个字段 → 空字符串 / 0, FireKey 仍生成唯一 key
	TriggerID        string `json:"trigger_id,omitempty"`
	ProblemStartUnix int64  `json:"problem_start_unix,omitempty"`
}

// handleAlertEvent 处理 alert 事件, 真发通知
func (w *Worker) handleAlertEvent(ctx context.Context, e eventbus.Event) error {
	var p AlertEventPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return err // 返 err → bus 自动重试 → DLQ
	}
	// M38-B Round 7: fire 路径 60s 内同 (trigger + problem_start) 不重复推送
	// TriggerID 来自 payload (alert.source_trigger_id / zabbix 的 trigger_id);
	// 老事件 payload 没这个字段 → 空字符串, 退化到 key="|<unix>" 仍能 dedup 同事件
	// 注意: bus 接收方是单 goroutine 处理 (handleAlertEvent 同步执行),
	// dedup 的并发安全只在「同一进程多实例」或「多 topic 同时 publish」场景里生效
	if w.deduper != nil && !w.deduper.Allow(FireKey(p.TriggerID, p.ProblemStartUnix)) {
		log.Printf("[notification subscriber] dedup 60s 命中: trigger=%s problem_start=%d skip",
			stripControlChars(p.TriggerID), p.ProblemStartUnix)
		return nil
	}
	// 加载所有启用的 channel
	var channels []models.NotificationChannel
	if err := w.db.WithContext(ctx).Where("is_enabled = ?", true).Find(&channels).Error; err != nil {
		return err
	}
	// M38-B Round 10 修复: 即使 channels 全表为空, 也要走 NotifyUsers 路径 (NotifyUsers 与 channels 平级)
	// 旧逻辑: len(channels) == 0 → return nil → 不发 user 通知, 把 NotifyUsers 也吞掉了
	// 新逻辑: 0 条时 for 循环天然空, NotifyUsers 在 channel 路径后独立走
	// M37-A：AlertRule.NotifyChannels 写不读修复
	// NotifyChannelIDs != nil → 调用方明确给出要推的 channel ID 列表（解析自 AlertRule.NotifyChannels）
	//   - 非空 → 过滤 channels，只推被勾的
	//   - 空数组 → 运维明确清空 → 推 0 次
	// NotifyChannelIDs == nil → 旧路径，向后兼容（payload 没 RuleID，走全启用 fallback）
	if p.NotifyChannelIDs != nil {
		channels = filterChannelsByIDs(channels, p.NotifyChannelIDs)
		if len(channels) == 0 {
			log.Printf("[notification subscriber] rule %s: no enabled channels after NotifyChannels filter (configured=%d), skip",
				stripControlChars(p.RuleID), len(p.NotifyChannelIDs))
			return nil
		}
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
			// M29-F：ch.Name 来自 notification_channels.name（仅校验非空，无控制字符校验），
			// 行式消费的日志里 CR/LF 能伪造出一整行。它是运维自己的显示名，不是错误文本，
			// 故只剥控制字符、不脱敏（脱敏会误伤含 URL 形状的正常渠道名）。
			log.Printf("[notification subscriber] channel %s: no recipient in config", stripControlChars(ch.Name))
			continue
		}
		sender, err := w.resolver(ch)
		if err != nil {
			// G-28：第三方 sender（RegisterSender）的构造错误不受我们控制，出口统一脱敏
			log.Printf("[notification subscriber] resolver err for channel %s: %s",
				stripControlChars(ch.Name), redact.Text(stripControlChars(err.Error())))
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = sender.Send(sendCtx, recipient, content)
		cancel()
		if err != nil {
			log.Printf("[notification subscriber] send err for channel %s: %s",
				stripControlChars(ch.Name), redact.Text(stripControlChars(err.Error())))
		}
	}
	// M38-B Round 6: NotifyUsers 处理（与 channel 推送平级，独立路径）
	// p.NotifyUserIDs != nil 但 len == 0 → 运维显式清空 → 不发 user 通知（与 NotifyChannelIDs 语义对齐）
	// p.NotifyUserIDs == nil → 老路径（payload 没 NotifyUserIDs 字段, 历史 alert）→ 不发 user 通知
	// p.NotifyUserIDs 非空 → 对每个 user_id 查 users.email / users.phone → 按 channel.type 选推送方式
	if p.NotifyUserIDs != nil {
		w.deliverToUsers(ctx, p.NotifyUserIDs, p)
	}
	return nil
}

// deliverToUsers M38-B Round 6: 按 user_id 解析 users.email / users.phone，逐个发通知
//
// 推送方式：
//   - users.email 非空 → 找 email channel (type='email'), Send
//   - users.phone 非空 → 找 wechat / 第三方 channel 推送
//   - contact 都空 → log warn + 跳过该 user（不漏 channel 通知）
//
// 与 channels 推送的关系：
//   - channels 是「群发」语义（一个 channel 多人收），NotifyUsers 是「单发」语义
//   - 实际推送可同时进行（用户既配了 channel 接收组也配了 user 直发）
//
// 写日志保留用户 ID 部分以便排障，但**绝不**直接 log 用户的 email/phone（PII / 凭据）
func (w *Worker) deliverToUsers(ctx context.Context, userIDs []string, p AlertEventPayload) {
	if len(userIDs) == 0 {
		return
	}
	parsedIDs := make([]uuid.UUID, 0, len(userIDs))
	skipUser := make(map[string]bool, len(userIDs))
	for _, sid := range userIDs {
		id, err := uuid.Parse(sid)
		if err != nil {
			log.Printf("[notification subscriber] NotifyUsers: 非法 UUID %q 跳过", stripControlChars(sid))
			skipUser[sid] = true
			continue
		}
		parsedIDs = append(parsedIDs, id)
	}
	if len(parsedIDs) == 0 {
		return
	}
	var users []models.User
	if err := w.db.WithContext(ctx).
		Where("id IN ?", parsedIDs).
		Find(&users).Error; err != nil {
		log.Printf("[notification subscriber] NotifyUsers: load users failed: %v → skip", err)
		return
	}
	if len(users) == 0 {
		log.Printf("[notification subscriber] NotifyUsers: 0 users found, skip")
		return
	}
	verb := "告警"
	if p.EventType == "resolved" {
		verb = "告警恢复"
	}
	content := "[ITmanager " + verb + "] " + p.Trigger +
		"\n主机: " + p.HostName +
		"\n级别: " + severityName(p.Severity) +
		"\n时间: " + time.Now().Format("2006-01-02 15:04:05")
	for i := range users {
		u := &users[i]
		contact := pickUserContact(u)
		if contact == "" {
			log.Printf("[notification subscriber] NotifyUsers user %s: email/phone 为空，跳过", u.ID)
			continue
		}
		// 复用现有 channel 的发送通道：找含 recipient 与该 contact 匹配的 enabled channel，
		// 找不到则降级推邮件 (email channel)。
		ch, err := w.findUserChannel(ctx, contact)
		if err != nil || ch == nil {
			log.Printf("[notification subscriber] NotifyUsers user %s: no usable channel for contact, skip", u.ID)
			continue
		}
		sender, err := w.resolver(ch)
		if err != nil {
			log.Printf("[notification subscriber] NotifyUsers user %s resolver err: %s",
				u.ID, redact.Text(stripControlChars(err.Error())))
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = sender.Send(sendCtx, contact, content)
		cancel()
		if err != nil {
			log.Printf("[notification subscriber] NotifyUsers user %s send err: %s",
				u.ID, redact.Text(stripControlChars(err.Error())))
		}
	}
}

// pickUserContact 选 user 的 contact: email 优先，没 email 用 phone，都空返 ""
// 把「任一联系渠道能用」的语义明确写出来，别漏。
func pickUserContact(u *models.User) string {
	if u.Email != "" {
		return u.Email
	}
	return u.Phone
}

// findUserChannel 给定收件人 email/phone, 找一条 enabled channel whose recipient 包含该值。
// 返回 nil 不算 err — 表示当前没有可用的 channel 推给该 contact。
//
// 实现按「channel.Config JSON 内含有 contact 字段」查 — 用 sql LIKE 而不是 JSON 操作，
// 因为 contact 长这样：`email@x.com`，结构不固定，LIKE 足够窄匹配。
// 单实例性能足够, 千人级 NotifyUsers 不构成 hot path。
func (w *Worker) findUserChannel(ctx context.Context, contact string) (*models.NotificationChannel, error) {
	if contact == "" {
		return nil, nil
	}
	var ch models.NotificationChannel
	err := w.db.WithContext(ctx).
		Where("is_enabled = ? AND config LIKE ?", true, "%"+contact+"%").
		First(&ch).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
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
	// 错误文本来自第三方（SMTP 服务端文本、上游响应体），可能带凭据（URL 的
	// query/path/userinfo）。两个职责都要做，而**顺序是安全边界**：
	//   先剥控制字符，再脱敏。
	// 反过来的话，CR/LF 会把脱敏规则的值类截断（`password=abc\nDEF` 时 Text 只遮到
	// `\n` 为止），随后再把 `\n` 剥掉，等于把**未遮盖的尾部接回**一个已被认成凭据的
	// 串上 → `password=***DEF` 明文入库。
	// M29 之前这里正是反的（Text → … → stripControlChars），是真泄漏；同包的
	// sender.sanitizeSnippet 一直是正确顺序（Strip→Text），以它为准。
	errMsg = stripControlChars(errMsg)
	errMsg = redact.Text(errMsg)
	// 非法 UTF-8：PostgreSQL 直接拒收（22021）→ 这一行永远停在 pending 被无限重发。
	// stripControlChars 经 rune 迭代已把非法字节换成 U+FFFD，故这里是兜底（通常 no-op），
	// 保留它是因为「保证合法 UTF-8」这个不变式不该依赖调用顺序。
	errMsg = strings.ToValidUTF8(errMsg, "\ufffd")
	// 按 rune 截断：列是 varchar(500)（**字符**数，migrations/000009），按字节截断
	// 会切断多字节字符 → PostgreSQL 拒收（22021）→ 这一行永远停在 pending 被重发。
	if utf8.RuneCountInString(errMsg) > 500 {
		errMsg = string([]rune(errMsg)[:500])
	}
	if err := w.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":    "failed",
			"error_msg": errMsg,
		}).Error; err != nil {
		// 旧版丢弃返回值：写库失败无声无息（行留在 pending 被无限重发）
		log.Printf("[notification worker] markFailed %s: %s", id, redact.Text(stripControlChars(err.Error())))
	}
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

// M37-A：filterChannelsByIDs 保留在 wantIDs 列表里的 channel (UUID 字符串比对)
// 用 map 避免 O(N*M) 双层循环；wantIDs 是 payload 里 snapshot 的 AlertRule.NotifyChannels 解析结果
// 顺序保留输入顺序 (运维 UI 通常按勾选顺序展示, 用户期望推送顺序稳定)
func filterChannelsByIDs(channels []models.NotificationChannel, wantIDs []string) []models.NotificationChannel {
	if len(wantIDs) == 0 {
		return nil
	}
	wantSet := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		wantSet[id] = struct{}{}
	}
	out := make([]models.NotificationChannel, 0, len(wantIDs))
	for i := range channels {
		ch := &channels[i]
		if _, ok := wantSet[ch.ID.String()]; ok {
			out = append(out, *ch)
		}
	}
	return out
}

// M37-A：测试专用 exported wrapper, 让外部包 (db_smoke) 能驱动 handleAlertEvent
// 不动方法语义, 仅做导出 (设计理由: 真 PG 集成测试需要"喂事件→观察推送"链路, 单测 sqlmock 已覆盖单元边界)
func (w *Worker) HandleAlertEventForTest(ctx context.Context, e eventbus.Event) error {
	return w.handleAlertEvent(ctx, e)
}

// M38-B：测试专用 exported wrapper, 让外部包 (db_smoke) 能注入 deduper
// 生产路径靠 Start() 自动注入；测试需要直接 Set (Start 起 goroutine 不便)
func (w *Worker) SetDeduperForTest(d *Deduper) {
	w.deduper = d
}
