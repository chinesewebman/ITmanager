package grpcserver

import (
	"context"
	"errors"
	"time"

	"network-monitor-platform/api/proto/alert/v1"
	"network-monitor-platform/internal/cursor"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

// AlertServer 包装 AlertService 暴露 gRPC
type AlertServer struct {
	alertv1.UnimplementedAlertServiceServer
	svc service.AlertService
}

func NewAlertServer(svc service.AlertService) *AlertServer {
	return &AlertServer{svc: svc}
}

func (s *AlertServer) ListAlerts(ctx context.Context, req *alertv1.ListAlertsRequest) (*alertv1.ListAlertsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	filter := service.AlertFilter{
		Severity: severityToInt(req.MinSeverity),
		Status:   alertStatusToString(req.Status),
		HostID:   req.Source,
		Limit:    limit,
	}

	// cursor 模式优先
	if req.Cursor != "" {
		cur, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid cursor: %v", err)
		}
		uid, perr := uuid.Parse(cur.ID)
		if perr != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid cursor id: %v", perr)
		}
		filter.CursorTS = cur.TS
		filter.CursorID = uid
	}

	alerts, _, err := s.svc.List(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	// 老 page/size 不支持 — 走 offset 模拟
	if req.Cursor == "" && req.Page > 1 {
		offset := int((req.Page - 1)) * limit
		filter.Limit = limit + offset
		alerts, _, err = s.svc.List(ctx, filter)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		if len(alerts) > offset {
			alerts = alerts[offset:]
		} else {
			alerts = nil
		}
	}
	return alertsToResponse(alerts, limit), nil
}

func (s *AlertServer) GetAlert(ctx context.Context, req *alertv1.GetAlertRequest) (*alertv1.Alert, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id required")
	}
	a, err := s.svc.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "alert not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return alertToProto(a), nil
}

func (s *AlertServer) AckAlert(ctx context.Context, req *alertv1.AckAlertRequest) (*alertv1.Alert, error) {
	if req.Id == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "id and user_id required")
	}
	if err := s.svc.Acknowledge(ctx, req.Id, req.UserId); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	a, err := s.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return alertToProto(a), nil
}

func (s *AlertServer) ResolveAlert(ctx context.Context, req *alertv1.ResolveAlertRequest) (*alertv1.ResolveAlertResponse, error) {
	if req.Id == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "id and user_id required")
	}
	if err := s.svc.Resolve(ctx, req.Id, req.UserId); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	a, err := s.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &alertv1.ResolveAlertResponse{Alert: alertToProto(a)}, nil
}

// ---- helpers ----

type decodedCursor struct {
	TS time.Time
	ID string
}

func decodeCursor(s string) (decodedCursor, error) {
	ts, id, err := cursor.Decode(s)
	if err != nil {
		return decodedCursor{}, err
	}
	return decodedCursor{TS: ts, ID: id.String()}, nil
}

func severityToInt(s alertv1.Severity) int {
	switch s {
	case alertv1.Severity_SEVERITY_INFO:
		return 1
	case alertv1.Severity_SEVERITY_WARNING:
		return 2
	case alertv1.Severity_SEVERITY_ERROR:
		return 3
	case alertv1.Severity_SEVERITY_CRITICAL:
		return 4
	}
	return 0
}

func alertStatusToString(s alertv1.AlertStatus) string {
	switch s {
	case alertv1.AlertStatus_ALERT_STATUS_PENDING:
		return "pending"
	case alertv1.AlertStatus_ALERT_STATUS_ACKED:
		return "acked"
	case alertv1.AlertStatus_ALERT_STATUS_RESOLVED:
		return "resolved"
	}
	return ""
}

func intToSeverityProto(s int) alertv1.Severity {
	switch s {
	case 1:
		return alertv1.Severity_SEVERITY_INFO
	case 2:
		return alertv1.Severity_SEVERITY_WARNING
	case 3:
		return alertv1.Severity_SEVERITY_ERROR
	case 4, 5:
		return alertv1.Severity_SEVERITY_CRITICAL
	}
	return alertv1.Severity_SEVERITY_UNSPECIFIED
}

func stringToStatusProto(s string) alertv1.AlertStatus {
	switch s {
	case "pending":
		return alertv1.AlertStatus_ALERT_STATUS_PENDING
	case "acked", "acknowledged":
		return alertv1.AlertStatus_ALERT_STATUS_ACKED
	case "resolved":
		return alertv1.AlertStatus_ALERT_STATUS_RESOLVED
	}
	return alertv1.AlertStatus_ALERT_STATUS_UNSPECIFIED
}

func alertToProto(a *models.Alert) *alertv1.Alert {
	if a == nil {
		return nil
	}
	out := &alertv1.Alert{
		Id:          a.ID.String(),
		Source:      "host", // 模型无 source 字段, 默认 host (Zabbix 风格)
		SourceId:    a.TriggerID,
		Severity:    intToSeverityProto(a.Severity),
		Status:      stringToStatusProto(a.Status),
		Title:       a.TriggerName,
		Description: a.Problem,
		RuleId:      a.TriggerID,
		ResolvedBy:  a.ResolveUser,
		Labels:      map[string]string{},
	}
	if !a.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(a.CreatedAt)
	}
	if a.ResolveTime != nil && !a.ResolveTime.IsZero() {
		out.ResolvedAt = timestamppb.New(*a.ResolveTime)
	}
	if a.HostName != "" {
		out.Labels["host_name"] = a.HostName
	}
	if a.HostIP != "" {
		out.Labels["host_ip"] = a.HostIP
	}
	return out
}

func alertsToResponse(alerts []models.Alert, limit int) *alertv1.ListAlertsResponse {
	resp := &alertv1.ListAlertsResponse{
		Alerts: make([]*alertv1.Alert, 0, len(alerts)),
		Total:  uint32(len(alerts)),
	}
	for i := range alerts {
		resp.Alerts = append(resp.Alerts, alertToProto(&alerts[i]))
	}
	// 满页 → 有下一页
	if len(alerts) >= limit && len(alerts) > 0 {
		last := alerts[len(alerts)-1]
		resp.NextCursor = cursor.Encode(last.CreatedAt, last.ID)
	}
	return resp
}
