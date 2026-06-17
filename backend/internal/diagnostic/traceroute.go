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

// Traceroute 调用系统 traceroute 工具。
//   - host 必传，IPv4/IPv6/hostname
//   - maxHops 必传 1-64（防滥用）
//   - ctx 必传（用于超时控制）
func Traceroute(ctx context.Context, host string, maxHops int) (*TracerouteResult, error) {
	if err := validateHost(host); err != nil {
		return nil, err
	}
	if maxHops < MinTracerouteHops || maxHops > MaxTracerouteHops {
		return nil, fmt.Errorf("%w: maxHops 须在 1-%d", ErrInvalidHost, MaxTracerouteHops)
	}

	var args []string
	// 平台差异：macOS -m/-w；Linux 同名
	// -w 3：每跳等 3s（Linux traceroute 默认 5s，3s 是首跳优化，2s 容易丢包）
	if runtime.GOOS == "darwin" {
		args = []string{"-m", strconv.Itoa(maxHops), "-w", "2", host}
	} else {
		// Linux traceroute: -m max hops, -w wait seconds (default 5)
		args = []string{"-m", strconv.Itoa(maxHops), "-w", "3", host}
	}
	bin, err := resolveTracerouteBinary()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	raw := string(out)

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: 超时 (raw=%s)", ErrTracerouteFailed, truncate(raw, 200))
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, fmt.Errorf("%w: 取消", ErrTracerouteFailed)
		}
		// traceroute 通常所有跳都返回 0，但若没输出则硬错误
		if raw == "" {
			return nil, fmt.Errorf("%w: %v", ErrTracerouteFailed, err)
		}
	}

	result := parseTracerouteOutput(raw, host, maxHops)
	result.RawOutput = raw
	result.DurationMs = duration.Milliseconds()
	return result, nil
}

// traceroute 行格式（macOS / Linux 类似）：
//
//	1  localhost (127.0.0.1)  1.773 ms  0.877 ms  0.127 ms
//	2  * * *
//	5  router.example.com (10.0.0.1)  5.123 ms  4.987 ms  5.045 ms
var hopRe = regexp.MustCompile(`^\s*(\d+)\s+(.*)$`)

func parseTracerouteOutput(raw, host string, maxHops int) *TracerouteResult {
	res := &TracerouteResult{Host: host, MaxHops: maxHops, Hops: []TracerouteHop{}}

	for _, line := range splitLines(raw) {
		m := hopRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		hopNum, err := strconv.Atoi(m[1])
		if err != nil || hopNum < 1 || hopNum > maxHops {
			continue
		}
		hop := parseHopLine(m[2])
		hop.Hop = hopNum
		res.Hops = append(res.Hops, hop)
	}

	// 判断是否到达目标：最后一跳 host 包含目标 host/IP
	if len(res.Hops) > 0 {
		last := res.Hops[len(res.Hops)-1]
		if !last.Lossed && (containsFold(last.Host, host) || last.IP == host) {
			res.Reached = true
		}
	}

	return res
}

// parseHopLine 解析一跳的内容
// 例：localhost (127.0.0.1)  1.773 ms  0.877 ms  0.127 ms
// 例：* * *
// 例：10.0.0.1  5.123 ms  4.987 ms  5.045 ms
func parseHopLine(rest string) TracerouteHop {
	hop := TracerouteHop{}

	// 三个 * 视为丢包
	if isAllStars(rest) {
		hop.Lossed = true
		return hop
	}

	// 拆 time: regex `\d+(?:\.\d+)?\s*ms`
	timeRe := regexp.MustCompile(`([\d.]+)\s*ms`)
	timeMatches := timeRe.FindAllStringSubmatch(rest, -1)
	rtts := make([]string, 0, len(timeMatches))
	for _, m := range timeMatches {
		rtts = append(rtts, m[1]+" ms")
	}
	hop.RTTs = rtts

	// 拆 host 与 ip
	// 形式: "host.name (1.2.3.4)" 或 "1.2.3.4"
	parenRe := regexp.MustCompile(`^(\S+)\s+\(([^\)]+)\)`)
	if m := parenRe.FindStringSubmatch(rest); m != nil {
		hop.Host = m[1]
		hop.IP = m[2]
	} else {
		// 无括号，第一个 token 视为 host
		tokens := splitTokens(rest)
		if len(tokens) > 0 && isIPOrHost(tokens[0]) {
			hop.Host = tokens[0]
			hop.IP = tokens[0]
		}
	}

	return hop
}

func isAllStars(s string) bool {
	// 至少 3 个 *
	count := 0
	for _, c := range s {
		if c == '*' {
			count++
		}
	}
	return count >= 3
}

func isIPOrHost(s string) bool {
	if s == "" {
		return false
	}
	// 简版：含 . 或 : 视为 IP/host
	for _, c := range s {
		if c == '.' || c == ':' {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	res := []string{}
	cur := ""
	for _, c := range s {
		if c == '\n' {
			res = append(res, cur)
			cur = ""
		} else if c != '\r' {
			cur += string(c)
		}
	}
	if cur != "" {
		res = append(res, cur)
	}
	return res
}

func splitTokens(s string) []string {
	res := []string{}
	cur := ""
	for _, c := range s {
		if c == ' ' || c == '\t' {
			if cur != "" {
				res = append(res, cur)
				cur = ""
			}
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		res = append(res, cur)
	}
	return res
}

func containsFold(s, sub string) bool {
	if s == "" || sub == "" {
		return false
	}
	return indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := len(s), len(sub)
	if lsub > ls {
		return -1
	}
	for i := 0; i+lsub <= ls; i++ {
		if eqFold(s[i:i+lsub], sub) {
			return i
		}
	}
	return -1
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// resolveTracerouteBinary 一次性解析 traceroute 路径，缓存到 package var。
// 缺失时返回 ErrBinaryNotFound（fail fast，避免每次请求都 LookPath）。
//
//nolint:revive // 内部 helper
func resolveTracerouteBinary() (string, error) {
	tracerouteBinOnce.Do(func() {
		path, err := exec.LookPath("traceroute")
		if err != nil {
			return
		}
		tracerouteBinPath = path
	})
	if tracerouteBinPath == "" {
		return "", fmt.Errorf("%w: traceroute", ErrBinaryNotFound)
	}
	return tracerouteBinPath, nil
}
