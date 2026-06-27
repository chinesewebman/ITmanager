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

// SyncFromNetBox 从 NetBox 同步资产（C-P6：批量 upsert；C-P7：ctx 透传）。
func (s *IntegrationService) SyncFromNetBox(ctx context.Context) (int, error) {
	devices, err := s.netbox.SyncDevices(ctx)
	if err != nil {
		return 0, err
	}
	if len(devices) == 0 {
		return 0, nil
	}

	// 1. 一次 select 查所有已存在 netbox_id
	netboxIDs := make([]int, 0, len(devices))
	for _, d := range devices {
		netboxIDs = append(netboxIDs, d.ID)
	}
	var existing []models.Asset
	if err := database.DB.WithContext(ctx).
		Where("netbox_id IN ?", netboxIDs).
		Find(&existing).Error; err != nil {
		return 0, fmt.Errorf("NetBox 已存在查询失败: %w", err)
	}
	existingByNB := make(map[int]*models.Asset, len(existing))
	for i := range existing {
		existingByNB[*existing[i].NetBoxID] = &existing[i]
	}

	// 2. 构造 upsert 列表
	now := time.Now()
	toUpsert := make([]models.Asset, 0, len(devices))
	for _, d := range devices {
		asset := d.ConvertToAsset()
		base := models.Asset{
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
			Tags:         "{}",
			CustomFields: "{}",
			UpdatedAt:    now,
		}
		if _, ok := existingByNB[d.ID]; ok {
			base.ID = existingByNB[d.ID].ID // 更新而非插入
		}
		toUpsert = append(toUpsert, base)
	}

	// 3. 批量 upsert（C-P6：用 ON CONFLICT 走 netbox_id 唯一键）
	if err := database.DB.WithContext(ctx).
		Clauses(buildUpsertClause("netbox_id",
			"Name", "AssetType", "Status", "Brand", "Model", "SN", "UpdatedAt")).
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
	if err := database.DB.WithContext(ctx).
		Clauses(buildUpsertClause("trigger_id", "Status")).
		CreateInBatches(toInsert, 100).Error; err != nil {
		return 0, fmt.Errorf("Zabbix 批量 upsert 失败: %w", err)
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
	if err := database.DB.WithContext(ctx).
		Clauses(buildUpsertClause("external_id")).
		CreateInBatches(toUpsert, 100).Error; err != nil {
		return 0, fmt.Errorf("GLPI 批量 upsert 失败: %w", err)
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
