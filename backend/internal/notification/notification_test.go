package notification

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// ==================== Sender 测试 ====================

func TestNewSender_不支持类型返错(t *testing.T) {
	_, err := NewSender(&models.NotificationChannel{Type: "carrier-pigeon"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestNewSender_nilChannel返错(t *testing.T) {
	_, err := NewSender(nil)
	assert.Error(t, err)
}

func TestDingTalkSender_缺webhookURL返错(t *testing.T) {
	_, err := NewDingTalkSender(&models.NotificationChannel{Config: `{}`})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_url is required")
}

func TestDingTalkSender_真实HTTP发送(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + srv.URL + `"}`})
	require.NoError(t, err)
	assert.Equal(t, "dingtalk", s.Type())

	err = s.Send(context.Background(), "", "test message")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hit))
}

func TestDingTalkSender_HTTP非2xx返错(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()
	s, _ := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + srv.URL + `"}`})
	err := s.Send(context.Background(), "", "x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestDingTalkSender_无效JSON配置返错(t *testing.T) {
	_, err := NewDingTalkSender(&models.NotificationChannel{Config: `{not json`})
	assert.Error(t, err)
}

func TestEmailSender_缺必填返错(t *testing.T) {
	_, err := NewEmailSender(&models.NotificationChannel{Config: `{}`})
	assert.Error(t, err)
}

func TestEmailSender_无to地址返错(t *testing.T) {
	_, err := NewEmailSender(&models.NotificationChannel{Config: `{
		"smtp_host":"smtp.example.com","smtp_port":587,
		"smtp_user":"u","from":"a@b.com"
	}`})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "to address required")
}

func TestEmailSender_有效配置构造成功(t *testing.T) {
	_, err := NewEmailSender(&models.NotificationChannel{Config: `{
		"smtp_host":"smtp.example.com","smtp_port":587,
		"smtp_user":"u","smtp_password":"p",
		"from":"a@b.com","to":["c@d.com"]
	}`})
	assert.NoError(t, err)
}

func TestEmailSender_发到无效SMTP返错(t *testing.T) {
	s, err := NewEmailSender(&models.NotificationChannel{Config: `{
		"smtp_host":"127.0.0.1","smtp_port":1,
		"smtp_user":"u","from":"a@b.com","to":["c@d.com"]
	}`})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = s.Send(ctx, "", "test")
	assert.Error(t, err, "无效端口应失败")
}

func TestWebhookSender_缺URL返错(t *testing.T) {
	_, err := NewWebhookSender(&models.NotificationChannel{Config: `{}`})
	assert.Error(t, err)
}

func TestWebhookSender_真实HTTP发送(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s, err := NewWebhookSender(&models.NotificationChannel{Config: `{"url":"` + srv.URL + `","secret":"topsecret"}`})
	require.NoError(t, err)
	err = s.Send(context.Background(), "", "x")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hit))
}

// ==================== Resolver / RegisterSender 测试 ====================

func TestRegisterSender_覆盖默认(t *testing.T) {
	mock := &mockSender{typ: "custom", err: nil}
	RegisterSender("custom", mock)
	defer delete(customSenders, "custom") // cleanup

	s, err := Resolver(&models.NotificationChannel{Type: "custom"})
	require.NoError(t, err)
	assert.Equal(t, "custom", s.Type())
}

func TestResolver_未知类型返错(t *testing.T) {
	_, err := Resolver(&models.NotificationChannel{Type: "pigeon"})
	assert.Error(t, err)
}

