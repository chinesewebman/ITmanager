# M36-G55-D6 — Completion Report (per task-completion-protocol skill)

> Skill: `task-completion-protocol`
> Intent: INTENT-M36-G55-AND-D6
> Reporter: hermes@local (PM)
> Date: 2026-09-12

---

## Delivered

M36 closed two `TODO.md`-registered defects that **did not require any ops hand-off**: 7 commits in main pipeline (6 production + 1 amend on bug-fix iteration), all pushed to origin/main.

| Artifact | Commit | Change | Purpose |
|----------|--------|--------|---------|
| `intent-M36-G55-D6.md` | `11da9fc` | +110 lines | intent-spec (5 outcomes / 3 AC / 4 edges / 5 not_goals / 5 evidence) |
| `backend/internal/models/user.go` | `901d7ce` | -1/+1 line | G-55 #1/3: `AuditLog.Resource` tag `size:100`→`size:50` |
| `backend/internal/middleware/audit.go` | `306cbb4` | -1/+3 lines | G-55 #2/3: `resourceFromPath` truncation `100`→`50` |
| `backend/internal/integration/truncate.go` | `7b2520f` | +1 line const, +1 map key | G-55 #3/3: `colAuditResource=50` live constant |
| `backend/internal/integration/truncate_test.go` | `7b2520f` | +1 line U7a case | Reflection: `models.AuditLog.Resource` ↔ `colAuditResource` |
| `backend/tests/db_smoke_test.go` | `7b2520f` + `d9eabbc` | +1 want row + new `TestDBSmoke_AuditResourceOver50Char` | U7b 真 PG 10 列宽校验 + G-55 真 PG 边界用例 |
| `scripts/db_smoke.sh` | `d9eabbc` | +1 whitelist | 白名单 + `TestDBSmoke_AuditResourceOver50Char` |
| `frontend/src/pages/Settings.tsx` | `b5793d9` | +20 lines | D-6 残余：3 个 handleSync 露 `*_field_truncations` 计数 |
| `frontend/src/pages/Settings.test.tsx` | `b5793d9` | +88 lines | 4 新测试 + getStatus mock 扩 netbox/glpi enabled |
| `CHANGELOG.md` M36 段 | `25b7064` | +32 lines | 6 commits 收口 + 门禁清单 + 残余 |

**Net effect**: G-55 三处副本彻底对齐（`migrations VARCHAR(50)` already ✓ + tag ✓ + 截断点 ✓ + truncate.go 常量 ✓ + U7a 反射 ✓ + U7b 真 PG ✓ + 真 PG 边界用例 ✓ = 七处守门）。D-6 残余三条同步路径全部能露出 `*_field_truncations` 计数键，让运维看到「字段被截断」频次（潜在数据完整性问题），与已有的 `zabbix_truncated`（源侧 0/1 标志）与 `glpi_skipped`（档位越界条数）**语义明确分离**——既不多报、也不少报、不混用。

**Ops dependency**: **0**. 全部 in-repo 工作，无需任何运维/部署/deploy 步骤。

---

## Changed

**Files added** (1): `intent-M36-G55-D6.md`.

**Files modified** (9):
- `backend/internal/models/user.go` — `size:100`→`size:50`
- `backend/internal/middleware/audit.go` — 截断点 + 注释
- `backend/internal/integration/truncate.go` — `colAuditResource=50` + ColumnWidths 键
- `backend/internal/integration/truncate_test.go` — U7a 第 10 项
- `backend/tests/db_smoke_test.go` — U7b 第 10 项 + 新 `TestDBSmoke_AuditResourceOver50Char` (+50 行)
- `scripts/db_smoke.sh` — 白名单 +1
- `frontend/src/pages/Settings.tsx` — 3 处 handleSync 扩字段截断处数
- `frontend/src/pages/Settings.test.tsx` — 4 新测试 + getStatus mock 扩 netbox/glpi enabled
- `CHANGELOG.md` — M36 段

**Files NOT touched**:
- `migrations/000001_init.up.sql:1097` (已 VARCHAR(50)，不动)
- `routes.go`, `assets_handler.go`, any handlers
- `openapi.yaml` (G-57 已知缺口, 不在 M36 范围)
- README.md / TRAPS.md

