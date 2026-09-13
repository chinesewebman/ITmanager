package service

import (
	"errors"
	"fmt"
)

// ErrInvalidJSONBInput jsonb 入参非法 (G-23 / docs/FIX-PLAN-ASSET-JSONB.md §2.3 R-1 推广).
// 与 handlers.ErrInvalidJSONBInput 同款语义, 但放在 service 包是因为 service 层兜底.
// caller 应当用 errors.Is 判断后转 400.
var ErrInvalidJSONBInput = errors.New("jsonb 输入非法")

// validateJSONBField 校验单个 jsonb 列的入参值.
// 与 handlers/jsonb_normalize.go checkJSONBValue 同款口径, 在 service 层复刻一份
// 以避免 service 依赖 handlers 包 (handler -> service 单向).
//
// 拒 (返 ErrInvalidJSONBInput):
//   - nil (interface{} nil 或显式 null)
//   - "" (空字符串)
//   - string 标量 (任何非空字符串)
//   - 数值/布尔标量
//   - 非空数组 (空数组 [] OK)
//
// 通过:
//   - 空数组 []
//   - 任意 map[string]interface{}
//   - 非 jsonb 列 (本函数不适用, 由 caller 决定)
//
// 不修改入参, 只校验.
func validateJSONBField(col string, v interface{}) error {
	if v == nil {
		return fmt.Errorf("%w: %s 不能为 null", ErrInvalidJSONBInput, col)
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return fmt.Errorf("%w: %s 不能为空字符串", ErrInvalidJSONBInput, col)
		}
		return fmt.Errorf("%w: %s 不能为字符串 %q, 应为 JSON 对象或数组", ErrInvalidJSONBInput, col, x)
	case []interface{}:
		if len(x) > 0 {
			return fmt.Errorf("%w: %s 非空数组会让 PG jsonb 渲染失败 (空数组 [] 可以传)", ErrInvalidJSONBInput, col)
		}
		return nil
	case map[string]interface{}:
		return nil
	case bool, int, int64, float64:
		return fmt.Errorf("%w: %s 不能为标量值, 应为 JSON 对象或数组", ErrInvalidJSONBInput, col)
	}
	return fmt.Errorf("%w: %s 类型 %T 不被支持", ErrInvalidJSONBInput, col, v)
}

// ValidateJSONBFieldForTest 跨包测试用: 导出版 validateJSONBField.
func ValidateJSONBFieldForTest(col string, v interface{}) error {
	return validateJSONBField(col, v)
}
