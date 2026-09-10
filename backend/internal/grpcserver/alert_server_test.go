package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"network-monitor-platform/api/proto/alert/v1"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

// requireCode 断言 err 的 gRPC 状态码。err 为 nil 时直接失败。
func requireCode(t *testing.T, err error, want codes.Code) *status.Status {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %v，实际拿到 nil error", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("不是 gRPC status error: %v", err)
	}
	if st.Code() != want {
		t.Fatalf("错误码 = %v，期望 %v（message=%q）", st.Code(), want, st.Message())
	}
	return st
}

// fakeAlertService 是测试用 stub, 不依赖 gorm.DB
type fakeAlertService struct {
	service.AlertService
	listFn    func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error)
	getFn     func(ctx context.Context, id string) (*models.Alert, error)
	resolveFn func(ctx context.Context, id, userID string) error
	ackFn     func(ctx context.Context, id, userID string) error
}

func (f *fakeAlertService) List(ctx context.Context, filter service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}
	return nil, service.AlertStats{}, 0, nil
}

func (f *fakeAlertService) Get(ctx context.Context, id string) (*models.Alert, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return nil, nil
}

func (f *fakeAlertService) Resolve(ctx context.Context, id, userID string) error {
	if f.resolveFn != nil {
		return f.resolveFn(ctx, id, userID)
	}
	return nil
}

func (f *fakeAlertService) Acknowledge(ctx context.Context, id, userID string) error {
	if f.ackFn != nil {
		return f.ackFn(ctx, id, userID)
	}
	return nil
}

func TestListAlerts_EmptyResult(t *testing.T) {
	srv := &AlertServer{
		svc: &fakeAlertService{
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
				return nil, service.AlertStats{}, 0, nil
			},
		},
	}
	resp, err := srv.ListAlerts(context.Background(), &alertv1.ListAlertsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(resp.Alerts))
	}
	if resp.NextCursor != "" {
		t.Errorf("expected empty next_cursor, got %q", resp.NextCursor)
	}
}

func TestListAlerts_WithCursor_HasNextPage(t *testing.T) {
	now := time.Now()
	uid := newUUID(t)
	uid2 := newUUID(t)
	srv := &AlertServer{
		svc: &fakeAlertService{
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
				if f.CursorID == (uuidZero()) {
					t.Errorf("expected non-zero cursor id")
				}
				// 返回满页 → 应有 next_cursor
				return []models.Alert{
					{ID: uid, Status: "pending", Severity: 2, CreatedAt: now},
					{ID: uid2, Status: "acked", Severity: 3, CreatedAt: now.Add(-time.Second)},
				}, service.AlertStats{}, 0, nil
			},
		},
	}
	// 构造合法 cursor
	cur := encodeForTest(now.Add(-time.Hour), newUUID(t))
	resp, err := srv.ListAlerts(context.Background(), &alertv1.ListAlertsRequest{Limit: 2, Cursor: cur})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Alerts) != 2 {
		t.Errorf("expected 2 alerts, got %d", len(resp.Alerts))
	}
	if resp.NextCursor == "" {
		t.Error("expected non-empty next_cursor (满页)")
	}
}

func TestListAlerts_InvalidCursor(t *testing.T) {
	srv := &AlertServer{
		svc: &fakeAlertService{
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
				t.Fatal("service.List should not be called with invalid cursor")
				return nil, service.AlertStats{}, 0, nil
			},
		},
	}
	_, err := srv.ListAlerts(context.Background(), &alertv1.ListAlertsRequest{Cursor: "not-a-valid-cursor!!!"})
	if err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestListAlerts_PartialPage_NoNextCursor(t *testing.T) {
	uid := newUUID(t)
	srv := &AlertServer{
		svc: &fakeAlertService{
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
				// 返回 1 条 (< limit=10) → 无 next_cursor
				return []models.Alert{{ID: uid, Status: "resolved", CreatedAt: time.Now()}}, service.AlertStats{}, 0, nil
			},
		},
	}
	resp, err := srv.ListAlerts(context.Background(), &alertv1.ListAlertsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(resp.Alerts))
	}
	if resp.NextCursor != "" {
		t.Errorf("expected empty next_cursor for partial page, got %q", resp.NextCursor)
	}
}