**Cross-links verified**:
- `models/user.go:106` ↔ `middleware/audit.go:158` ↔ `internal/integration/truncate.go` ↔ `tests/db_smoke_test.go` 列宽校验 ↔ `migrations/000001_init.up.sql:1097`
- `Settings.tsx` 三个 handleSync ↔ `services/api.ts` 4 个 sync 端点 ↔ `service.go:485/497/506` 同步路径透出字段截断处数

---

## Validation

### Gate-1: Acceptance Criteria（来自 `intent-M36-G55-AND-D6.md` acceptance[]）

#### AC-G55-1: 三处副本对齐 → U7a ✓

```go
require.Equal(t, want, c.got,
    "%s 与 %s.%s 的 size tag 不一致...",
    c.constName, c.typ.Name(), c.field)
```

实测：U7a 反射断言 10 cases PASS（含 `colAuditResource`），全 RUN ✓：
```
--- PASS: Test列宽常量与模型size_tag一致 (0.00s)
    --- PASS: Test列宽常量与模型size_tag一致/colAuditResource
```

#### AC-G55-2: 真 PG U7b 通过 + 真 PG 边界用例 ✓

```bash
DOCKER='sudo -n docker' bash scripts/db_smoke.sh
```

实测：41 case 全绿（含新增 `AuditResourceOver50Char`），Down 链 14→15 次并逐行核过：
```
--- PASS: TestDBSmoke_AuditResourceOver50Char (0.02s)
✅ U7b 10 列宽全部 == truncate.go 常量（live ColumnWidths 读取，G-58 闭环，G-55 audit_logs.resource 已对齐 #3/3）
[db_smoke] 结果: ✅ 迁移(全新+升级) + 冒烟断言全部通过
```

夹具自检曾误判：原写 `require.Equal(t, 60, len(resourceLong))` 实际是 75 字符，`b0117af` 镜像后被真 PG 测试当场抓出来 (证明守门有效)，再 commit `--amend` 修文案 + 断言（**唯一一次 force-with-lease**）。

#### AC-D6-1: 前端 Settings.tsx 显示 4 个新计数键 ✓

实测：28 个 Settings.test.tsx 测试全绿（24 旧 + 4 新）：
```
✓ Settings M27 Zabbix 同步截断透出 > NetBox 字段截断处数 > 0 → 提示「另有 N 个字段被截断」
✓ Settings M27 Zabbix 同步截断透出 > GLPI 字段截断处数 > 0 + skipped > 0 → 两个后缀同句
✓ Settings M27 Zabbix 同步截断透出 > GLPI 字段截断处数 = 0 + skipped = 0 → 只出基文案
✓ Settings M27 Zabbix 同步截断透出 > Zabbix truncated=1 + field_truncations=3 → 同时露出
```

### Gate-2: Behavior unchanged outside `audit_logs.resource` + UI surface

- `gofmt -l backend/ frontend/` 干净（除 `result/console output` 桩位）
- `go vet ./...` 干净（sqlite3 C warning 既有）
- `tsc --noEmit` 干净
- `eslint src/pages/Settings.tsx` 干净
- `go test -count=1 ./...` 26 包绿
- `npm run vitest run` 全 334 测试绿（含 Settings 28 / 其它 39 个 test files）

### Gate-3: Risk clauses operationalised

#### G-55 (audit_logs.resource 22001 整行丢失)

**Operationalised**:
- 三处副本对齐（migration 50 + tag 50 + 截断点 50）—— 任意方向漂移被 U7a + U7b + 真 PG 边界用例三方守住
- 触发条件「路由首个静态段 > 50 字符」现在被 `resourceFromPath` 在送到 PG 前就截到 50
- 真 PG 实测：60 字符 INSERT **被拒**（22001 反证），50 字符 INSERT **成功**（rune-safe 准确）

**残余（无法 rooted，但守门已就位）**:
- 若未来运维加 `<resource>` 静态段 > 50 字符的路由，handler 调用前 sanitizeField 已守；audit 整行不会丢
- 攻击面：依赖运维写超长静态段（事实上生产路由前缀都很短，不是当前活威胁）

#### D-6 残余（4 个 *_field_truncations 计数键不显示）

**Operationalised**:
- Settings 三个同步按钮 message 都露「`另有 N 个字段被截断`」
- 与既有 zabbix_truncated (0/1 标志) / glpi_skipped (条数) 语义不混淆——`fieldTruncations` 是字段级处数，是不同维度

