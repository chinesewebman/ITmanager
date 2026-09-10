package models_test

import (
	"regexp"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ==================== Setup ====================

// newTestDB 开 :memory: sqlite + 手写 schema（避开 gorm AutoMigrate 用 gen_random_uuid）
func newTestDB(t *testing.T, modelsToCreate ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 根据传入的 model 模板类型建表
	for _, m := range modelsToCreate {
		switch m.(type) {
		case *models.User, models.User:
			require.NoError(t, db.Exec(userSchema).Error)
		case *models.Ticket, models.Ticket:
			require.NoError(t, db.Exec(ticketSchema).Error)
		case *models.Asset, models.Asset:
			require.NoError(t, db.Exec(assetSchema).Error)
		default:
			t.Fatalf("newTestDB 不支持 model 类型 %T", m)
		}
	}
	return db
}

const userSchema = `CREATE TABLE users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	nickname TEXT,
	email TEXT,
	phone TEXT,
	avatar TEXT,
	department_id TEXT,
	status TEXT DEFAULT 'active',
	role TEXT DEFAULT 'user',
	failed_login INTEGER DEFAULT 0,
	locked_until DATETIME,
	last_login DATETIME,
	last_login_ip TEXT,
	-- C7: 跟 production 000012 migration 同步
	must_change_password INTEGER DEFAULT 1,
	password_set_at DATETIME,
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME
)`

const ticketSchema = `CREATE TABLE tickets (
	id TEXT PRIMARY KEY,
	-- UNIQUE 是 D-2 论证的前提：并发撞号靠唯一索引拒绝，再靠 service 重试。
	-- 少了它，本文件的「30 张不重复」用例只验证 seqLabel 进位，验证不了兜底。
	ticket_number TEXT UNIQUE,
	title TEXT,
	description TEXT,
	ticket_type TEXT,
	priority TEXT,
	status TEXT,
	requester_id TEXT,
	requester_name TEXT,
	requester_email TEXT,
	assignee_id TEXT,
	assignee_name TEXT,
	category TEXT,
	asset_id TEXT,
	asset_name TEXT,
	external_id TEXT,
	source TEXT,
	resolution TEXT,
	resolved_at DATETIME,
	closed_at DATETIME,
	due_date DATETIME,
	tags TEXT,
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME
)`

// assetSchema 只覆盖 Asset 模型的列（G-20 用例用；tags/custom_fields 在 sqlite 上是 TEXT，
// 不校验 JSON —— 所以这里断言的是**钩子改值**，真 PG 的列默认值由 dbsmoke 守）。
const assetSchema = `CREATE TABLE assets (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	asset_tag TEXT,
	sn TEXT,
	asset_type TEXT,
	brand TEXT,
	model TEXT,
	site_id TEXT,
	site_name TEXT,
	rack_id TEXT,
	rack_name TEXT,
	rack_position TEXT,
	purchase_date DATETIME,
	warranty_end DATETIME,
	vendor TEXT,
	vendor_contact TEXT,
	status TEXT DEFAULT 'active',
	online_time DATETIME,
	offline_time DATETIME,
	last_known_ip4 TEXT,
	last_known_ip6 TEXT,
	retired_at DATETIME,
	retired_reason TEXT,
	retired_by TEXT,
	business_unit TEXT,
	service_name TEXT,
	tags TEXT,
	custom_fields TEXT,
	net_box_id INTEGER,
	source TEXT,
	created_at DATETIME,
	updated_at DATETIME
)`

// ==================== User.BeforeCreate ====================
func TestUser_BeforeCreate_ID为空时自动赋值(t *testing.T) {
	db := newTestDB(t, &models.User{})

	user := models.User{
		Username:     "alice",
		PasswordHash: "hashed",
	}
	require.NoError(t, db.Create(&user).Error)

	// id 应被 hook 填了（非 zero）
	assert.NotEqual(t, uuid.Nil, user.ID, "BeforeCreate 应自动填 UUID")

	// 验数据库里也是非 nil
	var got models.User
	require.NoError(t, db.First(&got, "username = ?", "alice").Error)
	assert.NotEqual(t, uuid.Nil, got.ID)
}

func TestUser_BeforeCreate_已传ID时不覆盖(t *testing.T) {
	db := newTestDB(t, &models.User{})

	presetID := uuid.New()
	user := models.User{
		ID:           presetID,
		Username:     "bob",
		PasswordHash: "hashed",
	}
	require.NoError(t, db.Create(&user).Error)

	assert.Equal(t, presetID, user.ID, "已传 ID 时 hook 不应覆盖")
}

func TestUser_BeforeCreate_每次Create生成新UUID(t *testing.T) {
	db := newTestDB(t, &models.User{})

	u1 := models.User{Username: "u1", PasswordHash: "h"}
	u2 := models.User{Username: "u2", PasswordHash: "h"}
	require.NoError(t, db.Create(&u1).Error)
	require.NoError(t, db.Create(&u2).Error)

	assert.NotEqual(t, u1.ID, u2.ID, "两次 Create 须不同 UUID")
}

func TestUser_BeforeCreate_并发Create_UUID不重复(t *testing.T) {
	db := newTestDB(t, &models.User{})

	const n = 10
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		u := models.User{
			Username:     "concurrent_" + uuid.New().String()[:8],
			PasswordHash: "h",
		}
		require.NoError(t, db.Create(&u).Error)
		ids[i] = u.ID
	}

	// 验证 UUID 不重复
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		assert.False(t, seen[id], "UUID %s 重复", id)
		seen[id] = true
	}
}

