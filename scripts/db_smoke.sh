#!/usr/bin/env bash
#
# scripts/db_smoke.sh — 真 Postgres 冒烟测试台
#
# 目的: 用「迁移建出来的库」跑核心链路(登录查询 / 审计写入 / 建工单),
#       暴露 GORM 模型与 backend/migrations/*.up.sql 之间的 schema 漂移。
#       背景见 docs/v3-架构优化需求.md §9 D-1 / D-4 / D-5。
#
# 为什么需要它: CI 只有 `go test`(sqlite/mock), 没有 Postgres, 所以
#       模型与迁移的漂移长期不可见。本脚本按生产路径(迁移 DDL)建库再跑。
#
# 两条路径:
#   ① 全新安装 —— 空库上跑生产执行器 migrate.Up, 全部迁移应用 + 核心链路
#      (含类型转换往返 / ticket_number 唯一约束 / 000013 重放幂等 / jsonb 列默认值)
#   ② 存量升级 —— 库已到 000012 且有存量 admin 与存量资产, 只跑 000013 之后的迁移,
#      校验 role 回填(若失效, 存量 admin 会被 D-6 的 admin 门禁锁在门外)、
#      jsonb 回填(NULL → []/{} 且合法值不被改写) + 回滚不丢旧列
#
# 用法:
#   scripts/db_smoke.sh
#
# 2026-09-09 更新: 迁移改由 Go 测试里的 migrate.Up 执行(不再 psql 喂),
#   保证冒烟走的是与 cmd/server 相同的语句切分路径(DO $$ ... $$ 块)。
#
# 前置: go + 其一 ——
#   a) docker + 本地 postgres 镜像(默认 postgres:18-alpine)   [默认, 本机/CI 均可]
#   b) 外部 Postgres: 设 SMOKE_PG_HOST(必填) / SMOKE_PG_PORT(默认 5432), 需本机有 psql/createdb
#      (CI 用 service 容器跑这条路, 见 .github/workflows/ci.yml 的 dbsmoke job)
# 幂等: 每次起一个临时容器(名字含 PID, 随机宿主端口), trap 保证退出时删除; 可重复执行。
# 退出码: 0 = 迁移 + 全部冒烟断言通过; 非 0 = 迁移或断言失败(原始报错已打印)。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGR_DIR="$REPO_ROOT/backend/migrations"
BACKEND_DIR="$REPO_ROOT/backend"

IMAGE="${SMOKE_PG_IMAGE:-postgres:18-alpine}"
DB_NAME="${SMOKE_DB_NAME:-itmanager_smoke}"
DB_USER="${SMOKE_DB_USER:-postgres}"
DB_PASS="${SMOKE_DB_PASS:-smoke_pw}"
CONTAINER="itmanager-dbsmoke-$$"
# DOCKER override: 在 docker group 缺权限的环境(NAS 上 webman 默认)里,
# 设 DOCKER="sudo -n docker" 走 sudoer 的 NOPASSWD:ALL;否则留空走系统 PATH 的 docker。
DOCKER="${DOCKER:-}"

log()  { printf '[db_smoke] %s\n' "$*"; }
fail() { printf '[db_smoke][FAIL] %s\n' "$*" >&2; }

# ---- 定位 go ----
GO_BIN="${GO_BIN:-}"
if [[ -z "$GO_BIN" ]]; then
  if command -v go >/dev/null 2>&1; then GO_BIN="$(command -v go)"
  elif [[ -x /usr/local/go/bin/go ]]; then GO_BIN=/usr/local/go/bin/go
  else fail "找不到 go, 请用 GO_BIN=/path/to/go 指定"; exit 127
  fi
fi

# ---- 前置检查 ----
EXTERNAL=0
[[ -n "${SMOKE_PG_HOST:-}" ]] && EXTERNAL=1
if [[ "$EXTERNAL" -eq 0 ]]; then
  command -v docker >/dev/null 2>&1 || {
    fail "找不到 docker; 或设 SMOKE_PG_HOST/SMOKE_PG_PORT 走外部 Postgres"
    exit 127
  }
else
  command -v psql >/dev/null 2>&1 || { fail "外部模式下找不到 psql"; exit 127; }
  command -v createdb >/dev/null 2>&1 || { fail "外部模式下找不到 createdb"; exit 127; }
fi
[[ -d "$MIGR_DIR" ]] || { fail "迁移目录不存在: $MIGR_DIR"; exit 1; }
[[ -d "$BACKEND_DIR" ]] || { fail "backend 目录不存在: $BACKEND_DIR"; exit 1; }

