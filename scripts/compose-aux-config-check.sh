#!/usr/bin/env bash
# compose-aux-config-check.sh — G-17 / M89 aux profile 配置静态校验
#
# 扫描 docker-compose.yml aux profile 7 服务 (netbox / zabbix / zabbix-web / glpi /
# 退出码:
#   0 = 干净 (无问题)
#   1 = 占位值 (SECRET_KEY / GRAYLOG_PASSWORD_SECRET 等字面占位)
#   2 = 可变 tag (:latest / :X.Y / :X 不带补丁号)
#   3 = xpack.security.enabled=false
#   4 = aux 服务未接 aux_net
#   (运行时错误 = 99)
#
# 沿用 G-6 (TLS check) + M88 (mutation inversion) 范本, 仅扫静态 yaml
# 不实际 docker compose up. CI 不强制 (M89 留 future M99+).
#
# 用法: bash scripts/compose-aux-config-check.sh [path/to/docker-compose.yml]
# 默认扫描 ./docker-compose.yml.
set -uo pipefail

COMPOSE_FILE="${1:-docker-compose.yml}"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "❌ compose file not found: $COMPOSE_FILE" >&2
  exit 99
fi

# 7 个 aux 服务名 (M89 = 6 主服务 + 新增 zabbix-web).
AUX_SERVICES=(netbox zabbix zabbix-web glpi graylog elasticsearch mongoDB)
# 仅这 3 个服务进 aux_net. 其余 4 个留 default (api 通过 DNS 集成).
AUX_NET_SERVICES=(graylog elasticsearch mongoDB)

# 占位值白名单 (与 backend/internal/config/config.go isPlaceholderToken G-15 同源).
# M89: 加 graylog 的字面占位.
PLACEHOLDER_PATTERNS=(
  "your-secret-key"
  "your-password-secret"
  "your-hashed-password"
  "change-in-production"
  "placeholder"
  "example-value"
  "nmp123"
)

EXIT_CODE=0
WARNINGS=()

# 守门函数: 检测字面占位值 (返 0 = 是占位).
# M1 mutation 实证: 把函数体改成 `:` 永远返 1 → 测试红.
is_placeholder_value() {
  local value="$1"
  for pattern in "${PLACEHOLDER_PATTERNS[@]}"; do
    if [[ "$value" == *"$pattern"* ]]; then
      return 0
    fi
  done
  return 1
}

# 守门函数: 检测可变 tag (返 0 = 是可变).
# 接受 X.Y.Z 与 X.Y.Z-foo 形式 (≥2 个点). 拒绝 :latest / :X / :X.Y (≤1 个点) / 末尾 . .
is_mutable_tag() {
  local tag="$1"
  if [ -z "$tag" ]; then return 1; fi
  if [ "$tag" = "latest" ]; then return 0; fi
  # 取 tag 主体 (去掉后缀 -xxx)
  local main="${tag%%-*}"
  # 数点个数. <2 → 可变
  local dots
  dots=$(echo "$main" | tr -cd '.' | wc -c)
  if [ "$dots" -lt 2 ]; then
    return 0
  fi
  # 末尾是 . 也算非稳定
  if [[ "$main" == *. ]]; then
    return 0
  fi
  return 1
}

# 工具: 把 "KEY=value" 行的 value 部分剥离掉 (兼容 ${VAR:?...} / "quoted" / 'quoted').
# 输出纯字面值 (空字符串若 value 完全被 ${VAR:?} 替代).
extract_value() {
  local raw="$1"
  # 整行若是 KEY=${VAR:?...} 或 KEY=${VAR...} 形式, 视为环境变量注入, 非字面占位
  if [[ "$raw" =~ ^[^=]+=\$\{[A-Z_]+(\??[^}]*)\} ]]; then
    echo ""
    return 0
  fi
  # 去除 key= 前缀 (用 bash 内置避免 sed escaping)
  echo "${raw#*=}"
}

