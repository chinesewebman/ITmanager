// metric_sync.go: Zabbix 兜底采集 worker + 单次同步方法（v2.3）。
//
// 目的：内部采集器（Prometheus exporter、自家 agent）失效时，Zabbix 的 lastvalue
// 作为数据源写入 metric_snapshots 表，让 NMP 看板在断流期间仍有单点数据可展示。
//
// 设计取舍（vs history.get）：
//   - item.get + lastvalue = 1 API call + 无时间范围参数
//   - history.get 需要 time_from/time_to + history 处理 + 多次分页
//   - 兜底语义下"最新一个值"已足够；时序连续性是采集器该负责的事，不是兜底的责任
//
// 已知 gap（v2.3 接受）：
//   - Zabbix host.name 必须 == Asset.Name 才能关联上；不匹配 → 跳过 + 计数
//     后续可加 `assets.zabbix_host` 列做硬映射（不在 v2.3 scope）
//   - 仅拉 numeric value_type（0/1/3/4），跳过 log
//   - lastvalue 是字符串，"1.23" 解析失败 → 跳过 + 计数
package integration

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
)

// MetricSyncConfig worker 配置
type MetricSyncConfig struct {
	// Tick 拉取间隔（默认 5min）
	Tick time.Duration
	// BatchLimit 单次 bulk insert 上限（默认 1000，与 service 一致）
	BatchLimit int
	// Disabled true 时 worker 不启动（用于测试 / 临时关闭）
	Disabled bool
}

// MetricSyncWorker Zabbix → metric_snapshots 兜底 worker
type MetricSyncWorker struct {
	svc        *IntegrationService // v2.3: 复用同一个 ZabbixClient，UI Reload 立即生效
	db         *gorm.DB
	tick       time.Duration
	batchLimit int
	stop       chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	started    bool
	stopped    bool // 防止 double Stop close channel panic
}

// NewMetricSyncWorker 构造 worker（不启动）
// v2.3: 直接复用 svc.zabbix，不另起 ZabbixClient（避免 UI Reload 后 worker 用旧 URL）。
func NewMetricSyncWorker(svc *IntegrationService, db *gorm.DB, cfg MetricSyncConfig) *MetricSyncWorker {
	if cfg.Tick == 0 {
		cfg.Tick = 5 * time.Minute
	}
	if cfg.BatchLimit == 0 {
		cfg.BatchLimit = 1000
	}
	return &MetricSyncWorker{
		svc:        svc,
		db:         db,
		tick:       cfg.Tick,
		batchLimit: cfg.BatchLimit,
		stop:       make(chan struct{}),
	}
}

// Start 启动后台 goroutine，重复调用 no-op
func (w *MetricSyncWorker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.started = true
	w.wg.Add(1)
	go w.run(ctx)
}

// Stop 停 worker，阻塞等 goroutine 退出。
// 重复调用 / 未 Start 情况下调用都是 no-op（防止 close(w.stop) double panic）。
func (w *MetricSyncWorker) Stop() {
	w.mu.Lock()
	if !w.started || w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()
	close(w.stop)
	w.wg.Wait()
}

// run ticker loop
func (w *MetricSyncWorker) run(ctx context.Context) {
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
			if _, err := SyncMetricsFromZabbix(ctx, w.svc.zabbix, w.db, w.batchLimit); err != nil {
				log.Printf("[zabbix metric sync] tick error: %v", err)
			}
		}
	}
}

// SyncMetricsFromZabbix 单次同步：从 Zabbix 拉所有 numeric item 的 lastvalue，
// 按 Host.name == Asset.Name 关联，写入 metric_snapshots。
// 返回写入条数。集成未配置（URL 空）→ 返 0, nil。
//
// 幂等：每条 (asset_id, key, ts) 唯一。同一 tick 内 ts 一致 → 重启时仍可能
// 因 ts 偏移产生重复行 — 接受，作为时序数据的"重复点"语义。
func SyncMetricsFromZabbix(ctx context.Context, z *ZabbixClient, db *gorm.DB, batchLimit int) (int, error) {
	// 1) Zabbix 未配置（URL 空）→ 静默 no-op，避免每次 tick 都刷日志
	if z == nil || z.user == "" || z.password == "" {
		return 0, nil
	}

	// 2) 拉 item.get（自动 login 复用 client）
	items, err := z.GetMetricItems(ctx)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	// 3) 收集 host 列表（去重），一次 select 查所有 asset
	hostSet := make(map[string]struct{}, len(items))
	for i := range items {
		if len(items[i].Hosts) == 0 {
			continue
		}
		hostSet[items[i].Hosts[0].Host] = struct{}{}
	}
	if len(hostSet) == 0 {
		return 0, nil
	}
	hostNames := make([]string, 0, len(hostSet))
	for n := range hostSet {
		hostNames = append(hostNames, n)
	}
	var assets []models.Asset
	if err := db.WithContext(ctx).
		Where("name IN ?", hostNames).
		Find(&assets).Error; err != nil {
		return 0, err
	}
	assetByName := make(map[string]uuid.UUID, len(assets))
	for i := range assets {
		assetByName[assets[i].Name] = assets[i].ID
	}

	// 4) 构造 snaps（每 tick 一条 / asset_id+key）
	now := time.Now()
	snaps := make([]models.MetricSnapshot, 0, len(items))
	var skipped int
	for i := range items {
		it := &items[i]
		if len(it.Hosts) == 0 {
			skipped++
			continue
		}
		hostName := it.Hosts[0].Host
		assetID, ok := assetByName[hostName]
		if !ok {
			skipped++
			continue
		}
		if it.LastValue == "" {
			skipped++
			continue
		}
		// value_type=1 (char) 可能是 "AA"/"BB" 这种，无法 ParseFloat — 跳过即可
		v, err := strconv.ParseFloat(it.LastValue, 64)
		if err != nil {
			skipped++
			continue
		}
		snaps = append(snaps, models.MetricSnapshot{
			AssetID: assetID,
			Key:     it.Key,
			Value:   v,
			TS:      now,
		})
	}
	if len(snaps) == 0 {
		log.Printf("[zabbix metric sync] items=%d, skipped=%d (no host match / parse fail)", len(items), skipped)
		return 0, nil
	}

	// 5) 批量 insert（按 batchLimit 分批，避免单条 BulkInsert 1000 上限）
	written := 0
	for start := 0; start < len(snaps); start += batchLimit {
		end := start + batchLimit
		if end > len(snaps) {
			end = len(snaps)
		}
		batch := snaps[start:end]
		if err := bulkInsert(ctx, db, batch); err != nil {
			return written, err
		}
		written += len(batch)
	}
	log.Printf("[zabbix metric sync] items=%d, skipped=%d, written=%d", len(items), skipped, written)
	return written, nil
}

// bulkInsert 直走 GORM Create（与 MetricSnapshotService.BulkInsert 等价，
// 单独抽出来避免导入 service 包引发循环引用）。
func bulkInsert(ctx context.Context, db *gorm.DB, snaps []models.MetricSnapshot) error {
	now := time.Now()
	for i := range snaps {
		if snaps[i].ID == uuid.Nil {
			snaps[i].ID = uuid.New()
		}
		if snaps[i].CreatedAt.IsZero() {
			snaps[i].CreatedAt = now
		}
	}
	return db.WithContext(ctx).Create(&snaps).Error
}