# ---- 退出清理 ----
cleanup() {
  if [[ -n "${CONTAINER:-}" ]]; then
    ${DOCKER:-docker} rm -f "$CONTAINER" >/dev/null 2>&1 || true
  fi
  # M44 / G-30: 临时凭据文件清理 (0600 + trap rm)
  [[ -n "${ENVFILE:-}" ]] && rm -f "$ENVFILE"
  [[ -n "${PGPASS:-}" ]] && rm -f "$PGPASS"
  # unset 明文密码 env vars, 不让父进程 / 兄弟进程读到
  unset PGPASSWORD PGUSER PGHOST PGPORT PGDATABASE TEST_DATABASE_URL
}
trap cleanup EXIT INT TERM

# ---- 1. 起临时容器 或 复用外部 Postgres ----
if [[ "$EXTERNAL" -eq 1 ]]; then
  HOST_PORT="${SMOKE_PG_PORT:-5432}"
  log "外部 Postgres: ${SMOKE_PG_HOST}:${HOST_PORT} | 库: $DB_NAME"
  psql_q()     { psql     -h "$SMOKE_PG_HOST" -p "$HOST_PORT" -U "$DB_USER" -v ON_ERROR_STOP=1 -q "$@"; }
  createdb_q() { createdb -h "$SMOKE_PG_HOST" -p "$HOST_PORT" -U "$DB_USER" "$@"; }
else
  log "镜像: $IMAGE | 容器: $CONTAINER | 库: $DB_NAME"
  ${DOCKER:-docker} image inspect "$IMAGE" >/dev/null 2>&1 || {
    fail "本地没有镜像 $IMAGE (本脚本不做 pull; 可设 SMOKE_PG_IMAGE 指定已有镜像)"
    exit 1
  }
  # M44 / G-30: 临时凭据文件 — docker run --env-file 走临时文件
  # (0600 + trap rm), 不再用 `-e POSTGRES_PASSWORD=...` 把密码塞 argv
  # (argv 进 /proc/<pid>/cmdline, 同机任意用户 ps aux 可见).
  ENVFILE=$(mktemp -t dbsmoke-env.XXXXXX)
  chmod 0600 "$ENVFILE"
  printf 'POSTGRES_PASSWORD=%s\nPOSTGRES_DB=%s\n' "$DB_PASS" "$DB_NAME" > "$ENVFILE"
  log "启动临时 Postgres 容器..."
  ${DOCKER:-docker} run -d --name "$CONTAINER" \
    --env-file "$ENVFILE" \
    -p 127.0.0.1::5432 \
    "$IMAGE" >/dev/null

  HOST_PORT="$(${DOCKER:-docker} port "$CONTAINER" 5432/tcp | head -n1 | sed 's/.*://')"
  [[ -n "$HOST_PORT" ]] || { fail "无法获取容器映射端口"; exit 1; }
  log "容器已起, 宿主端口: $HOST_PORT"
  psql_q()     { ${DOCKER:-docker} exec -i "$CONTAINER" psql     -v ON_ERROR_STOP=1 -q -U "$DB_USER" "$@"; }
  createdb_q() { ${DOCKER:-docker} exec    "$CONTAINER" createdb -U "$DB_USER" "$@"; }
fi

# M44 / G-30: 写 .pgpass (0600 + trap rm), psql / go test 走 PGPASSFILE
# 不再 set PGPASSWORD="$DB_PASS" 进 env. 路径可任意, 不含密码.
PGPASS=$(mktemp -t pgpass.XXXXXX)
chmod 0600 "$PGPASS"
printf '%s:%s:%s:%s:%s\n' \
  "${SMOKE_PG_HOST:-127.0.0.1}" "${HOST_PORT:-5432}" "$DB_NAME" "$DB_USER" "$DB_PASS" \
  > "$PGPASS"
# 也覆盖其他可能用到的库 (升级路径 _upgrade)
printf '%s:%s:%s:%s:%s\n' \
  "${SMOKE_PG_HOST:-127.0.0.1}" "${HOST_PORT:-5432}" "${DB_NAME}_upgrade" "$DB_USER" "$DB_PASS" \
  >> "$PGPASS"
export PGPASSFILE="$PGPASS"

# ---- 2. 等库 ready ----
log "等待 Postgres 就绪..."
ready=0
for i in $(seq 1 60); do
  if psql_q -d "$DB_NAME" -tAc 'SELECT 1' >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep 1
done
if [[ "$ready" -ne 1 ]]; then
  fail "Postgres 60s 内未就绪"
  if [[ "$EXTERNAL" -eq 0 ]]; then
    ${DOCKER:-docker} logs --tail 40 "$CONTAINER" >&2 || true
  fi
  exit 1
