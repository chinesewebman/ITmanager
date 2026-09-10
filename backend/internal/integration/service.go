package integration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/httpx"
	"network-monitor-platform/internal/models"
)

// IntegrationMetricsRecorder 把 httpx 事件桥接到 metrics registry。
type IntegrationMetricsRecorder struct {
	Reg httpx.MetricsRecorder
}

// IncRequest / ObserveDuration 直接转发
func (i *IntegrationMetricsRecorder) IncRequest(system, status string) {
	if i.Reg == nil {
		return
	}
	i.Reg.IncRequest(system, status)
}
func (i *IntegrationMetricsRecorder) ObserveDuration(system string, s float64) {
	if i.Reg == nil {
		return
	}
	i.Reg.ObserveDuration(system, s)
}

// IntegrationService 集成服务
type IntegrationService struct {
	netbox *NetBoxClient
	zabbix *ZabbixClient
	glpi   *GLPIClient
}

// NewIntegrationService 创建集成服务（C-P7：注入 metrics 记录器）。
func NewIntegrationService(cfg *config.Config, m httpx.MetricsRecorder) *IntegrationService {
	rec := &IntegrationMetricsRecorder{Reg: m}
	return &IntegrationService{
		netbox: NewNetBoxClient(&cfg.Integrations.Netbox, rec),
		zabbix: NewZabbixClient(&cfg.Integrations.Zabbix, rec),
		glpi:   NewGLPIClient(&cfg.Integrations.GLPI, rec),
	}
}

// TestZabbixConnection v2.2: 仅尝试 Login 验证 Zabbix URL/user/password 通不通。
// 不动数据库、不入指标，正常返回说明三件套对得上。
func (s *IntegrationService) TestZabbixConnection(ctx context.Context) error {
	return s.zabbix.Login(ctx)
}

// ReloadZabbix v2.2: UI 改完配置点保存后调。清缓存让下次 GetTriggers 重新 Login。
func (s *IntegrationService) ReloadZabbix(cfg *config.ZabbixConfig) {
	s.zabbix.Reload(cfg)
}

// TestNetBoxConnection v2.2: 拉 1 条设备验证 NetBox URL/Token 通不通。
func (s *IntegrationService) TestNetBoxConnection(ctx context.Context) error {
	return s.netbox.TestConnection(ctx)
}

// ReloadNetBox v2.2: UI 改完配置点保存后调。
func (s *IntegrationService) ReloadNetBox(cfg *config.NetboxConfig) {
	s.netbox.Reload(cfg)
}

// TestGLPIConnection v2.2: InitSession 验证 GLPI URL + 两个 token 通不通。
func (s *IntegrationService) TestGLPIConnection(ctx context.Context) error {
	return s.glpi.InitSession(ctx)
}

// ReloadGLPI v2.2: UI 改完配置点保存后调。
func (s *IntegrationService) ReloadGLPI(cfg *config.GLPIConfig) {
	s.glpi.Reload(cfg)
}

// netboxUpdateCols 是 SyncFromNetBox 冲突时更新的列（**DB 列名**，见 buildUpsertClause）。
//
// 提成包级变量的唯一理由是**让单测断言的就是调用点真正用的那份清单**：
// 若测试里另抄一份，调用点被改成 Go 字段名时 DryRun 断言照样绿（审计 F-D）。
//
//   - 不含 status：ConvertToAsset 硬编码 Status="active"（netbox.go 不读 NetBox 状态），
//     写进去是常量、零信息量，却会把本地已退役（status='retired'，retired_at/by/reason
//     不在更新列里）或维护中的资产静默改回 active，产出「active + 已退役」的矛盾行。
//     新增行仍带 active（SyncFromNetBox 的结构体字面量）。
//   - 不含 tags / custom_fields：同步里硬编码 "[]"/"{}"，更新它们会抹掉人工标签。
//   - 不含 rack_name：ConvertToAsset 从不给它赋值（恒空串），更新等于清空。
var netboxUpdateCols = []string{"name", "asset_type", "brand", "model", "sn", "site_name", "updated_at"}

