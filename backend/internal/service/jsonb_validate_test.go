package service_test

import (
	"errors"
	"testing"

	"network-monitor-platform/internal/service"
)

// M43 / G-23: validateJSONBField 单测.
// 与 handlers.NormalizeJSONBFieldsForTest 同款口径, 在 service 层兜底,
// 防未来第二个 caller 绕过 handler 直接调 service.Update.

func TestValidateJSONBField_Nil(t *testing.T) {
	err := service.ValidateJSONBFieldForTest("tags", nil)
	if !errors.Is(err, service.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput for nil, got: %v", err)
	}
}

func TestValidateJSONBField_EmptyString(t *testing.T) {
	err := service.ValidateJSONBFieldForTest("tags", "")
	if !errors.Is(err, service.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput for empty string, got: %v", err)
	}
}

func TestValidateJSONBField_StringScalar(t *testing.T) {
	err := service.ValidateJSONBFieldForTest("tags", "x")
	if !errors.Is(err, service.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput for string scalar, got: %v", err)
	}
}

func TestValidateJSONBField_NonEmptyArray(t *testing.T) {
	err := service.ValidateJSONBFieldForTest("tags", []interface{}{"a", "b"})
	if !errors.Is(err, service.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput for non-empty array, got: %v", err)
	}
}

func TestValidateJSONBField_NumericScalar(t *testing.T) {
	err := service.ValidateJSONBFieldForTest("tags", 123)
	if !errors.Is(err, service.ErrInvalidJSONBInput) {
		t.Errorf("expected ErrInvalidJSONBInput for numeric, got: %v", err)
	}
}

func TestValidateJSONBField_EmptyArrayAccepted(t *testing.T) {
	if err := service.ValidateJSONBFieldForTest("tags", []interface{}{}); err != nil {
		t.Errorf("empty array should pass, got: %v", err)
	}
}

func TestValidateJSONBField_ObjectAccepted(t *testing.T) {
	if err := service.ValidateJSONBFieldForTest("tags", map[string]interface{}{"k": "v"}); err != nil {
		t.Errorf("object should pass, got: %v", err)
	}
}
