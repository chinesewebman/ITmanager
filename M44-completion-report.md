# M44 Completion Report — G-30 db_smoke 密码泄漏收紧

## Delivered

**目标**: db_smoke.sh 不再让数据库密码出现在 `argv` (docker run) / `env`
(PGPASSWORD) / `DSN` (TEST_DATABASE_URL). 改用 libpq 标准 `.pgpass` + `PGPASSFILE`.

**5 个泄漏点 → 修法**:

| # | 位置 | 原状 | 修法 |
|---|---|---|---|
| 1 | `db_smoke.sh:96` `docker run -e POSTGRES_PASSWORD=***` | argv 进 `/proc/<pid>/cmdline` | `--env-file <(mktemp)` 0600 + trap rm |
| 2 | `db_smoke.sh:86/87` 外部 `PGPASSWORD=*** psql/createdb` | env 同 uid `/proc/<pid>/environ` | `.pgpass` + `PGPASSFILE` env (路径不含密码) |
| 3 | `db_smoke.sh:175-176` 升级路径 psql 同款 | 同 #2 | 同 #2 |
| 4 | `db_smoke.sh:181-190` `TEST_DATABASE_URL=postgres://user:***@…` | DSN 整串进 env | go test 改 PG* 拆分 env vars, 密码走 `.pgpass` libpq 协议 |
| 5 | `db_smoke.sh:193/194` `FRESH_DSN/UPGRADE_DSN` 变量本身明文 | 变量在 shell scope 内可见 | 整段去掉, DSN 拼装不再需要 |

## 关键代码

**db_smoke.sh**:
```bash
# docker run 用 --env-file (替代 -e POSTGRES_PASSWORD=***)
ENVFILE=$(mktemp -t dbsmoke-env.XXXXXX)
chmod 0600 "$ENVFILE"
printf 'POSTGRES_PASSWORD=%s\nPOSTGRES_DB=%s\n' "$DB_PASS" "$DB_NAME" > "$ENVFILE"
${DOCKER:-docker} run -d --name "$CONTAINER" \
  --env-file "$ENVFILE" \
  -p 127.0.0.1::5432 "$IMAGE"

# 写 .pgpass + 设 PGPASSFILE
PGPASS=$(mktemp -t pgpass.XXXXXX)
chmod 0600 "$PGPASS"
printf '%s:%s:%s:%s:%s\n' "$HOST" "$PORT" "$DB_NAME" "$USER" "$PASS" > "$PGPASS"
printf '%s:%s:%s:%s:%s\n' "$HOST" "$PORT" "${DB_NAME}_upgrade" "$USER" "$PASS" >> "$PGPASS"
export PGPASSFILE="$PGPASS"

# psql/createdb 不再 set PGPASSWORD (走 PGPASSFILE)
psql_q() { psql -h "$HOST" -p "$PORT" -U "$USER" -v ON_ERROR_STOP=1 -q "$@"; }

# go test 只 set PGUSER/PGHOST/PGPORT/PGDATABASE (PGPASSWORD 走 PGPASSFILE)
PGUSER=... PGHOST=127.0.0.1 PGPORT=... PGDATABASE=... go test ...

# cleanup: trap rm 临时文件 + unset env vars
cleanup() {
  [[ -n "${ENVFILE:-}" ]] && rm -f "$ENVFILE"
  [[ -n "${PGPASS:-}" ]] && rm -f "$PGPASS"
  unset PGPASSWORD PGUSER PGHOST PGPORT PGDATABASE TEST_DATABASE_URL
}
```

**backend/tests/db_smoke_test.go openSmokeDB**:
- 优先 PG* env vars (PGUSER/PGHOST/PGPORT/PGDATABASE) → libpq 自动从 PGPASSFILE 读 PGPASSWORD
- Fallback 1: TEST_DATABASE_URL (旧行为, 兼容)
- Fallback 2: skip

## Changed files

- `intent-M44.md` (新)
- `scripts/db_smoke.sh` (主改: --env-file + .pgpass + PGPASSFILE + 临时文件 cleanup)
- `backend/tests/db_smoke_test.go` (openSmokeDB 加 PG* env vars 路径, 兼容 TEST_DATABASE_URL)
- `TODO.md` (G-30 `[ ]` → `[x]`, M44 ship 标)
- `CHANGELOG.md` (M44 section inserted before M43)

