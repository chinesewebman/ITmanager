package apierr_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"network-monitor-platform/internal/apierr"

	"github.com/gin-gonic/gin"
)

// M45 / G-31 收口: 4xx 路径 message 应已脱敏 + 去控制字符.
// 中文静态文案 no-op 不变.

// helper: 跑 BadRequest + 解析 response body
func runBadRequest(t *testing.T, msg string) (string, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	apierr.BadRequest(c, msg)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body apierr.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v (raw=%q)", err, w.Body.String())
	}
	return body.Message, w.Code
}

func TestBadRequest_StripControl(t *testing.T) {
	// 含 \n / \r / NUL 的 message 应被 StripControl 去控制字符
	// 注: StripControl 保留 \t = 0x09, 去 < 0x20 其他 + 0x7f.
	msg, status := runBadRequest(t, "abc\ndef\rghi\x00jkl")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d", status)
	}
	// 验证控制字符都被去掉 (留普通字母 + tab, tab 不算控制字符在 redact.StripControl 的实现里)
	if strings.ContainsAny(msg, "\n\r\x00") {
		t.Errorf("message 仍含控制字符: %q", msg)
	}
	// 验证普通字母保留
	if !strings.Contains(msg, "abc") || !strings.Contains(msg, "def") {
		t.Errorf("message 字母丢失: %q", msg)
	}
}

func TestBadRequest_RedactToken(t *testing.T) {
	// 含 token= 的 message 应被 redact 遮盖
	msg, _ := runBadRequest(t, "请求参数错误: ?token=abc123secret")
	// redact.Text 应把 token= 整段替换为 ***
	if strings.Contains(msg, "abc123secret") {
		t.Errorf("token 明文仍可见: %q", msg)
	}
	if !strings.Contains(msg, "***") {
		t.Errorf("redact 应输出 *** 标记: %q", msg)
	}
}

func TestBadRequest_ChineseNoop(t *testing.T) {
	// 中文静态文案不应被破坏
	msg, _ := runBadRequest(t, "工单标题不能为空")
	if msg != "工单标题不能为空" {
		t.Errorf("中文文案被改: %q (want 原文)", msg)
	}
}

func TestNotFound_StripControl(t *testing.T) {
	// NotFound 也走 sanitizeMessage
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	apierr.NotFound(c, "resource\nnot\nfound")
	var body apierr.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.ContainsAny(body.Message, "\n\r") {
		t.Errorf("NotFound message 仍含控制字符: %q", body.Message)
	}
}
