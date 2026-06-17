package service

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"
)

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"gorm.ErrDuplicatedKey", gorm.ErrDuplicatedKey, true},
		{"PG 23505 SQLSTATE", errors.New(`ERROR: duplicate key value violates unique constraint "x" (SQLSTATE 23505)`), true},
		{"SQLite UNIQUE constraint", errors.New(`UNIQUE constraint failed: assets.name`), true},
		{"gorm ErrRecordNotFound", gorm.ErrRecordNotFound, false},
		{"random error", errors.New("connection refused"), false},
		{"wrapped ErrDuplicatedKey", fmt.Errorf("create: %w", gorm.ErrDuplicatedKey), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueViolation(tt.err); got != tt.want {
				t.Errorf("isUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
