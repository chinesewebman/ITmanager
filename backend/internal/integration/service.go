package integration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
//
// 返回 (synced, truncated, err)：truncated 是 **0/1 标志**（不是条数）—— 源侧的进行中
// 告警超过 zabbixTriggerLimit 时为 1。单独透出是因为「静默丢告警」比「同步报错」更难
// 发现（同 SyncFromGLPI 的 skipped）。
func (s *IntegrationService) SyncFromZabbix(ctx context.Context) (synced, truncated int, err error) {
	triggers, err := s.zabbix.GetTriggers(ctx)
	if err != nil {
		return 0, 0, err
	}
	// M27/B：GetTriggers 请求 limit = zabbixTriggerLimit+1，所以「收到超过上限」是可判定的。
	// 恰好多要 1 条是必须的：源侧正常返回正好等于 limit 时（就是这么多），
	// 与「被截断到 limit」不可区分。
	if len(triggers) > zabbixTriggerLimit {
		truncated = 1
		// 日志只插值常量（不含 t.TriggerID 等源侧可控文本）→ 不引入日志注入面。
		// 按 lastchange 倒序（GetTriggers 的 sortorder=DESC），故被丢的是**最老的**告警。
		log.Printf("Zabbix 返回的告警数超过上限 %d，源侧仍有告警本次未导入（按 lastchange 倒序，被丢的是最老的）",
			zabbixTriggerLimit)
		triggers = triggers[:zabbixTriggerLimit]
	}
	if len(triggers) == 0 {
		return 0, truncated, nil
	}

	triggerIDs := make([]string, 0, len(triggers))
	for _, t := range triggers {
		triggerIDs = append(triggerIDs, t.TriggerID)
	}
	// M27/A：判据要两种，所以不再按 status 过滤 —— 留 status 过滤会让 usable 分支看不到
	// resolved 行，而那正是「本地已解决、源侧仍 firing」时必须命中的行。
	// 只取判据需要的三列（problem 是 TEXT，不拉）。
	// source = 'zabbix' 与 000027 的索引谓词一致，否则 Go 侧与库侧对「同一身份」判断分叉。
	var existing []models.Alert
	if err := database.DB.WithContext(ctx).
		Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).
		Select("trigger_id", "problem_start", "status").
		Find(&existing).Error; err != nil {
		return 0, truncated, fmt.Errorf("Zabbix 已存在查询失败: %w", err)
	}

	// exact：同一 trigger 的同一「故障发生」。
	// 零值/NULL 的 problem_start 都经 IsZero() 排除：真 PG 把 NULL 扫成零值，与真正的
	// 零值不可区分（M26 §1.6 实测）。排除的代价只是退回下面的降级判据，不会漏判成插入。
	exact := make(map[string]struct{}, len(existing))
	// open：**仅**降级分支用（lastchange 不可用 → 身份不可知）。
	// 判据必须是 Go 形态的 != "resolved"，不能写成 SQL 的 status <> 'resolved'：
	// 三值逻辑下后者对 NULL 求值为 NULL → 该行不进集合；Go 下 NULL 扫成 "" → 进集合。
	// 后者才与 D-2 的失败方向一致（宁可多一行可见的重复，不可静默吞掉后续再触发）。
	open := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		if !e.ProblemStart.IsZero() {
			exact[alertIdentityKey(e.TriggerID, e.ProblemStart)] = struct{}{}
		}
		if e.Status != "resolved" {
			open[e.TriggerID] = struct{}{}
		}
	}

	now := time.Now()
	toInsert := make([]models.Alert, 0, len(triggers))
	for _, t := range triggers {
		if len(t.Hosts) == 0 {
			continue
		}
		alert := t.ConvertToAlert()
		// M26：problem_start 取 Zabbix 的 lastchange（故障实际开始时刻），不是同步时刻。
		// 不写这一列会让告警列表有数据、而全部基于 problem_start 的 KPI/SLA 恒空
		// —— 静默的空，前端也不渲染该列，看不出来（需求 §1.2、§2.3）。
		// D-8：created_at 仍保持 now（入库时刻），两列语义不同，不要合并。
		problemStart, st := parseUnixSeconds(t.LastChange)
		if st == timeOK {
			// M27/A：同一 trigger 的同一故障只入一次 —— 无论本地是 problem / acknowledged
			// 还是 resolved。M26 之前的判据只认 problem，运维点一次「确认」就多出一条
			// 未确认的重复行（G-27）。
			if _, ok := exact[alertIdentityKey(t.TriggerID, problemStart)]; ok {
				continue
			}
		} else {
			// M27/A 降级：lastchange 不可用 → 身份不可知，退到「同 trigger 且未解决即算已存在」。
			logTimeUnusable("Zabbix trigger", t.TriggerID, "lastchange", t.LastChange, st)
			problemStart = now
			if _, ok := open[t.TriggerID]; ok {
				continue
			}
		}
		toInsert = append(toInsert, models.Alert{
			TriggerID:    t.TriggerID,
			HostName:     alert.HostName,
			TriggerName:  alert.TriggerName,
			Problem:      alert.Problem,
			Severity:     alert.Severity,
			SeverityName: alert.SeverityName,
			ProblemStart: problemStart,
			Status:       "problem",
			Source:       "zabbix",
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}

	if len(toInsert) == 0 {
		return 0, truncated, nil
	}
	// M27/D-4：迁移 000027 建了部分唯一索引 uq_alerts_zabbix_identity。上面的 exact/open
	// 预过滤是 TOCTOU —— 预查之后、插入之前若有并发同步插了同一身份，CreateInBatches
	// 整批原子 → 撞索引 → 整批新告警一起回滚，同步全失败。配 ON CONFLICT 把「硬失败」
	// 变成「幂等跳过」。
	// 谓词必须与索引逐字一致：不带 TargetWhere 的 ON CONFLICT (trigger_id, problem_start)
	// 在部分索引下 PG 报 42P10（同 GLPI 侧 :322-326 与 000026）。
	//
	// 计数用同事务 COUNT 前后差，而不是 res.RowsAffected：T-49 记的那个「RowsAffected
	// 虚报」是 Ticket 特有的（Ticket 的 BeforeCreate 自己填 ID，gorm 因此不加 RETURNING）；
	// Alert 路径实测不虚报。保留 COUNT 是因为它①对 INSERT 语句路径的变化免疫，
	// ②与 SyncFromGLPI 的计数法一致，读者不必分辨两种写法。
	if err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before, after int64
		if err := tx.Model(&models.Alert{}).
			Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).Count(&before).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "trigger_id"}, {Name: "problem_start"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''"},
			}},
			DoNothing: true,
		}).CreateInBatches(toInsert, 100).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Alert{}).
			Where("source = ? AND trigger_id IN ?", "zabbix", triggerIDs).Count(&after).Error; err != nil {
			return err
		}
		synced = int(after - before)
		return nil
	}); err != nil {
		return 0, truncated, fmt.Errorf("Zabbix 批量插入失败: %w", err)
	}

	log.Printf("从 Zabbix 同步了 %d 个告警（截断标志 %d）", synced, truncated)
	return synced, truncated, nil
}

