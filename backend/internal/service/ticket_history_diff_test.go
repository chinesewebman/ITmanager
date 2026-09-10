package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffTicketRows_无变化返回空(t *testing.T) {
	row := map[string]any{"id": "t1", "title": "打印机坏了", "status": "open"}
	got := diffTicketRows(row, map[string]any{"id": "t1", "title": "打印机坏了", "status": "open"})
	assert.Empty(t, got, "值没变就不该有历史行")
}

func TestDiffTicketRows_系统列变化被排除(t *testing.T) {
	// 真实场景：同值 PUT 也会让 gorm 无条件补 updated_at（callbacks/update.go:238-241）。
	// 若把 updated_at 记进来，每次请求都留一行噪声，「谁改了什么」会被淹掉。
	pre := map[string]any{
		"id": "t1", "ticket_number": "TK-1", "created_at": time.Now(), "updated_at": time.Now(),
		"title": "没动",
	}
	post := map[string]any{
		"id": "t1", "ticket_number": "TK-1", "created_at": time.Now(),
		"updated_at": time.Now().Add(time.Hour), // 只有它变了
		"title":      "没动",
	}
	assert.Empty(t, diffTicketRows(pre, post),
		"updated_at/created_at/id/ticket_number 是系统列，变化不进历史")
}

func TestDiffTicketRows_单列变化(t *testing.T) {
	got := diffTicketRows(
		map[string]any{"id": "t1", "title": "旧标题", "status": "open"},
		map[string]any{"id": "t1", "title": "新标题", "status": "open"},
	)
	require.Len(t, got, 1)
	assert.Equal(t, "title", got[0].Field)
	require.NotNil(t, got[0].Old)
	require.NotNil(t, got[0].New)
	assert.Equal(t, "旧标题", *got[0].Old)
	assert.Equal(t, "新标题", *got[0].New)
}

func TestDiffTicketRows_库里有但模型没有的列也留痕(t *testing.T) {
	// tickets 表有 12 个列不在 models.Ticket 里（alert_id/progress/reviewer_id/cc_users/
	// planned_start …），而 gorm 的裸列兜底（callbacks/update.go:230-231）能让
	// {"alert_id": X} 真的写库。只比 struct 的话这些改动**永远不留痕**。
	got := diffTicketRows(
		map[string]any{"id": "t1", "alert_id": "a-1", "progress": int64(10)},
		map[string]any{"id": "t1", "alert_id": "a-2", "progress": int64(80)},
	)
	require.Len(t, got, 2, "模型外列的变化必须留痕 —— 这是选原始行 map 而不是 struct 的全部理由")
	assert.Equal(t, "alert_id", got[0].Field)
	assert.Equal(t, "progress", got[1].Field)
	require.NotNil(t, got[1].Old)
	assert.Equal(t, "10", *got[1].Old)
}

func TestDiffTicketRows_值被清空(t *testing.T) {
	// 重开一张已解决的工单：resolved_at 由时间戳变 NULL（用户 2026-09-11 拍板的语义）。
	// New 必须是 nil 而不是空串 —— 读端点要能分辨「原来没有值」。
	solved := time.Date(2026, 9, 11, 10, 30, 0, 0, time.UTC)
	got := diffTicketRows(
		map[string]any{"id": "t1", "resolved_at": solved},
		map[string]any{"id": "t1", "resolved_at": nil},
	)
	require.Len(t, got, 1)
	assert.Equal(t, "resolved_at", got[0].Field)
	require.NotNil(t, got[0].Old, "被清掉的值必须留在历史里 —— 否则重开就等于丢掉「谁在何时解决的」")
	assert.Equal(t, "2026-09-11T10:30:00Z", *got[0].Old)
	assert.Nil(t, got[0].New, "清空后是 NULL，不是空串")
}

func TestDiffTicketRows_nil与空串不等同(t *testing.T) {
	got := diffTicketRows(
		map[string]any{"id": "t1", "resolution": nil},
		map[string]any{"id": "t1", "resolution": ""},
	)
	assert.Len(t, got, 1, "NULL 与空串是两种状态，不能判为相等")
}

func TestDiffTicketRows_输出按列名升序(t *testing.T) {
	// map 遍历无序；不排序的话同一次操作的 N 行写入顺序随机 → 用例断言精确序列会 flaky，
	// UI 上同批次字段顺序每次刷新都在变。
	pre := map[string]any{"id": "t1", "zeta": "1", "alpha": "1", "mid": "1"}
	post := map[string]any{"id": "t1", "zeta": "2", "alpha": "2", "mid": "2"}
	got := diffTicketRows(pre, post)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha", "mid", "zeta"},
		[]string{got[0].Field, got[1].Field, got[2].Field})
}

