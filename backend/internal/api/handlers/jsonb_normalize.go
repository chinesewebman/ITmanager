package handlers

import (
	"errors"
	"fmt"
)

// ErrInvalidJSONBInput jsonb 入参非法（G-21 / docs/FIX-PLAN-ASSET-JSONB.md §2.3 R-1）.
// caller 应当用 errors.Is 判断后转 400.
var ErrInvalidJSONBInput = errors.New("jsonb 输入非法")

// NormalizeJSONBFieldsForTest 跨包测试用：导出版 normalizeJSONBFields.
// 生产代码不应直接调用 — 应该用未导出的 normalizeJSONBFields (同 package).
// 这里导出仅供 handlers_test 包的单测使用.
func NormalizeJSONBFieldsForTest(updates map[string]interface{}) error {
	return normalizeJSONBFields(updates)
}

// jsonbFieldCols 列出需要规范化检查的 jsonb 列。
// 集中定义避免散在多个 handler 各写一遍.
var jsonbFieldCols = []string{"tags", "custom_fields"}

// normalizeJSONBFields 校验 PATCH 入参中 jsonb 列的值.
// 拒 (返 ErrInvalidJSONBInput):
//   - null (map 里值是 nil, 或 key 显式 null)
//   - "" (空字符串)
//   - JSON 字符串值 "x" / 123 / true (非对象非数组的标量)
//   - 非空数组 (空数组 [] OK; 非空会让 gorm 渲染成 PG array-of-text 22P02)
//
// 通过:
//   - 空数组 [] / 空对象 {}
//   - 数组 [{}, {}] / 对象 {"k":"v"} 等合法 jsonb value
//   - key 不在 jsonbFieldCols 里的非 jsonb 列 (原样保留)
//
// 不修改 map, 只校验. 通过校验后 caller 原样传给 service.Update.
func normalizeJSONBFields(updates map[string]interface{}) error {
	for _, col := range jsonbFieldCols {
		v, ok := updates[col]
		if !ok {
			continue // caller 没传这列
		}
		if err := checkJSONBValue(col, v); err != nil {
			return err
		}
	}
	return nil
}

func checkJSONBValue(col string, v interface{}) error {
	if v == nil {
		return fmt.Errorf("%w: %s 不能为 null (G-21)", ErrInvalidJSONBInput, col)
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return fmt.Errorf("%w: %s 不能为空字符串 (G-21)", ErrInvalidJSONBInput, col)
		}
		// 字符串值: 比如 "x" / "[1,2]" — PG jsonb 接受但语义不明.
		// 拒掉, 让客户端显式传数组/对象.
		return fmt.Errorf("%w: %s 不能为字符串 %q, 应为 JSON 对象或数组 (G-21)", ErrInvalidJSONBInput, col, x)
	case []interface{}:
		// 非空 string array 会让 gorm 渲染成 PG 数组 `('{x}')` → 22P02.
		// 空数组 [] OK. 非空但元素不是 object 的, 一律拒.
		if len(x) > 0 {
			return fmt.Errorf("%w: %s 非空数组会让 PG jsonb 渲染失败 (G-21, 空数组 [] 可以传)", ErrInvalidJSONBInput, col)
		}
		return nil
	case map[string]interface{}:
		return nil // 任何对象通过
	case bool, int, int64, float64:
		return fmt.Errorf("%w: %s 不能为标量值, 应为 JSON 对象或数组 (G-21)", ErrInvalidJSONBInput, col)
	}
	// 兜底: 其它未知类型也拒 (如 []string, models.JSON 等), 避免下游 gorm 行为不确定.
	return fmt.Errorf("%w: %s 类型 %T 不被支持 (G-21)", ErrInvalidJSONBInput, col, v)
}
