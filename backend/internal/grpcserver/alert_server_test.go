package grpcserver

import (
	"context"
	"testing"
	"time"

	"network-monitor-platform/api/proto/alert/v1"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
)

// fakeAlertService 是测试用 stub, 不依赖 gorm.DB
type fakeAlertService struct {
	service.AlertService
	listFn    func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error)
	getFn     func(ctx context.Context, id string) (*models.Alert, error)
	resolveFn func(ctx context.Context, id, userID string) error
	ackFn     func(ctx context.Context, id, userID string) error
}

func (f *fakeAlertService) List(ctx context.Context, filter service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}
	return nil, service.AlertStats{}, nil
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
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
				return nil, service.AlertStats{}, nil
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
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
				if f.CursorID == (uuidZero()) {
					t.Errorf("expected non-zero cursor id")
				}
				// 返回满页 → 应有 next_cursor
				return []models.Alert{
					{ID: uid, Status: "pending", Severity: 2, CreatedAt: now},
					{ID: uid2, Status: "acked", Severity: 3, CreatedAt: now.Add(-time.Second)},
				}, service.AlertStats{}, nil
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
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
				t.Fatal("service.List should not be called with invalid cursor")
				return nil, service.AlertStats{}, nil
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
			listFn: func(ctx context.Context, f service.AlertFilter) ([]models.Alert, service.AlertStats, error) {
				// 返回 1 条 (< limit=10) → 无 next_cursor
				return []models.Alert{{ID: uid, Status: "resolved", CreatedAt: time.Now()}}, service.AlertStats{}, nil
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