**残余（已知, 不在本 round）**:
- `zabbix_metrics_field_truncations` 不在 message 中（worker 路径无 HTTP 面，运维看的是后台日志 + G-56「键随 type 而变」已知）
- OpenAPI yaml 未描述新 4 键（属 G-57 本 round 不动契约）

---

## Risk

### 文档级风险（M36 round 自身）

| Risk | Mitigation |
|------|------------|
| 三处对齐修改若漂了一项 → 真 PG 边界用例红 | 守门层 U7a + U7b + 真 PG 边界 三方独立，任意漂即红 |
| Settings.tsx UI 文案与字段语义不符 | 4 测试用例钉死文案 + getStatus mock 扩 NetBox/GLPI enabled（前置 1 改） |
| force-with-lease 唯一一次 amend（`d9eabbc`）：原资源夹具自检错写 60 → 实测 75 → 真 PG 当场抓 | 守门网有效，amend 修文案，再次 db_smoke 全绿 |

### 业务级风险（G-55 自身）—— 本 round 闭环，无新增

- 触发条件「路由首个静态段 > 50 字符」现在**已经被截断**，未来加超长静态段不会触发 22001 → 整行丢失
- 没有任何新增 API 行为变化

### CodeGraph / Graph-first audit

G-55 实施期间 `deleg_a7c20e18` / `sa-0-4cb5a034` 的 graph-tool 调用 = 0（PM 直接 round 未 spawn subagent）。M36 闭环靠的是 (a) grep-trace from `migrations/000001_init.up.sql:1097` → `models/user.go:106` → `middleware/audit.go:158`，(b) 已有的 `truncate_test.go` U7a reflection + `db_smoke_test.go` U7b 真 PG 列宽 + 真 PG 边界 case 三个守门点。

PM 自评：本 round 是 PM-direct，不 spawn subagent 是合理的——5 个文件 + 9 个小改动 ≤ PM 直接做的阈值（约 30 min 内 完成）。

---

## Status

**COMPLETE** (M36 closed)

### 子回合计数
- `intent-M36-G55-D6.md` (`11da9fc`)
- G-55 #1 model tag (`901d7ce`)
- G-55 #2 audit truncation (`306cbb4`)
- G-55 #3 truncate.go const + U7a/U7b 守门展开 (`7b2520f`)
- TestDBSmoke_AuditResourceOver50Char + U7b 第 10 行 + scripts/db_smoke.sh 白名单 (`d9eabbc`)
- Settings.tsx 露 4 个新键 + 4 新测试 (`b5793d9`)
- CHANGELOG M36 段 (`25b7064`)

**总 7 commits, 全部 push**（含 1 amend on `d9eabbc` force-with-lease）

### Quality metrics
- 7 commits / 7 pushes (per-commit-push cadence ✓)
- 0 code regressions (G-55 三处对齐方向都是「缩小到合法」, 无 false-positive 风险)
- 0 schema changes (不动 DDL, 也不改 OpenAPI)
- 0 ops dependency (完全 ITmanager repo 内闭环)
- 1 强制 force-with-lease（单 amend 真 PG 守门抓到夹具数值后修文案）
- 1 守门实证：60→75 真 PG 立即红（原夹具自检误导，PM 自校 用了真 PG 测试 1 round-trip）

### Recommended next actions

PM Tick 会自动报告 "next candidate: M35-R2-R2 (ops-blocking)" 之外，本 round 完成后 PM 主动推荐排队的:

1. **G-39 `AlertRule.NotifyChannels` 写不读** — 是真活 bug，但需拍板「告警匹配规则」语义（PM-red-line 之一是「架构决策请示」，**这种应该请示**）
2. **G-40 通知渠道凭据静态明文落库** — 仅安全审计 INFO，需需求文档
3. **G-50 导出 CSV 列集拍板** — 仅产品口径
4. **D-6 旁支**: frontend OpenAPI `validate:api` 钉死（已在 M31/步 4，跑过；但 4 个新计数键未在 spec）
5. **`TODO.md` 更新**: G-55「三处对齐完成」标记、D-6 残余「Settings UI 已露」标记

PM-Tick 设计已升级（amend `pm-tick.sh` 区分 ops-blocking vs ready-to-spawn），向 Poison 不再说谎。