// ==================== Ticket.BeforeCreate ====================

func TestTicket_BeforeCreate_ID为空时自动赋值(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	ticket := models.Ticket{
		Title:     "CPU 飙高",
		Status:    "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&ticket).Error)
	assert.NotEqual(t, uuid.Nil, ticket.ID)
}

func TestTicket_BeforeCreate_已传ID时不覆盖(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	presetID := uuid.New()
	ticket := models.Ticket{
		ID:        presetID,
		Title:     "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&ticket).Error)
	assert.Equal(t, presetID, ticket.ID)
}

func TestTicket_BeforeCreate_无ticket_number时自动生成(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	ticket := models.Ticket{
		Title:     "auto-number-1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&ticket).Error)

	// 形如 TICKET-20260616-A（YYYYMMDD + A-Z 字母）
	assert.NotEmpty(t, ticket.TicketNumber, "ticket_number 应自动生成")
	assert.Regexp(t, regexp.MustCompile(`^TICKET-\d{8}-[A-Z]$`), ticket.TicketNumber,
		"格式应 TICKET-YYYYMMDD-X，实际: %s", ticket.TicketNumber)
}

func TestTicket_BeforeCreate_已传ticket_number时不覆盖(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	custom := "MY-CUSTOM-T-001"
	ticket := models.Ticket{
		Title:        "test",
		TicketNumber: custom,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(&ticket).Error)
	assert.Equal(t, custom, ticket.TicketNumber, "已传 ticket_number 不应被 hook 覆盖")
}

func TestTicket_BeforeCreate_第N张工单_字母递增(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	var nums []string
	for i := 0; i < 3; i++ {
		tk := models.Ticket{
			Title:     "test-" + uuid.New().String()[:8],
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		require.NoError(t, db.Create(&tk).Error)
		nums = append(nums, tk.TicketNumber)
	}

	// 3 个 ticket 的字母应递增（A → B → C）
	// 注意：依赖 Count 查询排序，sqlite 默认行为
	for i := 0; i < len(nums)-1; i++ {
		assert.NotEqual(t, nums[i], nums[i+1], "第 %d 和第 %d 张 ticket_number 应不同", i, i+1)
	}
}

// TestTicket_BeforeCreate_当天30张_编号不重复 是缺陷 D-2 的回归测试：
// 原实现用全表 Count()%26，同一天第 27 张会与第 1 张同号（都是 -A），
// 撞 ticket_number 唯一索引后建单失败。
func TestTicket_BeforeCreate_当天30张_编号不重复(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	const n = 30
	nums := make([]string, 0, n)
	for i := 0; i < n; i++ {
		tk := models.Ticket{
			Title:     "seq-" + uuid.New().String()[:8],
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		require.NoError(t, db.Create(&tk).Error, "第 %d 张工单创建失败", i+1)
		nums = append(nums, tk.TicketNumber)
	}

	// 1) 编号全局不重复（D-2 的直接断言）
	seen := map[string]int{}
	for i, num := range nums {
		if first, dup := seen[num]; dup {
			t.Fatalf("第 %d 张与第 %d 张工单号重复: %s", i+1, first+1, num)
		}
		seen[num] = i
	}

	// 2) 当天序号严格按 A..Z, AA..AD 递增
	prefix := "TICKET-" + time.Now().Format("20060102") + "-"
	wantLabels := []string{
		"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M",
		"N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
		"AA", "AB", "AC", "AD",
	}
	for i, label := range wantLabels {
		assert.Equal(t, prefix+label, nums[i], "第 %d 张工单号", i+1)
	}
}

// TestTicket_BeforeCreate_只统计当天 昨天已有工单不占用今天的序号。
func TestTicket_BeforeCreate_只统计当天(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	yesterday := time.Now().AddDate(0, 0, -1)
	for i := 0; i < 3; i++ {
		tk := models.Ticket{
			Title:        "old-" + uuid.New().String()[:8],
			TicketNumber: "TICKET-" + yesterday.Format("20060102") + "-" + string(rune('A'+i)),
			CreatedAt:    yesterday,
			UpdatedAt:    yesterday,
		}
		require.NoError(t, db.Create(&tk).Error)
	}

	tk := models.Ticket{
		Title:     "today",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&tk).Error)

	assert.Equal(t, "TICKET-"+time.Now().Format("20060102")+"-A", tk.TicketNumber,
		"昨天的 3 张不应让今天从 -D 开始")
}

func TestTicket_BeforeCreate_uuid_priority_先uuid后number(t *testing.T) {
	// ID 为空时: BeforeCreate 填 uuid
	// ticket_number 为空时: 填 TICKET-YYYYMMDD-X
	db := newTestDB(t, &models.Ticket{})

	ticket := models.Ticket{
		Title:     "both auto",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&ticket).Error)
	assert.NotEqual(t, uuid.Nil, ticket.ID)
	assert.NotEmpty(t, ticket.TicketNumber)
}

// ==================== 边界条件 ====================

func TestUser_BeforeCreate_重复username_DBUNIQUE拒收(t *testing.T) {
	// 注：sqlite 对 NOT NULL 接受空字符串（空字符串 ≠ NULL）
	// 这里测 UNIQUE 约束（更可靠触发）
	db := newTestDB(t, &models.User{})

	u1 := models.User{Username: "dup", PasswordHash: "h1"}
	require.NoError(t, db.Create(&u1).Error)

	u2 := models.User{Username: "dup", PasswordHash: "h2"}
	err := db.Create(&u2).Error
	assert.Error(t, err, "重复 username 应被 UNIQUE 约束拒收")
}

func TestUser_BeforeCreate_即使DB报错_hook已跑_id已填(t *testing.T) {
	// 验证 hook 的执行顺序：gorm v2 是先跑 BeforeCreate 再 INSERT
	// 即使 INSERT 失败，hook 已设置过字段
	db := newTestDB(t, &models.User{})

	// 故意制造 UNIQUE 冲突
	u1 := models.User{Username: "first", PasswordHash: "h"}
	require.NoError(t, db.Create(&u1).Error)

	u2 := models.User{Username: "first", PasswordHash: "h2"}
	err := db.Create(&u2).Error
	require.Error(t, err)

	// hook 仍跑了（id 已填）
	assert.NotEqual(t, uuid.Nil, u2.ID, "DB 错误时 hook 仍跑过")
}

func TestTicket_BeforeCreate_已有tickets时再Create_字母应递增(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	// 先建 2 张
	for i := 0; i < 2; i++ {
		tk := models.Ticket{
			Title:     "first-" + uuid.New().String()[:8],
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		require.NoError(t, db.Create(&tk).Error)
	}

	// 再建 1 张，新 ticket_number 应是第 3 个
	tk := models.Ticket{
		Title:     "third",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&tk).Error)

	// 不依赖具体字母，只验证格式
	assert.Regexp(t, regexp.MustCompile(`^TICKET-\d{8}-[A-Z]$`), tk.TicketNumber)
}

// ==================== Ticket.AssignTicketNumbers (G-25) ====================
//
// 缺陷背景：SyncFromGLPI 用 CreateInBatches 落库，而 gorm 的 before_create 回调对整片
// slice 的每一行都跑完才进 INSERT —— 每行各自按「当天条数」算号拿到同一个值 →
// 整批同一个号 → ticket_number 唯一索引整批拒绝。一次新增 ≥2 张票的同步全失败。

// TestTicket_CreateInBatches_不预分配则整批同号 是 G-25 的**根因特征化测试**：
// 钉住「批量路径天然算不出不同号」这一事实，正是 AssignTicketNumbers 存在的理由。
// 谁把批量路径的预分配删掉，红的就是下面那条集成用例；这条说明为什么不能靠钩子。
func TestTicket_CreateInBatches_不预分配则整批同号(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	now := time.Now()
	batch := []models.Ticket{
		{ID: uuid.New(), Title: "batch-1", CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), Title: "batch-2", CreatedAt: now, UpdatedAt: now},
	}

	err := db.CreateInBatches(batch, 100).Error
	require.Error(t, err, "整批同号应被 ticket_number 唯一索引拒绝（G-25 的根因）")
	assert.Equal(t, batch[0].TicketNumber, batch[1].TicketNumber,
		"两行的 BeforeCreate 在同一批插入前跑完，算出的号必然相同")
	assert.Contains(t, err.Error(), "UNIQUE", "拒绝来源应是唯一索引，实际: %v", err)
}

// TestTicket_AssignTicketNumbers_批量预分配互不相同 守 G-25 的修复本身。
func TestTicket_AssignTicketNumbers_批量预分配互不相同(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	now := time.Now()
	batch := []models.Ticket{
		{ID: uuid.New(), Title: "b1", CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), Title: "b2", CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), Title: "b3", CreatedAt: now, UpdatedAt: now},
	}
	models.AssignTicketNumbers(db, batch)

	prefix := "TICKET-" + time.Now().Format("20060102") + "-"
	assert.Equal(t, []string{prefix + "A", prefix + "B", prefix + "C"},
		[]string{batch[0].TicketNumber, batch[1].TicketNumber, batch[2].TicketNumber},
		"预分配应从当天下一个可用序号起连续递增")

	// 预分配之后整批才插得进去（钩子见号不再覆盖）
	require.NoError(t, db.CreateInBatches(batch, 100).Error, "预分配后整批应可插入")

	// 第二批要接着上一批的序号，不能从 -A 重来
	second := []models.Ticket{{ID: uuid.New(), Title: "b4", CreatedAt: now, UpdatedAt: now}}
	models.AssignTicketNumbers(db, second)
	assert.Equal(t, prefix+"D", second[0].TicketNumber, "第二批应续着当天已有条数往后排")
	require.NoError(t, db.Create(second).Error)

	var total int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&total).Error)
	assert.Equal(t, int64(4), total)
}