# ----- Check 1: 占位值 -----
check_placeholder_values() {
  local file="$1"
  grep -nE "(SECRET_KEY|GRAYLOG_PASSWORD_SECRET|GRAYLOG_ROOT_PASSWORD_SHA2|GRAYLOG_ROOT_PASSWORD|ELASTIC_PASSWORD)=" "$file" 2>/dev/null | while IFS= read -r matched; do
    local ln rest value
    ln="${matched%%:*}"
    rest="${matched#*:}"
    value=$(extract_value "$rest")
    if [ -z "$value" ]; then continue; fi
    if is_placeholder_value "$value"; then
      echo "placeholder:${ln}:${rest}"
    fi
  done
}

# ----- Check 2: 可变 tag -----
check_mutable_tags() {
  local file="$1"
  for svc in "${AUX_SERVICES[@]}"; do
    awk -v svc="  ${svc}:" '
      $0 ~ svc && !img_captured { svc_line=NR; next }
      svc_line && /^[a-zA-Z]/ && NR > svc_line { svc_line=0 }
      svc_line && /^[[:space:]]+image:/ && !img_captured {
        img = $0
        sub(/^[[:space:]]+image:[[:space:]]+/, "", img)
        sub(/^[[:space:]]+|[[:space:]]+$/, "", img)
        print NR ":" img
        img_captured = 1
        svc_line = 0
      }
    ' "$file" | while IFS= read -r matched; do
      local ln img tag
      ln="${matched%%:*}"
      img="${matched#*:}"
      # 取 tag: 最后一栏 (含不规则的 :foo 后缀形式)
      tag="${img##*:}"
      if is_mutable_tag "$tag"; then
        echo "mutable_tag:${ln}:${img}"
      fi
    done
  done
}

# ----- Check 3: xpack.security -----
check_xpack_disabled() {
  local file="$1"
  grep -nE "xpack.security.enabled" "$file" 2>/dev/null | grep "=false" | while IFS= read -r matched; do
    ln="${matched%%:*}"
    echo "xpack_disabled:${ln}:xpack.security.enabled=false"
  done
}

# ----- Check 4: aux 服务是否接 aux_net -----
check_aux_network_isolation() {
  local file="$1"
  for svc in "${AUX_NET_SERVICES[@]}"; do
    local ln end
    ln=$(grep -nE "^  ${svc}:" "$file" 2>/dev/null | head -1 | cut -d: -f1)
    if [ -z "$ln" ]; then continue; fi
    # 取服务段结束行 (下一个 ^[a-zA-Z] 行; 若无, 取文件末行+1)
    end=$(awk -v start="$ln" 'NR > start && /^[a-zA-Z]/ { print NR; found=1; exit } END { if (!found) print NR+1 }' "$file")
    if ! sed -n "${ln},${end}p" "$file" | grep -qE "^[[:space:]]+- aux_net\$"; then
      echo "no_aux_net:${ln}:${svc}"
    fi
  done
}
# 主流程
WARNINGS_RAW=$(
  check_placeholder_values "$COMPOSE_FILE"
  check_mutable_tags "$COMPOSE_FILE"
  check_xpack_disabled "$COMPOSE_FILE"
  check_aux_network_isolation "$COMPOSE_FILE"
)

# 转数组
WARNINGS=()
while IFS= read -r w; do
  if [ -n "$w" ]; then
    WARNINGS+=("$w")
  fi
done <<< "$WARNINGS_RAW"

# 输出 & 退出码
if [ ${#WARNINGS[@]} -eq 0 ]; then
  echo "✅ compose aux 配置干净 (${COMPOSE_FILE})"
  exit 0
fi

# 按类别累加退出码
for w in "${WARNINGS[@]}"; do
  echo "⚠️  ${w}" >&2
  case "$w" in
    placeholder:*)    EXIT_CODE=$((EXIT_CODE | 1));;
    mutable_tag:*)    EXIT_CODE=$((EXIT_CODE | 2));;
    xpack_disabled:*) EXIT_CODE=$((EXIT_CODE | 3));;
    no_aux_net:*)     EXIT_CODE=$((EXIT_CODE | 4));;
  esac
done

echo "❌ compose aux 配置有 ${#WARNINGS[@]} 处问题 (exit code=$EXIT_CODE)" >&2
exit $EXIT_CODE
