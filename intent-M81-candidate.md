# M81-candidate — G-41 zabbix_truncated 跨语言裸字符串约定改类型/常量 + 加契约测试 (OMH ulw-loop 第 12 cycle)

> **Loop cycle**: 12 of `itmanager-grit-2026q3`
> **Loop mode**: A → B (auto-switch from PM-direct dispatch this round)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T12:49:xx+08:00 (auto-dispatched M81-candidate after M80-candidate cycle 11 ship, 10 min commit-age gate 沿用 M79 D3)
> **Scope**: backend + frontend, ≤1h estimated
> **Source**: PM_QUEUE.json M81-candidate = G-41, derived from TODO.md G-41 (M27/B 收尾自查发现, 低危)

## Goal

PM_QUEUE M81-candidate = **G-41 zabbix_truncated 跨语言裸字符串约定改类型/常量 + 加契约测试**:
1. **跨语言裸字符串 → 类型化常量**: 后端 `integration_handler.go:88` / `service.go:630` 与前端 `Settings.tsx:294` 各写一遍 `"zabbix_truncated"` 字面量 → 改名/拼写漂移会**静默失效**（前端读不到 → `?? 0` 兜底 → UI 永远显示「未截断」，运维错过 6000-1=5999 条丢告警的真相）。本 round 把这个跨语言约定固化成：
   - **后端 Go**: 包级常量 `integration.KeyZabbixTruncated` (包 `backend/internal/integration`)
   - **前端 TS**: 模块常量 `services/syncKeys.SYNC_KEY_ZABBIX_TRUNCATED` (frontend/src/services/)
2. **契约测试**: 加跨语言漂移守卫 — 任一侧常量值改了/裸字符串偷偷写回来/常量值漂离 OpenAPI spec 描述 → CI 立刻红。
3. **scope 内不动**: 顺手不修 `glpi_skipped` / `*_field_truncations` 同族裸字符串（属 M57 B-6 同型残留，本 round 沿用 TODO.md G-57 登记不修）。

## Non-goals

- **不动** `zabbix_truncated` 键本身的语义（仍是 0/1 源侧截断标志；M27/D-6 与 M33/D-6 已 ship）
- **不动** OpenAPI schema 的 `additionalProperties: {type: integer}` 自由形态（M33/D-5 已知登记不修 — `data.synced` 收口属 G-57 范围，本轮刻意不动）
- **不动** `glpi_skipped` / `netbox` / `glpi` / `*_field_truncations` 同族裸字符串（G-57 同型残留，登记不修）
- **不动** `db_smoke_test.go:2887` 的 `for _, k := range []string{...}` 列表里的 `"zabbix_truncated"` 字面量（测试 fixture 引用，约定本身不变，契约测试不覆盖这条路径）
- **不动** `Settings.test.tsx:728/749/849` 的 mock 数据里的 `zabbix_truncated`（测试夹具）
- **不动** `api.types.ts:2212` 的 description 注释里的 `zabbix_truncated`（生成产物 `gen:api` 自动同步；契约测试覆盖 `openapi.yaml` 真源，不重复锁生成产物）
- **不动** `frontend/src/pages/Settings.test.tsx` 既有 4 处 mock 字面量（它们是「模拟后端响应」的 fixture；契约是「生产代码两侧必须用同一个常量」，mock 端是「被模拟的后端」本身 — 若后端常量值变了，mock fixture 仍能模拟「旧后端返回值」，让前端的「字段缺失兜底」分支被测到）
- **不动** sing-box / keyring / OMH config / setup-profile.json / display.skin / interface (Poison 红线 + M67 standing rule)
- **不动** PM_QUEUE 既有 shipped entries
- **不**给 OMH 加新功能 / 不写新 systemd timer / service (M79 已 ship)
- **不** bypass poison-stop-gates-v1 sticky rule

## Assumptions

- ITmanager repo HEAD = `f8b9e7f` (M80-candidate cycle 11 ship), working tree clean, branch `main` up-to-date with `origin/main`
- 后端 Go module 根 = `backend/`, OpenAPI spec = `backend/internal/api/openapi.yaml:3771-3787` (SyncResult description 含 `zabbix_truncated`)
- 前端根 = `frontend/`, vitest 配置存在 (`vite.config.ts`), jsdom 环境
- `integration.KeyZabbixTruncated` 常量值约定 = `"zabbix_truncated"` (与现有所有引用一致 — 这是个纯重构, 不改协议)
- 契约测试的「同步源」选择 `backend/internal/api/openapi.yaml` 真源 (M78 已用此模式: G-15 URL-aware 占位符拒启也以 OpenAPI 为契约真源)
- 跨语言契约测试策略:
  - **Go 端测试**: 用 `os.ReadFile` 读 `frontend/src/services/syncKeys.ts` → 正则提取 `SYNC_KEY_ZABBIX_TRUNCATED = "..."` 字面量 → assert == Go 常量值
  - **TS 端测试**: 用 `fs.readFileSync` 读 `backend/internal/integration/sync_keys.go` → 正则提取 `KeyZabbixTruncated = "..."` 字面量 → assert == TS 常量值
  - 这是 G-41 的「两端常量值必须漂移同步」的最强保证 — 改一边忘另一边即红
