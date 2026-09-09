package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"
)

// ==================== Channel Service 测试 ====================

func TestChannelService_List_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "name", "type", "config"}).
		AddRow(uuid.NewString(), "email-1", "email", "{}").
		AddRow(uuid.NewString(), "webhook-1", "webhook", "{}")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(rows)

	chs, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Len(t, chs, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Get_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "type"}).
		AddRow(id, "test-channel", "email")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "test-channel", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Get(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChannelService_Create_空name返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	err := svc.Create(ctx, &models.NotificationChannel{Name: ""}) //nolint:exhaustruct
	assert.ErrorIs(t, err, ErrInvalidInput)

	err = svc.Create(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestChannelService_Delete_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_channels"`).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Delete(ctx, id)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Update_空updates走Get(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(id, "unchanged")

	// 空 updates：Update 内部直接走 Get，不发 UPDATE
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Update(ctx, id, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "unchanged", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== Create 补全 ====================

func TestChannelService_Create_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	// G-33 M1：配置必须能构造出 Sender（原 fixture Type:dingtalk + Config:"{}" 会被新校验拒）
	ch := &models.NotificationChannel{Name: "钩子", Type: "webhook", Config: `{"url":"https://example.com/hook"}`} //nolint:exhaustruct

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	err := svc.Create(ctx, ch)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Create_nil返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewChannelService(gormDB)
	err := svc.Create(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestChannelService_Create_唯一冲突返回ErrAlreadyExists(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	// G-33 M1：配置校验早于 INSERT，fixture 必须给合法 type/config，否则到不了唯一冲突分支
	ch := &models.NotificationChannel{Name: "dup", Type: "webhook", Config: `{"url":"https://example.com/hook"}`} //nolint:exhaustruct
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_channels"`).
		WillReturnError(errors.New("duplicate key value violates unique constraint"))
	mock.ExpectRollback()

	err := svc.Create(ctx, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

// ==================== Update 补全 ====================

func TestChannelService_Update_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(id, "原名")
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_channels"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.Update(ctx, id, map[string]interface{}{"name": "新名"})
	require.NoError(t, err)
	assert.Equal(t, "新名", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Update_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Update(context.Background(), "nonexistent", map[string]interface{}{"name": "x"})
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== Test 真发 ====================

func TestChannelService_Test_不存在返ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	err := svc.Test(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChannelService_Test_未知类型返错(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "type", "config"}).
		AddRow(id, "weird", "magic", "{}")
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	err := svc.Test(ctx, id)
	require.Error(t, err, "未知 type 应返错")
	assert.NotErrorIs(t, err, ErrNotFound, "不是 NotFound, 是 Resolver 错")
}

// ==================== G-33 M1：配置契约校验 ====================

// channelFixturePath 跨语言契约样本（前端表单产出 → 后端必须能构造）。
// Go 测试 cwd = 包目录，故向上三级到仓库根。
const channelFixturePath = "../../../frontend/src/pages/__fixtures__/channelConfigSamples.json"

// newChannelSQLiteDB 真 sqlite（手写 DDL）。
// 不用 AutoMigrate：models.NotificationChannel.ID 带 `default:gen_random_uuid()`，
// sqlite 上没有该函数，AutoMigrate 必炸（同 cmd/seed/main_test.go 的既有做法）。
func newChannelSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE notification_channels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		config TEXT,
		is_enabled INTEGER DEFAULT 1,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	return db
}

func channelCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.NotificationChannel{}).Count(&n).Error)
	return n
}

// V-1 跨语言契约：前端表单产出的样本，后端构造器必须全收。
func TestChannelConfig_跨语言样本全部可构造(t *testing.T) {
	raw, err := os.ReadFile(channelFixturePath)
	require.NoError(t, err, "契约样本文件必须存在（缺失即失败，不得 skip）")

	var samples map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &samples))
	require.Len(t, samples, 4, "样本应覆盖 email/dingtalk/wechat/webhook 四类型")

	for typ, cfg := range samples {
		_, err := notification.NewSender(&models.NotificationChannel{Type: typ, Config: string(cfg)}) //nolint:exhaustruct
		assert.NoError(t, err, "样本 %s 必须能构造出 Sender（前端键名与 channelConfig tag 已对齐？）", typ)
	}
}