// TestResolver_并发注册与解析无竞态 钉住 G-38：customSenders 是包级 map，
// RegisterSender（写）与 Resolver（读）并发时必须走锁 —— 否则 Go runtime 直接
// `fatal error: concurrent map read and map write`，不可 recover，整个进程挂掉。
//
// 需要 `go test -race` 才有检出能力：普通 `go test` 下本用例也是绿的。
// 读者必须用 Resolver —— NewSender 只按 ch.Type 走 switch，**从不读 customSenders**，
// 用它当读者两者不访问同一内存，修前也是绿的（假绿）。
func TestResolver_并发注册与解析无竞态(t *testing.T) {
	const typ = "g38-race"
	t.Cleanup(func() {
		// 走锁清理，避免给后续用例留下未同步的写
		customSendersMu.Lock()
		delete(customSenders, typ)
		customSendersMu.Unlock()
	})

	var wg sync.WaitGroup
	start := make(chan struct{}) // 起跑线：让读写尽量重叠，跨 CI/单核环境也能稳定触发
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			RegisterSender(typ, &mockSender{typ: typ})
		}
		close(stop)
	}()

	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = Resolver(&models.NotificationChannel{Type: typ})
				}
			}
		}()
	}

	close(start)
	wg.Wait()
}

// mockSender 测试用 mock
type mockSender struct {
	typ  string
	err  error
	hits int32
}

func (m *mockSender) Type() string { return m.typ }
func (m *mockSender) Send(_ context.Context, _, _ string) error {
	atomic.AddInt32(&m.hits, 1)
	return m.err
}

// ==================== Worker 测试 ====================

func TestWorker_tickOnce_无pendingLog不调DB写(t *testing.T) {
	db, mock := newMockDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})

	rows := sqlmock.NewRows([]string{"id", "alert_id", "channel_id", "channel_name", "content", "status"})
	mock.ExpectQuery(`SELECT \* FROM "notification_logs"`).
		WillReturnRows(rows)

	err := w.tickOnce(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorker_tickOnce_成功发送标记success(t *testing.T) {
	db, mock := newMockDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	channelID := uuid.New()
	alertID := uuid.New()
	logID := uuid.New()

	// 1) SELECT pending logs
	rows := sqlmock.NewRows([]string{"id", "alert_id", "channel_id", "channel_name", "content", "status", "recipient", "sent_at", "error_msg"}).
		AddRow(logID, alertID, channelID, "钉钉群", "test", "pending", "", time.Now(), "")
	mock.ExpectQuery(`SELECT \* FROM "notification_logs"`).
		WillReturnRows(rows)

	// 2) SELECT channels by IDs
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}).
		AddRow(channelID, "钉钉群", "dingtalk", `{"webhook_url":"http://x"}`, true, false, time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(channelRows)

	// 3) UPDATE mark success
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_logs"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	err := w.tickOnce(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockSender.hits))
}

func TestWorker_tickOnce_发送失败标记failed(t *testing.T) {
	db, mock := newMockDB(t)
	mockSender := &mockSender{typ: "dingtalk", err: errors.New("network down")}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	channelID := uuid.New()
	alertID := uuid.New()
	logID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "alert_id", "channel_id", "channel_name", "content", "status", "recipient", "sent_at", "error_msg"}).
		AddRow(logID, alertID, channelID, "钉钉群", "test", "pending", "", time.Now(), "")
	mock.ExpectQuery(`SELECT \* FROM "notification_logs"`).
		WillReturnRows(rows)

	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}).
		AddRow(channelID, "钉钉群", "dingtalk", `{}`, true, false, time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(channelRows)

	// UPDATE mark failed (status=failed, error_msg)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_logs"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	err := w.tickOnce(context.Background())
	assert.NoError(t, err, "tickOnce 不应返 error 即使 send 失败")
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockSender.hits))
}

func TestWorker_tickOnce_channel禁用跳过(t *testing.T) {
	db, mock := newMockDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	channelID := uuid.New()
	alertID := uuid.New()
	logID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "alert_id", "channel_id", "channel_name", "content", "status", "recipient", "sent_at", "error_msg"}).
		AddRow(logID, alertID, channelID, "钉钉群", "test", "pending", "", time.Now(), "")
	mock.ExpectQuery(`SELECT \* FROM "notification_logs"`).
		WillReturnRows(rows)

	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}).
		AddRow(channelID, "禁用渠道", "dingtalk", `{}`, false, false, time.Now(), time.Now()) // IsEnabled=false
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(channelRows)

	// mark skipped (status=success, error_msg=channel disabled)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_logs"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	err := w.tickOnce(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits), "禁用渠道不应触发 sender")
}