- 既有 `Settings.test.tsx` 不退化 (4 处 mock 数据保持原样, 不动; 契约测试是**新增** file `services/syncKeys.test.ts`)
- 既有 `zabbix_truncate_test.go` 不退化 (无 API/签名变更)
- fact_store fact_id = 20 (沿用 M18 = 17 / M79 = 18 / M80 = 19 / M81 = 20 advisory 序列)
- watchdog tick 时段: PM-direct dispatch (本 round 由 Poison 触发 watchdog Mode B; 实证末态 PM_LOOP_MODE = "B", commit age ≥ 10 min 才起下一 round)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M81-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `backend/internal/integration/sync_keys.go` 新建, 含 `KeyZabbixTruncated = "zabbix_truncated"` 常量 | verify |
| `backend/internal/integration/service.go:630` 用 `KeyZabbixTruncated` 替换裸字符串 | verify (grep) |
| `backend/internal/api/handlers/integration_handler.go:88` 用 `integration.KeyZabbixTruncated` 替换裸字符串 | verify (grep) |
| `frontend/src/services/syncKeys.ts` 新建, 含 `SYNC_KEY_ZABBIX_TRUNCATED` 常量 | verify |
| `frontend/src/pages/Settings.tsx:294` 用 `SYNC_KEY_ZABBIX_TRUNCATED` 替换裸 access | verify (grep) |
| `backend/internal/integration/sync_keys_test.go` 新建, 4 类断言: 常量值 / OpenAPI 同步 / no-bare-string grep guard / cross-lang TS 同步 | verify (go test) |
| `frontend/src/services/syncKeys.test.ts` 新建, 3 类断言: 常量值 / Settings.tsx 用常量 / cross-lang Go 同步 | verify (vitest) |
| `go test -count=1 ./internal/integration/...` 全绿 | 27 packages (M78/M79 baseline 沿用) |
| `go test -count=1 ./...` 全绿 | 27 packages 全绿 |
| `vitest` 不退化 | Settings.test.tsx 既有 4 处 mock 不动 |
| **mutation inversion 实证** — bypass 常量 (改 Go 常量值 → 跨语言测试红) / 偷偷写回裸字符串 (grep guard 红) / 改 OpenAPI 描述 (同步测试红) | 实证 (见 Verification §3) |
| `M81-completion-report.md` + `M81-graph-analysis.md` + CHANGELOG + TODO 更新 | docs commit 2 |
| `git log` 2-3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. 跨语言常量值同步 (Green: 4 PASS)

```bash
$ grep -n "KeyZabbixTruncated" backend/internal/integration/sync_keys.go
const KeyZabbixTruncated = "zabbix_truncated"

$ grep -n "SYNC_KEY_ZABBIX_TRUNCATED" frontend/src/services/syncKeys.ts
export const SYNC_KEY_ZABBIX_TRUNCATED = "zabbix_truncated"

$ grep -n "zabbix_truncated" backend/internal/integration/service.go  # 应只有 1 行 (注释)
$ grep -n "zabbix_truncated" backend/internal/api/handlers/integration_handler.go  # 应只有 1 行 (注释)
$ grep -n "zabbix_truncated" frontend/src/pages/Settings.tsx  # 应只有注释 (2 处), 没有裸 access
```

任一 FAIL = 裸字符串漏改/常量值漂离。

### 2. 契约测试 (Green: 7 PASS, 跨语言 + 漂移守卫)

**Go 端 4 测试** (在 `sync_keys_test.go`):
- `TestKeyZabbixTruncated_Value` — 常量 == `"zabbix_truncated"`
- `TestKeyZabbixTruncated_InOpenAPISync` — 读 `backend/internal/api/openapi.yaml`, 断言 SyncResult description 段含 `zabbix_truncated` 字面量
- `TestKeyZabbixTruncated_NoBareStringInCode` — 读 `service.go` + `integration_handler.go` 源码, 断言无 `"zabbix_truncated"` 裸字符串字面量 (允许注释)
- `TestKeyZabbixTruncated_CrossLangWithTS` — 读 `frontend/src/services/syncKeys.ts`, 正则提取 TS 常量字面量, 断言 == Go 常量