// V-2 坏配置不落库（真 sqlite + 正控：同表同 helper，合法配置必须真插进去）
func TestChannelService_Create_坏配置不落库(t *testing.T) {
	badCases := []struct {
		name, typ, cfg string
	}{
		{"email缺to", "email", `{"smtp_host":"h","smtp_port":587,"smtp_user":"u","from":"f@x.com"}`},
		{"webhook缺url", "webhook", `{}`},
		{"未知类型feishu", "feishu", `{"url":"https://example.com/hook"}`},
		{"端口是字符串", "email", `{"smtp_host":"h","smtp_port":"587","smtp_user":"u","from":"f@x.com","to":["t@x.com"]}`},
		{"config不是JSON", "webhook", `not-json`},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newChannelSQLiteDB(t)
			svc := NewChannelService(db)
			ch := &models.NotificationChannel{ID: uuid.New(), Name: "bad", Type: tc.typ, Config: tc.cfg} //nolint:exhaustruct
			err := svc.Create(context.Background(), ch)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Zero(t, channelCount(t, db), "坏配置不得落库")
		})
	}

	t.Run("正控_合法配置落库", func(t *testing.T) {
		db := newChannelSQLiteDB(t)
		svc := NewChannelService(db)
		ch := &models.NotificationChannel{ID: uuid.New(), Name: "good", Type: "webhook", Config: `{"url":"https://example.com/hook"}`} //nolint:exhaustruct
		require.NoError(t, svc.Create(context.Background(), ch))
		assert.EqualValues(t, 1, channelCount(t, db), "正控失败：合法配置没插进去，V-2 的『未落库』断言会恒真")
	})
}

