package grpcserver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"network-monitor-platform/api/proto/alert/v1"
	"network-monitor-platform/internal/cursor"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/redact"
	"network-monitor-platform/internal/service"
	"network-monitor-platform/pkg/logger"
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

	alerts, _, _, err := s.svc.List(ctx, filter)
	if err != nil {
		return nil, serviceErrToStatus("ListAlerts", err)
	}

	// 老 page/size 不支持 — 走 offset 模拟
	if req.Cursor == "" && req.Page > 1 {
		offset := int((req.Page - 1)) * limit
		filter.Limit = limit + offset
		alerts, _, _, err = s.svc.List(ctx, filter)
		if err != nil {
			return nil, serviceErrToStatus("ListAlerts", err)
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
		return nil, serviceErrToStatus("GetAlert", err)
	}
	return alertToProto(a), nil
}

func (s *AlertServer) AckAlert(ctx context.Context, req *alertv1.AckAlertRequest) (*alertv1.Alert, error) {
	if req.Id == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "id and user_id required")
	}
	if err := s.svc.Acknowledge(ctx, req.Id, req.UserId); err != nil {
		// M19: 状态冲突要能被客户端识别成「别再试了」而不是「服务端炸了」
		return nil, serviceErrToStatus("AckAlert", err)
	}
	a, err := s.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, serviceErrToStatus("AckAlert", err)
	}
	return alertToProto(a), nil
}

func (s *AlertServer) ResolveAlert(ctx context.Context, req *alertv1.ResolveAlertRequest) (*alertv1.ResolveAlertResponse, error) {
	if req.Id == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "id and user_id required")
	}
	if err := s.svc.Resolve(ctx, req.Id, req.UserId); err != nil {
		// M19: 同 AckAlert
		return nil, serviceErrToStatus("ResolveAlert", err)
	}
	a, err := s.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, serviceErrToStatus("ResolveAlert", err)
	}
	return &alertv1.ResolveAlertResponse{Alert: alertToProto(a)}, nil
}

// ---- helpers ----

// serviceErrToStatus 把 service 层的哨兵错误翻译成 gRPC 状态码，一处集中。
//
// 为什么集中而不是各调用点自己 if：散着写必定会漏，而**漏掉的那支静默降级成
// codes.Internal** —— 客户端把「告警不存在」当成「服务端炸了」去重试，正是 M19
// 在 HTTP 侧修掉的那个形态（409 落 500）。故本函数覆盖 service 包的**全部**哨兵，
// 而不是只覆盖今天这条路径可达的那几个：翻译表本来就是用来防「将来新增的那支没人管」。
//
// 与 apierr.Respond 共用同一份出口契约（G-28）：**原始错误文本永不进响应**，
// 只在 codes.Internal 时经 redact.Text 记日志 —— 内部错误可能带 DSN / URL userinfo。
// 哨兵那几支的文案是我们自己写的、可安全外露：M19 刻意让状态冲突带上真实原因，
// 好让客户端区分「别再试了」和「改参数重试」。
//
// method 只进日志，用于定位是哪个 RPC。
func serviceErrToStatus(method string, err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		// 用具体文案而非哨兵的通用文本（"resource not found"）：gRPC 侧目前只服务告警。
		return status.Error(codes.NotFound, "alert not found")
	case errors.Is(err, service.ErrInvalidState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, service.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrTooManyItems):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	}
	logger.Errorf("gRPC 调用内部错误", "method", method, "err", redact.Text(err.Error()))
	return status.Error(codes.Internal, "internal error")
}

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
