#!/usr/bin/env bash
# compose-aux-config-check_test.sh — 4 场景 PASS-FAIL-PASS 实证
#
# 验证 scripts/compose-aux-config-check.sh 的 4 类守门真在门:
#   1. 干净 compose → exit 0
#   2. 占位字面 → exit 1 (含 "placeholder" 关键字)
#   3. 可变 tag → exit 2 (含 "mutable_tag" 关键字)
#   4. xpack off → exit 3 (含 "xpack_disabled" 关键字)
#
# mutation inversion M1/M2 实证见 intent-M89-candidate.md §Verification §3:
#   M1: 临时把 is_placeholder_value 函数体改成 `:` (剥守门) → 该测试红 → 还原 → 绿
#   M2: 临时把 zabbix image tag 从 7.0.13-alpine 改回 :latest → 该测试红 → 还原 → 绿
set -uo pipefail

CHECK_SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/compose-aux-config-check.sh"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

PASS_COUNT=0
FAIL_COUNT=0

# 测试用例辅助: 跑某 compose 文件 + 期望 exit + 期望关键字 (在 stderr 中).
run_test() {
  local name="$1"
  local compose_file="$2"
  local expected_exit="$3"
  local expected_keyword="$4"

  local stderr_file="$TMPDIR/${name}.stderr"
  bash "$CHECK_SCRIPT" "$compose_file" >/dev/null 2>"$stderr_file"
  local actual_exit=$?

  if [ "$actual_exit" -ne "$expected_exit" ]; then
    echo "❌ FAIL: $name"
    echo "    期望 exit=$expected_exit 实得 exit=$actual_exit"
    echo "    stderr: $(cat $stderr_file | head -5)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return 1
  fi
  if [ -n "$expected_keyword" ] && ! grep -q "$expected_keyword" "$stderr_file"; then
    echo "❌ FAIL: $name (缺关键字 '$expected_keyword')"
    echo "    stderr: $(cat $stderr_file | head -5)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return 1
  fi
  echo "✅ PASS: $name (exit=$actual_exit)"
  PASS_COUNT=$((PASS_COUNT + 1))
  return 0
}

# ---- 场景 1: 干净 compose ----
COMPOSE_CLEAN="$TMPDIR/clean.yml"
cat > "$COMPOSE_CLEAN" <<'YAML'
x-logging: &default-logging
  logging:
    driver: json-file
    options:
      max-size: "10m"
      max-file: "5"
services:
  netbox:
    image: netboxcommunity/netbox:v4.0.3
    profiles: ["aux"]
    environment:
      - SECRET_KEY=${NMP_AUX_NETBOX_SECRET_KEY:?必须在 .env.aux 里设置}
  zabbix:
    image: zabbix/zabbix-server-pgsql:7.0.13-alpine
    profiles: ["aux"]
  zabbix-web:
    image: zabbix/zabbix-web-nginx-pgsql:7.0.13-alpine
    profiles: ["aux"]
  glpi:
    image: linuxserver/glpi:3.0.11
    profiles: ["aux"]
  graylog:
    image: graylog/graylog:6.0.3-1
    profiles: ["aux"]
    environment:
      - GRAYLOG_PASSWORD_SECRET=${NMP_AUX_GRAYLOG_PASSWORD_SECRET:?必须在 .env.aux 里设置}
      - GRAYLOG_ROOT_PASSWORD_SHA2=${NMP_AUX_GRAYLOG_ROOT_PASSWORD_SHA2:?必须 sha256sum}
      - GRAYLOG_ROOT_PASSWORD=${NMP_AUX_GRAYLOG_ROOT_PASSWORD:?必须在 .env.aux 里设置}
    networks:
      - aux_net
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.4
    profiles: ["aux"]
    environment:
      - xpack.security.enabled=true
      - ELASTIC_PASSWORD=${NMP_AUX_ELASTICSEARCH_PASSWORD:?必须在 .env.aux 里设置}
    networks:
      - aux_net
  mongoDB:
    image: mongo:7.0.14
    profiles: ["aux"]
    networks:
      - aux_net
networks:
  aux_net:
    driver: bridge
    internal: true
YAML
run_test "test_compose_aux_config_check_passes_on_clean_compose" \
  "$COMPOSE_CLEAN" 0 ""

# ---- 场景 2: 占位字面 ----
COMPOSE_PLACEHOLDER="$TMPDIR/placeholder.yml"
cat > "$COMPOSE_PLACEHOLDER" <<'YAML'
services:
  netbox:
    image: netboxcommunity/netbox:v4.0.3
    profiles: ["aux"]
    environment:
      - SECRET_KEY=your-secret-key-here-change-in-production
YAML
run_test "test_compose_aux_config_check_detects_placeholder_values" \
  "$COMPOSE_PLACEHOLDER" 1 "placeholder"

# ---- 场景 3: 可变 tag ----
COMPOSE_TAG="$TMPDIR/mutable.yml"
cat > "$COMPOSE_TAG" <<'YAML'
services:
  zabbix:
    image: zabbix/zabbix-web-nginx-pgsql:latest
    profiles: ["aux"]
YAML
run_test "test_compose_aux_config_check_detects_mutable_tags" \
  "$COMPOSE_TAG" 2 "mutable_tag"

# ---- 场景 4: xpack off (注意: 此 compose 同时含 aux_net, 避免检查 4 干扰) ----
COMPOSE_XPACK="$TMPDIR/xpack.yml"
cat > "$COMPOSE_XPACK" <<'YAML'
services:
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.4
    profiles: ["aux"]
    environment:
      - xpack.security.enabled=false
    networks:
      - aux_net
YAML
run_test "test_compose_aux_config_check_detects_xpack_disabled" \
  "$COMPOSE_XPACK" 3 "xpack_disabled"

echo
echo "================================"
echo "Total: $((PASS_COUNT + FAIL_COUNT)) | PASS: $PASS_COUNT | FAIL: $FAIL_COUNT"
echo "================================"
if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