// V-3 Update 局部更新用「合并后的 (type, config)」校验
func TestChannelService_Update_合并校验(t *testing.T) {
	db := newChannelSQLiteDB(t)
	svc := NewChannelService(db)
	ctx := context.Background()

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationChannel{
		ID: id, Name: "钩子", Type: "webhook", Config: `{"url":"https://example.com/hook"}`,
	}).Error)

	t.Run("只改name不触发校验", func(t *testing.T) {
		// L-3（测试有效性审计）：必须用一条「存量坏行」才承重 —— 用合法行时无论是否
		// 真跳过校验都通过（把 touched 初值改成 true 恒校验，这条仍绿）。
		badID := uuid.New()
		require.NoError(t, db.Create(&models.NotificationChannel{
			ID: badID, Name: "存量坏行", Type: "email", Config: `{}`,
		}).Error)

		got, err := svc.Update(ctx, badID.String(), map[string]interface{}{"name": "改名"})
		require.NoError(t, err, "只改 name 不应触发配置校验")
		assert.Equal(t, "改名", got.Name)
	})

	t.Run("只改type用已存config校验_必拒", func(t *testing.T) {
		_, err := svc.Update(ctx, id.String(), map[string]interface{}{"type": "dingtalk"})
		require.ErrorIs(t, err, ErrInvalidInput, "type=dingtalk + 存量 url config 是坏组合")

		var after models.NotificationChannel
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Equal(t, "webhook", after.Type, "校验失败不得落库")
	})

	t.Run("type与config一起改且合法_通过", func(t *testing.T) {
		got, err := svc.Update(ctx, id.String(), map[string]interface{}{
			"type":   "dingtalk",
			"config": `{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=xxx"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "dingtalk", got.Type)
	})

	t.Run("只改config用已存type校验_坏config必拒", func(t *testing.T) {
		_, err := svc.Update(ctx, id.String(), map[string]interface{}{"config": `{}`})
		require.ErrorIs(t, err, ErrInvalidInput)
	})
}

// V-4 Update 的 config/type 非字符串一律 fail-closed（fail-open 时 float64/bool 会被 gorm 落库）
func TestChannelService_Update_非字符串config一律拒绝(t *testing.T) {
	const original = `{"url":"https://example.com/hook"}`

	for _, tc := range []struct {
		name string
		val  interface{}
	}{
		{"对象", map[string]interface{}{"url": "https://example.com/hook"}},
		{"数组", []interface{}{"a"}},
		{"数字", float64(12345)},
		{"布尔", true},
		{"null", nil},
	} {
		t.Run("config_"+tc.name, func(t *testing.T) {
			db := newChannelSQLiteDB(t)
			svc := NewChannelService(db)
			ctx := context.Background()

			id := uuid.New()
			require.NoError(t, db.Create(&models.NotificationChannel{ID: id, Name: "钩子", Type: "webhook", Config: original}).Error)

			_, err := svc.Update(ctx, id.String(), map[string]interface{}{"config": tc.val})
			require.ErrorIs(t, err, ErrInvalidInput)

			var after models.NotificationChannel
			require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
			assert.Equal(t, original, after.Config, "坏 config 不得落库")
		})
	}

	t.Run("type_非字符串", func(t *testing.T) {
		db := newChannelSQLiteDB(t)
		svc := NewChannelService(db)
		ctx := context.Background()

		id := uuid.New()
		require.NoError(t, db.Create(&models.NotificationChannel{ID: id, Name: "钩子", Type: "webhook", Config: original}).Error)

		_, err := svc.Update(ctx, id.String(), map[string]interface{}{"type": float64(1)})
		require.ErrorIs(t, err, ErrInvalidInput)

		var after models.NotificationChannel
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Equal(t, "webhook", after.Type, "坏 type 不得落库")
	})
}

// V-8 校验失败原因必须带构造器原因，且经过 redact.Text（源头收口）
func TestChannelService_校验错误文本_带原因且已脱敏(t *testing.T) {
	db := newChannelSQLiteDB(t)
	svc := NewChannelService(db)
	ctx := context.Background()

	t.Run("名称空_带原因", func(t *testing.T) {
		err := svc.Create(ctx, &models.NotificationChannel{ID: uuid.New(), Name: "", Type: "webhook", Config: `{"url":"https://example.com/hook"}`}) //nolint:exhaustruct
		require.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "渠道名称不能为空")
	})

	t.Run("缺url_带构造器原因", func(t *testing.T) {
		err := svc.Create(ctx, &models.NotificationChannel{ID: uuid.New(), Name: "x", Type: "webhook", Config: `{}`}) //nolint:exhaustruct
		require.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "webhook: url is required")
	})

	// sender.go 的 default 分支不再回显 ch.Type（安全审计 M-1）：该文本会经 handler 原样进
	// 400 body，而 redact.Text 只挡 URL / 键值形态 —— 裸 token、JWT、percent 编码、
	// 无 scheme URL、多行文本都会穿过。这五类形态都必须拿不到回显。
	t.Run("未知类型不回显调用方字符串", func(t *testing.T) {
		for _, tc := range []struct {
			name, leaky, fragment string
		}{
			{"URL形态", "https://hooks.slack.com/services/T000/B000/SECRETPATH", "SECRETPATH"},
			{"裸token", "ghp-ABCDEF1234567890SECRET", "ghp-ABCDEF"},
			{"JWT", "eyJhbGciOiJIUzI1NiJ9.SECRETPAYLOAD.sig", "eyJhbGciOiJIUzI1NiJ9"},
			{"percent编码", "https%3A%2F%2Fhooks.slack.com%2Fservices%2FSECRETPATH", "SECRETPATH"},
			{"无scheme", "hooks.slack.com/services/SECRETPATH", "SECRETPATH"},
			{"多行", "line1\nPASSWORD=hunter2", "hunter2"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := svc.Create(ctx, &models.NotificationChannel{ID: uuid.New(), Name: "x", Type: tc.leaky, Config: `{}`}) //nolint:exhaustruct
				require.ErrorIs(t, err, ErrInvalidInput)
				assert.Contains(t, err.Error(), "unsupported channel type", "仍要能定位到「类型不支持」")
				assert.NotContains(t, err.Error(), tc.fragment, "错误文本不得含调用方字符串")
			})
		}
	})
}

// H-1 键归一化 + 白名单：gorm 的 Updates(map) 对每个键走 Schema.LookUpField（先列名再 Go 字段名），
// 所以 {"Config": …} / {"Type": …} 会绕过只匹配小写键的校验照样写列，{"id": …} 还能改主键。
// 真 PG 实测这些请求在修复前返回 HTTP 200 且坏值落库（安全审计 H-1 / 正确性审计 H-1）。
func TestChannelService_Update_键白名单(t *testing.T) {
	const original = `{"url":"https://example.com/hook"}`

	seed := func(t *testing.T) (*gorm.DB, ChannelService, uuid.UUID) {
		t.Helper()
		db := newChannelSQLiteDB(t)
		id := uuid.New()
		require.NoError(t, db.Create(&models.NotificationChannel{ID: id, Name: "钩子", Type: "webhook", Config: original}).Error)
		return db, NewChannelService(db), id
	}

	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"Go字段名Config_数字", map[string]interface{}{"Config": float64(12345)}},
		{"Go字段名Config_非法JSON", map[string]interface{}{"Config": "not-json"}},
		{"Go字段名Type_坏组合", map[string]interface{}{"Type": "dingtalk"}},
		{"小写主键id", map[string]interface{}{"id": uuid.New().String()}},
		{"Go字段名ID", map[string]interface{}{"ID": uuid.New().String()}},
		{"created_at", map[string]interface{}{"created_at": "2020-01-01T00:00:00Z"}},
		{"未知键", map[string]interface{}{"foo": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, svc, id := seed(t)
			_, err := svc.Update(context.Background(), id.String(), tc.updates)
			require.ErrorIs(t, err, ErrInvalidInput)

			var after models.NotificationChannel
			require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
			assert.Equal(t, id, after.ID, "主键不得被改写")
			assert.Equal(t, "webhook", after.Type)
			assert.Equal(t, original, after.Config)
		})
	}

	t.Run("正控_大小写变体归一化后合法即通过", func(t *testing.T) {
		db, svc, id := seed(t)
		_, err := svc.Update(context.Background(), id.String(), map[string]interface{}{"Name": "新名字"})
		require.NoError(t, err)

		var after models.NotificationChannel
		require.NoError(t, db.First(&after, "id = ?", id.String()).Error)
		assert.Equal(t, "新名字", after.Name)
	})
}