// TestTicket_AssignTicketNumbers_已有号不覆盖 守逃生门：外部系统自带号的行走原值。
func TestTicket_AssignTicketNumbers_已有号不覆盖(t *testing.T) {
	db := newTestDB(t, &models.Ticket{})

	batch := []models.Ticket{
		{ID: uuid.New(), Title: "preset", TicketNumber: "GLPI-42"},
		{ID: uuid.New(), Title: "auto"},
	}
	models.AssignTicketNumbers(db, batch)

	assert.Equal(t, "GLPI-42", batch[0].TicketNumber, "已填号的工单不得被改写")
	assert.Equal(t, "TICKET-"+time.Now().Format("20060102")+"-A", batch[1].TicketNumber,
		"自动号不应因跳过已填行而空转（从当天起点开始）")
}

// ==================== Asset.BeforeSave (G-20) ====================
//
// 缺陷背景：tags/custom_fields 是 jsonb，模型字段是 string，零值 "" 被写进 INSERT
// → PG 22P02（invalid input syntax for type json）。真 PG 上 seed 的资产全建不出来、
// POST /api/assets 不带 custom_fields 直接 500；sqlite 不校验 JSON 所以单测全绿。

func TestAsset_BeforeSave_零值归一为合法JSON(t *testing.T) {
	db := newTestDB(t, &models.Asset{})

	asset := models.Asset{ID: uuid.New(), Name: "srv-zero", AssetType: "server"}
	require.NoError(t, db.Create(&asset).Error)

	assert.Equal(t, "[]", asset.Tags, "tags 零值应归一为 JSON 数组")
	assert.Equal(t, "{}", asset.CustomFields, "custom_fields 零值应归一为 JSON 对象")

	// 落库的也必须是归一后的值（钩子在驱动之前改值）
	var got models.Asset
	require.NoError(t, db.First(&got, "name = ?", "srv-zero").Error)
	assert.Equal(t, "[]", got.Tags)
	assert.Equal(t, "{}", got.CustomFields)
}

