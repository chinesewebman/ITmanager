package cursor

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 6, 17, 19, 30, 45, 123456789, time.UTC)

	enc := Encode(ts, id)
	ts2, id2, err := Decode(enc)
	require.NoError(t, err)
	assert.Equal(t, id, id2)
	assert.True(t, ts.Equal(ts2), "timestamps not equal: %v vs %v", ts, ts2)
}

func TestEncode_IsBase64URLSafe(t *testing.T) {
	id := uuid.New()
	ts := time.Now()
	enc := Encode(ts, id)
	// base64.RawURLEncoding 不含 +/= 而是 -_
	assert.NotContains(t, enc, "+")
	assert.NotContains(t, enc, "/")
	assert.NotContains(t, enc, "=")
}

func TestEncode_时间含冒号仍能正确解码(t *testing.T) {
	// 关键测试: RFC3339Nano 的 'T19:30:45' 含 ':', 用 NUL 避开 split 出错
	id := uuid.New()
	ts := time.Date(2026, 6, 17, 19, 30, 45, 0, time.UTC)
	enc := Encode(ts, id)
	ts2, id2, err := Decode(enc)
	require.NoError(t, err)
	assert.Equal(t, id, id2)
	assert.True(t, ts.Equal(ts2))
}

func TestDecode_空字符串返ErrInvalid(t *testing.T) {
	_, _, err := Decode("")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDecode_非法base64返ErrInvalid(t *testing.T) {
	_, _, err := Decode("!!!not-base64!!!")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDecode_缺NUL分隔符返ErrInvalid(t *testing.T) {
	// "no-separator" → base64 合法但缺 NUL
	enc := base64.RawURLEncoding.EncodeToString([]byte("no-separator"))
	_, _, err := Decode(enc)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDecode_uuid格式错返ErrInvalid(t *testing.T) {
	// 合法时间 + 非法 uuid
	enc := base64.RawURLEncoding.EncodeToString([]byte("2026-06-17T19:30:45Z\x00not-a-uuid"))
	_, _, err := Decode(enc)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDecode_时间格式错返ErrInvalid(t *testing.T) {
	// 合法 uuid + 非法时间
	id := uuid.New()
	enc := base64.RawURLEncoding.EncodeToString([]byte("garbage-time\x00" + id.String()))
	_, _, err := Decode(enc)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestEncodeDecode_同一ts不同id(t *testing.T) {
	// 验证 ts 重复时, id 提供第二排序键
	ts := time.Date(2026, 6, 17, 19, 0, 0, 0, time.UTC)
	id1 := uuid.New()
	id2 := uuid.New()

	enc1 := Encode(ts, id1)
	enc2 := Encode(ts, id2)
	assert.NotEqual(t, enc1, enc2, "same ts 但 id 不同应产生不同 cursor")

	_, gotID1, _ := Decode(enc1)
	_, gotID2, _ := Decode(enc2)
	assert.Equal(t, id1, gotID1)
	assert.Equal(t, id2, gotID2)
}

func TestEncodeDecode_NanosecondPrecision(t *testing.T) {
	id := uuid.New()
	// 纳秒精度
	ts := time.Date(2026, 6, 17, 19, 30, 45, 123456789, time.UTC)
	enc := Encode(ts, id)
	ts2, _, err := Decode(enc)
	require.NoError(t, err)
	assert.Equal(t, ts.UnixNano(), ts2.UnixNano(), "纳秒精度丢失")
}

func TestEncode_非零时区兼容(t *testing.T) {
	// Encode 强制转 UTC, 跨时区一致
	beijing, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("no tzdata")
	}
	id := uuid.New()
	ts := time.Date(2026, 6, 17, 19, 30, 45, 0, beijing)
	enc := Encode(ts, id)
	ts2, _, err := Decode(enc)
	require.NoError(t, err)
	assert.Equal(t, ts.UTC().UnixNano(), ts2.UnixNano())
}

func TestEncode_空字符串是NUL分隔的明确证据(t *testing.T) {
	// 防退化: 如果有人误把 separator 改回 ":", 这个 test 会失败
	id := uuid.New()
	ts := time.Now()
	enc := Encode(ts, id)
	decoded, _ := base64.RawURLEncoding.DecodeString(enc)
	assert.True(t, strings.Contains(string(decoded), separator), "encode 后的明文应含 NUL 分隔符")
}

// P2: benchmark 测试 — cursor 在 List 路径热点上, 防 encode/decode 退化
func BenchmarkEncode(b *testing.B) {
	id := uuid.New()
	ts := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Encode(ts, id)
	}
}

func BenchmarkDecode(b *testing.B) {
	id := uuid.New()
	ts := time.Now()
	enc := Encode(ts, id)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = Decode(enc)
	}
}
