package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/postmortem"
)

// mockTimelineFetcher 实现 TimelineFetcher 接口，避免拉 DiagnosticService 真 DB
type mockTimelineFetcher struct {
	timeline *models.DiagnosticTimeline
	err      error
}

func (m *mockTimelineFetcher) GetTimeline(ctx context.Context, assetID uuid.UUID, filter DiagnosticFilter) (*models.DiagnosticTimeline, error) {
	return m.timeline, m.err
}

// fakeRenderer 测试用 Renderer, 写入到 buf + 返回 err
type fakeRenderer struct {
	called bool
	data   *postmortem.ReportData
	buf    io.Writer
	err    error
}

func (f *fakeRenderer) Render(data *postmortem.ReportData, w io.Writer) error {
	f.called = true
	f.data = data
	f.buf = w
	if f.err != nil {
		return f.err
	}
	if w != nil {
		_, werr := w.Write([]byte("PDF-STUB"))
		return werr
	}
	return nil
}

// ==================== NewPostmortemService / SetRenderer ====================

func TestNewPostmortemService_默认FpdfRenderer(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{}
	svc := NewPostmortemService(db, fetcher)
	require.NotNil(t, svc)
	assert.NotNil(t, svc.renderer, "默认应有 FpdfRenderer")
	assert.Same(t, db, svc.db)
	assert.Same(t, fetcher, svc.diag)
}

func TestSetRenderer_覆盖默认Renderer(t *testing.T) {
	db, _ := newMockDB(t)
	svc := NewPostmortemService(db, &mockTimelineFetcher{})
	custom := &fakeRenderer{}
	svc.SetRenderer(custom)
	assert.Same(t, custom, svc.renderer)
}

// ==================== effectiveDays ====================

func TestEffectiveDays_默认30(t *testing.T) {
	assert.Equal(t, 30, effectiveDays(0))
	assert.Equal(t, 30, effectiveDays(-1))
	assert.Equal(t, 30, effectiveDays(-999))
}

func TestEffectiveDays_上限365(t *testing.T) {
	assert.Equal(t, 365, effectiveDays(366))
	assert.Equal(t, 365, effectiveDays(99999))
}

func TestEffectiveDays_区间内返回原值(t *testing.T) {
	assert.Equal(t, 1, effectiveDays(1))
	assert.Equal(t, 7, effectiveDays(7))
	assert.Equal(t, 30, effectiveDays(30))
	assert.Equal(t, 100, effectiveDays(100))
	assert.Equal(t, 365, effectiveDays(365))
}

// ==================== GenerateReport ====================

func TestGenerateReport_成功_空Timeline(t *testing.T) {
	db, _ := newMockDB(t)
	timeline := &models.DiagnosticTimeline{
		Asset:   &models.DiagnosticAsset{ID: uuid.New(), Name: "test-asset"},
		Events:  []models.TimelineEvent{},
		Summary: &models.DiagnosticSummary{},
	}
	fetcher := &mockTimelineFetcher{timeline: timeline}
	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	var buf bytes.Buffer
	data, err := svc.GenerateReport(context.Background(), &buf, uuid.New(), GenerateReportParams{Days: 7, Limit: 50})
	require.NoError(t, err)
	assert.True(t, renderer.called, "Renderer.Render 必调")
	assert.NotNil(t, data)
	assert.Equal(t, 7, data.WindowDays)
	assert.Equal(t, "test-asset", data.Timeline.Asset.Name)
	assert.Equal(t, "PDF-STUB", buf.String(), "应写入 PDF-STUB")
}

func TestGenerateReport_默认参数_Days30_Limit200(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{timeline: &models.DiagnosticTimeline{}}
	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	_, err := svc.GenerateReport(context.Background(), io.Discard, uuid.New(), GenerateReportParams{})
	require.NoError(t, err)
	assert.Equal(t, 30, renderer.data.WindowDays, "Days=0 → effective 30")
}

func TestGenerateReport_超365_截断(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{timeline: &models.DiagnosticTimeline{}}
	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	_, err := svc.GenerateReport(context.Background(), io.Discard, uuid.New(), GenerateReportParams{Days: 9999})
	require.NoError(t, err)
	assert.Equal(t, 365, renderer.data.WindowDays, "Days>365 → effective 365")
}

func TestGenerateReport_资产不存在_返回ErrNotFound(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{err: ErrNotFound}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(&fakeRenderer{})

	assetID := uuid.New()
	_, err := svc.GenerateReport(context.Background(), io.Discard, assetID, GenerateReportParams{Days: 7})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), assetID.String(), "错误信息应含 assetID")
}

func TestGenerateReport_GetTimeline其他错误_包装返回(t *testing.T) {
	db, _ := newMockDB(t)
	otherErr := errors.New("db connection lost")
	fetcher := &mockTimelineFetcher{err: otherErr}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(&fakeRenderer{})

	_, err := svc.GenerateReport(context.Background(), io.Discard, uuid.New(), GenerateReportParams{Days: 7})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound, "其他错误不应被转 ErrNotFound")
	assert.Contains(t, err.Error(), "拉取时间线")
	assert.ErrorIs(t, err, otherErr, "原始错误应可 Is 追溯")
}