func TestAsset_BeforeSave_纯空白也归一(t *testing.T) {
	db := newTestDB(t, &models.Asset{})

	asset := models.Asset{ID: uuid.New(), Name: "srv-blank", Tags: "   ", CustomFields: "\t\n"}
	require.NoError(t, db.Create(&asset).Error)

	assert.Equal(t, "[]", asset.Tags)
	assert.Equal(t, "{}", asset.CustomFields)
}

func TestAsset_BeforeSave_显式值不覆盖(t *testing.T) {
	db := newTestDB(t, &models.Asset{})

	asset := models.Asset{
		ID:           uuid.New(),
		Name:         "srv-explicit",
		Tags:         `["web","production"]`,
		CustomFields: `{"owner":"yanru"}`,
	}
	require.NoError(t, db.Create(&asset).Error)

	assert.Equal(t, `["web","production"]`, asset.Tags, "显式值不应被钩子改写")
	assert.Equal(t, `{"owner":"yanru"}`, asset.CustomFields)
}

// TestAsset_BeforeSave_Save路径也归一 是选钩子而非 gorm default tag 的直接理由：
// default:'[]' 只在 Create 的参数替换里生效，Save/结构体 Updates 不吃，仍会写零值。
func TestAsset_BeforeSave_Save路径也归一(t *testing.T) {
	db := newTestDB(t, &models.Asset{})

	asset := models.Asset{ID: uuid.New(), Name: "srv-save", Tags: `["web"]`, CustomFields: `{"a":1}`}
	require.NoError(t, db.Create(&asset).Error)

	// 模拟 PATCH 把两列清空后再 Save
	asset.Tags = ""
	asset.CustomFields = "  "
	require.NoError(t, db.Save(&asset).Error)

	assert.Equal(t, "[]", asset.Tags)
	assert.Equal(t, "{}", asset.CustomFields)

	var got models.Asset
	require.NoError(t, db.First(&got, "name = ?", "srv-save").Error)
	assert.Equal(t, "[]", got.Tags)
	assert.Equal(t, "{}", got.CustomFields)
}

