// Package cursor 实现基于 (timestamp, id) 二元组的游标分页。
//
// 为什么用 cursor 而非 offset/limit?
//   - offset 翻到末页 O(N), 100k 行 alerts 翻到末页 P95 > 2s
//   - cursor 始终 O(log N), 走 (created_at, id) 联合索引
//
// 格式: base64( RFC3339Nano "\x00" uuid ) — 用 NUL 分隔避坑 (RFC3339Nano 含 ':')
// 排序: ORDER BY created_at DESC, id DESC (id 用于同 ts 排序稳定性)
//
// 用法:
//
//	enc, _ := cursor.Encode(time.Now(), uuid.New())
//	// 传给客户端 → 下次 ?cursor=enc&limit=20
//	ts, id, _ := cursor.Decode(enc)
//	// 服务端 WHERE (created_at, id) < (ts, id) ORDER BY ... LIMIT 20
package cursor

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const separator = "\x00"

// ErrInvalid 游标格式错误 (客户端传入坏数据时返)
var ErrInvalid = errors.New("invalid cursor")

// Encode 把 (timestamp, uuid) 编码成 base64 字符串。
// 时间精度到纳秒, 保证 ORDER BY created_at 的稳定顺序。
func Encode(ts time.Time, id uuid.UUID) string {
	raw := ts.UTC().Format(time.RFC3339Nano) + separator + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// Decode 解析 base64 字符串回 (timestamp, uuid)。
// 任何错误返 ErrInvalid, 客户端应当忽略并用默认 offset 分页降级。
func Decode(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: base64: %v", ErrInvalid, err)
	}
	// 用 NUL 分隔避坑: RFC3339Nano 含 ':' 不能直接 SplitN(":")
	parts := strings.SplitN(string(raw), separator, 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: missing separator", ErrInvalid)
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: time parse %q: %v", ErrInvalid, parts[0], err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: uuid parse %q: %v", ErrInvalid, parts[1], err)
	}
	return ts, id, nil
}