func TestGenerateReport_Renderer失败_返回错误(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{timeline: &models.DiagnosticTimeline{}}
	renderErr := errors.New("fpdf: out of memory")
	renderer := &fakeRenderer{err: renderErr}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	_, err := svc.GenerateReport(context.Background(), io.Discard, uuid.New(), GenerateReportParams{Days: 7})
	require.Error(t, err)
	assert.ErrorIs(t, err, renderErr, "renderer 错误应透传")
	assert.Contains(t, err.Error(), "渲染 PDF")
}

// ==================== fetchIP ====================

func TestFetchIP_IPv4优先(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()
	rows := sqlmock.NewRows([]string{"ipv4_address", "ipv_address"}).
		AddRow("", "2001:db8::1").  // IPv6 first
		AddRow("10.0.0.5", "")        // IPv4 second, 应优先
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnRows(rows)

	svc := NewPostmortemService(db, &mockTimelineFetcher{})
	ip, err := svc.fetchIP(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", ip, "IPv4 优先于 IPv6")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFetchIP_只有IPv6(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()
	rows := sqlmock.NewRows([]string{"ipv4_address", "ipv_address"}).
		AddRow("", "fe80::1").
		AddRow("", "2001:db8::42")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnRows(rows)

	svc := NewPostmortemService(db, &mockTimelineFetcher{})
	ip, err := svc.fetchIP(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, "fe80::1", ip, "取第一个 IPv6")
}

func TestFetchIP_空网络_返空字符串(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()
	rows := sqlmock.NewRows([]string{"ipv4_address", "ipv_address"}) // 空
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnRows(rows)

	svc := NewPostmortemService(db, &mockTimelineFetcher{})
	ip, err := svc.fetchIP(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, "", ip)
}

func TestFetchIP_查询错误_返error(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()
	dbErr := errors.New("relation does not exist")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnError(dbErr)

	svc := NewPostmortemService(db, &mockTimelineFetcher{})
	ip, err := svc.fetchIP(context.Background(), assetID)
	require.Error(t, err)
	assert.Equal(t, "", ip)
	assert.ErrorIs(t, err, dbErr)
}

// ==================== GenerateReport IP 集成 ====================

func TestGenerateReport_有IP_填入ReportData(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()

	// 1) GetTimeline mock (不需要真 db, 用 fetcher 替)
	timeline := &models.DiagnosticTimeline{Asset: &models.DiagnosticAsset{ID: assetID}}
	fetcher := &mockTimelineFetcher{timeline: timeline}

	// 2) fetchIP 的 sqlmock
	ipRows := sqlmock.NewRows([]string{"ipv4_address", "ipv_address"}).AddRow("192.168.1.100", "")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnRows(ipRows)

	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	data, err := svc.GenerateReport(context.Background(), io.Discard, assetID, GenerateReportParams{Days: 7})
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.100", data.IPAddress)
}

func TestGenerateReport_IP查不到_不阻塞_空字符串(t *testing.T) {
	db, mock := newMockDB(t)
	assetID := uuid.New()

	timeline := &models.DiagnosticTimeline{Asset: &models.DiagnosticAsset{ID: assetID}}
	fetcher := &mockTimelineFetcher{timeline: timeline}

	// fetchIP 失败 — GenerateReport 不应阻塞, IP 留空
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ipv4_address, ipv_address FROM "asset_networks"`)).
		WithArgs(assetID).
		WillReturnError(errors.New("permission denied"))

	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	data, err := svc.GenerateReport(context.Background(), io.Discard, assetID, GenerateReportParams{Days: 7})
	require.NoError(t, err, "IP 查不到不阻塞主流程")
	assert.Equal(t, "", data.IPAddress)
	assert.True(t, renderer.called, "Renderer 仍应跑")
}

func TestGenerateReport_GeneratedAt是UTC(t *testing.T) {
	db, _ := newMockDB(t)
	fetcher := &mockTimelineFetcher{timeline: &models.DiagnosticTimeline{}}
	renderer := &fakeRenderer{}
	svc := NewPostmortemService(db, fetcher)
	svc.SetRenderer(renderer)

	before := time.Now().UTC()
	data, err := svc.GenerateReport(context.Background(), io.Discard, uuid.New(), GenerateReportParams{Days: 7})
	after := time.Now().UTC()
	require.NoError(t, err)
	assert.Equal(t, time.UTC, data.GeneratedAt.Location(), "时间应为 UTC")
	assert.True(t, data.GeneratedAt.After(before.Add(-time.Second)) && data.GeneratedAt.Before(after.Add(time.Second)),
		"时间应在 before/after 之间")
}

// 防止无 _ = gorm.DB 之类 unused import 报错
var _ = (*gorm.DB)(nil)