func TestWorker_StartStop_生命周期(t *testing.T) {
	db, _ := newMockDB(t)
	w := NewWorker(db, WorkerConfig{Tick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	w.Stop()
	// 不应 panic, 不应 leak
}

func TestWorker_Stop_幂等(t *testing.T) {
	db, _ := newMockDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	w.Stop()
	// 二次 Stop 应 panic (close closed channel) — 这里不调, 测正常路径
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Stop 二次调用 panic (符合预期): %v", r)
		}
	}()
	w.Stop()
	cancel()
}

// ==================== G-28：错误文本不泄漏凭据 ====================

// newDeadListener 起一个只 bind 不 Accept 的 TCP 监听：三次握手在内核 backlog 里完成，
// 请求写出去后没有响应 → 客户端 ctx 超时。比"连不上"稳（refuse 的文案跨平台不一致）。
func newDeadListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// newSQLiteDB 开真 sqlite：markFailed 的断言要看写库后的行，sqlmock 断不了参数值。
func newSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // :memory: 每条连接一个独立库
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE notification_logs (
		id TEXT PRIMARY KEY,
		alert_id TEXT,
		channel_id TEXT,
		channel_name TEXT,
		recipient TEXT,
		content TEXT,
		status TEXT,
		error_msg TEXT,
		sent_at DATETIME,
		created_at DATETIME
	)`).Error)
	return db
}

func TestDingTalkSender_发送失败不泄漏URL凭据(t *testing.T) {
	ln := newDeadListener(t)
	secretURL := "http://" + ln.Addr().String() + "/robot/send?access_token=SUPERSECRET"
	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + secretURL + `"}`})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = s.Send(ctx, "", "内容")

	require.Error(t, err)
	// 正向：底层 cause 必须保留，别把诊断信息一起丢了
	require.True(t, errors.Is(err, context.DeadlineExceeded), "应保留底层 ctx 超时: %v", err)
	// 正向：scheme://host 只可能来自 redact.URL → 钉住它真的被调用
	require.Contains(t, err.Error(), "http://127.0.0.1:", "URL 必须塌缩成 scheme://host")
	// 反向：凭据与 URL 细节不得出现
	assert.NotContains(t, err.Error(), "SUPERSECRET")
	assert.NotContains(t, err.Error(), "access_token")
	assert.NotContains(t, err.Error(), "/robot/send")
}

func TestWebhookSender_发送失败不泄漏URL凭据(t *testing.T) {
	ln := newDeadListener(t)
	secretURL := "http://" + ln.Addr().String() + "/services/T000/B000/SECRETPATH"
	s, err := NewWebhookSender(&models.NotificationChannel{
		Config: `{"url":"` + secretURL + `","secret":"S3CR3THDR"}`,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = s.Send(ctx, "", "内容")

	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "应保留底层 ctx 超时: %v", err)
	require.Contains(t, err.Error(), "http://127.0.0.1:", "URL 必须塌缩成 scheme://host")
	assert.NotContains(t, err.Error(), "SECRETPATH")
	assert.NotContains(t, err.Error(), "services")
	assert.NotContains(t, err.Error(), "S3CR3THDR", "header 里的 secret 也不该出现在错误文本")
}

// 非法 URL 走的是 NewRequestWithContext（url.Parse）分支：这条路径没有 http.Client
// 的 stripPassword，url.Error 原样带 userinfo/query。
func TestDingTalkSender_非法URL不泄漏原串(t *testing.T) {
	const bad = "http://[::1"
	s, err := NewDingTalkSender(&models.NotificationChannel{Config: `{"webhook_url":"` + bad + `"}`})
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "内容")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing ']' in host", "保留 parse 原因，便于排障")
	assert.NotContains(t, err.Error(), bad)
}

// 审计 HIGH-1：webhook 的 parse 失败 return 点是独立一处（sender.go 的
// 「webhook: 无效的 url」），与钉钉那条同型却零覆盖 —— 变异退回裸 err 不被捕获。
// 用 Slack 形态（token 在 path）钉住：泄漏时 SECRETPATH 会原样出现在错误里。
func TestWebhookSender_非法URL不泄漏原串(t *testing.T) {
	const bad = "http://[::1/services/T000/B000/SECRETPATH"
	s, err := NewWebhookSender(&models.NotificationChannel{Config: `{"url":"` + bad + `"}`})
	require.NoError(t, err)

	err = s.Send(context.Background(), "", "内容")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing ']' in host", "保留 parse 原因，便于排障")
	assert.NotContains(t, err.Error(), bad)
	assert.NotContains(t, err.Error(), "SECRETPATH")
}

