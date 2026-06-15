package apikey

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ==================== Hash 测试 ====================

func TestHash_Deterministic_SameInputSameOutput(t *testing.T) {
	pepper := "test-pepper-32-bytes-for-hmac-sha256!!"
	h1 := Hash("my-api-key-12345", pepper)
	h2 := Hash("my-api-key-12345", pepper)
	assert.Equal(t, h1, h2, "相同 input+pepper 应返相同 hash")
}

func TestHash_DifferentPlaintext_DifferentHash(t *testing.T) {
	pepper := "test-pepper-32-bytes-for-hmac-sha256!!"
	h1 := Hash("key-1", pepper)
	h2 := Hash("key-2", pepper)
	assert.NotEqual(t, h1, h2, "不同 plaintext 应返不同 hash")
}

func TestHash_DifferentPepper_DifferentHash(t *testing.T) {
	plaintext := "my-api-key"
	h1 := Hash(plaintext, "pepper-A-32-bytes-for-hmac-sha256!!!!")
	h2 := Hash(plaintext, "pepper-B-32-bytes-for-hmac-sha256!!!!")
	assert.NotEqual(t, h1, h2, "不同 pepper 应返不同 hash（防 offline rainbow）")
}

func TestHash_OutputIsHex_64Chars(t *testing.T) {
	// SHA-256 → 32 bytes → 64 hex chars
	h := Hash("any", "any-pepper-with-at-least-32-bytes!!!")
	assert.Len(t, h, 64, "SHA-256 hex 长度应为 64")
	_, err := hex.DecodeString(h)
	assert.NoError(t, err, "hash 应是合法 hex 字符串")
}

func TestHash_EmptyPepper_Panics(t *testing.T) {
	// 启动时 config.Validate 拦空 pepper；Hash 是 runtime 兜底
	assert.Panics(t, func() {
		Hash("key", "")
	}, "空 pepper 必须 panic（防运行时静默用错）")
}

func TestHash_EmptyPlaintext_StillProducesValidHash(t *testing.T) {
	// 边界：空 plaintext 也应生成有效 hash（调用方应拦截，不是 Hash 责任）
	h := Hash("", "pepper-32-bytes-for-testing-aaaaaaaaa")
	assert.Len(t, h, 64)
	_, err := hex.DecodeString(h)
	assert.NoError(t, err)
}

// ==================== Verify 测试 ====================

func TestVerify_CorrectPlaintext_ReturnsTrue(t *testing.T) {
	pepper := "test-pepper-32-bytes-for-hmac-sha256!!"
	h := Hash("my-api-key", pepper)
	assert.True(t, Verify("my-api-key", pepper, h))
}

func TestVerify_WrongPlaintext_ReturnsFalse(t *testing.T) {
	pepper := "test-pepper-32-bytes-for-hmac-sha256!!"
	h := Hash("correct-key", pepper)
	assert.False(t, Verify("wrong-key", pepper, h))
}

func TestVerify_WrongPepper_ReturnsFalse(t *testing.T) {
	h := Hash("my-key", "pepper-A-32-bytes-for-hmac-sha256!!!!")
	assert.False(t, Verify("my-key", "pepper-B-32-bytes-for-hmac-sha256!!!!", h),
		"不同 pepper 验证必失败（防 pepper 泄漏时 hash 仍可信）")
}

func TestVerify_InvalidHexHash_ReturnsFalse(t *testing.T) {
	// 兜底：库里存了损坏的 hash（DB migration 异常）也不能 panic
	assert.False(t, Verify("any", "any-pepper", "not-valid-hex-!!!@#"))
}

func TestVerify_EmptyHash_ReturnsFalse(t *testing.T) {
	assert.False(t, Verify("any", "any-pepper-with-32-bytes-aaaaaaaaaaa", ""))
}

func TestVerify_HashMismatchDifferentLength_ReturnsFalse(t *testing.T) {
	// 长度不等的 hash（不是 SHA-256）应判 false，不 panic
	shortHash := "abc123"
	assert.False(t, Verify("any", "any-pepper-32-bytes-aaaaaaaaaaaaa", shortHash))
}