**TS 端 3 测试** (在 `syncKeys.test.ts`):
- `SYNC_KEY_ZABBIX_TRUNCATED === 'zabbix_truncated'`
- `SyncKeys_SettingsUsesConstant` — 读 `frontend/src/pages/Settings.tsx`, 断言用 `SYNC_KEY_ZABBIX_TRUNCATED` 引用 (无裸 `['" ]zabbix_truncated['" ]` 出现于生产代码路径, 注释除外)
- `SyncKeys_CrossLangWithGo` — 读 `backend/internal/integration/sync_keys.go`, 正则提取 Go 常量字面量, 断言 == TS 常量

### 3. mutation inversion 实证 (3 反证全红 → 还原全绿)

| # | 变异 | 期望红测试 | 期望红后还原绿 |
|---|---|---|---|
| 1 | Go `KeyZabbixTruncated = "zabbix_truncated"` → `"zabbix_truncated_v2"` (故意漂离) | `TestKeyZabbixTruncated_CrossLangWithTS` (Go 端跨语言测试) + `SyncKeys_CrossLangWithGo` (TS 端跨语言测试) 双红 | 还原 → 双绿 ✓ |
| 2 | 偷偷在 `integration_handler.go:88` 写回裸字符串 `"zabbix_truncated"` (绕过常量) | `TestKeyZabbixTruncated_NoBareStringInCode` (grep guard) 红 | 还原 → 绿 ✓ |
| 3 | 改 `openapi.yaml:3776` 的 `zabbix_truncated` → `zabbix_truncated_v2` (改 spec 不改代码) | `TestKeyZabbixTruncated_InOpenAPISync` 红 | 还原 → 绿 ✓ |

3 变异 → 3 类红测试 → 3 类还原绿 = 漂移守卫真工作。

### 4. go test / vitest 全绿

```bash
$ cd backend && go test -count=1 ./internal/integration/...
ok  network-monitor-platform/internal/integration
... (no FAIL, no SKIP unexpected)

$ cd backend && go test -count=1 ./...
... 27 packages, all ok

$ cd frontend && pnpm vitest run src/services/syncKeys.test.ts src/pages/Settings.test.tsx
... all PASS, Settings.test.tsx 既有 4 处 mock 不退化
```

### 5. 风险沿用 + poison-stop-gates-v1 沿用

PM_LOOP_MODE = "B" (或 "A" 取决于 dispatch 时序) — 不 freeze ✓
commit age ≥ 10 min 沿用 M79 D3 ✓
watchdog 30-min dispatch hist 沿用 M79 D4 ✓

## Risks

- **OpenAPI yaml 真源同步**: 契约测试以 `backend/internal/api/openapi.yaml` 为 SyncResult 描述的同步源。`gen:api` (生成 `frontend/src/services/api.types.ts:2212`) 是机械产物, 不参与契约测试 (它会随 commit 重新生成)。若有人手改 `api.types.ts` 而不重跑 `gen:api`, CI 不抓 — 沿用 M33 §6 残余登记 (G-58 / G-59 同款「生成产物手动漂移」已知不修)。
- **跨语言正则解析脆弱性**: Go 测试用 `regexp.MustCompile` 提取 TS 常量字面量, TS 测试用 regex 提取 Go 常量。任一侧常量声明格式微调 (e.g. 加 const 块注释 / 改等号空格) 都会破正则 → 假红。**缓解**: 契约测试的 regex 都是字面字符匹配 (`KeyZabbixTruncated\s*=\s*"([^"]+)"` / `SYNC_KEY_ZABBIX_TRUNCATED\s*=\s*['"]([^'"]+)['"]`), 对空白宽容, 对引号宽容。**若**未来重构改了声明风格 (e.g. 改成 `const X = (...)`), 契约测试同步更新 regex (1 行改动)。
- **db_smoke_test.go:2887 不在契约测试覆盖**: 该列表是 test fixture, 不是生产代码路径。契约是「生产代码用同一个常量值」, fixture 是「模拟返回」。改 fixture 模拟「旧后端」是合法测试手段 (覆盖前端「字段缺失兜底」分支)。**缓解**: Settings.test.tsx:766 已有专门用例「后端没给 zabbix_truncated」 → 前端不炸, 已验证该兜底路径。
- **scope 漂移诱惑**: G-41 同族有 6 个裸字符串 (`zabbix_truncated` / `glpi_skipped` / `netbox` / `glpi` / `*_field_truncations` 共 4 个). 本 round 只动 `zabbix_truncated` (brief 明确). 顺手修同族 → scope creep → 不 ship。**缓解**: Non-goals 段第 3/4 条明确「不动」 + G-57 登记不修。
- **vitest jsdom fs 读 backend 文件**: vitest 默认在 jsdom env, `fs.readFileSync` 仍可用 (node builtin)。**缓解**: 测试用相对路径 `../../../backend/internal/integration/sync_keys.go` (vitest 工作目录 = frontend/).
- **Telegram CLI 不可用**: 沿用 M79. watchdog 报告写到本地文件, Poison 主动看.
- **systemd user 守护**: 沿用 M79 D1.
- **watchdog Mode B 自旋防**: 沿用 M79 D2/D4 (NOW_MIN=LAST_MIN + 30-min dispatch hist). 本 round 实证末态 PM_LOOP_MODE = "B" (Poison-triggered), commit age ≥ 10 min 才起下一 round.
- **fact_store fact_id**: 沿用 M18 = 17 / M79 = 18 / M80 = 19 / M81 = 20 advisory, 未实际落库 (M79 同款 "fact_store truth stream" 是 advisory).

