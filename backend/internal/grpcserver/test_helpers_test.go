package grpcserver

import (
	"testing"
	"time"

	"network-monitor-platform/internal/cursor"

	"github.com/google/uuid"
)

// newUUID 辅助测试: 生成新 uuid
func newUUID(t *testing.T) uuid.UUID {
	t.Helper()
	return uuid.New()
}

// uuidZero 零值 UUID 用于断言
func uuidZero() uuid.UUID { return uuid.UUID{} }

// encodeForTest 辅助测试: 编码 cursor
func encodeForTest(ts time.Time, id uuid.UUID) string {
	return cursor.Encode(ts, id)
}
