# M37-A — Completion Report (per task-completion-protocol skill)

> Skill: `task-completion-protocol` v0.1.0
> Intent: INTENT-M37-A-NotifyChannelsWorker (intent-M37-A.md)
> Reporter: hermes@local (PM)
> Date: 2026-09-12
> Round: M37-A (part of Option E = M37-A + M38-B; Poison 拍板 2026-09-12 23:35)

---

## Delivered

G-39 (`AlertRule.NotifyChannels` 写不读) 运维主动 ResolveAlert 路径已 ship。运维在 AlertRule 编辑页勾 channel 时，只有被勾的会收到 webhook；未勾的 0 次推送。历史 alert（`alert_rule_id=NULL`）行为零变化。6 commits in main pipeline，全部 push to origin/main。

| Artifact | Commit | Purpose |
|----------|--------|---------|
| `intent-M37-A.md` | `4e10de7` | +222 行 intent-spec (5 outcomes / 5 AC / 5 edges / 6 not_goals / 7 op constraints / 6 commits plan) |
| `models/alert.go` | `f7d2c2a` | +5 行：AlertRuleID `*uuid.UUID` 字段 + gorm tag + json tag (修复模型↔DB 漂移) |
| `notification/worker.go` (struct + handler) | `f7d2c2a` + `85c148f` | AlertEventPayload +2 字段（RuleID, NotifyChannelIDs）；handler 加过滤分支 |
| `notification/worker.go` (helper) | `85c148f` | +30 行 `filterChannelsByIDs` helper + 1 行 HandleAlertEventForTest exported wrapper |
| `service/alert_service.go` | `71f946b` | +41 行 `loadRuleNotifyChannelIDs` helper + ResolveAlert publish 改用 struct |
| `notification/notification_test.go` | `f36b900` | +126 行：6 个新单元测试 |
| `service/alert_service_test.go` | `f36b900` | +77 行：4 个新单元测试 |
| `tests/db_smoke_test.go` | `71ac5c3` | +143 行：1 个真 PG 端到端用例 + 1 helper + 1 mock sender |
| `notification/sender.go` | `71ac5c3` | +1 行注释 (GetCustomSendersForTest 已删，因真 PG 测试走 Resolver 不需注册表) |
| `scripts/db_smoke.sh` | `71ac5c3` | whitelist +1 (M37A test) |
| `CHANGELOG.md` (本 round) | 6th commit | +38 行 M37-A 段 (5-section + 决策拍板 + 残余) |

**Net effect**: G-39 写不读 bug 的 ResolveAlert 路径彻底闭环。运维编辑 AlertRule → ResolveAlert 触发 → 只推被勾的 channels。同时修复 GORM 模型↔DB 漂移（DB 已有 alert_rule_id 列但 GORM 看不见），让历史 alerts 也能被关联到 rule（为 M38-B fire 路径打底）。

**Ops dependency**: 0 (完全 in-repo 工作)。

---

## Changed

**Files added** (1): `intent-M37-A.md`.

**Files modified** (7):
- `backend/internal/models/alert.go` — AlertRuleID 字段
- `backend/internal/notification/worker.go` — AlertEventPayload + handler + 2 helpers
- `backend/internal/notification/sender.go` — 注释
- `backend/internal/notification/notification_test.go` — 6 新测试
- `backend/internal/service/alert_service.go` — ResolveAlert publish + loadRuleNotifyChannelIDs
- `backend/internal/service/alert_service_test.go` — 4 新测试
- `backend/tests/db_smoke_test.go` — 1 新真 PG 测试 + 1 helper + 1 mock sender
- `scripts/db_smoke.sh` — whitelist +1

**Cross-links verified**:
- DB migration 000001 (`alerts.alert_rule_id UUID REFERENCES alert_rules(id)`) ↔ `models.Alert.AlertRuleID` ↔ 真 PG `information_schema.columns` 列存在性
- `AlertRule.NotifyChannels` (JSON text) ↔ `loadRuleNotifyChannelIDs` 解析 ↔ `AlertEventPayload.NotifyChannelIDs` ↔ `filterChannelsByIDs` ↔ `worker.handleAlertEvent` 过滤
- 旧 alert (`alert_rule_id=NULL`) ↔ ResolveAlert payload `NotifyChannelIDs == nil` ↔ worker fallback 全启用 channels

---

## Validation

### Gate-1: Acceptance Criteria（来自 `intent-M37-A.md`）

#### AC-M37-A-1: rule 有 2 channels（应发 2）✓

**Given**: enabled AlertRule `notify_channels=["ch-A","ch-B"]`；3 条 enabled notification_channels；alert 对应该 rule。

**When**: `alert_service.ResolveAlert` 被调用 → publish `AlertEventPayload`。

**Then**: `worker.handleAlertEvent` 仅调 sender.Send 2 次（chA + chB），chC 0 次。

