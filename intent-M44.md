# intent-M44: G-30 db_smoke 密码泄漏收紧 (DSN/.pgpass/env-file)

## Context

`TODO.md` G-30:
> `db_smoke.sh:96` 用 `docker run -e POSTGRES_PASSWORD="$DB_PASS"`: `-e` 的
> 值进 `docker run` 的 argv, 同机任意用户 `ps aux` 即可看到
> (`/proc/<pid>/cmdline` 默认全局可读). `:86-87` 外部模式的 `PGPASSWORD=
> "$DB_PASS" psql`, `:175-176` 构造 + `:181-190` 的 `TEST_DATABASE_URL=
> "postgres://user:***@…"` 落在子进程环境里 (同 uid 可读 `/proc/<pid>/environ`).

**实际评估**: 影响面限本地一次性冒烟库 + DB_PASS 默认固定 `smoke_pw` (低危);
但**写法不安全**, 改 `--env-file` / `.pgpass` 是行业标配. ≤1.5h, 单文件改动
(`scripts/db_smoke.sh`) + 加 `.pgpass` 模板 + go test `TEST_DATABASE_URL` 重构.

## 5 个泄漏点 (PM-direct 评估)

1. **L96 `docker run -e POSTGRES_PASSWORD="$DB_PASS"`**
   → argv 泄漏 (`/proc/<pid>/cmdline` 默认全局可读).
   修法: `--env-file <(mktemp)` (0600, trap rm).

2. **L86/L87 外部模式 `PGPASSWORD="$DB_PASS" psql/createdb`**
   → env 泄漏 (同 uid `/proc/<pid>/environ`).
   修法: 改 `.pgpass` 文件 + `PGPASSFILE=<path>` env 引用 (路径不泄值).

3. **L175-176 升级路径 psql `PGPASSWORD="$DB_PASS"`**
   → 同 #2.

4. **L181-190 `TEST_DATABASE_URL=postgres://user:***@…`**
   → DSN 含明文密码进 env.
   修法: 改 go test 单独 env vars (PGUSER/PGHOST/PGPORT/PGDATABASE) +
   `PGPASSWORD` 走 `PGPASSFILE` 引用. go test `cmd/server` 的 DSN 解析
   看 `internal/database/database.go` 是否已支持.

5. **L193/L194 DSN 拼装含密码** (虽然 L198 log 是 `***`, 但 FRESH_DSN 变量本身明文)
   → 同 #4. 修法: DSN 拼装仅日志时脱敏 (`printf` mask).

## Approach

### Step 1: docker run 改 env-file (≤15min)

`db_smoke.sh` 起容器改成:
```bash
ENVFILE=$(mktemp -t dbsmoke-env.XXXXXX)
chmod 0600 "$ENVFILE"
printf 'POSTGRES_PASSWORD=%s\nPOSTGRES_DB=%s\n' "$DB_PASS" "$DB_NAME" > "$ENVFILE"
trap 'rm -f "$ENVFILE"' EXIT
${DOCKER:-docker} run -d --name "$CONTAINER" \
  --env-file "$ENVFILE" \
  -p 127.0.0.1::5432 "$IMAGE" >/dev/null
```

### Step 2: 写 `.pgpass` 模板 + `PGPASSFILE` (≤20min)

`scripts/db_smoke.sh` 起容器后 / 外部模式入口:
```bash
PGPASS=$(mktemp -t pgpass.XXXXXX)
chmod 0600 "$PGPASS"
printf '%s:%s:%s:%s:%s\n' \
  "$PGHOST" "$PGPORT" "$DB_NAME" "$DB_USER" "$DB_PASS" > "$PGPASS"
trap 'rm -f "$PGPASS"' EXIT
export PGPASSFILE="$PGPASS"
```

`psql_q` / `createdb_q` 不再 set `PGPASSWORD`, 走 `PGPASSFILE` 即可.