func TestVerify_CaseInsensitive_HexDecodeFails(t *testing.T) {
	// hex.DecodeString 默认大写或小写都行 — 这测试只是防回归
	// 实际上 hex.DecodeString 接受大小写，所以这里只断言行为稳定
	h := Hash("key", "pepper-32-bytes-for-testing-aaaaaaaaa")
	assert.True(t, Verify("key", "pepper-32-bytes-for-testing-aaaaaaaaa", h))
	// 反向：把 hash 转大写后再 verify 也应成功（hex 兼容）
	assert.True(t, Verify("key", "pepper-32-bytes-for-testing-aaaaaaaaa", strings.ToUpper(h)))
}

// ==================== 安全属性 ====================

func TestHash_OutputNotPredictableFromPlaintext(t *testing.T) {
	// 同一 plaintext 反复 hash 应稳定（确定性），但不同 plaintext 的 hash 应有足够熵
	// sanity：5 个不同 key 各自的 hash 都唯一
	pepper := "pepper-32-bytes-for-property-test-aaa"
	keys := []string{
		"key-1-aaaaa",
		"key-2-bbbbb",
		"key-3-ccccc",
		"key-4-ddddd",
		"key-5-eeeee",
	}
	seen := make(map[string]string)
	for _, k := range keys {
		h := Hash(k, pepper)
		if prev, exists := seen[h]; exists {
			t.Fatalf("hash 碰撞: %q 和 %q 都 hash 到 %s", prev, k, h)
		}
		seen[h] = k
	}
}

func TestVerify_ConstantTime_NoObservableSideEffect(t *testing.T) {
	// 我们不直接测时间（CI 抖动大），但验证：
	//   1) wrong key 走完整计算
	//   2) 不同长度 hash 不 panic
	pepper := "pepper-32-bytes-for-constant-time-test"
	h := Hash("correct", pepper)
	// wrong key
	assert.False(t, Verify("wrong", pepper, h))
	// 极长 hash（攻击者塞巨大数据）也不 panic
	longHash := strings.Repeat("a", 1000)
	assert.False(t, Verify("any", pepper, longHash))
}

// ==================== 旧 SHA-256 兼容（双读迁移场景） ====================

// TestVerify_CompatibleWithLegacyHexHash 模拟旧 SHA-256 库（无 HMAC）的 hash 格式
// 旧 hash 是裸 SHA-256 hex（64 chars），与新 HMAC 格式**不同**
// 验证函数会因 hex decode OK 但 HMAC 计算结果不同而返 false
// 这是**正确**行为：迁移期应强制用户重新签发
func TestVerify_CompatibleWithLegacyHexHash(t *testing.T) {
	// 旧 SHA-256("legacy-key") = 已知值（不在这里重现，假设 DB 里这种 hash 存在）
	// 我们的 Verify 看到非 HMAC 格式 hash 应该 fail（让双读代码层 fallback 处理）
	legacyHash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	pepper := "pepper-32-bytes-for-legacy-compat-test"
	assert.False(t, Verify("any-key", pepper, legacyHash),
		"Verify 必须用 HMAC 验证，旧的裸 SHA-256 hash 不应被误认（迁移期由调用方 fallback）")
}

// ==================== 内部一致性 ====================

func TestHashThenVerify_RoundTrip(t *testing.T) {
	pepper := "pepper-32-bytes-for-roundtrip-test"
	keys := []string{
		"short",
		"a-very-long-api-key-with-many-characters-and-special-!@#$%^&*()",
		"unicode-键-🔑-test",
		"key\nwith\nnewlines",
		"key with spaces",
	}
	for _, k := range keys {
		h := Hash(k, pepper)
		assert.True(t, Verify(k, pepper, h), "roundtrip 应成功: %q", k)
		assert.False(t, Verify(k+"x", pepper, h), "改一字符应失败: %q", k)
	}
}

// TestHashThenVerify_RoundTrip 后用