**Evidence**:
- 单元: `TestHandleAlertEvent_按Rule过滤_发被勾的Ch` PASS (sqlmock, hits=2)
- 真 PG: `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter` 场景 ② PASS (m37aIDs=[chA.ID,chB.ID], len=2, chC 0 次)

#### AC-M37-A-2: 旧 alert (rule_id=NULL) — fallback 全发 ✓

**Given**: 旧 alert (`alert_rule_id=NULL`)，eventbus payload 没 RuleID 字段。

**When**: alert 触发 ResolveAlert。

**Then**: worker 走 fallback，推全启用 channels（与改动前一致）。

**Evidence**:
- 单元: `TestHandleAlertEvent_RuleID空_走fallback全发` PASS (sqlmock, hits=3)
- 真 PG: `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter` 场景 ③ PASS (m37aIDs 全 3 个, len=3)
- service: `TestAlertService_LoadRuleNotifyChannelIDs_规则不存在返nil` PASS (rule 不存在 → 返 nil → worker fallback)

#### AC-M37-A-3: rule 显式空（应发 0）✓

**Given**: AlertRule 存在但 `notify_channels=""`。

**When**: alert 触发 ResolveAlert。

**Then**: worker 推 0 次。

**Evidence**:
- 单元: `TestHandleAlertEvent_Rule显式空_推0次` PASS (sqlmock, hits=0)
- service: `TestAlertService_LoadRuleNotifyChannelIDs_显式空返空切片` PASS (返 `[]string{}` 非 nil, worker 收到后直接推 0)
- 真 PG: `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter` 场景 ① PASS (m37aIDs 空)

#### AC-M37-A-4: GORM 模型 ↔ DB 漂移修复 ✓

**Given**: DB alerts 表已有 `alert_rule_id` 列（migration 000001）；models.Alert 原本没 AlertRuleID。

**When**: M37-A 完成后。

**Then**: `models.Alert.AlertRuleID *uuid.UUID` 字段存在；GORM 用它能 SELECT/INSERT。

**Evidence**:
- 真 PG: `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter` 头部 `information_schema.columns` 断言 column 存在 ✓
- 真 PG 末尾 roundtrip: `db.Create(&roundtrip)` (AlertRuleID=&ruleTwo.ID) → `db.First(&got)` 回读 → got.AlertRuleID 非 nil 且 == ruleTwo.ID ✓

#### AC-M37-A-5: 全部 gate exit 0 ✓

- `go vet ./...` exit 0 (sqlite3 C warning 系既有)
- `gofmt -l` 干净 (gofmt binary at `/home/webman/.local/share/mise/installs/go/1.25.14/bin/gofmt`, 不用 mise shim)
- `go test -count=1 ./internal/{notification,service}/...` 全绿 (含 10 个新单元测试)
- `go test -tags dbsmoke` 真 PG: **42 cases PASS / 0 FAIL** (含新增 `M37A_AlertRuleNotifyChannelsWorkerFilter`)

### Gate-2: Mutation inversion（守门网有效证据）

| Mutation | Test | Expected FAIL | Actual | PASS-FAIL-PASS |
|----------|------|---------------|--------|----------------|
| worker.go filter `if p.NotifyChannelIDs != nil` → `if false` | TestDBSmoke_M37A (sqlmock) | hits 应从 2 变 3 | FAIL (expected 2, actual 3) | ✓ |
| worker.go filter 同上 | TestDBSmoke_M37A (真 PG) | 场景 ① 应空但收到 3 个 m37aIDs | FAIL (`Should be empty, but was [...]`) | ✓ |
| service.go `loadRuleNotifyChannelIDs` swap return values (成功返 nil, 失败返 ids) | TestAlertService_LoadRuleNotifyChannelIDs_有效JSON | len 应为 2 但 0 | FAIL (`[] should have 2 item(s), but has 0`) | ✓ |

3 个 mutation 全部被对应测试抓到。守门网不是绿桩。

### Gate-3: Behavior unchanged outside ResolveAlert path

- `alerts` 表 DDL: 0 改动（DB 已有 alert_rule_id 列，GORM 模型只是把它读出来）
- `notification_channels` 表: 0 改动
- `alert_rules` 表: 0 改动（NotifyChannels 字段已存在）
- 旧 alerts (`alert_rule_id=NULL`) 行为: 0 改动 (payload NotifyChannelIDs=nil → worker fallback 全启用)
- 既有 7+ 个 worker 单元测试: 0 回归 (notification package 26 包测试全绿)
- 既有 service 测试: 0 回归

---

## Risk

### 已 Operationalised 的风险