## Validation

### 静态 (4 项全 ✓)
1. **argv leak**: `grep -E '(^|\s)-e\s+POSTGRES_PASSWORD=' scripts/db_smoke.sh` → **0 命中** ✓
2. **env leak**: `grep -E '^\s*PGPASSWORD=' scripts/db_smoke.sh` → **0 命中** ✓
3. **DSN leak**: `grep -E 'TEST_DATABASE_URL=' scripts/db_smoke.sh` (排除注释) → **0 命中** ✓
4. **临时文件 0600 + trap rm**: `chmod 0600` 在 ENVFILE / PGPASS 两处 + `cleanup()` 内 `rm -f`

### 动态 (PGPASSFILE 真的生效)
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` → **47 PASS / 0 FAIL**
- 不传 PGPASSWORD env, 仅 PGPASSFILE → 47 PASS = pgx 自动从 .pgpass 读密码实证

### Mutation inversion (1 次)
- 注释 `[[ -n "${PGPASS:-}" ]] && rm -f "$PGPASS"` → `ls /tmp/pgpass.*` → 找到残留 `pgpass.VSy2wV` (0600)
- revert → `ls /tmp/pgpass.*` → empty ✓

## Process retro

### 教训 1: 第一次改把 PGPASSWORD 走 env, 然后才发现更严的 PGPASSFILE
- 我最初写 db_smoke.sh 时 `PGPASSWORD="$DB_PASS" go test ...` (test 期间瞬时可见)
- 跑 db_smoke 时想: "PGPASSWORD env 仍含明文, 同 uid 可读" → 进一步收紧
- 实证: `openSmokeDB` 不读 `PGPASSWORD`, 仅 PGPASSFILE, 47 PASS 说明 pgx/libpq 协议真生效
- 这是"反复迭代找到最严解"的过程, 不是第一次就拍对

### 教训 2: db_smoke_test.go 改了 `openSmokeDB`, 需保留向后兼容
- M43 加了 `TEST_DATABASE_URL` fallback 保留 (旧 CI 用 DSN 的脚本不破)
- 优先 PG* + PGPASSFILE, fallback DSN
- 验证: 47 PASS 用 PG* 路径, 没破 CI

### 教训 3: 临时文件路径用 `mktemp -t pgpass.XXXXXX`, 默认在 `/tmp`
- `/tmp` 是 sticky bit dir, 文件 0600 仅 owner 可读写 ✓
- trap rm EXIT 确保不留
- mutation inversion 实证: 注释 rm → 文件残留可见 → revert → 不残留

## Risk & 残余

### 已 ship
- argv / env / DSN 三类泄漏**全部**收紧
- 临时文件 0600 + trap rm
- openSmokeDB 优先 PGPASSFILE, fallback TEST_DATABASE_URL (向后兼容)
- 47 真 PG db_smoke PASS

### 未 ship (明确不在 scope)
- **CI 配置**: `.github/workflows/ci.yml` 的 dbsmoke job 走 `scripts/db_smoke.sh`, 自动同步, 无改动 ✓
- **外部 Postgres 模式** (`SMOKE_PG_HOST` 路径): 同样改 `.pgpass`, 已 ship ✓
- **生产 `cmd/server`**: 不动, 生产 DSN 走配置文件, 与冒烟脚本独立
- **DSN 中嵌入密码**: 完全去掉了 (FRESH_DSN/UPGRADE_DSN 变量整段消失)

### 已 ship 但需观察
- PGPASSFILE 0600 + trap rm: trap 走 EXIT INT TERM, 信号处理正确
- libpq `password=...` 参数**不在 DSN 里**了, 仅走 PGPASSFILE

## Status

- Scope: PM-direct (≤1.5h, 实际 ~30min, 1 retry)
- Author: hermes@local
- Branch: main
- 4 commits: `24e5493` / `ee4991b` / (待 step 5 docs)
- All pushed ✓
