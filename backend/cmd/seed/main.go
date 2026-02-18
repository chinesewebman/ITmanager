package main

import (
	"fmt"
	"log"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	db, err := database.Init(&cfg.Database)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	seedData(db)
	log.Println("✅ 初始数据创建完成")
}

func seedData(db *gorm.DB) {
	// 创建默认管理员用户
	var userCount int64
	db.Model(&models.User{}).Count(&userCount)
	if userCount == 0 {
		// 加密密码
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("密码加密失败: %v", err)
			return
		}

		admin := models.User{
			Username:     "admin",
			PasswordHash: string(hash),
			Nickname:    "管理员",
			Email:       "admin@example.com",
			Role:        "admin",
			Status:      "active",
		}
		if err := db.Create(&admin).Error; err != nil {
			log.Printf("创建管理员用户失败: %v", err)
			return
		}
		log.Printf("创建管理员用户: admin (密码: admin123)")
	}

	// 检查是否需要初始化其他数据
	db.Model(&models.Site{}).Count(&userCount)
	if userCount > 0 {
		log.Println("数据库已有数据，跳过初始化")
		return
	}

	// 创建机房 - ID 不需要手动设置，让 GORM 自动生成
	site := models.Site{
		Name:     "机房A",
		Code:     "DC-01",
		Province: "北京",
		City:     "北京",
		Address:  "朝阳区xxx",
		IsActive: true,
	}
	if err := db.Create(&site).Error; err != nil {
		log.Printf("创建机房失败: %v", err)
		return
	}
	log.Printf("创建机房: %s, ID: %s", site.Name, site.ID)

	// 创建机柜
	rack := models.Rack{
		SiteID:   site.ID,
		SiteName: site.Name,
		Name:     "Rack-01",
		TotalU:   42,
		Status:   "active",
	}
	if err := db.Create(&rack).Error; err != nil {
		log.Printf("创建机柜失败: %v", err)
		return
	}
	log.Printf("创建机柜: %s", rack.Name)

	// 创建资产
	asset := models.Asset{
		Name:         "web-server-01",
		AssetType:    "server",
		Status:       "active",
		SiteID:       &site.ID,
		SiteName:     site.Name,
		RackID:       &rack.ID,
		RackName:     rack.Name,
		RackPosition: "42U",
		Brand:        "Dell",
		Model:        "PowerEdge R740",
		Tags:         "{}",  // JSON 字段需要有效的 JSON
		CustomFields: "{}",   // JSON 字段需要有效的 JSON
	}
	if err := db.Create(&asset).Error; err != nil {
		log.Printf("创建资产失败: %v", err)
		return
	}
	log.Printf("创建资产: %s", asset.Name)

	// 创建网络接口
	network := models.AssetNetwork{
		AssetID:       asset.ID,
		InterfaceName: "eth0",
		InterfaceType: "ethernet",
		IPv4Address:   "192.168.1.10",
		MACAddress:    "00:11:22:33:44:55",
		Purpose:       "mgmt",
		Status:        "up",
	}
	db.Create(&network)
	log.Printf("创建网络接口: %s", network.InterfaceName)

	// 创建告警
	alert := models.Alert{
		HostID:       &asset.ID,
		HostName:     asset.Name,
		HostIP:       "192.168.1.10",
		TriggerName:  "CPU使用率超过90%",
		Severity:     5,
		SeverityName: "灾难",
		Problem:      "CPU使用率超过90%",
		Status:       "problem",
	}
	db.Create(&alert)
	log.Printf("创建告警: %s", alert.TriggerName)

	// 创建告警规则
	rule := models.AlertRule{
		Name:      "CPU使用率告警",
		Condition: "cpu_usage > 90",
		Threshold: 90,
		Severity:  5,
		IsEnabled: true,
		Priority:  1,
	}
	db.Create(&rule)

	// 创建通知渠道
	channel := models.NotificationChannel{
		Name:     "邮件通知",
		Type:     "email",
		Config:   fmt.Sprintf(`{"smtp": "smtp.company.com", "port": 587}`),
		IsEnabled: true,
	}
	db.Create(&channel)

	log.Printf("创建完成")
}
