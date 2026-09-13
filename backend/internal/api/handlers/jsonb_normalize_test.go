package handlers_test

import (
	"errors"
	"testing"

	"network-monitor-platform/internal/api/handlers"
)

// M42 / G-21: normalizeJSONBFields 单测.
// 矩阵覆盖 docs/FIX-PLAN-ASSET-JSONB.md §2.3 R-1 列出的"Updates(map) 一族"所有非法入参.

func TestNormalizeJSONBFields_NilJSONBValue(t *testing.T) {
	// tags=null (Go interface{} nil) 应被拒
	updates := map[string]interface{}{"tags": nil}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if err == nil {
		t.Fatal("expected error for tags=null")
	}
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}

func TestNormalizeJSONBFields_EmptyStringJSONBValue(t *testing.T) {
	// custom_fields="" 应被拒
	updates := map[string]interface{}{"custom_fields": ""}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if err == nil {
		t.Fatal("expected error for custom_fields=''")
	}
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}

func TestNormalizeJSONBFields_StringScalarRejected(t *testing.T) {
	// tags="x" 字符串标量 应被拒 (PG jsonb 接受但语义不明; G-21 拒)
	updates := map[string]interface{}{"tags": "x"}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if err == nil {
		t.Fatal("expected error for tags=\"x\"")
	}
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}

func TestNormalizeJSONBFields_NonEmptyArrayRejected(t *testing.T) {
	// tags=["a","b"] 非空 string array 会被 gorm 渲染成 PG 数组 → 22P02. 应被拒.
	updates := map[string]interface{}{"tags": []interface{}{"a", "b"}}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if err == nil {
		t.Fatal("expected error for tags=non-empty array")
	}
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}

func TestNormalizeJSONBFields_ScalarNumericRejected(t *testing.T) {
	// custom_fields=123 标量应被拒
	updates := map[string]interface{}{"custom_fields": 123}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if err == nil {
		t.Fatal("expected error for custom_fields=123")
	}
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}

func TestNormalizeJSONBFields_EmptyArrayAccepted(t *testing.T) {
	// tags=[] 空数组 OK
	updates := map[string]interface{}{"tags": []interface{}{}}
	if err := handlers.NormalizeJSONBFieldsForTest(updates); err != nil {
		t.Errorf("empty array should pass, got: %v", err)
	}
}

func TestNormalizeJSONBFields_EmptyObjectAccepted(t *testing.T) {
	// custom_fields={} 空对象 OK
	updates := map[string]interface{}{"custom_fields": map[string]interface{}{}}
	if err := handlers.NormalizeJSONBFieldsForTest(updates); err != nil {
		t.Errorf("empty object should pass, got: %v", err)
	}
}

func TestNormalizeJSONBFields_NonEmptyObjectAccepted(t *testing.T) {
	// custom_fields={"k":"v"} 非空对象 OK
	updates := map[string]interface{}{"custom_fields": map[string]interface{}{"k": "v"}}
	if err := handlers.NormalizeJSONBFieldsForTest(updates); err != nil {
		t.Errorf("non-empty object should pass, got: %v", err)
	}
}

func TestNormalizeJSONBFields_NonJSONBColIgnored(t *testing.T) {
	// 非 jsonb 列 (status/name) 即使是 string 也不该被 normalizeJSONBFields 拦
	updates := map[string]interface{}{
		"status": "active",
		"name":   "my-asset",
	}
	if err := handlers.NormalizeJSONBFieldsForTest(updates); err != nil {
		t.Errorf("non-jsonb cols should pass through, got: %v", err)
	}
}

func TestNormalizeJSONBFields_MixedPassAndFail(t *testing.T) {
	// 同时传 name + tags=null — tags 错应被拒 (fail-fast)
	updates := map[string]interface{}{
		"name": "my-asset",
		"tags": nil,
	}
	err := handlers.NormalizeJSONBFieldsForTest(updates)
	if !errors.Is(err, handlers.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput, got %v", err)
	}
}
