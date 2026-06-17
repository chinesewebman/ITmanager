// Package diagnostic 提供 ICMP ping / traceroute 等网络诊断工具。
//
// 设计原则：
//   - 零依赖：直接 exec /sbin/ping 与 /usr/sbin/traceroute，parse stdout
//   - 跨平台：macOS (BSD) 与 Linux (iputils) ping 输出差异都覆盖
//   - 安全：host 白名单 + 参数上限 + exec.CommandContext 强制超时
//   - 可测：parse 函数纯逻辑，单测不依赖真实 binary
//
// 使用：
//
//	res, err := diagnostic.Ping(ctx, "192.168.1.1", 5)
//	res, err := diagnostic.Traceroute(ctx, "192.168.1.1", 30)
package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

// PingResult 单次 ping 的结构化结果
type PingResult struct {
	Host        string  `json:"host"`
	Count       int     `json:"count"`
	Transmitted int     `json:"transmitted"`
	Received    int     `json:"received"`
	LossPercent float64 `json:"loss_percent"`
	MinMs       float64 `json:"min_ms,omitempty"`
	AvgMs       float64 `json:"avg_ms,omitempty"`
	MaxMs       float64 `json:"max_ms,omitempty"`
	StddevMs    float64 `json:"stddev_ms,omitempty"`
	RawOutput   string  `json:"raw_output,omitempty"`
	DurationMs  int64   `json:"duration_ms"`
}

// TracerouteHop 单跳结果
type TracerouteHop struct {
	Hop    int      `json:"hop"`
	Host   string   `json:"host,omitempty"` // IP 或 hostname，可能空（* * *）
	IP     string   `json:"ip,omitempty"`
	RTTs   []string `json:"rtts,omitempty"` // 多个 time 字符串
	Lossed bool     `json:"lossed"`         // 三个都是 * *
}

// TracerouteResult traceroute 结构化结果
type TracerouteResult struct {
	Host       string          `json:"host"`
	MaxHops    int             `json:"max_hops"`
	Hops       []TracerouteHop `json:"hops"`
	Reached    bool            `json:"reached"` // 是否到达目标
	RawOutput  string          `json:"raw_output,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}

// 参数上限（防滥用）
const (
	MaxPingCount      = 20
	MaxTracerouteHops = 64
	MinPingCount      = 1
	MinTracerouteHops = 1
)

// ErrInvalidHost host 非法（空、超长、含非法字符）
var ErrInvalidHost = errors.New("非法 host")

// ErrPingFailed ping 执行失败
var ErrPingFailed = errors.New("ping 执行失败")

// ErrTracerouteFailed traceroute 执行失败
var ErrTracerouteFailed = errors.New("traceroute 执行失败")

// validateHost 检查 host 合法性：1-255 字符、不含 shell 元字符、不是 . 也不是 ..
// 允许：字母数字 . : - _ （含 IPv4 / IPv6 / hostname）
var invalidHostChars = regexp.MustCompile(`[^a-zA-Z0-9.\-:_]`)

//nolint:revive // exported for tests in same package
func validateHost(host string) error {
	if host == "" || len(host) > 255 {
		return fmt.Errorf("%w: 长度须在 1-255", ErrInvalidHost)
	}
	if host == "." || host == ".." {
		return fmt.Errorf("%w: 不允许 . 或 ..", ErrInvalidHost)
	}
	if invalidHostChars.MatchString(host) {
		return fmt.Errorf("%w: 含非法字符", ErrInvalidHost)
	}
	return nil
}

// Ping 调用系统 ping 工具。
//   - host 必传，IPv4/IPv6/hostname
//   - count 必传 1-20（防滥用）
//   - ctx 必传（用于超时控制）
func Ping(ctx context.Context, host string, count int) (*PingResult, error) {
	if err := validateHost(host); err != nil {
		return nil, err
	}
	if count < MinPingCount || count > MaxPingCount {
		return nil, fmt.Errorf("%w: count 须在 1-%d", ErrInvalidHost, MaxPingCount)
	}

	// 平台差异：macOS (BSD) -c/-W 数字；Linux iputils 同
	// 不调 shell，直接 exec 避免注入
	var args []string
	if runtime.GOOS == "darwin" {
		args = []string{"-c", strconv.Itoa(count), "-W", "2000", host}
	} else {
		// Linux iputils: -W 是秒，不是毫秒
		args = []string{"-c", strconv.Itoa(count), "-W", "2", host}
	}
	bin := "ping"

	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	raw := string(out)

	if err != nil {
		// ping 退出码非 0 不一定是错误（部分丢包也算失败但有输出）
		// 但 context 取消要报错
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: 超时 (raw=%s)", ErrPingFailed, truncate(raw, 200))
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, fmt.Errorf("%w: 取消", ErrPingFailed)
		}
		// 没输出视为硬错误
		if raw == "" {
			return nil, fmt.Errorf("%w: %v", ErrPingFailed, err)
		}
	}

	result := parsePingOutput(raw, host, count)
	result.RawOutput = raw
	result.DurationMs = duration.Milliseconds()
	return result, nil
}

// parsePingOutput 解析 ping 输出。
// 支持 macOS (BSD) 与 Linux (iputils) 格式。
//
// macOS 示例：
//
//	PING 127.0.0.1 (127.0.0.1): 56 data bytes
//	64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.081 ms
//	...
//	--- 127.0.0.1 ping statistics ---
//	2 packets transmitted, 2 packets received, 0.0% packet loss
//	round-trip min/avg/max/stddev = 0.081/0.205/0.328/0.123 ms
//
// Linux 示例：相似，stddev = stddev/0.000
func parsePingOutput(raw, host string, count int) *PingResult {
	res := &PingResult{Host: host, Count: count}

	// 1) packets transmitted / received
	transmittedRe := regexp.MustCompile(`(\d+)\s+packets?\s+transmitted,\s+(\d+)\s+(?:packets?\s+)?received`)
	if m := transmittedRe.FindStringSubmatch(raw); m != nil {
		res.Transmitted, _ = strconv.Atoi(m[1])
		res.Received, _ = strconv.Atoi(m[2])
	}

	// 2) loss percent
	lossRe := regexp.MustCompile(`([\d.]+)%\s+packet\s+loss`)
	if m := lossRe.FindStringSubmatch(raw); m != nil {
		res.LossPercent, _ = strconv.ParseFloat(m[1], 64)
	}

	// 3) min/avg/max/stddev
	rttRe := regexp.MustCompile(`(round-trip|rtt)\s+min/avg/max(?:/mdev)?(?:/stddev)?\s*=\s*([\d.]+)/([\d.]+)/([\d.]+)(?:/([\d.]+))?\s*ms`)
	if m := rttRe.FindStringSubmatch(raw); m != nil {
		res.MinMs, _ = strconv.ParseFloat(m[2], 64)
		res.AvgMs, _ = strconv.ParseFloat(m[3], 64)
		res.MaxMs, _ = strconv.ParseFloat(m[4], 64)
		if m[5] != "" {
			res.StddevMs, _ = strconv.ParseFloat(m[5], 64)
		}
	}

	return res
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
