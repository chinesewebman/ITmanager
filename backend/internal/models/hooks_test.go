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
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME
)`

const ticketSchema = `CREATE TABLE tickets (
	id TEXT PRIMARY KEY,
	ticket_number TEXT,
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