fi
log "Postgres 就绪"

# ---- 3. 两个库：fresh(全新安装) + upgrade(存量升级模拟) ----
# 迁移不再由 psql 喂：改由生产执行器 internal/migrate 在 Go 测试里跑。
# 原因: psql 与 migrate.Up 的语句切分不同(后者要处理 DO $$ ... $$ 块),
#       用 psql 建库会让「生产路径跑不通」的缺陷漏过冒烟(假绿)。
UPGRADE_DB="${DB_NAME}_upgrade"
createdb_q "$UPGRADE_DB" >/dev/null
log "库: $DB_NAME(全新) / $UPGRADE_DB(升级路径)"

# ---- 4. 升级路径库: 先建到 000012, 再伪造 schema_migrations=1..12 + 存量 admin ----
# 按**版本号**过滤，不要硬编码 `! -name '000013_*'`：新增 000014 后后者会把 000014
# 也预应用到这个「存量库」上，于是升级路径上的 000014 变成「在已有默认值的表上再设一次」，
# 回填恒命中 0 行 —— 用例全绿却什么也没验证（数据完整性审查 阻断项）。
LEGACY_MIGRATIONS=()
for f in "$MIGR_DIR"/*.up.sql; do
  v="$(basename "$f")"; v="${v%%_*}"
  if (( 10#$v < 13 )); then LEGACY_MIGRATIONS+=("$f"); fi
done
[[ "${#LEGACY_MIGRATIONS[@]}" -gt 0 ]] || { fail "没找到 000013 之前的 *.up.sql"; exit 1; }
log "升级路径库: 用 psql 应用 ${#LEGACY_MIGRATIONS[@]} 个旧迁移(000001~000012)..."
for f in "${LEGACY_MIGRATIONS[@]}"; do
  name="$(basename "$f")"
  if ! psql_q -d "$UPGRADE_DB" < "$f"; then
    fail "旧迁移失败: $name (Postgres 原始报错见上)"
    exit 1
  fi
done

log "升级路径库: 伪造 schema_migrations=1..12 + 造一个「升级前就存在的 admin」"
psql_q -d "$UPGRADE_DB" <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO schema_migrations(version) SELECT generate_series(1, 12);
-- 存量用户: 建它们时 users 表还没有 role 列(000013 才加)
INSERT INTO users (username, password_hash) VALUES
    ('legacy_admin', 'x'), ('legacy_plain', 'x');
-- admin-bootstrap 会写 user_roles(见 cmd/admin-bootstrap/main.go:104)
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u, roles r
WHERE u.username = 'legacy_admin' AND r.code = 'admin';

-- G-20: 预置两行「升级前就存在」的资产，让 000014 的回填有真实命中对象
--   legacy-null-jsonb: 两列 NULL → 必须被回填为 []/{}
--   legacy-json-jsonb: 合法 JSON → 必须原样保留（回填不是全表改写）
-- 用 asset_name 列（000013 才 RENAME 成 name）
INSERT INTO assets (asset_type, asset_name, tags, custom_fields) VALUES
    ('server', 'legacy-null-jsonb', NULL, NULL),
    ('server', 'legacy-json-jsonb', '["x"]'::jsonb, '{"k":"v"}'::jsonb);

-- M16: 预置两行「升级前就存在」的工单。
--   LEGACY-M16-1 (medium): 必须被 000023 归一为 normal。
--     没有它, `UPDATE ... WHERE priority='medium'` 恒命中 0 行, 用例会变成
--     「把迁移整个删掉也是绿」的假绿 —— 与 000014 回填踩过的坑同型。
--   LEGACY-M16-2 (high): 对照组, 必须原样保留。
--     没有它, 把 up 的 WHERE 去掉(全表一律改成 normal)也能让上面那条断言通过 ——
--     等于没有验证「只动 medium 这一个同义词」。
-- 列名用 pre-000013 的名字(ticket_no / creator_id), 000013 才 RENAME 成 ticket_number / requester_id。
INSERT INTO tickets (ticket_no, ticket_type, priority, title, status, creator_id)
SELECT 'LEGACY-M16-1', 'incident', 'medium', '存量 medium 工单', 'open', u.id
  FROM users u WHERE u.username = 'legacy_admin'
UNION ALL
SELECT 'LEGACY-M16-2', 'incident', 'high', '存量 high 工单(对照)', 'open', u.id
  FROM users u WHERE u.username = 'legacy_admin';
SQL

# ---- 5. 跑 Go 冒烟测试(两条路径) ----
FRESH_DSN="postgres://${DB_USER}:${DB_PASS}@127.0.0.1:${HOST_PORT}/${DB_NAME}?sslmode=disable"
UPGRADE_DSN="postgres://${DB_USER}:${DB_PASS}@127.0.0.1:${HOST_PORT}/${UPGRADE_DB}?sslmode=disable"

rc=0
log "① 全新安装路径: migrate.Up 从零建库 + 核心链路 (build tag: dbsmoke)"
log "   PGUSER=$DB_USER PGHOST=127.0.0.1 PGPORT=$HOST_PORT PGDATABASE=$DB_NAME (PGPASSWORD 走 .pgpass + PGPASSFILE)"
# M44 / G-30: 不再用 TEST_DATABASE_URL (含明文密码进 env). 改 PG* 拆分 +
# PGPASSFILE (pgx/gorm 都支持 libpq env vars).
( cd "$BACKEND_DIR" && \
  PGUSER="$DB_USER" PGHOST=127.0.0.1 PGPORT="$HOST_PORT" PGDATABASE="$DB_NAME" \
  "$GO_BIN" test \
    -tags dbsmoke -count=1 -v \
    -run 'TestDBSmoke_MigrateRunner|TestDBSmoke_LoginQuery|TestDBSmoke_AuditInsert|TestDBSmoke_AuditFieldTruncation|TestDBSmoke_AuditResourceOver50Char|TestDBSmoke_TicketInsert|TestDBSmoke_TicketsSchemaRoundTrip|TestDBSmoke_GenerateTicketNumberDayScoped|TestDBSmoke_TicketNumberRetry|TestDBSmoke_TypeConvertedModels|TestDBSmoke_TicketNumberUnique|TestDBSmoke_MigrationReapply|TestDBSmoke_MigrationNoSessionGUCLeak|TestDBSmoke_AssetJSONBDefaults|TestDBSmoke_NetBoxUpsert|TestDBSmoke_NetBoxFieldTruncation|TestDBSmoke_ZabbixFieldTruncation|TestDBSmoke_ThirdPartyFieldTruncation|TestDBSmoke_ColumnWidthMatchesConstant|TestDBSmoke_SyncAllFailureOmitsKeys|TestDBSmoke_NotificationPendingIndex|TestDBSmoke_AlertsProblemStartIndex|TestDBSmoke_AlertsTriggerIDIndex|TestDBSmoke_TicketsExternalIDIndex|TestDBSmoke_AssetsNameIndex|TestDBSmoke_AuditLogsPathIndex|TestDBSmoke_AlertBulkTransitionGuards|TestDBSmoke_AlertStatusDefault|TestDBSmoke_TicketHistory|TestDBSmoke_TicketResolvedAt|TestDBSmoke_TicketsGLPIExternalIDUnique|TestDBSmoke_GLPITimeZoneWallClock|TestDBSmoke_AlertsZabbixIdentityUnique|TestDBSmoke_ZabbixSyncOnConflict|TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter|TestDBSmoke_M38B_FirePathEnd2End|TestDBSmoke_M40_JWTDisableTakesEffect|TestDBSmoke_G21_JSONBUpdateReject|TestDBSmoke_G23_TicketTagsUpdateReject' \
    ./tests/ ) || rc=$?

if [[ "$rc" -eq 0 ]]; then
  log "② 存量升级路径: 只应用 000013 之后的迁移, 校验 role/jsonb 回填 + 回滚不丢旧列"
  ( cd "$BACKEND_DIR" && \
    PGUSER="$DB_USER" PGHOST=127.0.0.1 PGPORT="$HOST_PORT" PGDATABASE="${UPGRADE_DB}" \
    SMOKE_EXPECT_UPGRADE=1 \
    "$GO_BIN" test \
      -tags dbsmoke -count=1 -v \
      -run 'TestDBSmoke_UpgradePath|TestDBSmoke_AssetJSONBBackfill|TestDBSmoke_TicketPriorityNormalize|TestDBSmoke_DownPreservesLegacyColumns|TestDBSmoke_Migration026BlockedByDuplicates|TestDBSmoke_Migration027BlockedByDuplicates|TestDBSmoke_Migration027AllowsNullProblemStart' ./tests/ ) || rc=$?
fi

if [[ "$rc" -eq 0 ]]; then
  log "结果: ✅ 迁移(全新+升级) + 冒烟断言全部通过"
else
  log "结果: ❌ 冒烟失败 (exit=$rc) —— 上面的 Postgres 原始报错就是漂移证据"
fi
exit "$rc"
