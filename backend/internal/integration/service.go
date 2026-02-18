package integration

import (
	"log"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"
)

// IntegrationService 集成服务
type IntegrationService struct {
	netbox *NetBoxClient
	zabbix *ZabbixClient
	glpi   *GLPIClient
}

// NewIntegrationService 创建集成服务
func NewIntegrationService(cfg *config.Config) *IntegrationService {
	return &IntegrationService{
		netbox: NewNetBoxClient(&cfg.Integrations.Netbox),
		zabbix: NewZabbixClient(&cfg.Integrations.Zabbix),
		glpi:   NewGLPIClient(&cfg.Integrations.GLPI),
	}
}

// SyncFromNetBox 从 NetBox 同步资产
func (s *IntegrationService) SyncFromNetBox() (int, error) {
	devices, err := s.netbox.SyncDevices()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, d := range devices {
		// 检查是否已存在
		var existing models.Asset
		result := database.DB.Where("netbox_id = ?", d.ID).First(&existing)

		asset := d.ConvertToAsset()

		if result.RowsAffected == 0 {
			// 创建新资产
			newAsset := models.Asset{
				Name:         asset.Name,
				AssetType:    asset.AssetType,
				Status:       asset.Status,
				Brand:        asset.Brand,
				Model:        asset.Model,
				SN:           asset.SN,
				SiteID:       asset.SiteID,
				SiteName:     asset.SiteName,
				RackID:       asset.RackID,
				RackName:     asset.RackName,
				NetBoxID:     asset.NetboxID,
				Source:       "netbox",
				Tags:         "{}",
				CustomFields: "{}",
			}
			database.DB.Create(&newAsset)
			count++
		} else {
			// 更新现有资产
			existing.Name = asset.Name
			existing.Brand = asset.Brand
			existing.Model = asset.Model
			existing.SN = asset.SN
			database.DB.Save(&existing)
		}
	}

	log.Printf("从 NetBox 同步了 %d 个设备", count)
	return count, nil
}

// SyncFromZabbix 从 Zabbix 同步告警
func (s *IntegrationService) SyncFromZabbix() (int, error) {
	triggers, err := s.zabbix.GetTriggers()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, t := range triggers {
		if len(t.Hosts) == 0 {
			continue
		}

		alert := t.ConvertToAlert()
		alert.Status = "problem"
		alert.Source = "zabbix"

		// 检查是否已存在
		var existing models.Alert
		result := database.DB.Where("trigger_id = ? AND status = ?", t.TriggerID, "problem").First(&existing)

		if result.RowsAffected == 0 {
			// 创建新告警
			newAlert := models.Alert{
				TriggerID:    t.TriggerID,
				HostName:    t.Hosts[0].Host,
				TriggerName:  t.Description,
				Problem:      t.Description,
				Severity:     t.Priority,
				SeverityName: alert.SeverityName,
				Status:       "problem",
				Source:       "zabbix",
			}
			database.DB.Create(&newAlert)
			count++
		}
	}

	log.Printf("从 Zabbix 同步了 %d 个告警", count)
	return count, nil
}

// SyncFromGLPI 从 GLPI 同步工单
func (s *IntegrationService) SyncFromGLPI() (int, error) {
	tickets, err := s.glpi.GetTickets()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, t := range tickets {
		localTicket := t.ConvertToTicket()

		// 检查是否已存在
		var existing models.Ticket
		result := database.DB.Where("external_id = ?", t.ID).First(&existing)

		if result.RowsAffected == 0 {
			// 创建新工单
			newTicket := models.Ticket{
				ExternalID: localTicket.ExternalID,
				Title:      localTicket.Title,
				Description: localTicket.Description,
				Status:     localTicket.Status,
				Priority:   localTicket.Priority,
				TicketType: localTicket.TicketType,
				Source:     "glpi",
			}
			database.DB.Create(&newTicket)
			count++
		}
	}

	log.Printf("从 GLPI 同步了 %d 个工单", count)
	return count, nil
}

// SyncAll 同步所有数据
func (s *IntegrationService) SyncAll() (map[string]int, error) {
	results := make(map[string]int)

	netboxCount, err := s.SyncFromNetBox()
	if err != nil {
		log.Printf("NetBox 同步失败: %v", err)
	} else {
		results["netbox"] = netboxCount
	}

	zabbixCount, err := s.SyncFromZabbix()
	if err != nil {
		log.Printf("Zabbix 同步失败: %v", err)
	} else {
		results["zabbix"] = zabbixCount
	}

	glpiCount, err := s.SyncFromGLPI()
	if err != nil {
		log.Printf("GLPI 同步失败: %v", err)
	} else {
		results["glpi"] = glpiCount
	}

	return results, nil
}