| Risk | Mitigation |
|------|------------|
| 三处副本漂移 (DB 列 / 模型 tag / GORM 可见性) | `models.Alert.AlertRuleID` 已建；真 PG 列存在性测试守门 |
| NotifyChannels JSON 解析失败 → 静默丢告警 | service 返 nil → worker fallback 全启用；log 留痕 (运维可查) |
| NotifyChannels 配错 (UUID 在 DB 不存在) | worker filter 自然清空 (ops 配错就不发)；运维 UI 端可另加校验 |
| 同一 alert 多次 ResolveAlert | eventbus 自身 publish-once；各次 publish 各自带 RuleID，filter 结果幂等 |
| AlertRule 删了但 alert 引用 | service `db.First` 失败 → 返 nil → worker fallback (不漏告警) |
| 测试单测 vs 真 PG 行为不一致 | 单元 + 真 PG 双守门；mutation inversion 验证 |

### 业务级残余风险（不在本 round，留 M38-B）

| Risk | 触发条件 | 缓解 |
|------|----------|------|
| **fire 路径不通知** | Zabbix sync 完 alert 后**不**publish TopicAlertCreated → worker 订阅形同虚设 | M38-B ingestion publish 整链路修复 |
| **alert ↔ rule 匹配** | Zabbix trigger 没结构化字段对应 AlertRule 5 维度 | M38-B E1.b：triggerid → rule_id 映射表 |
| **fire 去重** | 多源（Zabbix/GLPI/手动/ticket）对同一 alert | M38-B E2.a：`trigger_id + problem_start` 60s 窗口 dedup |

### CodeGraph / Graph-first audit

PM-direct round 未 spawn subagent — graph-tool 调用 = 0。靠 grep-trace:
- `models.Alert.AlertRuleID` ↔ `migrations/000001_init.up.sql:651` 双向锚定
- `AlertRule.NotifyChannels` 写入方 (alert_handler.go) ↔ 读出方 (worker 通过 payload) 完整链路 grep 跟踪
- `TopicAlertCreated` 全仓库 grep 已确认无生产 publisher（仅 eventbus_test + worker subscribe）— M38-B 残余确认

### 工程级 (out-of-repo) 决策

- **commit prefix**: 沿用历史 (`docs(M37-A):` / `feat(M37-A):` / `test(M37-A):`)
- **PM-direct vs subagent**: PM-direct（4 文件 + 9 改动 ≤ PM 直接阈值 ~30min），不 spawn omp
- **mutation inversion 实践**: 3 处全部 PASS-FAIL-PASS（unit × 2 + 真 PG × 1）
- **force-with-lease**: 0 次（无 amend 必要）

---

## Status

**COMPLETE** (M37-A closed, M38-B 已 plan)

### 子回合计数

6 commits:
1. `4e10de7` docs(M37-A): intent-spec
2. `f7d2c2a` feat(M37-A): model + payload 字段层
3. `85c148f` feat(M37-A): worker 按 Rule 过滤 + helper
4. `71f946b` feat(M37-A): ResolveAlert snapshot payload
5. `f36b900` test(M37-A): 10 个单元测试 + mutation inversion
6. `71ac5c3` test(M37-A): 真 PG 端到端 + whitelist

### Quality metrics

- 6 commits / 6 pushes (per-commit-push cadence ✓)
- 0 code regressions (旧 alert 行为 0 改动)
- 0 schema changes (不动 DDL)
- 0 ops dependency (完全 in-repo)
- 0 force-with-lease (无 amend 必要)
- 3 mutation inversion PASS-FAIL-PASS (unit + service + 真 PG)
- 1 真 PG 守门实证: filter 禁用 → 3 channels 漏发 → test FAIL (守门网有效证据)
- 10 新单元测试 + 1 真 PG 集成测试 = **11 new tests**
- 42 真 PG tests 全绿 / 0 FAIL (41 baseline + 1 M37-A)

### Recommended next actions (PM queue)

1. **M38-B (Option B 第二轮)** — fire 路径整链路修复：
   - ingestion publish TopicAlertCreated (修 D — fire 不通知)
   - alert↔rule 匹配：triggerid → rule_id 映射表 (新 migration)
   - fire 去重：`trigger_id + problem_start` 60s 窗口
   - NotifyUsers 一并处理
   - 多 worker 实例横向扩展 (如未来部署)
   - 预计 12-16h 真代码 task
2. **TODO.md update** — G-39 状态从 `known-bug-until-ResolveAlert-path` 改为 `ResolveAlert path: fixed (M37-A); fire path: pending (M38-B)`
3. **OpenAPI** — NotifyChannels 是 internal payload，不进 public API；但 alert/rule endpoints 若有相关字段需对齐 schema

### 拍板记录 (Poison 2026-09-12 23:35)

> 走 Option E（M37-A + M38-B 两轮）+ E1.b (triggerid→rule_id 映射表) + E2.a (60s dedup)

M37-A 已严格遵守：仅修 ResolveAlert 路径；fire 路径全部留 M38-B；决策点 E1.b/E2.a 全部在 M38-B 实施。
