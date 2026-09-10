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
	database.SetGormLogLevel(cfg.Log.Level)
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
		log.Printf("创建管理员用户: admin（默认演示密码，首次登录强制改密）")

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
		log.Printf("创建普通用户: operator（默认演示密码，首次登录强制改密）")

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
		log.Printf("创建只读用户: viewer（默认演示密码，首次登录强制改密）")
	}

	// 检查是否需要初始化其他数据
	db.Model(&models.Site{}).Count(&userCount)
	if userCount > 0 {
		log.Println("数据库已有数据，跳过初始化")
		return failures
	}

	// ========== 创建机房 ==========
	// 每个机房占一个 RFC 5737 文档网段（见 demoSiteNets），两者必须等长 ——
	// 否则多出来的机房会拿到越界下标（panic）或被静默复用同一段（IP 重复）。
	sites := []models.Site{
		{Name: "北京数据中心A", Code: "DC-BJ-01", Province: "北京", City: "北京", Address: "朝阳区科技园A座", Contact: "张明", ContactPhone: "13800000001", Tier: "T3", IsActive: true},
		{Name: "上海数据中心B", Code: "DC-SH-01", Province: "上海", City: "上海", Address: "浦东新区张江高科技园", Contact: "李华", ContactPhone: "13800000002", Tier: "T3", IsActive: true},
		{Name: "广州数据中心C", Code: "DC-GZ-01", Province: "广东", City: "广州", Address: "天河区软件园", Contact: "王芳", ContactPhone: "13800000003", Tier: "T4", IsActive: true},
	}
	if len(sites) != len(demoSiteNets) {
		fail("机房数与演示网段数不一致", fmt.Errorf(
			"sites=%d demoSiteNets=%d —— 加机房要同步扩 demoSiteNets，否则 IP 段会静默复用",
			len(sites), len(demoSiteNets)))
		return failures
	}

	for siteIdx, site := range sites {
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
		for rackIdx, rack := range racks {
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
			for serverIdx, server := range servers {
				if err := db.Create(&server).Error; err != nil {
					fail("创建服务器失败", err)
					continue
				}
				log.Printf("创建服务器: %s", server.Name)

				// 为服务器创建网络接口
				networks := []models.AssetNetwork{
					{AssetID: server.ID, InterfaceName: "eth0", InterfaceType: "ethernet", IPv4Address: demoIPv4(siteIdx, rackIdx, serverIdx, 0), MACAddress: generateMAC(), Status: "up", Purpose: "mgmt"},
					{AssetID: server.ID, InterfaceName: "eth1", InterfaceType: "ethernet", IPv4Address: demoIPv4(siteIdx, rackIdx, serverIdx, 1), MACAddress: generateMAC(), Status: "up", Purpose: "service"},
					{AssetID: server.ID, InterfaceName: "eth2", InterfaceType: "ethernet", IPv4Address: demoIPv4(siteIdx, rackIdx, serverIdx, 2), MACAddress: generateMAC(), Status: "up", Purpose: "backup"},
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
	//
	// HostIP 与资产网卡同一套编址（RFC 5737 文档网段，见 demoSiteNets/demoIPv4）：
	// 原实现写死 "192.168.A.10" / "192.168.B.10" 这类**非法 IPv4**（第三段是机柜字母行）。
	// 这些主机名不带机房后缀（同名资产在 3 个机房各有一份），统一取第一个机房
	// （北京DC-A，demoSiteNets[0]）的地址，值即 demoIPv4(0, rackIdx, serverIdx, 0)：
	// Rack-A01/A02/A03/B01 的 rackIdx 为 0/1/2/3，web/app/db 的 serverIdx 为 0/1/2。
	// switch-Rack-A03-01 留空是**预期**：seed 只给交换机建了无 IP 的接入端口（见上）。
	alerts := []models.Alert{
		{HostName: "web-server-Rack-A01-01", HostIP: "192.0.2.1", TriggerName: "CPU使用率超过90%", Severity: 5, SeverityName: "灾难", Problem: "CPU使用率达到95%，持续5分钟", Status: "problem"},
		{HostName: "app-server-Rack-A02-01", HostIP: "192.0.2.13", TriggerName: "内存使用率超过85%", Severity: 4, SeverityName: "严重", Problem: "内存使用率88%，接近阈值", Status: "acknowledged"},
		{HostName: "db-server-Rack-B01-01", HostIP: "192.0.2.34", TriggerName: "磁盘空间不足", Severity: 4, SeverityName: "严重", Problem: "/data 分区使用率92%", Status: "problem"},
		{HostName: "switch-Rack-A03-01", HostIP: "", TriggerName: "网络端口状态异常", Severity: 3, SeverityName: "一般严重", Problem: "端口 Gig1/0/23 进入 err-disable 状态", Status: "resolved"},
		{HostName: "web-server-Rack-B01-01", HostIP: "192.0.2.28", TriggerName: "HTTP响应时间过长", Severity: 3, SeverityName: "一般严重", Problem: "平均响应时间超过3秒", Status: "acknowledged"},
		{HostName: "app-server-Rack-A01-01", HostIP: "192.0.2.4", TriggerName: "SSL证书即将过期", Severity: 2, SeverityName: "警告", Problem: "证书将在15天后过期", Status: "problem"},
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
	// G-33 M1：键名必须与 notification.channelConfig 的 JSON tag 一致
	// （smtp_host/smtp_port/smtp_user/from/to、webhook_url/sign_secret、url）。
	// G-36 / M3：企业微信行 type 由 webhook 改回 wechat（WeChatSender 已落地，body 形状
	// 与通用 webhook 不同），键名仍是 url。
	channels := []models.NotificationChannel{
		{Name: "邮件通知", Type: "email", Config: `{"smtp_host": "smtp.company.com", "smtp_port": 587, "smtp_user": "nmp@company.com", "from": "nmp@company.com", "to": ["ops@company.com"]}`, IsEnabled: true, IsDefault: true},
		{Name: "钉钉群通知", Type: "dingtalk", Config: `{"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx", "sign_secret": ""}`, IsEnabled: true, IsDefault: false},
		{Name: "企业微信通知", Type: "wechat", Config: `{"url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"}`, IsEnabled: false, IsDefault: false},
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

// demoSiteNets 演示数据使用的三个 **RFC 5737 文档网段**（TEST-NET-1/2/3）。
//
// 这三段被标准保留给示例与文档，**永远不会出现在真实网络中、也永远不会被路由** ——
// 演示数据选它们，既保证地址合法，又不可能与任何真实设备撞址。
//
// 缺陷背景（TODO G-24）：原实现用 fmt.Sprintf("192.168.%s.10", rack.Row) 拼 IP，
// 而 rack.Row 是机柜排字母（A/B）→ 生成 "192.168.A.10" 这种**非法 IPv4**。
// 000013 把 asset_networks.ipv4_address 从 inet 改成 varchar(45) 之前，这类插入是
// 硬失败（被 fail() 记成日志、进程仍 exit 0）；改名后静默入库。同一机房下 A01/A02/A03
// 三排机柜的 Row 都是 "A"，还会拿到完全相同的地址。机房数与网段数必须一一对应。
var demoSiteNets = [3]string{"192.0.2", "198.51.100", "203.0.113"}

// demoIPv4 生成演示用 IPv4：第三段按机房取网段，第四段按「机柜/服务器/网卡」编码。
//
// 主机段 = rackIdx*9 + serverIdx*3 + nicIdx + 1 —— 三个下标各自独立进位，
// 机房内天然唯一（当前规模 4 机柜 × 3 服务器 × 3 网卡 = 36 个地址，远小于 /24）。
// 不引入计数器是为了避免「seed 长大以后静默溢出到 192.0.2.300」。
func demoIPv4(siteIdx, rackIdx, serverIdx, nicIdx int) string {
	return fmt.Sprintf("%s.%d", demoSiteNets[siteIdx], rackIdx*9+serverIdx*3+nicIdx+1)
}

func generateMAC() string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		0x00, 0x11, 0x22, byte(time.Now().UnixNano()%256),
		byte(time.Now().UnixNano()/1000%256), byte(time.Now().UnixNano()/1000000%256))
}
