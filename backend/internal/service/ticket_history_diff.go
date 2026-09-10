package service

import (
	"fmt"
	"sort"
	"time"
)

// 本文件是 M25 工单经手历史的**纯函数内核**：把 tickets 表两行原始列值比成「哪些列变了」。
// 设计见 docs/FIX-PLAN-TICKET-HISTORY.md §2.3。与 DB 无关，故可独立单测与变异。

// fieldChange 一行待写入的历史：某列从 Old 变成 New（nil = 该侧无值）。
type fieldChange struct {
	Field string
	Old   *string
	New   *string
}

// ticketHistorySystemFields 不进历史的系统列。
//
//   - `updated_at`：gorm 对 map 更新**无条件**补它（`callbacks/update.go:238-241`）——
//     同值 PUT 也会变，记它等于每次请求都留一行噪声，把「谁改了什么」淹掉。
//   - `id` / `created_at` / `ticket_number`：由系统维护、M17 已列入禁改集合。它们真变了
//     只可能是绕过服务的裸 SQL 写入，那不是「一次经手」，不该伪装成经手记录。
//
// 排除集是契约的一部分（docs §2.3），改这里要同步文档与用例。
var ticketHistorySystemFields = map[string]bool{
	"id":            true,
	"ticket_number": true,
	"created_at":    true,
	"updated_at":    true,
}

// ticketHistoryValueMaxRunes 单个值入历史的字符上限。
//
// 一次 PUT 可能带 2KB 描述，同一列改 N 次就存 N 份全文 —— 历史表的职责是「谁动了哪一列」，
// 不是保存正文的每个版本。截断只作用于**存储**，不影响下面的相等判定（先比后截）。
const ticketHistoryValueMaxRunes = 500

// historyValueTruncatedMark 截断标记。它同时是给读的人的信号：这段文本不完整。
const historyValueTruncatedMark = "…(截断)"

// diffTicketRows 比较两行原始列值，返回**按列名升序**的变更列表。
//
// 入参是 `SELECT *` 出来的列名→值映射，不是 struct —— struct 看不见「库里有、模型里没有」
// 的列（tickets 有 12 个这样的列），而 gorm 的裸列兜底能让 `{"alert_id": X}` 真的写库
// （`callbacks/update.go:230-231`）。用原始行才覆盖得了全部库列。
//
// 顺序：map 遍历本身无序，不排序会让「同一次操作的 N 行」写入顺序随机 —— 用例据此断言
// 精确序列就会 flaky，UI 上同批次字段顺序也会每次刷新都变。按列名升序是这里唯一稳定的
// 全序（T-45：没有 ORDER BY 的「顺序」没有定义）。
func diffTicketRows(pre, post map[string]any) []fieldChange {
	keys := make([]string, 0, len(post))
	for k := range post {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []fieldChange
	for _, k := range keys {
		if ticketHistorySystemFields[k] {
			continue
		}
		oldV, hadOld := pre[k]
		newV := post[k]
		// pre 里没有这一列（正常不该发生：两行都来自同一张表）时按「原来无值」处理，
		// 宁可多记一行也不静默漏掉一次变更。
		if hadOld && valuesEqual(oldV, newV) {
			continue
		}
		out = append(out, fieldChange{Field: k, Old: historyValue(oldV), New: historyValue(newV)})
	}
	return out
}

// valuesEqual 判定同一列的前后值是否相等。
//
// 三条判据各有理由：
//   - `time.Time` 用 `.Equal()` 而不是 `==`：`==` 连单调时钟读数与 Location 一起比，
//     同一时刻的 UTC 值与本地值会被判为不同 → 每次 PUT 都凭空多出一行时间戳变更。
//   - `[]byte` 与 `string` 归一后再比：驱动对不同列类型给的类型不一样，但**同一列**
//     前后两次读必然同型；归一只是为了不让一个 `[]byte` 在别处被当成「变了」。
//   - nil 与空串**不**等同：`resolved_at` 被清空（nil）和「被改成空串」是两件事，
//     后者在 PG 的 timestamp 列上根本存不进去、只可能是非法写入，不能混为一谈。
func valuesEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if at, ok := a.(time.Time); ok {
		bt, ok := b.(time.Time)
		return ok && at.Equal(bt)
	}
	return historyValueText(a) == historyValueText(b)
}

// historyValueText 把列值转成入库文本（不截断），供相等判定与格式化共用。
// 一个值只要「文本相同」就视为同一值 —— 这正是本表存文本快照的语义。
func historyValueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case time.Time:
		// 统一 UTC 且带纳秒：不这么做，同一时刻在不同连接时区下会格式化成不同文本。
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(x)
	}
}

// historyValue 把列值转成入库快照，超长按**字符**截断。
//
// 按 rune 不按 byte：中文被切在两个字节之间会留下非法 UTF-8（同 truncateRunes 的理由）。
// nil 保持 nil —— 与「空串」在库里是两回事，读端点要能分辨「这列原来没有值」。
func historyValue(v any) *string {
	if v == nil {
		return nil
	}
	s := historyValueText(v)
	if r := []rune(s); len(r) > ticketHistoryValueMaxRunes {
		s = string(r[:ticketHistoryValueMaxRunes]) + historyValueTruncatedMark
	}
	return &s
}
