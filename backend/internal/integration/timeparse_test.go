package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件守 M26 的核心不变式：导入路径**不发明**时间。
// 三态（absent / invalid / ok）必须可区分 —— 合并任意两态都会伪造时间线事件。
// 见 docs/FIX-PLAN-SYNC-FIDELITY.md §2.4、docs/IMPL-SYNC-FIDELITY.md §4。

// TestParseUnixSeconds 守 Zabbix lastchange 的三态。
func TestParseUnixSeconds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want timeParseStatus
	}{
		{"正常秒级时间戳", "1750000000", timeOK},
		{"空串即源没给", "", timeAbsent},
		{"非数字", "abc", timeInvalid},
		{"带小数（Zabbix 不发，但要看得见）", "1750000000.5", timeInvalid},
		{"负数仍是合法 Unix 秒", "-1", timeOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := parseUnixSeconds(c.raw)
			require.Equal(t, c.want, st, "raw=%q", c.raw)
			if c.want == timeOK {
				require.False(t, got.IsZero(), "OK 必须带回真实时刻")
			} else {
				assert.True(t, got.IsZero(), "非 OK 不得带回半个时刻")
			}
		})
	}

	t.Run("值本身正确", func(t *testing.T) {
		got, st := parseUnixSeconds("1750000000")
		require.Equal(t, timeOK, st)
		assert.True(t, got.Equal(time.Unix(1750000000, 0).UTC()),
			"应等于 Unix 秒换算结果，实际 %v", got)
	})
}

// TestParseGLPITime_格式 守三种 layout + 哨兵值归类。
func TestParseGLPITime_格式(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want timeParseStatus
	}{
		{"完整秒", "2026-06-15 10:00:00", timeOK},
		{"到分", "2026-06-15 10:00", timeOK},
		{"仅日期", "2026-06-15", timeOK},
		{"MySQL 零日期（带时间）", "0000-00-00 00:00:00", timeAbsent},
		{"MySQL 零日期（仅日期）", "0000-00-00", timeAbsent},
		{"空串", "", timeAbsent},
		{"无法识别", "not-a-date", timeInvalid},
		{"斜杠分隔（非 GLPI 形态）", "2026/06/15 10:00", timeInvalid},
		// 需求 §1.9 登记项：`"0001-01-01 00:00:00"` 这个输入。
		// 覆盖它是为了把**它到底会发生什么**钉死，而不是钉一个我们希望的结论：
		//   · 归 timeOK —— 它不是 GLPI/MySQL 的「未设置」约定（那是 0000-00-00），
		//     把它划进 absent 是替 GLPI 发明语义；
		//   · 但需求 §1.9 的原判断（「IsZero()==true → gorm autoCreateTime 会静默
		//     替换成 NowFunc」）在本实现下**不成立**：parseGLPITime 末尾的 `.UTC()`
		//     把 0001-01-01 00:00+08:00 变成 **0000-12-31 16:00 UTC**，已经不是零值了。
		//     所以真值不会被 gorm 换掉，而是**原样落库成一个公元 0 年的时间戳**。
		// 两条路都不是好事 → 见下面 subshell 里的显式断言 + §7 未关闭项登记。
		{"零值时间（能解析，但 .UTC() 后不再是零值）", "0001-01-01 00:00:00", timeOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := parseGLPITime(c.raw)
			require.Equal(t, c.want, st, "raw=%q", c.raw)
			if c.raw == "0001-01-01 00:00:00" {
				// 钉住真实行为：时区偏移把「零值」推走，所以**不是** IsZero。
				// 这条断言的价值是**防止有人写出**「这个输入会走 gorm autoCreateTime 回落」
				// 或「落库值与解析值一致」这类恒假的结论（需求 §1.9 就写反了方向）。
				// 它守的是 **glpiLoc 被换成 UTC**（实测：那时这个串正好落回零值 →
				// gorm 的 autoCreateTime 就会把它悄悄替换成 now，问题从「脏值」变成「假值」）。
				// 它**不守** `.UTC()` —— 实测有无 `.UTC()` 都是 false（LMT 把瞬间推离零值）。
				assert.False(t, got.IsZero(),
					"glpiLoc 必须是 +08 档的时区：若这里变 true，说明时区被换成了 UTC，"+
						"该输入会正好落回零值并被 gorm autoCreateTime 静默替换")
				// 只钉「落在公元 0 年」而**不钉具体时分**：tzdata 对 1901 年前的上海
				// 用的是 LMT(+08:05:43)，不是 +08:00 —— 实测落库 0000-12-31 15:54:17。
				// 那个字面值随 tzdata 版本可能变，钉死它就是给未来埋一次无故变红；
				// 「Year()==0」才是不变量，也正好是「这是个必须被看见的脏值」的判据。
				assert.Equal(t, 0, got.UTC().Year(),
					"真实落库值是个公元 0 年的时间戳（既不是零值、也不是 now）—— "+
						"GLPI 若真发过这个串，closed_at 就会长成这样，必须能被发现")
			}
		})
	}

	// 哨兵必须落在 absent 而非 invalid —— 这是「源说没有」与「值坏了」的分界。
	// 若哪天有人把 glpiZeroDates 删掉，Go 的三种 layout 会全拒零日期 → 落到 invalid，
	// 下面这条断言就是唯一会红的地方。
	t.Run("零日期不得被当成解析失败", func(t *testing.T) {
		_, st := parseGLPITime("0000-00-00 00:00:00")
		assert.NotEqual(t, timeInvalid, st,
			"零日期语义是「没有」，不是「坏了」；归到 invalid 会让调用方走进回落分支")
	})
}