### Step 3: go test `TEST_DATABASE_URL` 重构 (≤30min)

看 `backend/internal/database/database.go` DSN 解析是否支持拆解 env vars:
- 如果支持 (`PGUSER/PGHOST/PGPORT/PGDATABASE/PGPASSWORD` 拆解), 改 `db_smoke.sh`:
  ```bash
  unset TEST_DATABASE_URL  # 不传 DSN
  export PGUSER="$DB_USER" PGHOST="127.0.0.1" PGPORT="$HOST_PORT" PGDATABASE="$DB_NAME"
  export PGPASSWORD_FILE="$PGPASS"  # 或走 PGPASSFILE
  ```
- 如果不支持, 改 `database.go` 加拆解逻辑 (扩 1 个文件, 估 ≤30min).

**验证**: 真 PG db_smoke 跑通 (47 PASS / 0 FAIL) + grep 确认无明文密码在 env/argv.

### Step 4: 验证 (≤10min)

- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` → 47 PASS / 0 FAIL
- `ps auxf | grep docker` 时容器 cmdline 不含 POSTGRES_PASSWORD
- `strings /proc/<pid>/environ | grep smoke_pw` 无命中 (容器内)

### Step 5: docs (≤10min)

- `M44-completion-report.md`
- `CHANGELOG.md` M44 section
- `TODO.md` G-30 标 done

## Acceptance Criteria

AC-M44-1: `docker run` argv 不含 `POSTGRES_PASSWORD=...` (`ps auxf` 验证)
AC-M44-2: 真 PG db_smoke 47 PASS / 0 FAIL (不破现有断言)
AC-M44-3: env vars 不含明文 `smoke_pw` / `DB_PASS` (grep 验证)
AC-M44-4: `internal/database/database.go` DSN 解析支持拆解 env vars (若需扩)
AC-M44-5: 临时文件 (`ENVFILE` / `PGPASS`) 0600 权限 + `trap rm` 清理

## Trade-offs

- **DSN → env vars 拆解**: 改 `database.go` 是 backend 文件, 不动 ops 外部接口,
  但**改的是 startup 配置链路**, 影响所有 `cmd/server` 启动. PM-direct 边界内
  (≤1.5h, 单 backend 文件).
- **CI 影响**: `.github/workflows/ci.yml` 的 dbsmoke job 也走 `scripts/db_smoke.sh`,
  自动同步, 无额外改动.
- **外部 Postgres 模式** (`SMOKE_PG_HOST` 路径): 同样改, 用户体验不变.

## Validation plan

1. Pre-flight: 读 `internal/database/database.go` DSN 解析 (grep `TEST_DATABASE_URL` /
   `os.Getenv`), 决定 step 3 改 db_smoke 还是改 database.go.
2. 改完跑 `db_smoke.sh` → 47 PASS / 0 FAIL.
3. `ps auxf | grep docker` 容器启动后 (异步 sleep 1) 验证 cmdline 无密码.
4. `strings /proc/$(pgrep -f postgres:18-alpine | head -1)/environ | grep -E 'smoke_pw|DB_PASS'`
   → 应无命中.

## Commits (planned)

1. `docs(M44): intent-M44.md`
2. `fix(M44): db_smoke docker run argv 泄漏 — POSTGRES_PASSWORD 改 --env-file`
3. `fix(M44): psql / createdb / upgrade 路径 PGPASSWORD 走 .pgpass + PGPASSFILE`
4. `fix(M44): TEST_DATABASE_URL 拆解 PG* env vars (database.go DSN 解析支持)`
5. `docs(M44): CHANGELOG + completion report + TODO G-30 标 done`

## Status

- Scope: PM-direct (≤1.5h)
- Author: hermes@local
- Branch: main
- Pre-flight: G-30 in TODO.md, db_smoke.sh 已 ship 47 PASS baseline
- pm-tick 已 surface (side-path ready: G-30)