func TestGetAlert_MissingID(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{}}
	_, err := srv.GetAlert(context.Background(), &alertv1.GetAlertRequest{Id: ""})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestAckAlert_InvalidID(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{}}
	_, err := srv.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: ""})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestResolveAlert_InvalidUserID(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{}}
	_, err := srv.ResolveAlert(context.Background(), &alertv1.ResolveAlertRequest{Id: "x", UserId: ""})
	if err == nil {
		t.Fatal("expected error for empty user_id")
	}
}

// ---- M21 gRPC 错误映射 ----

// TestServiceErrToStatus_哨兵全覆盖 钉住 service 包**全部**哨兵的翻译。
// 漏掉任何一支都会静默降级成 Internal —— 那正是 M19 在 HTTP 侧修掉的形态。
func TestServiceErrToStatus_哨兵全覆盖(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"ErrNotFound", service.ErrNotFound, codes.NotFound},
		{"ErrInvalidState", service.ErrInvalidState, codes.FailedPrecondition},
		{"ErrInvalidInput", service.ErrInvalidInput, codes.InvalidArgument},
		{"ErrTooManyItems", service.ErrTooManyItems, codes.InvalidArgument},
		{"ErrAlreadyExists", service.ErrAlreadyExists, codes.AlreadyExists},
		// 包装过一层也要能认出来（service 层普遍用 %w 带上下文）
		{"包装后的 ErrNotFound", fmt.Errorf("查告警失败: %w", service.ErrNotFound), codes.NotFound},
		{"包装后的 ErrInvalidState", fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState), codes.FailedPrecondition},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireCode(t, serviceErrToStatus("Test", c.err), c.want)
		})
	}
}

// TestServiceErrToStatus_内部错误不外泄原文 钉住 G-28 的出口契约：
// 非哨兵错误一律 Internal，且**原始文本不进响应**（内部错误可能带 DSN / URL userinfo）。
func TestServiceErrToStatus_内部错误不外泄原文(t *testing.T) {
	raw := errors.New(`dial tcp 10.1.2.3:5432: failed: password=sup3rs3cr3t`)
	st := requireCode(t, serviceErrToStatus("Test", raw), codes.Internal)

	if strings.Contains(st.Message(), "sup3rs3cr3t") {
		t.Errorf("凭据泄漏进 gRPC 响应: %q", st.Message())
	}
	if strings.Contains(st.Message(), "10.1.2.3") {
		t.Errorf("内部地址泄漏进 gRPC 响应: %q", st.Message())
	}
	if strings.Contains(st.Message(), "dial tcp") {
		t.Errorf("原始错误文本泄漏进 gRPC 响应: %q", st.Message())
	}
}

// TestServiceErrToStatus_状态冲突保留真实原因 反面：哨兵那几支**不能**被一并抹平。
// M19 刻意让状态冲突带上原因，好让客户端区分「别再试了」和「改参数重试」。
func TestServiceErrToStatus_状态冲突保留真实原因(t *testing.T) {
	err := fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState)
	st := requireCode(t, serviceErrToStatus("Test", err), codes.FailedPrecondition)
	if !strings.Contains(st.Message(), "告警当前状态不允许该操作") {
		t.Errorf("状态冲突丢失了真实原因，客户端无从判断下一步: %q", st.Message())
	}
}

// TestGetAlert_NotFound_返NotFound 是 M21 的核心回归：
// 原实现判的是 `errors.Is(err, gorm.ErrRecordNotFound)`，而 service.Get 返回的是
// service.ErrNotFound —— 判据永不命中，于是「告警不存在」被报成 Internal，
// 客户端把它当服务端故障去重试。这里钉住 NotFound。
func TestGetAlert_NotFound_返NotFound(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{
		getFn: func(ctx context.Context, id string) (*models.Alert, error) {
			return nil, service.ErrNotFound
		},
	}}
	_, err := srv.GetAlert(context.Background(), &alertv1.GetAlertRequest{Id: "no-such-id"})
	requireCode(t, err, codes.NotFound)
}