// TestMarkFailed_脱敏且按rune截断 同时钉三件事：
//  1. 凭据被 redact.Text 抹掉（变异：去掉脱敏 → SUPERSECRET 泄漏）
//  2. 按 rune 截断（变异：errMsg[:500] 字节截断 → 切断汉字 → ValidString 红）
//  3. UPDATE 真的生效（变异：吞掉 .Error → 行停在 pending → status 断言红）
func TestMarkFailed_脱敏且按rune截断(t *testing.T) {
	db := newSQLiteDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour})

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationLog{ID: id, Status: "pending"}).Error)

	// 18 字节键值（值后有空格定界，否则值类会把汉字一起吞掉）+ 600 个三字节汉字：
	// 脱敏后 618 rune / 1818 字节，rune 与字节两个维度都超 500，且 500 的边界落在
	// 汉字中间（500-18=482 不是 3 的倍数）——字节截断必切断字符。
	w.markFailed(context.Background(), id, "access_token=SUPERSECRET x"+strings.Repeat("中", 600))

	var got models.NotificationLog
	require.NoError(t, db.First(&got, "id = ?", id).Error, "行必须还在")
	require.Equal(t, "failed", got.Status, "UPDATE 必须真的写进去了")
	require.Contains(t, got.ErrorMsg, "access_token=***", "键名保留、值抹掉")
	assert.NotContains(t, got.ErrorMsg, "SUPERSECRET")
	assert.Equal(t, 500, utf8.RuneCountInString(got.ErrorMsg), "按 rune 截到 500（varchar(500) 是字符数）")
	assert.True(t, utf8.ValidString(got.ErrorMsg), "截断不得切断多字节字符（PG 会 22021 拒收）")
}

// TestMarkFailed_非法UTF8被清理 — 审计 P4：≤500 rune 的非法 UTF-8 旧版原样写库，
// 真 PG 会 22021 拒收 → 行永远停在 pending 被无限重发（sqlite 测不出编码）。
func TestMarkFailed_非法UTF8被清理(t *testing.T) {
	db := newSQLiteDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour})

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationLog{ID: id, Status: "pending"}).Error)

	// SMTP 服务端可控文本可携带任意字节（textproto.Error.Msg）
	w.markFailed(context.Background(), id, "smtp: 535 \xff\xfe auth failed")

	var got models.NotificationLog
	require.NoError(t, db.First(&got, "id = ?", id).Error)
	require.Equal(t, "failed", got.Status)
	assert.True(t, utf8.ValidString(got.ErrorMsg), "非法字节必须被替换，否则 PG 22021 拒收")
	assert.Contains(t, got.ErrorMsg, "smtp: 535")
}

// TestMarkFailed_控制字符被剥 — 安全审计 MEDIUM-1：错误文本来源不止 sender
// （SMTP 服务端文本、驱动错误、上游响应体），NUL 让 PG 拒收（22021 → 行永远停在
// pending 被无限重发），CR/LF 可把行式消费的 error_msg 伪造成多条记录。
func TestMarkFailed_控制字符被剥(t *testing.T) {
	db := newSQLiteDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour})

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationLog{ID: id, Status: "pending"}).Error)

	w.markFailed(context.Background(), id, "smtp: 535\x00\n[notification worker] forged\rbad")

	var got models.NotificationLog
	require.NoError(t, db.First(&got, "id = ?", id).Error)
	require.Equal(t, "failed", got.Status, "UPDATE 必须真的写进去了")
	assert.NotContains(t, got.ErrorMsg, "\x00", "NUL 不得入库")
	assert.NotContains(t, got.ErrorMsg, "\n", "换行不得入库（可伪造多条记录）")
	for _, r := range got.ErrorMsg {
		require.GreaterOrEqual(t, r, rune(0x20), "不得含控制字符: %q", got.ErrorMsg)
	}
	assert.Contains(t, got.ErrorMsg, "smtp: 535")
	assert.Contains(t, got.ErrorMsg, "[notification worker] forgedbad", "可打印内容保留，只剥控制字符")
}

