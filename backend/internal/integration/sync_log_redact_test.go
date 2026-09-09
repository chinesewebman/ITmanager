package integration

import (
	"bytes"
	"context"
	"log"
	"os"
	"testing"

	"network-monitor-platform/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncAll_失败日志不泄漏URL凭据 — 安全审计 H-1 的守门用例（TODO G-28）。
//
// 三个未脱敏的写入点都在 SyncAll 里：`log.Printf("NetBox 同步失败: %v", err)` 等
// （service.go:282/289/296；metric_sync.go:111 同型）。集成 URL 的 query/path 里就有凭据
// （`?access_token=`、飞书/Slack 的 hook token），而 *url.Error 会把完整 URL 塞进 Error()。
//
// 修法是在 httpx 出口收口（redactedErr），这个用例从**日志出口**再钉一遍：将来若出现
// 绕开 httpx 的 URL 错误，这里必须红。
func TestSyncAll_失败日志不泄漏URL凭据(t *testing.T) {
	newUpsertTestDB(t)

	const secret = "SUPERSECRET"
	// 127.0.0.1:1 必然 refuse：三个集成都走失败分支 → 三行日志。
	base := "http://127.0.0.1:1/api?access_token=" + secret

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	svc := &IntegrationService{
		netbox: NewNetBoxClient(&config.NetboxConfig{URL: base, Token: "t"}, nil),
		zabbix: NewZabbixClient(&config.ZabbixConfig{URL: base, User: "u", Password: "p"}, nil),
		glpi:   NewGLPIClient(&config.GLPIConfig{URL: base, AppToken: "a", UserToken: "ut"}, nil),
	}
	_, err := svc.SyncAll(context.Background())
	require.Error(t, err, "三个集成都连不上，必须返回合并错误")

	out := buf.String()
	for _, want := range []string{"NetBox 同步失败", "Zabbix 同步失败", "GLPI 同步失败"} {
		assert.Contains(t, out, want, "失败必须落日志，不能被静默吞掉")
	}
	assert.NotContains(t, out, secret, "URL query 里的 token 不得出现在日志里")
	assert.Contains(t, out, "http://127.0.0.1:1", "host 保留，便于定位")
}
