package notification

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 跨语言配置契约的第三根钉子。
//
// 契约样本 frontend/src/pages/__fixtures__/channelConfigSamples.json 有三条腿：
//  1. 前端单测 deep-equal 表单值 ↔ 样本键名（Settings.test.tsx）；
//  2. service 单测「样本能构造出 Sender」（channel_service_test.go）；
//  3. 本文件：样本键集合 ↔ channelConfig 的 json tag。
//
// 缺第 3 条时，表单与样本「一起改名」（尤其可选键 sign_secret/secret/smtp_password）
// 前两条都绿、契约已破（正确性审计 M-2）。
const samplesPath = "../../../frontend/src/pages/__fixtures__/channelConfigSamples.json"

// allowedConfigKeys 每类型在样本里必须出现的键。清单本身也要被校验（见下），
// 否则 sender.go 改 tag 后这份清单会跟着漂移成"自说自话"。
var allowedConfigKeys = map[string][]string{
	"email":    {"smtp_host", "smtp_port", "smtp_user", "smtp_password", "from", "to"},
	"dingtalk": {"webhook_url", "sign_secret"},
	"webhook":  {"url", "secret"},
}

func TestChannelConfig_样本键集合与结构体tag一致(t *testing.T) {
	raw, err := os.ReadFile(samplesPath)
	require.NoError(t, err, "跨语言样本必须可读（与 channel_service_test.go 同一份）")

	var samples map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &samples))

	tags := jsonTagsOfChannelConfig()
	for chType, keys := range allowedConfigKeys {
		// 清单里的每个键都必须是 channelConfig 的真实 json tag
		for _, k := range keys {
			assert.True(t, tags[k], "%s: 期望键 %q 不是 channelConfig 的 json tag（清单已漂移）", chType, k)
		}
		sample, ok := samples[chType]
		require.True(t, ok, "样本缺类型 %s", chType)

		got := make([]string, 0, len(sample))
		for k := range sample {
			got = append(got, k)
		}
		sort.Strings(got)
		want := append([]string(nil), keys...)
		sort.Strings(want)
		assert.Equal(t, want, got, "%s 样本键集合与 channelConfig 契约不符", chType)
	}

	types := make([]string, 0, len(samples))
	for k := range samples {
		types = append(types, k)
	}
	sort.Strings(types)
	assert.Equal(t, []string{"dingtalk", "email", "webhook"}, types, "样本类型集合变了（前端下拉/后端工厂要同步）")
}

// jsonTagsOfChannelConfig 返回 channelConfig 全部 json tag（去掉 omitempty）。
func jsonTagsOfChannelConfig() map[string]bool {
	out := map[string]bool{}
	typ := reflect.TypeOf(channelConfig{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			out[tag] = true
		}
	}
	return out
}