// SyncFromNetBox 从 NetBox 同步资产（C-P6：批量 upsert；C-P7：ctx 透传）。
func (s *IntegrationService) SyncFromNetBox(ctx context.Context) (int, error) {
	devices, err := s.netbox.SyncDevices(ctx)
	if err != nil {
		return 0, err
	}
	if len(devices) == 0 {
		return 0, nil
	}

	// 1. 构造 upsert 列表
	//    不需要预查询「已存在」：ON CONFLICT (net_box_id) 由唯一索引仲裁（migrations/000015），
	//    冲突即 DO UPDATE（实测：混合批次下行数、字段、id 都正确）。
	now := time.Now().UTC() // 与 gorm 的 NowFunc（database.go 的 time.Now().UTC()）对齐，否则同行的 created_at/updated_at 差一个时区偏移
	toUpsert := make([]models.Asset, 0, len(devices))
	seen := make(map[int]struct{}, len(devices))
	for _, d := range devices {
		asset := d.ConvertToAsset()
		// 同一批次内重复的 net_box_id 会让 ON CONFLICT DO UPDATE 二次命中同一行，
		// PG 报 21000（command cannot affect row a second time）→ 整批回滚、同步失败。
		// NetBox 单页 id 唯一时不会发生，但挡掉只要几行（审计 F-5）。
		if _, dup := seen[d.ID]; dup {
			continue
		}
		seen[d.ID] = struct{}{}
		toUpsert = append(toUpsert, models.Asset{
			Source:       "netbox",
			NetBoxID:     asset.NetboxID,
			Name:         asset.Name,
			AssetType:    asset.AssetType,
			Status:       asset.Status,
			Brand:        asset.Brand,
			Model:        asset.Model,
			SN:           asset.SN,
			SiteName:     asset.SiteName,
			RackName:     asset.RackName,
			Tags:         "[]",
			CustomFields: "{}",
			UpdatedAt:    now,
		})
	}

	// 2. 批量 upsert（C-P6：ON CONFLICT 走 net_box_id 唯一索引）
	//    更新列清单见 netboxUpdateCols（**DB 列名**，不是 Go 字段名）。
	if err := database.DB.WithContext(ctx).
		Clauses(buildUpsertClause("net_box_id", netboxUpdateCols...)).
		CreateInBatches(toUpsert, 100).Error; err != nil {
		return 0, fmt.Errorf("NetBox 批量 upsert 失败: %w", err)
	}

	log.Printf("从 NetBox 同步了 %d 个设备 (新增+更新)", len(toUpsert))
	return len(toUpsert), nil
}