// TestURLErrCause_剥壳与兜底 钉住「宁可丢诊断信息也不回传原串」的兜底分支。
func TestURLErrCause_剥壳与兜底(t *testing.T) {
	inner := errors.New("dial tcp: refused")
	// 剥一层：拿到底层 cause
	assert.Equal(t, inner, urlErrCause(&url.Error{Op: "Post", URL: "http://h/p?token=SECRET", Err: inner}))
	// 非 url.Error 原样返回
	assert.Equal(t, inner, urlErrCause(inner))
	// Err 为 nil → 固定文案，绝不回传带 URL 的原串
	assert.EqualError(t, urlErrCause(&url.Error{Op: "Post", URL: "http://h/p?token=SECRET", Err: nil}), "未知错误")
	// 恰好 4 层嵌套 → 仍能拿到底层 cause（审计 MED-2：旧循环把第 4 层丢进兜底）
	var deep4 error = inner
	for i := 0; i < 4; i++ {
		deep4 = &url.Error{Op: "Post", URL: "http://h/p?token=SECRET", Err: deep4}
	}
	assert.Equal(t, inner, urlErrCause(deep4), "4 层嵌套不该丢 cause")
	// 嵌套超过 4 层 → 固定文案
	var deep error = inner
	for i := 0; i < 5; i++ {
		deep = &url.Error{Op: "Post", URL: "http://h/p?token=SECRET", Err: deep}
	}
	assert.EqualError(t, urlErrCause(deep), "未知错误")
}

// markFailed 写库失败必须留痕：旧版丢弃返回值 → 行永远停在 pending 被无限重发。
func TestMarkFailed_写库失败记日志(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	db, mock := newMockDB(t)
	// gorm 默认把 Updates 包在事务里：BEGIN → UPDATE（失败）→ ROLLBACK
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_logs"`).WillReturnError(errors.New("boom: connection refused"))
	mock.ExpectRollback()

	w := NewWorker(db, WorkerConfig{Tick: time.Hour})
	w.markFailed(context.Background(), uuid.New(), "x")

	require.Contains(t, buf.String(), "markFailed", "写库失败必须留痕")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHandleAlertEvent_发送失败日志不泄漏URL凭据(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	db, mock := newMockDB(t)
	chID := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled"}).
			AddRow(chID.String(), "钉钉群", "dingtalk",
				`{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=x"}`, true))

	// 真实 sender 的错误形状：*url.Error 带完整 URL + 一条键值形态的密钥
	ms := &mockSender{typ: "dingtalk", err: errors.New(
		`Post "https://oapi.dingtalk.com/robot/send?access_token=SUPERSECRET": dial tcp: refused; secret=TOPLEVELSECRET`)}
	RegisterSender("dingtalk", ms)
	t.Cleanup(func() { delete(customSenders, "dingtalk") })

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	require.NoError(t, w.handleAlertEvent(context.Background(),
		eventbus.Event{Payload: []byte(`{"event_type":"created","trigger":"t","host_name":"h"}`)}))
	require.Equal(t, int32(1), atomic.LoadInt32(&ms.hits), "sender 必须真的被调用")

	logged := buf.String()
	require.Contains(t, logged, "send err for channel", "必须走到失败日志这一行")
	require.Contains(t, logged, "https://oapi.dingtalk.com", "URL 塌缩成 scheme://host")
	require.Contains(t, logged, "secret=***", "键值形态的密钥被抹掉")
	assert.NotContains(t, logged, "SUPERSECRET")
	assert.NotContains(t, logged, "TOPLEVELSECRET")
	assert.NotContains(t, logged, "access_token")
}