// TestParseGLPITime_时区 守 R2 的唯一防线：GLPI 挂钟必须按 GLPI 实例时区解释再转 UTC。
//
// 去掉 .UTC()、或把 glpiLoc 换成 UTC，这条必红。
func TestParseGLPITime_时区(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		want     time.Time // 落库挂钟（UTC 数字）
		wantWall string    // 回转 GLPI 时区后应逐字还原的挂钟
	}{
		{
			// 10:00 Asia/Shanghai == 02:00 UTC
			"夏令半年（中国无夏令时，必须与冬令一致）",
			"2026-06-15 10:00",
			time.Date(2026, 6, 15, 2, 0, 0, 0, time.UTC),
			"2026-06-15 10:00:00",
		},
		{
			// 若时区误配到有夏令时的地区、或误用服务器本地时区，冬夏两季会差 1 小时
			// —— 这条与上一条成对，专抓那类错误。
			"冬令半年",
			"2026-01-15 10:00",
			time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC),
			"2026-01-15 10:00:00",
		},
		{
			"仅日期按 GLPI 当日 00:00 起算",
			"2026-06-15",
			time.Date(2026, 6, 14, 16, 0, 0, 0, time.UTC),
			"2026-06-15 00:00:00",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := parseGLPITime(c.raw)
			require.Equal(t, timeOK, st, "raw=%q", c.raw)

			const layout = "2006-01-02 15:04:05"

			// ① **返回值的 Location 必须是 UTC** —— 这是 `.UTC()` 在纯函数层唯一的可观测效果，
			//    也是整条时区链路上唯一能钉住它的断言。
			//
			//    为什么不能靠挂钟数字：`.UTC()` 不改绝对时刻、只改 Location。
			//    真 PG 实测（同一个时刻的两种表示，`Equal()` 返回 true）：
			//      Location=Asia/Shanghai，挂钟 10:00 → 落库 "10:00:00" → 渲染 18:00 ❌
			//      Location=UTC，          挂钟 02:00 → 落库 "02:00:00" → 渲染 10:00 ✅
			//    pgx 写的是「time.Time 在**其自身 Location** 下的挂钟数字」（discardTimeZone），
			//    所以少了 `.UTC()` 就往 TIMESTAMP 列里写 10:00 —— 而那些断言若自己再 .UTC()
			//    一次，会把差别抹平而恒绿（本用例初版就踩了这个坑）。
			assert.Equal(t, time.UTC, got.Location(),
				"raw=%q 必须归一为 UTC：pgx 丢弃 Location、只写挂钟数字，"+
					"带着 glpiLoc 返回会让 TIMESTAMP 落库 10:00（错 8 小时）", c.raw)

			// ② 落库的挂钟数字 == 预期 UTC 数字。
			assert.Equal(t, c.want.Format(layout), got.UTC().Format(layout),
				"raw=%q 落库挂钟应为 %s，实际 %s",
				c.raw, c.want.Format(layout), got.UTC().Format(layout))

			// ③ 往返性质：把落库值按 GLPI 时区读回，必须逐字还原源挂钟。
			//    这正是运维在浏览器里看到的那个时间（dayjs 按 Asia/Shanghai 渲染）。
			//    时区名配错（如误用服务器本地时区）这条必红。
			assert.Equal(t, c.wantWall, got.In(glpiLoc).Format(layout),
				"raw=%q 回转 GLPI 时区后应还原源挂钟，实际 %s",
				c.raw, got.In(glpiLoc).Format(layout))
		})
	}
}

// TestGLPILocIsUTC8 钉住 D-3 的常量本身：一旦有人改 glpiTZName、或系统 tzdata 缺该时区
// 导致 LoadLocation 走到别的分支，这条会直接红在常量上，而不是红在某个下游断言里。
func TestGLPILocIsUTC8(t *testing.T) {
	require.NotNil(t, glpiLoc, "glpiLoc 必须加载成功（失败时 mustLoadLocation 会 panic，不会走到这里）")
	_, off := time.Date(2026, 6, 15, 10, 0, 0, 0, glpiLoc).Zone()
	assert.Equal(t, 8*3600, off,
		"glpiTZName=%q 的偏移应为 +08:00；不是则 D-3 的取值需重新核对", glpiTZName)
}

// TestParseGLPITime_边界 登记已知的格式边界与它们的处置。
func TestParseGLPITime_边界(t *testing.T) {
	t.Run("尾随小数秒被静默截断", func(t *testing.T) {
		got, st := parseGLPITime("2026-06-15 10:00:00.123")
		require.Equal(t, timeOK, st, "Go 的 15:04:05 layout 接受并截断尾随小数秒")
		assert.Equal(t, "02:00", got.UTC().Format("15:04"), "截断不应影响时区换算")
	})

	// GLPI 高层 API v2 的 ISO 形态。当前目标实例是 REST v1，故归为 invalid（可见的失败）。
	// 若目标 GLPI 升到 v2，这里会先红 —— 正是想要的告警点，登记见需求 §6。
	t.Run("v2 的 ISO 形态当前不识别", func(t *testing.T) {
		_, st := parseGLPITime("2026-06-15T10:00:00Z")
		assert.Equal(t, timeInvalid, st,
			"未登记的格式必须走 invalid（可见），不得静默当成 absent")
	})
}