## Plan

1. **写 `intent-M81-candidate.md`** (本文件, 8 节 omh-plan 骨架 — `~/.omh/skills/planner/omh-plan/SKILL.md` 模板沿用) — **feat commit 1**: `feat(M81-candidate): intent spec (omh-plan 8 节骨架, G-41 跨语言常量 + 契约测试)`.
2. **改 backend**:
   - 新建 `backend/internal/integration/sync_keys.go` (~15 行: package doc + 1 个常量 + 同族预留注释)
   - 改 `backend/internal/integration/service.go:630` (用 `KeyZabbixTruncated` 替换 `"zabbix_truncated"`)
   - 改 `backend/internal/api/handlers/integration_handler.go:88` (用 `integration.KeyZabbixTruncated` 替换 `"zabbix_truncated"`)
   - 新建 `backend/internal/integration/sync_keys_test.go` (4 测试, 见 Verification §2)
3. **改 frontend**:
   - 新建 `frontend/src/services/syncKeys.ts` (~10 行: 模块注释 + 1 个常量)
   - 改 `frontend/src/pages/Settings.tsx:294` (用 `SYNC_KEY_ZABBIX_TRUNCATED` 替换 `.zabbix_truncated`)
   - 新建 `frontend/src/services/syncKeys.test.ts` (3 测试, 见 Verification §2)
4. **跑测试 + mutation inversion**:
   - `cd backend && go test -count=1 ./internal/integration/...` → 全绿
   - `cd frontend && pnpm vitest run src/services/syncKeys.test.ts src/pages/Settings.test.tsx` → 全绿
   - mutation inversion 3 反证 (bypass / 漂离 / spec 漂离) 全红 → 还原全绿
5. **写 docs**: `M81-completion-report.md` + `M81-graph-analysis.md` + `CHANGELOG.md` M81 段 + `TODO.md` G-41 完成条目 — **docs commit 2**: `docs(M81-candidate): completion + graph analysis + CHANGELOG + TODO`.
6. **commit + push** 2 commits 到 origin/main.
7. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证).

### Commit 序列

```
f8b9e7f (HEAD, M80-candidate)
   ↓
M81 commit 1: feat(M81-candidate): intent spec (omh-plan 8 节骨架)
M81 commit 2: feat(M81-candidate): G-41 zabbix_truncated 跨语言常量 + 契约测试 (sync_keys.go + syncKeys.ts + 2 契约测试)
M81 commit 3: docs(M81-candidate): completion + graph analysis + CHANGELOG + TODO
```

(2-3 commits 即可, 沿用 M78 / M79 / M80 pattern.)

## Decision gate

- **D1**: scope = **backend + frontend**, 仅 G-41 一个跨语言常量 + 2 契约测试, **不**顺手扩到 G-57 同族 6 个裸字符串 (Poison 红线: brief 明确 zabbix_truncated) ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min ✓
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4) ✓
- **D5**: mutation inversion = 3 反证 (bypass / 漂离 / spec 漂离) 全红 → 还原绿 ✓
- **D6**: 不写新 fact_store entry (M79 同款 advisory, 未实际落库) ✓
- **D7**: 2-3 commits 即可 (沿用 M78 / M79 / M80) ✓
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证 (Poison 看, watchdog 不读) ✓
