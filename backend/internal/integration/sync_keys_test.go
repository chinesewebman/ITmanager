package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyZabbixTruncated_Value 守住常量值不变。
//
// 若改了 KeyZabbixTruncated 字面量 (例如 → "zabbix_truncated_v2")，
// 这个测试会红。期望红测试: TestKeyZabbixTruncated_CrossLangWithTS + TestKeyZabbixTruncated_InOpenAPISync。
func TestKeyZabbixTruncated_Value(t *testing.T) {
	assert.Equal(t, "zabbix_truncated", KeyZabbixTruncated,
		"KeyZabbixTruncated 是跨语言契约值, 不应改字符串字面量")
}

// TestKeyZabbixTruncated_InOpenAPISync 守住 OpenAPI spec 描述含常量值。
//
// SyncResult.description (openapi.yaml:3771-3787) 是 data.synced 的「真源说明」——
// 它手写枚举了键名。改了 Go 常量值, 忘了同步 spec → 本测试红。
//
// 真源 = backend/internal/api/openapi.yaml (M78/G-15 已用此模式)。
func TestKeyZabbixTruncated_InOpenAPISync(t *testing.T) {
	// 测试运行时 cwd 是 backend/internal/integration/, 所以相对路径走 ../
	openAPIPath := filepath.Join("..", "api", "openapi.yaml")
	spec, err := os.ReadFile(openAPIPath)
	require.NoError(t, err, "读不到 %s — 测试运行环境异常 (期望 backend/ 为 cwd)", openAPIPath)

	specStr := string(spec)
	// SyncResult.description 是手写枚举, 抓包含 KeyZabbixTruncated 值的字符串
	assert.Contains(t, specStr, KeyZabbixTruncated,
		"OpenAPI SyncResult description 必须含常量值 %q (data.synced 的键清单真源)", KeyZabbixTruncated)

	// 额外防御: description 必须真的在 SyncResult 段附近 (而不是落在 audit.source 之类无关段)
	idx := strings.Index(specStr, "SyncResult:")
	require.GreaterOrEqual(t, idx, 0, "OpenAPI spec 找不到 SyncResult: schema 定义")

	// 抓 SyncResult: 之后下一个顶级 schema 定义前的一段
	after := specStr[idx:]
	// 取下一个 4-space-indent 顶级 key 的边界 — SyncResult 内的 properties 都缩进 8 空格
	endIdx := strings.Index(after[1:], "\n    [A-Z]")
	section := after
	if endIdx > 0 {
		section = after[:endIdx+1]
	}
	assert.Contains(t, section, KeyZabbixTruncated,
		"KeyZabbixTruncated %q 必须出现在 OpenAPI SyncResult 段 (不是其它 schema 的 description)", KeyZabbixTruncated)
}

// TestKeyZabbixTruncated_NoBareStringInCode 守住 production 代码无裸字符串字面量。
//
// 走读 service.go 和 integration_handler.go 的源码, 找 `"zabbix_truncated"` 字面量出现点。
// 期望: 0 处 (常量定义本身在 sync_keys.go, 不在本测试范围)。
//
// 变异反证: 若有人偷偷在 handler 写回裸字符串 `results = map[string]int{"zabbix_truncated": ...}` →
//
//	本测试红 → 守住「必须用 KeyZabbixTruncated 常量」。
func TestKeyZabbixTruncated_NoBareStringInCode(t *testing.T) {
	targets := []string{
		filepath.Join("service.go"),
		filepath.Join("..", "api", "handlers", "integration_handler.go"),
	}
	// 匹配 `["']zabbix_truncated["']` 字面量 (注释 / 字符串字面量 都命中)
	barePattern := regexp.MustCompile(`["']zabbix_truncated["']`)

	for _, p := range targets {
		src, err := os.ReadFile(p)
		require.NoError(t, err, "读不到 %s", p)

		matches := barePattern.FindAllIndex(src, -1)
		if len(matches) > 0 {
			// 把每个 match 行号 + 上下文打出来, 便于 CI 报错时定位
			var hints []string
			lines := strings.Split(string(src), "\n")
			for _, m := range matches {
				// 找该 byte offset 对应行号
				line := 1
				off := 0
				for i, l := range lines {
					off += len(l) + 1
					if off > m[0] {
						line = i + 1
						break
					}
				}
				hints = append(hints, string(src[m[0]:m[1]])+" @ "+p+":"+itoa(line))
			}
			t.Errorf("裸字符串字面量出现在生产代码 %s, 必须改用 KeyZabbixTruncated 常量: %v",
				p, hints)
		}
	}
}

// itoa 简版: 避免引入 strconv 在 test-only 路径
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	if neg {
		s = append([]byte{'-'}, s...)
	}
	return string(s)
}

// TestKeyZabbixTruncated_CrossLangWithTS 守住 Go 常量值与前端 TS 常量值一致。
//
// 走读 frontend/src/services/syncKeys.ts, 正则提取 SYNC_KEY_ZABBIX_TRUNCATED 字面量,
// 断言 == Go KeyZabbixTruncated。这是「跨语言漂移」的最强守卫 —
// 改一边忘另一边 → 双测试红。
//
// 工作目录: backend/, 跨仓走相对路径 ../../frontend/src/services/syncKeys.ts
func TestKeyZabbixTruncated_CrossLangWithTS(t *testing.T) {
	// 测试运行时 cwd = backend/internal/integration/, 跨仓走 ../../../frontend/...
	tsPath := filepath.Join("..", "..", "..", "frontend", "src", "services", "syncKeys.ts")
	tsSrc, err := os.ReadFile(tsPath)
	require.NoError(t, err, "读不到 %s — 测试运行环境异常", tsPath)
	// 匹配: SYNC_KEY_ZABBIX_TRUNCATED = "zabbix_truncated" 或 SYNC_KEY_ZABBIX_TRUNCATED = 'zabbix_truncated'
	// 对空白宽容 (等号前后空格 / tab)
	pattern := regexp.MustCompile(`SYNC_KEY_ZABBIX_TRUNCATED\s*=\s*['"]([^'"]+)['"]`)
	match := pattern.FindStringSubmatch(string(tsSrc))
	require.NotNil(t, match,
		"前端 syncKeys.ts 找不到 SYNC_KEY_ZABBIX_TRUNCATED 常量声明 — "+
			"契约测试期望声明形式为 `SYNC_KEY_ZABBIX_TRUNCATED = \"...\"`, "+
			"若改了声明风格, 本测试 regex 必须同步更新 (见 sync_keys_test.go 注释)")

	tsValue := match[1]
	assert.Equal(t, KeyZabbixTruncated, tsValue,
		"Go KeyZabbixTruncated (%q) 与 TS SYNC_KEY_ZABBIX_TRUNCATED (%q) 必须一致 — 跨语言契约",
		KeyZabbixTruncated, tsValue)
}