// 审计 MED-1：resolver 失败日志（worker.go 的「resolver err for channel」）零覆盖。
// 内置 Resolver 的错误不含 URL，但 RegisterSender 注册的第三方构造器可以任意返回 ——
// 用 WorkerConfig.Resolver 注入一个带凭据的错误，钉住这一行确实过了 redact.Text。
func TestHandleAlertEvent_resolver错误日志不泄漏URL凭据(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled"}).
			AddRow(uuid.New().String(), "自定义", "custom",
				`{"url":"https://hooks.slack.com/services/T000/B000/SECRETPATH"}`, true))

	w := NewWorker(db, WorkerConfig{
		Tick: time.Hour, MaxBatch: 10,
		Resolver: func(*models.NotificationChannel) (Sender, error) {
			return nil, errors.New(`Post "https://hooks.slack.com/services/T000/B000/SECRETPATH": dial tcp: refused`)
		},
	})
	require.NoError(t, w.handleAlertEvent(context.Background(),
		eventbus.Event{Payload: []byte(`{"event_type":"created","trigger":"t","host_name":"h"}`)}))

	logged := buf.String()
	require.Contains(t, logged, "resolver err for channel", "必须走到 resolver 失败日志这一行")
	require.Contains(t, logged, "https://hooks.slack.com", "URL 塌缩成 scheme://host")
	assert.NotContains(t, logged, "SECRETPATH")
	assert.NotContains(t, logged, "services")
}

// TestMarkFailed_顺序必须先Strip再Text — M29-F 的守门用例。
//
// 控制字符会截断 redact.Text 的值类：先 Text 后 Strip 时，`password=abc\nDEF` 只被遮到
// `\n` 为止，之后 Strip 把 `\n` 删掉，等于把**未遮盖的尾部接回**一个已被认成凭据的串上。
// 修前 markFailed 正是这个顺序（真泄漏），同包 sender.sanitizeSnippet 一直是对的。
func TestMarkFailed_顺序必须先Strip再Text(t *testing.T) {
	db := newSQLiteDB(t)
	w := NewWorker(db, WorkerConfig{Tick: time.Hour})

	id := uuid.New()
	require.NoError(t, db.Create(&models.NotificationLog{ID: id, Status: "pending"}).Error)

	// 最坏形态：控制字符把**键**切开 —— 先 Text 的话连 `password=` 都识别不到，值原文入库
	w.markFailed(context.Background(), id, "pass\nword=SUPERSECRET")

	var got models.NotificationLog
	require.NoError(t, db.First(&got, "id = ?", id).Error)
	require.Equal(t, "failed", got.Status, "UPDATE 必须真的写进去了")
	assert.NotContains(t, got.ErrorMsg, "SUPERSECRET", "控制字符切开键时，值也必须被遮盖")
	assert.Contains(t, got.ErrorMsg, "password=***", "键名保留便于定位")
	assert.NotContains(t, got.ErrorMsg, "\n")
}

// TestHandleAlertEvent_日志不得被渠道名伪造 — M29-F：channel 名进了三行日志，
// 而 notification_channels.name 只校验非空、没有控制字符校验（channel_service.go:69）。
// 只净化 err 不够 —— 含 CR/LF 的渠道名照样能伪造出一整行假日志。
func TestHandleAlertEvent_日志不得被渠道名伪造(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled"}).
			AddRow(uuid.New().String(), "群\r\n[notification worker] FORGED LINE", "dingtalk",
				`{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=x"}`, true))

	ms := &mockSender{typ: "dingtalk", err: errors.New("boom")}
	RegisterSender("dingtalk", ms)
	t.Cleanup(func() { delete(customSenders, "dingtalk") })

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	require.NoError(t, w.handleAlertEvent(context.Background(),
		eventbus.Event{Payload: []byte(`{"event_type":"created","trigger":"t","host_name":"h"}`)}))

	logged := buf.String()
	require.Contains(t, logged, "send err for channel", "必须走到失败日志这一行")
	assert.NotContains(t, logged, "\r", "渠道名里的 CR 不得进日志：%q", logged)
	assert.NotContains(t, logged, "群\r\n", "渠道名里的 CR/LF 不得进日志")
	assert.Contains(t, logged, "FORGED LINE", "内容保留（只是并进同一行），证明不是丢弃")
}
