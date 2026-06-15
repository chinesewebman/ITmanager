// Package apikey 提供 API Key 哈希与验证
// C-F6: 解决 codex 审计识别的两个问题：
//  1. 旧实现用裸 SHA-256（无 server-side pepper，泄漏后可被离线彩虹表攻击）
//  2. 验证用普通 == 比较（时序侧信道泄露）
//
// 新设计：
//   - HMAC-SHA256 + server-side pepper（来自 NMP_API_KEY_PEPPER 环境变量）
//   - crypto/subtle.ConstantTimeCompare 防时序侧信道
//   - 同时支持旧 SHA-256 hash（双读滚动迁移），但每次验证会提示
//
// 部署步骤：
//  1. 生成 pepper：openssl rand -hex 32
//  2. 设置环境变量 NMP_API_KEY_PEPPER=xxx
//  3. 重启服务后所有新 API Key 自动用新 hash
//  4. 旧 key 仍可登录（fallback），但建议在 console 重新签发并废弃旧 hash
package apikey

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Hash 用 HMAC-SHA256 + pepper 计算 API key 哈希
func Hash(plaintext, pepper string) string {
	if pepper == "" {
		panic("apikey.Hash: pepper 不能为空（启动时应由 config.Validate 拦截）")
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 用常量时间比较验证 API key
func Verify(plaintext, pepper, expectedHash string) bool {
	expectedBytes, err := hex.DecodeString(expectedHash)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(plaintext))
	sum := mac.Sum(nil)
	return subtle.ConstantTimeCompare(sum, expectedBytes) == 1
}