func TestAckAlert_InvalidState_返FailedPrecondition(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{
		ackFn: func(ctx context.Context, id, userID string) error {
			return fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState)
		},
	}}
	_, err := srv.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "x", UserId: "u"})
	st := requireCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(st.Message(), "告警当前状态不允许该操作") {
		t.Errorf("丢失真实原因: %q", st.Message())
	}
}

func TestResolveAlert_InvalidState_返FailedPrecondition(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{
		resolveFn: func(ctx context.Context, id, userID string) error {
			return fmt.Errorf("%w: 告警当前状态不允许该操作", service.ErrInvalidState)
		},
	}}
	_, err := srv.ResolveAlert(context.Background(), &alertv1.ResolveAlertRequest{Id: "x", UserId: "u"})
	requireCode(t, err, codes.FailedPrecondition)
}

// TestAckAlert_失败后重取也走映射 GetAlert 之前那次 s.svc.Get 也曾写死 Internal，
// 且复读拿到 ErrNotFound 是真实可达的（并发下记录被删）。
func TestAckAlert_复读失败走映射(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{
		ackFn: func(ctx context.Context, id, userID string) error { return nil },
		getFn: func(ctx context.Context, id string) (*models.Alert, error) {
			return nil, service.ErrNotFound
		},
	}}
	_, err := srv.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "x", UserId: "u"})
	requireCode(t, err, codes.NotFound)
}

// TestListAlerts_服务端错误走映射 列表路径同样不许外泄原文。
func TestListAlerts_服务端错误走映射(t *testing.T) {
	srv := &AlertServer{svc: &fakeAlertService{
		listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, int64, error) {
			return nil, service.AlertStats{}, 0, errors.New("pq: relation \"alerts\" does not exist")
		},
	}}
	_, err := srv.ListAlerts(context.Background(), &alertv1.ListAlertsRequest{Limit: 10})
	st := requireCode(t, err, codes.Internal)
	if strings.Contains(st.Message(), "relation") {
		t.Errorf("原始 SQL 报错泄漏进 gRPC 响应: %q", st.Message())
	}
}

func TestSeverityToInt(t *testing.T) {
	cases := []struct {
		in   alertv1.Severity
		want int
	}{
		{alertv1.Severity_SEVERITY_INFO, 1},
		{alertv1.Severity_SEVERITY_WARNING, 2},
		{alertv1.Severity_SEVERITY_ERROR, 3},
		{alertv1.Severity_SEVERITY_CRITICAL, 4},
		{alertv1.Severity_SEVERITY_UNSPECIFIED, 0},
	}
	for _, c := range cases {
		if got := severityToInt(c.in); got != c.want {
			t.Errorf("severityToInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestAlertStatusToString(t *testing.T) {
	if got := alertStatusToString(alertv1.AlertStatus_ALERT_STATUS_PENDING); got != "pending" {
		t.Errorf("pending: got %q", got)
	}
	if got := alertStatusToString(alertv1.AlertStatus_ALERT_STATUS_RESOLVED); got != "resolved" {
		t.Errorf("resolved: got %q", got)
	}
	if got := alertStatusToString(alertv1.AlertStatus_ALERT_STATUS_UNSPECIFIED); got != "" {
		t.Errorf("unspec: got %q, want empty", got)
	}
}

func TestIntToSeverityProto(t *testing.T) {
	if got := intToSeverityProto(1); got != alertv1.Severity_SEVERITY_INFO {
		t.Errorf("1 → %v, want INFO", got)
	}
	if got := intToSeverityProto(5); got != alertv1.Severity_SEVERITY_CRITICAL {
		t.Errorf("5 → %v, want CRITICAL (Zabbix Disaster)", got)
	}
	if got := intToSeverityProto(99); got != alertv1.Severity_SEVERITY_UNSPECIFIED {
		t.Errorf("99 → %v, want UNSPECIFIED", got)
	}
}
