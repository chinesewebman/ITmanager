package main

import (
	"fmt"
	"log"
	"time"

	"network-monitor-platform"
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

	// 必须注入 MigrationsFS：否则走 gorm AutoMigrate 兜底，在真实 postgres 上
	// 与迁移 DDL 漂移（实测 "insufficient arguments"），且不会创建迁移里的种子数据。
	database.SetMigrationsFS(network_monitor_platform.MigrationsFS)
	db, err := database.Init(&cfg.Database)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	// 种子失败必须以非零码退出：否则 make deploy / CI 会把「半种子状态」当成功
	// （G-20 正是这么藏住的——资产全建不出来，进程仍 exit 0）。
	if failures := seedData(db); failures > 0 {
		log.Fatalf("❌ 初始数据有 %d 处失败（原因见上方日志）", failures)
	}
	log.Println("✅ 初始数据创建完成")
}

// seedData 建演示数据，返回失败处数（0 = 全部成功）。
func seedData(db *gorm.DB) int {
	failures := 0
	// fail 记录一处失败：计入计数并打印原因，末尾由 main 统一非零退出。
	fail := func(what string, err error) {
		failures++
		log.Printf("%s: %v", what, err)
	}

	// 创建默认管理员用户
	var userCount int64
	db.Model(&models.User{}).Count(&userCount)
	if userCount == 0 {
		// 加密密码
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			fail("密码加密失败", err)
			return failures
		}

		admin := models.User{
			Username:     "admin",
			PasswordHash: string(hash),
			Nickname:     "系统管理员",
			Email:        "admin@company.com",
			Phone:        "13800138000",
			Role:         "admin",
			Status:       "active",
			// C7: seed 用默认密码 admin123 — 首次登录强改密
			MustChangePassword: true,
		}
		if err := db.Create(&admin).Error; err != nil {
			fail("创建管理员用户失败", err)
			return failures
		}
		log.Printf("创建管理员用户: admin (密码: admin123) — ⚠️ 首次登录需改密")

		// 创建普通用户
		userHash, _ := bcrypt.GenerateFromPassword([]byte("user123"), bcrypt.DefaultCost)
		operator := models.User{
			Username:     "operator",
			PasswordHash: string(userHash),
			Nickname:     "运维工程师",
			Email:        "operator@company.com",
			Phone:        "13800138001",
			// 角色取值 = migrations/000001 的 roles.code（权威词表）。
			// 原先写 "operator" 不在词表内，会导致该账号被 fail-safe 降为只读。
			Role:   "ops_user",
			Status: "active",
			// C7: seed 用默认密码 user123 — 首次登录强改密
			MustChangePassword: true,
		}
		if err := db.Create(&operator).Error; err != nil {
			fail("创建普通用户失败", err)
		}
		log.Printf("创建普通用户: operator (密码: user123)")

		readonly := models.User{
			Username:     "viewer",
			PasswordHash: string(userHash),
			Nickname:     "访客",
			Email:        "viewer@company.com",
			Role:         "readonly",
			Status:       "active",
		}
		if err := db.Create(&readonly).Error; err != nil {
			fail("创建只读用户失败", err)
		}
		log.Printf("创建只读用户: viewer (密码: user123)")
	}

	// 检查是否需要初始化其他数据
	db.Model(&models.Site{}).Count(&userCount)
	if userCount > 0 {
		log.Println("数据库已有数据，跳过初始化")
		return failures
	}

	// ========== 创建机房 ==========
	sites := []models.Site{
		{Name: "北京数据中心A", Code: "DC-BJ-01", Province: "北京", City: "北京", Address: "朝阳区科技园A座", Contact: "张明", ContactPhone: "13800000001", Tier: "T3", IsActive: true},
		{Name: "上海数据中心B", Code: "DC-SH-01", Province: "上海", City: "上海", Address: "浦东新区张江高科技园", Contact: "李华", ContactPhone: "13800000002", Tier: "T3", IsActive: true},
		{Name: "广州数据中心C", Code: "DC-GZ-01", Province: "广东", City: "广州", Address: "天河区软件园", Contact: "王芳", ContactPhone: "13800000003", Tier: "T4", IsActive: true},
	}
	for _, site := range sites {
		if err := db.Create(&site).Error; err != nil {
			fail("创建机房失败", err)
			continue
		}
		log.Printf("创建机房: %s", site.Name)

		// 为每个机房创建多个机柜
		racks := []models.Rack{
			{SiteID: site.ID, SiteName: site.Name, Name: "Rack-A01", TotalU: 42, MaxWeight: 800, Floor: "1F", Row: "A", Column: "01", Status: "active"},
			{SiteID: site.ID, SiteName: site.Name, Name: "Rack-A02", TotalU: 42, MaxWeight: 800, Floor: "1F", Row: "A", Column: "02", Status: "active"},
			{SiteID: site.ID, SiteName: site.Name, Name: "Rack-A03", TotalU: 42, MaxWeight: 800, Floor: "1F", Row: "A", Column: "03", Status: "active"},
			{SiteID: site.ID, SiteName: site.Name, Name: "Rack-B01", TotalU: 42, MaxWeight: 800, Floor: "2F", Row: "B", Column: "01", Status: "active"},
		}
		for _, rack := range racks {
			if err := db.Create(&rack).Error; err != nil {
				fail("创建机柜失败", err)
				continue
			}
			log.Printf("创建机柜: %s", rack.Name)

			// 为每个机柜创建多个服务器
			warrantyEnd := time.Now().AddDate(3, 0, 0)
			purchaseDate := time.Now()
			servers := []models.Asset{
				{Name: fmt.Sprintf("web-server-%s-01", rack.Name), AssetTag: fmt.Sprintf("AST-%s-%s-001", site.Code, rack.Name), SN: fmt.Sprintf("SN-WEB-%s-001", rack.Name), AssetType: "server", Brand: "Dell", Model: "PowerEdge R740", Status: "active", SiteID: &site.ID, SiteName: site.Name, RackID: &rack.ID, RackName: rack.Name, RackPosition: "1U", Vendor: "Dell Official", PurchaseDate: &purchaseDate, WarrantyEnd: &warrantyEnd, BusinessUnit: "互联网业务", ServiceName: "Web服务", Tags: `["web", "production"]`},
				{Name: fmt.Sprintf("app-server-%s-01", rack.Name), AssetTag: fmt.Sprintf("AST-%s-%s-002", site.Code, rack.Name), SN: fmt.Sprintf("SN-APP-%s-001", rack.Name), AssetType: "server", Brand: "HP", Model: "ProLiant DL380 Gen10", Status: "active", SiteID: &site.ID, SiteName: site.Name, RackID: &rack.ID, RackName: rack.Name, RackPosition: "2U", Vendor: "HP Official", PurchaseDate: &purchaseDate, WarrantyEnd: &warrantyEnd, BusinessUnit: "互联网业务", ServiceName: "应用服务", Tags: `["app", "production"]`},
				{Name: fmt.Sprintf("db-server-%s-01", rack.Name), AssetTag: fmt.Sprintf("AST-%s-%s-003", site.Code, rack.Name), SN: fmt.Sprintf("SN-DB-%s-001", rack.Name), AssetType: "server", Brand: "Huawei", Model: "RH2288H V3", Status: "active", SiteID: &site.ID, SiteName: site.Name, RackID: &rack.ID, RackName: rack.Name, RackPosition: "3U", Vendor: "Huawei Official", PurchaseDate: &purchaseDate, WarrantyEnd: &warrantyEnd, BusinessUnit: "数据服务", ServiceName: "数据库", Tags: `["database", "production"]`},
			}
			for _, server := range servers {
				if err := db.Create(&server).Error; err != nil {
					fail("创建服务器失败", err)
					continue
				}
				log.Printf("创建服务器: %s", server.Name)

				// 为服务器创建网络接口
				networks := []models.AssetNetwork{
					{AssetID: server.ID, InterfaceName: "eth0", InterfaceType: "ethernet", IPv4Address: fmt.Sprintf("192.168.%s.10", rack.Row), MACAddress: generateMAC(), Status: "up", Purpose: "mgmt"},
					{AssetID: server.ID, InterfaceName: "eth1", InterfaceType: "ethernet", IPv4Address: fmt.Sprintf("192.168.%s.11", rack.Row), MACAddress: generateMAC(), Status: "up", Purpose: "service"},
					{AssetID: server.ID, InterfaceName: "eth2", InterfaceType: "ethernet", IPv4Address: fmt.Sprintf("192.168.%s.12", rack.Row), MACAddress: generateMAC(), Status: "up", Purpose: "backup"},
				}
				for _, net := range networks {
					if err := db.Create(&net).Error; err != nil {
						fail("创建网络接口失败", err)
					}
				}
			}

			// 创建网络设备
			warrantyEndSwitch := time.Now().AddDate(5, 0, 0)
			switches := []models.Asset{
				{Name: fmt.Sprintf("switch-%s-01", rack.Name), AssetTag: fmt.Sprintf("AST-%s-%s-NET01", site.Code, rack.Name), SN: fmt.Sprintf("SN-SW-%s-001", rack.Name), AssetType: "switch", Brand: "Cisco", Model: "Catalyst 2960X-48FPS-L", Status: "active", SiteID: &site.ID, SiteName: site.Name, RackID: &rack.ID, RackName: rack.Name, RackPosition: "40U", Vendor: "Cisco Official", PurchaseDate: &purchaseDate, WarrantyEnd: &warrantyEndSwitch, BusinessUnit: "网络基础设施", ServiceName: "接入交换", Tags: `["switch", "access"]`},
			}
			for _, sw := range switches {
				if err := db.Create(&sw).Error; err != nil {
					fail("创建交换机失败", err)
					continue
				}
				log.Printf("创建交换机: %s", sw.Name)

				// 为交换机创建端口
				for i := 1; i <= 48; i++ {
					net := models.AssetNetwork{
						AssetID: sw.ID, InterfaceName: fmt.Sprintf("GigabitEthernet1/0/%d", i),
						InterfaceType: "ethernet", IPv4Address: "", MACAddress: generateMAC(), Status: "up", Purpose: "access",
					}
					if err := db.Create(&net).Error; err != nil {
						fail("创建交换机端口失败", err)
					}
				}
			}
		}
	}

	// ========== 创建告警 ==========

	// 模拟告警数据
	alerts := []models.Alert{
		{HostName: "web-server-Rack-A01-01", HostIP: "192.168.A.10", TriggerName: "CPU使用率超过90%", Severity: 5, SeverityName: "灾难", Problem: "CPU使用率达到95%，持续5分钟", Status: "problem"},
		{HostName: "app-server-Rack-A02-01", HostIP: "192.168.A.11", TriggerName: "内存使用率超过85%", Severity: 4, SeverityName: "严重", Problem: "内存使用率88%，接近阈值", Status: "acknowledged"},
		{HostName: "db-server-Rack-B01-01", HostIP: "192.168.B.10", TriggerName: "磁盘空间不足", Severity: 4, SeverityName: "严重", Problem: "/data 分区使用率92%", Status: "problem"},
		{HostName: "switch-Rack-A03-01", HostIP: "", TriggerName: "网络端口状态异常", Severity: 3, SeverityName: "一般严重", Problem: "端口 Gig1/0/23 进入 err-disable 状态", Status: "resolved"},
		{HostName: "web-server-Rack-B01-01", HostIP: "192.168.B.11", TriggerName: "HTTP响应时间过长", Severity: 3, SeverityName: "一般严重", Problem: "平均响应时间超过3秒", Status: "acknowledged"},
		{HostName: "app-server-Rack-A01-01", HostIP: "192.168.A.12", TriggerName: "SSL证书即将过期", Severity: 2, SeverityName: "警告", Problem: "证书将在15天后过期", Status: "problem"},
	}

	for i, alert := range alerts {
		alert.AlertID = fmt.Sprintf("ALT-%d", 1000+i)
		alert.TriggerID = fmt.Sprintf("TRG-%d", 5000+i)
		alert.ProblemStart = time.Now().Add(-time.Duration(i+1) * time.Hour)
		if alert.Status == "resolved" {
			endTime := time.Now().Add(-time.Duration(i) * time.Hour)
			alert.ProblemEnd = &endTime
			alert.Duration = int(time.Since(alert.ProblemStart).Seconds())
		}
		alert.Source = "zabbix"
		alert.RepeatCount = 0

		if err := db.Create(&alert).Error; err != nil {
			fail("创建告警失败", err)
		}
	}
	log.Printf("创建 %d 条告警数据", len(alerts))

	// ========== 创建告警规则 ==========
	rules := []models.AlertRule{
		{Name: "CPU使用率告警", Description: "CPU使用率超过90%时触发", Condition: `{"metric": "cpu_usage", "operator": ">", "threshold": 90}`, AssetType: "server", Metric: "cpu_usage", Operator: ">", Threshold: 90, Duration: 300, Severity: 5, SeverityName: "灾难", NotifyEnabled: true, NotifyChannels: `["email", "dingtalk"]`, IsEnabled: true, Priority: 1},
		{Name: "内存使用率告警", Description: "内存使用率超过85%时触发", Condition: `{"metric": "memory_usage", "operator": ">", "threshold": 85}`, AssetType: "server", Metric: "memory_usage", Operator: ">", Threshold: 85, Duration: 300, Severity: 4, SeverityName: "严重", NotifyEnabled: true, NotifyChannels: `["email"]`, IsEnabled: true, Priority: 2},
		{Name: "磁盘空间告警", Description: "磁盘使用率超过80%时触发", Condition: `{"metric": "disk_usage", "operator": ">", "threshold": 80}`, AssetType: "server", Metric: "disk_usage", Operator: ">", Threshold: 80, Duration: 600, Severity: 4, SeverityName: "严重", NotifyEnabled: true, NotifyChannels: `["email", "dingtalk"]`, IsEnabled: true, Priority: 3},
		{Name: "网络延迟告警", Description: "网络延迟超过100ms时触发", Condition: `{"metric": "ping_latency", "operator": ">", "threshold": 100}`, AssetType: "network", Metric: "ping_latency", Operator: ">", Threshold: 100, Duration: 180, Severity: 3, SeverityName: "一般严重", NotifyEnabled: true, NotifyChannels: `["dingtalk"]`, IsEnabled: true, Priority: 4},
		{Name: "服务不可用告警", Description: "服务无法访问时触发", Condition: `{"metric": "service_status", "operator": "=", "threshold": 0}`, AssetType: "server", Metric: "service_status", Operator: "=", Threshold: 0, Duration: 60, Severity: 5, SeverityName: "灾难", NotifyEnabled: true, NotifyChannels: `["email", "dingtalk", "webhook"]`, IsEnabled: true, Priority: 5},
	}
	for _, rule := range rules {
		if err := db.Create(&rule).Error; err != nil {
			fail("创建告警规则失败", err)
		}
	}
	log.Printf("创建 %d 条告警规则", len(rules))

	// ========== 创建通知渠道 ==========
	channels := []models.NotificationChannel{
		{Name: "邮件通知", Type: "email", Config: `{"smtp_host": "smtp.company.com", "smtp_port": 587, "smtp_user": "nmp@company.com", "from": "nmp@company.com"}`, IsEnabled: true, IsDefault: true},
		{Name: "钉钉群通知", Type: "dingtalk", Config: `{"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx", "secret": ""}`, IsEnabled: true, IsDefault: false},
		{Name: "企业微信通知", Type: "webhook", Config: `{"webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"}`, IsEnabled: false, IsDefault: false},
	}
	for _, ch := range channels {
		if err := db.Create(&ch).Error; err != nil {
			fail("创建通知渠道失败", err)
		}
	}
	log.Printf("创建 %d 个通知渠道", len(channels))

	// ========== 创建工单 ==========
	tickets := []models.Ticket{
		{TicketNumber: "TICKET-20260215-A", Title: "Web服务器CPU使用率异常", Description: "web-server-01 CPU使用率持续在95%以上，需要检查处理", TicketType: "incident", Priority: "high", Status: "open", RequesterName: "张三", RequesterEmail: "zhangsan@company.com", Category: "服务器故障", Source: "manual"},
		{TicketNumber: "TICKET-20260215-B", Title: "数据库存储扩容申请", Description: "数据库存储空间不足，申请扩容500GB", TicketType: "request", Priority: "medium", Status: "in_progress", RequesterName: "李四", RequesterEmail: "lisi@company.com", Category: "资源申请", Source: "manual"},
		{TicketNumber: "TICKET-20260214-A", Title: "网络交换机端口故障", Description: "Cisco交换机端口23进入err-disable状态", TicketType: "incident", Priority: "high", Status: "resolved", RequesterName: "王五", RequesterEmail: "wangwu@company.com", Category: "网络故障", Source: "zabbix", Resolution: "已重启端口，恢复正常", ResolvedAt: timePtr(time.Now().Add(-24 * time.Hour))},
		{TicketNumber: "TICKET-20260213-A", Title: "新服务器上线部署", Description: "新采购的Dell R740服务器需要安装部署", TicketType: "request", Priority: "low", Status: "closed", RequesterName: "赵六", RequesterEmail: "zhaoliu@company.com", Category: "新业务部署", Source: "manual", Resolution: "已完成部署并交付使用", ResolvedAt: timePtr(time.Now().Add(-48 * time.Hour)), ClosedAt: timePtr(time.Now().Add(-47 * time.Hour))},
	}
	for _, ticket := range tickets {
		ticket.Tags = `["` + ticket.TicketType + `"]`
		if err := db.Create(&ticket).Error; err != nil {
			fail("创建工单失败", err)
		}
	}
	log.Printf("创建 %d 个工单", len(tickets))

	log.Printf("创建完成")
	return failures
}

// Helper functions
func timeNow() *time.Time {
	now := time.Now()
	return &now
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func generateMAC() string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		0x00, 0x11, 0x22, byte(time.Now().UnixNano()%256),
		byte(time.Now().UnixNano()/1000%256), byte(time.Now().UnixNano()/1000000%256))
}