func TestDiffTicketRows_列顺序确定(t *testing.T) {
	// Go 的 map 遍历顺序是**随机化**的；不排序的话同一份输入每次调用会给出不同序列 ——
	// 「同一次操作的 N 行」写入顺序随机，UI 上字段顺序每次刷新都在变，用例断言精确序列
	// 也会 flaky。7 个键随机碰巧有序的概率是 1/5040，跑 30 次足以证明排序真的在起作用。
	pre := map[string]any{"id": "t1", "a": "1", "b": "1", "c": "1", "d": "1", "e": "1", "f": "1", "g": "1"}
	post := map[string]any{"id": "t1", "a": "2", "b": "2", "c": "2", "d": "2", "e": "2", "f": "2", "g": "2"}
	var first []string
	for i := 0; i < 30; i++ {
		var got []string
		for _, c := range diffTicketRows(pre, post) {
			got = append(got, c.Field)
		}
		require.Len(t, got, 7)
		if i == 0 {
			first = got
			assert.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g"}, got, "必须按列名升序")
			continue
		}
		assert.Equal(t, first, got, "同一份输入的输出顺序必须每次都一样")
	}
}

func TestDiffTicketRows_pre缺列时按无值处理(t *testing.T) {
	// 两行都来自同一张表的 SELECT *，列集本该相同；真出现差异时按「原来无值」处理。
	// 这条分支的取向是**宁可多记一行也不静默漏掉一次变更**（漏记会让经手链条断掉且无人察觉）。
	got := diffTicketRows(
		map[string]any{"id": "t1"},
		map[string]any{"id": "t1", "reviewer_id": nil},
	)
	require.Len(t, got, 1, "pre 里没有这一列不等于「没变化」")
	assert.Equal(t, "reviewer_id", got[0].Field)
	assert.Nil(t, got[0].Old)
	assert.Nil(t, got[0].New)
}

func TestDiffTicketRows_先比后截_只在截断区外不同才留痕(t *testing.T) {
	// 长文本只在超过上限的部分不同 —— 仍必须记为一次变更。
	// 若实现先截断再比较，两串都会被截成同样的 500 字 → 判为相等 → **静默漏记一次修改**。
	base := strings.Repeat("中", 500)
	got := diffTicketRows(
		map[string]any{"id": "t1", "description": base + "AAAA"},
		map[string]any{"id": "t1", "description": base + "BBBB"},
	)
	assert.Len(t, got, 1, "差异落在截断区之外也必须留痕 —— 判定用完整值，截断只作用于存储")
}

func TestHistoryValue_按字符截断不切坏中文(t *testing.T) {
	long := strings.Repeat("中", 600)
	got := historyValue(long)
	require.NotNil(t, got)
	assert.True(t, strings.HasSuffix(*got, historyValueTruncatedMark), "截断要有标记")
	// 断言里写**字面量 500**，不引用 ticketHistoryValueMaxRunes：引用常量的话，把阈值改成
	// 任何值断言都会跟着变，这条用例就永远绿着什么都不守（本轮变异 V-7 实测踩到）。
	// 阈值是契约的一部分（改它 = 换一次 PUT 在历史里留下的体积），必须被钉住。
	assert.Equal(t, 500+len([]rune(historyValueTruncatedMark)),
		len([]rune(*got)), "按 rune 截断到 500 字符 —— 按 byte 切会把中文切成非法 UTF-8")
	assert.True(t, strings.HasPrefix(*got, strings.Repeat("中", 10)))
}

func TestHistoryValue_nil保持nil(t *testing.T) {
	assert.Nil(t, historyValue(nil), "无值就是 NULL，不能退化成空串")
	empty := historyValue("")
	require.NotNil(t, empty)
	assert.Equal(t, "", *empty)
}

func TestValuesEqual_时间按时刻判定(t *testing.T) {
	// 同一时刻的不同表示必须判等：用 `==` 比会把 Location 与单调时钟读数一起比进去，
	// 于是每次 PUT 都凭空多出一行「时间戳变了」。
	now := time.Now()
	assert.True(t, valuesEqual(now, now.In(time.FixedZone("UTC+8", 8*3600))),
		"同一时刻的 UTC 与本地表示是同一个值")
	assert.True(t, valuesEqual(now, now.Round(0)),
		"Round(0) 去掉单调读数，时刻没变 —— == 会判为不同")
	assert.False(t, valuesEqual(now, now.Add(time.Second)))
}

func TestValuesEqual_字节切片与字符串(t *testing.T) {
	assert.True(t, valuesEqual([]byte("abc"), "abc"),
		"驱动给 []byte 还是 string 不该被当成值变了")
	assert.False(t, valuesEqual([]byte("abc"), "abd"))
}

func TestValuesEqual_nil语义(t *testing.T) {
	assert.True(t, valuesEqual(nil, nil))
	assert.False(t, valuesEqual(nil, ""), "NULL 与空串是两回事")
	assert.False(t, valuesEqual("", nil))
	assert.False(t, valuesEqual(nil, 0), "NULL 与数值 0 也不是一回事")
}