// SyncFromZabbix 从 Zabbix 同步告警（C-P6 + C-P7）。
func (s *IntegrationService) SyncFromZabbix(ctx context.Context) (int, error) {
	triggers, err := s.zabbix.GetTriggers(ctx)
	if err != nil {
		return 0, err
	}
	if len(triggers) == 0 {
		return 0, nil
	}

	triggerIDs := make([]string, 0, len(triggers))
	for _, t := range triggers {
		triggerIDs = append(triggerIDs, t.TriggerID)
	}
	var existing []models.Alert
	if err := database.DB.WithContext(ctx).
		Where("trigger_id IN ? AND status = ?", triggerIDs, "problem").
		Find(&existing).Error; err != nil {
		return 0, fmt.Errorf("Zabbix 已存在查询失败: %w", err)
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e.TriggerID] = struct{}{}
	}

	now := time.Now()
	toInsert := make([]models.Alert, 0, len(triggers))
	for _, t := range triggers {
		if len(t.Hosts) == 0 {
			continue
		}
		if _, ok := existingSet[t.TriggerID]; ok {
			continue // 跳过已存在（避免重复）
		}
		alert := t.ConvertToAlert()
		toInsert = append(toInsert, models.Alert{
			TriggerID:    t.TriggerID,
			HostName:     alert.HostName,
			TriggerName:  alert.TriggerName,
			Problem:      alert.Problem,
			Severity:     alert.Severity,
			SeverityName: alert.SeverityName,
			Status:       "problem",
			Source:       "zabbix",
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}

	if len(toInsert) == 0 {
		return 0, nil
	}
	// 不加 ON CONFLICT：同一 trigger 会「触发 → 恢复 → 再触发」，多行历史是预期语义
	// （上面的预过滤只跳过「当前未恢复」的），且 alerts.trigger_id 上没有唯一索引 ——
	// 加了只会让整条语句在真 PG 上 42P10 失败（见 docs/FIX-PLAN-NETBOX-UPSERT.md §1.2-3）。
	if err := database.DB.WithContext(ctx).
		CreateInBatches(toInsert, 100).Error; err != nil {
		return 0, fmt.Errorf("Zabbix 批量插入失败: %w", err)
	}
	log.Printf("从 Zabbix 同步了 %d 个告警", len(toInsert))
	return len(toInsert), nil
}

// SyncFromGLPI 从 GLPI 同步工单（C-P6 + C-P7）。
func (s *IntegrationService) SyncFromGLPI(ctx context.Context) (int, error) {
	tickets, err := s.glpi.GetTickets(ctx)
	if err != nil {
		return 0, err
	}
	if len(tickets) == 0 {
		return 0, nil
	}

	externalIDs := make([]string, 0, len(tickets))
	for _, t := range tickets {
		externalIDs = append(externalIDs, fmt.Sprintf("%d", t.ID))
	}
	var existing []models.Ticket
	if err := database.DB.WithContext(ctx).
		Where("external_id IN ?", externalIDs).
		Find(&existing).Error; err != nil {
		return 0, fmt.Errorf("GLPI 已存在查询失败: %w", err)
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e.ExternalID] = struct{}{}
	}

	toUpsert := make([]models.Ticket, 0, len(tickets))
	now := time.Now()
	for _, t := range tickets {
		local := t.ConvertToTicket()
		if _, ok := existingSet[local.ExternalID]; ok {
			continue // 工单状态走 PATCH 更新，不在同步阶段覆盖
		}
		toUpsert = append(toUpsert, models.Ticket{
			ExternalID:  local.ExternalID,
			Title:       local.Title,
			Description: local.Description,
			Status:      local.Status,
			Priority:    local.Priority,
			TicketType:  local.TicketType,
			Source:      "glpi",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if len(toUpsert) == 0 {
		return 0, nil
	}
	// 批量插入前显式分配工单号（TODO G-25）：CreateInBatches 会把整批的 BeforeCreate
	// 都在 INSERT 之前跑完，每行各自按「当天条数」算号 → 整批同一个号 →
	// tickets.ticket_number 唯一索引整批拒绝（一次新增 ≥2 张票的同步全失败）。
	models.AssignTicketNumbers(database.DB.WithContext(ctx), toUpsert)

	// 不加 ON CONFLICT：工单已存在时上面已跳过（状态更新走 PATCH），且 tickets.external_id
	// 上没有唯一索引 —— 加了只会在真 PG 上 42P10 失败（见 docs/FIX-PLAN-NETBOX-UPSERT.md §1.2）。
	if err := database.DB.WithContext(ctx).
		CreateInBatches(toUpsert, 100).Error; err != nil {
		return 0, fmt.Errorf("GLPI 批量插入失败: %w", err)
	}
	log.Printf("从 GLPI 同步了 %d 个工单", len(toUpsert))
	return len(toUpsert), nil
}

// SyncAll 同步所有数据（P1-审计：返回 errors.Join 合并所有失败，不再静默吞错）
//   - 行为变更：v1.0.3 之前只 log，现在返回合并 error 给调用方
//   - 调用方 SyncFromNetBox/Zabbix/GLPI 任一失败都会被记到 errors.Join
//   - 成功的同步条目数仍写到 results map
func (s *IntegrationService) SyncAll(ctx context.Context) (map[string]int, error) {
	results := make(map[string]int)
	var errs []error

	if n, err := s.SyncFromNetBox(ctx); err != nil {
		log.Printf("NetBox 同步失败: %v", err)
		errs = append(errs, fmt.Errorf("netbox: %w", err))
	} else {
		results["netbox"] = n
	}

	if n, err := s.SyncFromZabbix(ctx); err != nil {
		log.Printf("Zabbix 同步失败: %v", err)
		errs = append(errs, fmt.Errorf("zabbix: %w", err))
	} else {
		results["zabbix"] = n
	}

	if n, err := s.SyncFromGLPI(ctx); err != nil {
		log.Printf("GLPI 同步失败: %v", err)
		errs = append(errs, fmt.Errorf("glpi: %w", err))
	} else {
		results["glpi"] = n
	}

	if len(errs) > 0 {
		return results, errors.Join(errs...)
	}
	return results, nil
}

// SyncMetricsFromZabbix v2.3: Zabbix → metric_snapshots 兜底单次同步。
// 给 HTTP handler 手动触发用（运维 / 调试）；cron worker 也走同一个函数。
// Zabbix 未配置 → 返 0, nil；登录失败 / item.get 失败 → 返 error。
func (s *IntegrationService) SyncMetricsFromZabbix(ctx context.Context) (int, error) {
	return SyncMetricsFromZabbix(ctx, s.zabbix, s.db(), 1000)
}

// db 拿 *gorm.DB 句柄（与 SyncFromNetBox 一致走 database.DB）。
// 单独抽函数便于测试 mock（v2.3 暂未引入 mock 框架，先保持简单）。
func (s *IntegrationService) db() *gorm.DB { return database.DB }
