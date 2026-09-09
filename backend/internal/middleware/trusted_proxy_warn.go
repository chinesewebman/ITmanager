package middleware

import (
	"log/slog"
	"net"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// forwardedForWarned 进程内一次性标记（G-7）。
// 用 atomic 而非 sync.Once：测试需要复位，且将来若有多个 router 实例应共享同一标记。
var forwardedForWarned atomic.Bool

// WarnUntrustedForwardedFor 在「请求带 X-Forwarded-For，但直连对端不在受信代理表里」
// 时告警一次（G-7）。
//
// 为什么需要它：ClientIP() 对未受信来源会忽略 XFF（安全上正确），但若服务其实在
// 反向代理后，后果是**静默的**——登录限流按代理聚合、审计 IP 变成代理地址、
// 配了 IP 白名单的 API Key 一律 403（docs/FIX-PLAN-TRUSTED-PROXY.md R-1）。
// 启动期日志是静态的，运维未必会看；这条由真实流量触发，是第二道信号。
//
// 判定用「对端是否在表内」而不是「表是否为空」：**配了但配错比不配更危险**
// ——非空配置连启动期告警都不会有，故障完全静默。这同时覆盖多跳漏配、
// 代理换 IP、LB 与容器直连等场景（安全审计建议 2/5）。
//
// trusted 传已校验的条目（裸 IP 等价 /32、/128）。
func WarnUntrustedForwardedFor(trusted []string) gin.HandlerFunc {
	trustedNets := parseTrustedNets(trusted)
	return func(c *gin.Context) {
		if c.GetHeader("X-Forwarded-For") != "" &&
			!isTrustedPeer(c.Request.RemoteAddr, trustedNets) &&
			forwardedForWarned.CompareAndSwap(false, true) {
			slog.Warn("收到 X-Forwarded-For，但直连对端不在 server.trusted_proxies 中：该头被忽略，"+
				"ClientIP() 取直连对端。若本服务在反向代理后，请把**每一跳代理本身**的 IP/CIDR 配进 "+
				"server.trusted_proxies，否则登录限流会按代理聚合、审计 IP 失真、IP 白名单 API Key 将一律 403",
				slog.String("remote_addr", c.Request.RemoteAddr),
				slog.Any("trusted_proxies", trusted))
		}
		c.Next()
	}
}

// parseTrustedNets 把配置条目解析成网段（裸 IP 等价 /32 或 /128）。
// 非法条目跳过：配置已在启动期校验过，这里只做「尽力判断」，不重复拒绝。
func parseTrustedNets(trusted []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(trusted))
	for _, raw := range trusted {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			ip := net.ParseIP(entry)
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				nets = append(nets, &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)})
			} else {
				nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
			}
			continue
		}
		if _, ipNet, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}

// isTrustedPeer 判断直连对端是否落在受信网段内（口径与 gin 一致：比的是对端，不是 XFF）。
func isTrustedPeer(remoteAddr string, nets []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr // 测试里可能直接给裸 IP
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resetUntrustedForwardedForWarn 测试用：复位一次性标记。
func resetUntrustedForwardedForWarn() { forwardedForWarned.Store(false) }