// TestAsset_BeforeSave_Updates路径不在覆盖范围 是 G-20 修复边界的**特征化测试**
// （记录现状，不是期望行为；边界登记在 TODO G-21）。
//
// 实测（gorm v1.30 + sqlite）：
//   - 结构体 Updates 的零值被 gorm 跳过 → 不写、不报错（静默 no-op）；
//   - Select(...).Updates / Updates(map) 绕过模型钩子 → 写出 ”，
//     真 PG 上是 22P02（map 传 JSON 数组会被渲染成 ('x') 同样 22P02，传 nil 则写入 NULL）。
//
// 将来若在 UpdateAsset 里做了入参规范化（G-21），本用例应改成断言归一结果。
func TestAsset_BeforeSave_Updates路径不在覆盖范围(t *testing.T) {
	db := newTestDB(t, &models.Asset{})

	asset := models.Asset{ID: uuid.New(), Name: "srv-upd", Tags: `["web"]`, CustomFields: `{"a":1}`}
	require.NoError(t, db.Create(&asset).Error)

	// ① 结构体 Updates 的零值被跳过：原值保留，钩子不参与
	require.NoError(t, db.Model(&asset).Updates(models.Asset{Tags: "", CustomFields: ""}).Error)
	var got models.Asset
	require.NoError(t, db.First(&got, "name = ?", "srv-upd").Error)
	assert.Equal(t, `["web"]`, got.Tags, "结构体 Updates 的零值应被 gorm 跳过（静默 no-op）")
	assert.Equal(t, `{"a":1}`, got.CustomFields)

	// ② Select 强制写零值：绕过钩子，落 ''（真 PG 会 22P02）
	require.NoError(t, db.Model(&asset).Select("tags", "custom_fields").
		Updates(models.Asset{Tags: "", CustomFields: ""}).Error)
	require.NoError(t, db.First(&got, "name = ?", "srv-upd").Error)
	assert.Equal(t, "", got.Tags, "Select(...).Updates 绕过钩子，写出空串（G-21 边界）")
	assert.Equal(t, "", got.CustomFields)
}
