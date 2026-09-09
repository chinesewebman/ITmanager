#!/usr/bin/env bash
#
# scripts/check-tls.sh — 断言某个 HTTPS 端点**只**接受 TLS 1.2 及以上
#
# 目的: PCI DSS 4.2.1 要求持卡人数据在开放网络上传输时使用强加密，且不得回退到
#       不安全的协议版本。本仓库不含 TLS 终止（`frontend/nginx.conf` 只有 80，
#       Go 后端是明文 `ListenAndServe`），最低版本完全取决于外部反代/负载均衡的
#       配置——**无法自证**。本脚本把「自证」变成一条可重复执行的命令。
#       背景见 TODO.md G-6 / docs/FIX-PLAN-AUTHZ-CLOSURE.md §9.3。
#
# 为什么用 openssl s_client 而不是 curl:
#       curl 只报最终协商结果，无法**主动降级尝试**；PCI 要的是「弱版本握手被拒」，
#       必须由客户端显式发起 TLS 1.0 / 1.1 握手并观察服务端拒绝。
#
# 用法:
#   scripts/check-tls.sh <host[:port]>            # 默认 443
#   scripts/check-tls.sh itmanager.example.com:8443
#   TLS_SNI=itmanager.example.com scripts/check-tls.sh 10.0.0.5:8443
#
# 退出码:
#   0 = 合规（TLS 1.2 可协商，且 TLS 1.0/1.1 均被拒）
#   1 = 不合规（弱版本可协商，或 TLS 1.2 不可协商）
#   2 = 无法判定（缺 openssl / 本机 openssl 无法发起弱版本握手 / 连接失败）
#
# 依赖: openssl（必需）。OpenSSL 3.x 需 legacy provider 才能发起 TLS 1.0/1.1
#       握手；若本机缺 legacy provider，脚本按「无法判定」退出而不是假通过。

set -euo pipefail

TARGET="${1:-}"
if [[ -z "$TARGET" ]]; then
  echo "用法: $0 <host[:port]>    (环境变量 TLS_SNI 可覆盖 SNI，默认取 host)" >&2
  exit 2
fi

# 允许传入 https://host:port/path 形式
TARGET="${TARGET#https://}"
TARGET="${TARGET%%/*}"

HOST="${TARGET%%:*}"
if [[ "$TARGET" == *:* ]]; then
  PORT="${TARGET##*:}"
else
  PORT=443
fi
SNI="${TLS_SNI:-$HOST}"

command -v openssl >/dev/null 2>&1 || { echo "❌ 找不到 openssl" >&2; exit 2; }

OPENSSL_MAJOR="$(openssl version | sed -n 's/^OpenSSL \([0-9]*\).*/\1/p')"
[[ -n "$OPENSSL_MAJOR" ]] || OPENSSL_MAJOR=1

# probe <版本> → stdout: 协商到的协议版本；rc 0 = 协商成功，rc 1 = 被拒/失败
#
# 弱版本（1.0/1.1）在 OpenSSL 3.x 下默认连「发起」都不允许（no protocols
# available），必须显式加载 legacy provider + 降安全等级，否则会把「客户端发不
# 出请求」误读成「服务端拒绝」。
probe() {
  local flag="$1" out rc=0
  local args=(-connect "$HOST:$PORT" -servername "$SNI" -"$flag" -brief)
  if [[ "$flag" == "tls1" || "$flag" == "tls1_1" ]] && [[ "$OPENSSL_MAJOR" -ge 3 ]]; then
    args+=(-provider default -provider legacy -cipher 'DEFAULT@SECLEVEL=0')
  fi
  out="$(echo | openssl s_client "${args[@]}" 2>&1)" || rc=$?
  if [[ $rc -eq 0 ]]; then
    grep -oE 'Protocol version: TLSv[0-9.]+' <<<"$out" | head -1 | sed 's/.*: //'
    return 0
  fi
  # 区分「本机发不出弱版本握手」与「服务端拒绝」：前者不可判定
  if grep -q "no protocols available" <<<"$out"; then
    echo "__NO_LOCAL_SUPPORT__"
    return 1
  fi
  return 1
}

echo "▶ 目标: $HOST:$PORT (SNI=$SNI)"
echo "  本机 openssl: $(openssl version)"

fail=0
inconclusive=0

for spec in "tls1:TLS 1.0:weak" "tls1_1:TLS 1.1:weak" "tls1_2:TLS 1.2:required" "tls1_3:TLS 1.3:info"; do
  IFS=':' read -r flag label kind <<<"$spec"
  negotiated="$(probe "$flag")" && ok=1 || ok=0

  case "$kind" in
    weak)
      if [[ $ok -eq 1 ]]; then
        echo "  ❌ $label 可协商（$negotiated）—— 违反 PCI DSS 4.2.1"
        fail=1
      elif [[ "$negotiated" == "__NO_LOCAL_SUPPORT__" ]]; then
        echo "  ⚠️  $label 无法判定：本机 openssl 无法发起该版本握手（缺 legacy provider）"
        inconclusive=1
      else
        echo "  ✅ $label 已被拒绝"
      fi
      ;;
    required)
      if [[ $ok -eq 1 ]]; then
        echo "  ✅ $label 可协商（$negotiated）"
      else
        echo "  ❌ $label 无法协商 —— 端点不支持最低要求的版本"
        fail=1
      fi
      ;;
    info)
      if [[ $ok -eq 1 ]]; then
        echo "  ℹ️  $label 可协商（$negotiated）"
      else
        echo "  ℹ️  $label 未启用"
      fi
      ;;
  esac
done

echo
if [[ $fail -eq 1 ]]; then
  echo "结论: ❌ 不合规 —— 端点未满足 TLS 1.2+ 最低版本要求"
  exit 1
fi
if [[ $inconclusive -eq 1 ]]; then
  echo "结论: ⚠️  无法判定 —— 弱版本握手未被真正验证（见上方提示）"
  exit 2
fi
echo "结论: ✅ 合规 —— 仅接受 TLS 1.2 及以上"