// alertIdentityKey 是 M27/A 的去重身份：同一 trigger 的**同一次故障发生**。
// 用 Unix 秒而不是格式化字符串：lastchange 本身就是 Unix 秒，且 Unix() 与 Location 无关
// —— M26 在 pgx 与 sqlite 上各踩过一次时区坑（T-48），这里不给自己留第二个入口。
// 抽成函数而不是在两处内联同一段格式化：exact 的**构造**和**查询**必须同源，
// 写成两遍就是给将来「只改一处」留一个静默失效的口子（方向是漏判 → 插重复行）。
func alertIdentityKey(triggerID string, problemStart time.Time) string {
	return triggerID + "|" + strconv.FormatInt(problemStart.Unix(), 10)
}

// SyncFromGLPI 从 GLPI 同步工单（C-P6 + C-P7）。
//
// 返回 (synced, skipped, err)：skipped 是档位越界被跳过的条数（M26/D-1、D-6）。
// 单独计数并透出，是因为「静默丢票」比「同步报错」更难发现 —— 报错有人看，少几张没人看。
func (s *IntegrationService) SyncFromGLPI(ctx context.Context) (synced, skipped int, err error) {
	tickets, err := s.glpi.GetTickets(ctx)
	if err != nil {
		return 0, 0, err
	}
	if len(tickets) == 0 {
		return 0, 0, nil
	}

	externalIDs := make([]string, 0, len(tickets))
	for _, t := range tickets {
		externalIDs = append(externalIDs, fmt.Sprintf("%d", t.ID))
	}
	var existing []models.Ticket
	if err := database.DB.WithContext(ctx).
		Where("external_id IN ?", externalIDs).
		Find(&existing).Error; err != nil {
		return 0, 0, fmt.Errorf("GLPI 已存在查询失败: %w", err)
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
			// 已存在：按 ADR-0004，ITmanager 是工单 SoT、GLPI 降级为可选只读参考，
			// 不在同步阶段覆盖本地状态（这是设计，不是缺口）。全仓无 PATCH 路由。
			continue
		}
		// M26/D-1：越界档位不兜底、不静默改写 —— 跳过并计数，由调用方暴露给运维。
		// 判据是**词表命中**（ConvertToTicket 对越界值留空串），不是硬编码数值范围：
		// 词表是契约的投影，改了契约这里自动跟上。
		if local.Status == "" || local.Priority == "" {
			skipped++
			log.Printf("M26: GLPI 工单 %s 档位越界（status=%d priority=%d），已跳过",
				local.ExternalID, t.Status, t.Priority)
			continue
		}

		// M26：created_at 取 GLPI 的 date（工单实际创建时刻），不是同步时刻。
		created, st := parseGLPITime(local.CreatedAt)
		if st != timeOK {
			logTimeUnusable("GLPI 工单", local.ExternalID, "date", local.CreatedAt, st)
			created = now // created_at 是 NOT NULL 非指针列，没有 NULL 语义，只能回落
		}

		nt := models.Ticket{
			ExternalID:  local.ExternalID,
			Title:       local.Title,
			Description: local.Description,
			Status:      local.Status,
			Priority:    local.Priority,
			TicketType:  local.TicketType,
			Source:      "glpi",
			CreatedAt:   created,
			UpdatedAt:   now,
		}

		// M26/D-2：导入**不发明时间**。源缺失或解析失败 → 保持 NULL，**不回落 now**。
		// 补 now 会伪造事件：diagnostic_service.go 只看这两个指针是否非空、不看 status，
		// 会凭空长出一条「工单已解决/已关闭」的时间线记录，并污染 dashboard 的 SLA 窗口。
		// 源说「没有这个时间」是合法状态（票还没解决/还没关闭），必须原样保留为 NULL。
		if local.Status == "resolved" || local.Status == "closed" {
			if v, st := parseGLPITime(local.ResolvedAt); st == timeOK {
				nt.ResolvedAt = &v
			} else {
				logTimeUnusable("GLPI 工单", local.ExternalID, "solvedate", local.ResolvedAt, st)
			}
		}
		if local.Status == "closed" {
			if v, st := parseGLPITime(local.ClosedAt); st == timeOK {
				nt.ClosedAt = &v
			} else {
				logTimeUnusable("GLPI 工单", local.ExternalID, "closedate", local.ClosedAt, st)
			}
		}

		toUpsert = append(toUpsert, nt)
	}

	if len(toUpsert) == 0 {
		return 0, skipped, nil
	}
	// 批量插入前显式分配工单号（TODO G-25）：CreateInBatches 会把整批的 BeforeCreate
	// 都在 INSERT 之前跑完，每行各自按「当天条数」算号 → 整批同一个号 →
	// tickets.ticket_number 唯一索引整批拒绝（一次新增 ≥2 张票的同步全失败）。
	// 留在事务外：它只读当天已用标签，与下面的插入不构成同一个一致点。
	models.AssignTicketNumbers(database.DB.WithContext(ctx), toUpsert)

	// M26/D-4：迁移 000026 建了部分唯一索引 uq_tickets_glpi_external_id
	// （WHERE source='glpi' AND external_id <> ''）。上面的 existingSet 预过滤是 TOCTOU，
	// 并发同步会漏进重复行；CreateInBatches 又是**整批原子**的（gorm finisher_api.go），
	// 撞索引会让整批新票一起回滚。故配 ON CONFLICT 把「硬失败」变成「幂等跳过」。
	// 谓词必须与索引一致 —— 不带 TargetWhere 的 ON CONFLICT (external_id) 在部分索引下 42P10。
	//
	// synced **不能**用 res.RowsAffected：Ticket.ID 带 default:gen_random_uuid()，
	// gorm 会自动追加 RETURNING "id" → create 回调走 QueryContext + gorm.Scan，
	// 而 scan.go 的 slice 分支拿 RowsAffected 当下标，导致「只要有 ≥1 行插入，
	// RowsAffected 就等于 len(batch)」—— true PG 与 sqlite 实测皆然
	// （1 冲突 + 2 新 → 实际插 2，RowsAffected = 3）。恰好就在 D-4 存在的那个场景虚报。
	// 改用同事务内 COUNT 前后差。已排除的替代：db.Omit("RETURNING") 无效（SQL 里还在）；
	// clause.Returning{} 空列会 panic（scan.go 对不可寻址 slice 做 SetLen）。
	if err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		extIDs := make([]string, 0, len(toUpsert))
		for i := range toUpsert {
			extIDs = append(extIDs, toUpsert[i].ExternalID)
		}
		var before, after int64
		if err := tx.Model(&models.Ticket{}).
			Where("source = ? AND external_id IN ?", "glpi", extIDs).Count(&before).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "external_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "source = 'glpi' AND external_id <> ''"},
			}},
			DoNothing: true,
		}).CreateInBatches(toUpsert, 100).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Ticket{}).
			Where("source = ? AND external_id IN ?", "glpi", extIDs).Count(&after).Error; err != nil {
			return err
		}
		synced = int(after - before)
		return nil
	}); err != nil {
		return 0, skipped, fmt.Errorf("GLPI 批量插入失败: %w", err)
	}

	log.Printf("从 GLPI 同步了 %d 个工单（跳过越界 %d 条）", synced, skipped)
	return synced, skipped, nil
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

	if n, trunc, err := s.SyncFromZabbix(ctx); err != nil {
		log.Printf("Zabbix 同步失败: %v", err)
		errs = append(errs, fmt.Errorf("zabbix: %w", err))
	} else {
		results["zabbix"] = n
		// M27/D-6：截断标志一并透出 —— 静默丢告警比同步报错更难发现。
		// 失败分支刻意不写：与 glpi_skipped 同形（失败时连键都没有，前端 ?? 0 兜住）。
		results["zabbix_truncated"] = trunc
	}

	if n, skip, err := s.SyncFromGLPI(ctx); err != nil {
		log.Printf("GLPI 同步失败: %v", err)
		errs = append(errs, fmt.Errorf("glpi: %w", err))
	} else {
		results["glpi"] = n
		results["glpi_skipped"] = skip
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
