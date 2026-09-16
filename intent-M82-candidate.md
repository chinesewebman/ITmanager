# M82-candidate — G-39 AlertRule.NotifyChannels 真读 (tickOnce 路径收口, OMH ulw-loop 第 13 cycle)

> **Loop cycle**: 13 of `itmanager-grit-2026q3`
> **Loop mode**: B (PM-direct dispatch 沿用, watchdog Mode B auto-dispatched M82-candidate after M81-candidate cycle 12 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T13:xx:xx+08:00 (PM_QUEUE M82-candidate = G-39, derived from TODO.md G-39, M3 正确性审计观察项, 未修)
> **Scope**: backend service package, ≤2h estimated
> **Prerequisite**: M37-A (b9baeaf) + M38-B (ff1bced) — bus 路径 (handleAlertEvent) 已按 rule.NotifyChannels 过滤

## Goal

PM_QUEUE M82-candidate = **G-39 AlertRule.NotifyChannels 真读 — worker.handleAlertEvent 改走规则选择** 的**剩余 tickOnce 路径收口**:

1. **当前剩余漂移**: `service.alertService.writeNotificationTrigger` (`backend/internal/service/alert_service.go:489`) 被 `Acknowledge` (`alert_service.go:327`) + `Resolve` (`alert_service.go:381`) 调用, 把 `notification_logs (pending)` 写库。worker 端 `tickOnce` (`worker.go:367`) 逐条消费这些 pending log, **完全忽略** alert 关联的 AlertRule.NotifyChannels → 管理员在规则里勾掉的 channel, ack/resolve 时照样收告警通知 (与 G-39 原始症状"通知渠道只写不读"完全同形)。
2. **bus 路径已修**: M37-A (ResolveAlert) + M38-B (fire path) 都把 `NotifyChannelIDs` 注入 payload → `worker.handleAlertEvent` 按 ID 过滤。但这两条修复**只覆盖** bus 路径 — `writeNotificationTrigger` 是独立 service 层 sink, 全然无 rule filter。
3. **本 round 收口**: 让 `writeNotificationTrigger` 也按 alert.AlertRuleID 加载 rule.NotifyChannels 并过滤, 与 bus 路径**语义对齐** (同样的 fallback 链: rule 加载失败 → 全启用; rule.NotifyChannels 空数组 → 推 0 次; alert 无 rule → 全启用 [兼容历史 alert])。
4. **加 sql mock 测试**: 4 个新场景 (rule 显式空 → 0 log / rule 勾 2 channel → 2 log / rule 不存在 → 全启用 fallback / alert 无 rule → 全启用 fallback), 配套 mutation inversion 3 反证。

## Non-goals

- **不动** bus 路径 (handleAlertEvent + AlertEventPayload) — M37-A + M38-B 已 ship, 不退化
- **不动** AlertRule / Alert 模型字段
- **不动** migrations (alert_rule_id 列已存在 000001)
- **不动** `loadRuleNotifyChannelIDs` helper (M37-A 已 ship, 本 round 复用)
- **不动** `filterChannelsByIDs` (notification package) — service package 不依赖 notification 包, 新写本地 `filterNotificationChannelsByIDs` helper
- **不动** `notification_logs` 表结构 / `tickOnce` 消费逻辑 — 仅 producer 侧过滤
- **不动** MarkFalsePositive 路径 (不写 notification_logs, 不在 G-39 范围)
- **不动** `BulkAcknowledge` / `BulkResolve` — 走 `writeNotificationTrigger` 同 path, 自动继承本 round 修复 (不需要单独改)
- **不动** sing-box / keyring / OMH config / setup-profile.json / display.skin / interface (Poison 红线 + M67 standing rule)
- **不动** PM_QUEUE 既有 shipped entries
- **不**给 OMH 加新功能 / 不写新 systemd timer
- **不** bypass poison-stop-gates-v1

## Assumptions

- ITmanager repo HEAD = `c3d61d1` (M81-candidate cycle 12 ship), working tree clean, branch `main` up-to-date with `origin/main`
- `writeNotificationTrigger` 当前签名 `(ctx, alertID uuid.UUID, newStatus, userID string)` — 本 round 改为 `(ctx, alert *models.Alert, newStatus, userID string)`, 把 alert.AlertRuleID 透出
- 调用方 `Acknowledge` / `Resolve` 都已经 `s.Get(ctx, id)` 拿到 `*models.Alert`, 改签名不引入额外 DB 读
- `loadRuleNotifyChannelIDs(ctx, ruleID) []string` 已存在 (M37-A ship), 复用不重写
- fallback 链与 bus 路径完全对齐 (handler.handleAlertEvent line 161-173 的语义):
  - alert.AlertRuleID == nil → 不查 rule, 推全启用 channels (历史 alert 兼容)
  - rule load 失败 → 推全启用 channels (不漏告警)
  - rule.NotifyChannels == "" → 推 0 次 (明确空语义, 运维主动清空)
  - rule.NotifyChannels JSON parse 失败 → 推全启用 channels (fallback)
  - rule.NotifyChannels 解析成功但 UUID 在 DB 找不到对应 channel → 推 0 次
- 既有 3 个 sqlmock 测试 (`TestWriteNotificationTrigger_*`) 仅签名变更, 逻辑路径不变 (alert.AlertRuleID nil 触发 fallback)
- BulkAcknowledge / BulkResolve 不显式改 — 但它们调 `writeNotificationTrigger` 单条路径, alert.AlertRuleID 取自 `s.Get(ctx, id)` 已读, 行为自动正确
- fact_store fact_id = 21 advisory (沿用 M18 = 17 / M79 = 18 / M80 = 19 / M81 = 20 / M82 = 21)
- watchdog tick 时段: PM-direct dispatch, commit-age ≥ 10 min 才起下一 round

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M82-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `writeNotificationTrigger` 改签名: `(ctx, alert *models.Alert, newStatus, userID string)` | verify (grep) |
| `writeNotificationTrigger` 在加载 channels 后, 若 alert.AlertRuleID != nil → 调 `loadRuleNotifyChannelIDs` 过滤 | verify (grep + tests) |
| `Acknowledge` / `Resolve` 调用方改为 `s.writeNotificationTrigger(ctx, alert, ...)` | verify (grep) |
| 新增 helper `filterNotificationChannelsByIDs(channels, wantIDs)` 与 `worker.filterChannelsByIDs` 语义对齐 | verify (grep) |
| 4 个新 sqlmock 测试: `TestWriteNotificationTrigger_RuleEmpty_NoLogs` / `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` / `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` / `TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` | verify (go test) |
| 既有 3 个 sqlmock 测试签名更新后仍全绿 | verify (go test) |
| `go test -count=1 ./internal/service/...` 全绿 | verify |
| `go test -count=1 ./internal/notification/...` 全绿 (bus 路径不退化) | verify |
| `go test -count=1 ./...` 全绿 | verify |
| **mutation inversion 实证** — 3 反证全红 → 还原全绿 | verify (见 Verification §3) |
| `M82-completion-report.md` + `M82-graph-analysis.md` + CHANGELOG + TODO 更新 | docs commit 2 |
| git log 2-3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. writeNotificationTrigger 签名/语义 (grep verify)

```bash
$ grep -n "func (s \*alertService) writeNotificationTrigger" backend/internal/service/alert_service.go
func (s *alertService) writeNotificationTrigger(ctx context.Context, alert *models.Alert, newStatus, userID string) error {

$ grep -n "s.writeNotificationTrigger" backend/internal/service/alert_service.go
... s.writeNotificationTrigger(ctx, alert, "acknowledged", userID)   # Acknowledge
... s.writeNotificationTrigger(ctx, alert, "resolved", userID)        # Resolve

$ grep -n "filterNotificationChannelsByIDs" backend/internal/service/alert_service.go
func filterNotificationChannelsByIDs(channels []models.NotificationChannel, wantIDs []string) []models.NotificationChannel {
```

任一 FAIL = 签名/调用点漏改。

### 2. 契约测试 (Green: 既有 3 + 新增 4 = 7 PASS)

**既有 3 测试** (签名更新, 逻辑不变):
- `TestWriteNotificationTrigger_HasChannels_WritesPendingLogs` — alert 无 rule → 全启用 → 1 log
- `TestWriteNotificationTrigger_NoChannels_NoInsert` — alert 无 rule → 0 启用 channel → 0 log
- `TestWriteNotificationTrigger_ChannelQueryFails_DoesNotError` — channel 查失败 → 静默不阻塞

**新增 4 测试**:
- `TestWriteNotificationTrigger_RuleEmpty_NoLogs` — alert 有 rule, rule.NotifyChannels="" → 0 log (运维明确空)
- `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` — alert 有 rule, rule 勾 [chA, chB], DB 有 chA/B/C 三启用 → 2 log (chC 不写)
- `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` — alert 有 rule, rule 查询 gorm.ErrRecordNotFound → 全启用 → 3 log (compat fallback)
- `TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` — alert.AlertRuleID == nil → 不查 rule → 全启用 → 3 log (历史 alert 兼容)

### 3. mutation inversion 实证 (3 反证全红 → 还原全绿)

| # | 变异 | 期望红测试 | 期望红后还原绿 |
|---|---|---|---|
| **M1** | `writeNotificationTrigger` 删掉 `if alert.AlertRuleID != nil { ... }` 整段 (bypass rule filter) | `TestWriteNotificationTrigger_RuleEmpty_NoLogs` 红 (会写 1 log) + `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` 红 (会写 3 log 含 chC) | 还原 → 2 绿 ✓ |
| **M2** | `filterNotificationChannelsByIDs` 把 `if _, ok := wantSet[ch.ID.String()]; ok` 改成无条件 `out = append(out, *ch)` (等同返回 channels) | `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` 红 (会写 3 log 含 chC) + `TestWriteNotificationTrigger_RuleEmpty_NoLogs` 仍绿 (empty 不进循环) | 还原 → 2 绿 ✓ |
| **M3** | 把 `notifyIDs != nil` 守卫改成 `len(notifyIDs) > 0` (空数组走 fallback 全启用, 失去"明确空"语义) | `TestWriteNotificationTrigger_RuleEmpty_NoLogs` 红 (会写 3 log, 期望 0) | 还原 → 绿 ✓ |

3 变异 → 3 类红测试 → 3 类还原绿 = rule filter 守卫真工作。

### 4. go test 全绿

```bash
$ cd backend && go test -count=1 ./internal/service/... ./internal/notification/...
ok  network-monitor-platform/internal/service
ok  network-monitor-platform/internal/notification
... (no FAIL, no SKIP unexpected)

$ cd backend && go test -count=1 ./...
... 27+ packages, all ok
```

### 5. bus 路径不退化

```bash
$ cd backend && go test -count=1 -run "TestHandleAlertEvent|TestDBSmoke_M37A|TestDBSmoke_M38B" ./...
... all PASS (M37-A + M38-B 既有用例 0 回归)
```

## Risks

- **签名变更 0 外部 API 影响**: `writeNotificationTrigger` 是 `alertService` 的私有方法 (lowercase), 调用方仅 `Acknowledge` + `Resolve` 两处, 改签名安全。
- **BulkAcknowledge / BulkResolve 不显式改**: 它们内部对每条 id 调 `Acknowledge`/`Resolve` (单条路径, 复用本 round 修复), 不引入新路径。**验证**: grep `BulkAcknowledge\|BulkResolve` 内部不直接调 `writeNotificationTrigger`。
- **MarkFalsePositive 不写 notification_logs**: 验证 — `MarkFalsePositive` 不调 `writeNotificationTrigger` (只看 `s.Get(ctx, id)` 回读, 不发通知), 不在 G-39 范围。
- **loadRuleNotifyChannelIDs 复用已有 helper**: 不重写 JSON 解析、nil/空 fallback 等细节; 直接复用 M37-A 的契约 (rule 不存在返 nil / 显式空返 []string{} / parse 失败返 nil)。
- **filter helper 与 notification.filterChannelsByIDs 重复**: 两包不互相依赖, 重复 ~15 行 helper 是合理的隔离 (notification 包不能反向依赖 service 包)。注释标明"对齐 notification.filterChannelsByIDs 语义"。
- **mutation M2 仅 1 类测试红**: 是预期的 (rule empty 路径不进 helper loop, 自然绿) — 不强求 M2 同时红 2 类, 1 类足以证明 helper 在工作。
- **db_smoke 真 PG 测试**: 不在本 round 范围 — 本 round 改动纯 sqlmock 覆盖, 真 PG 留给后续 round (或 M38-B 既有真 PG 测试覆盖 e2e 已经够)。
- **fact_store fact_id 21 advisory**: 沿用 M79 同款 advisory, 未实际落库。
- **watchdog Mode B 自旋防**: 沿用 M79 D2/D4, commit age ≥ 10 min。
- **Telegram CLI 不可用**: 沿用 M79, watchdog 报告写到本地。

## Plan

1. **写 `intent-M82-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M82-candidate): intent spec (omh-plan 8 节骨架, G-39 tickOnce 路径收口)`.
2. **改 `backend/internal/service/alert_service.go`**:
   - `writeNotificationTrigger` 签名改为 `(ctx, alert *models.Alert, newStatus, userID string)`
   - 加 `filterNotificationChannelsByIDs` helper (~15 行, 与 `notification.filterChannelsByIDs` 语义对齐)
   - 在加载 channels 后插入 rule filter 块:
     ```go
     if alert.AlertRuleID != nil {
         notifyIDs := s.loadRuleNotifyChannelIDs(ctx, *alert.AlertRuleID)
         if notifyIDs != nil {
             channels = filterNotificationChannelsByIDs(channels, notifyIDs)
             if len(channels) == 0 {
                 return nil
             }
         }
     }
     ```
   - `Acknowledge` (line 327) 调用点改为 `s.writeNotificationTrigger(ctx, alert, "acknowledged", userID)`
   - `Resolve` (line 381) 调用点改为 `s.writeNotificationTrigger(ctx, alert, "resolved", userID)`
3. **更新既有 3 个 sqlmock 测试** (`alert_notification_trigger_test.go`): 把 `alertID` 参数改为 `&models.Alert{ID: alertID}` 形式, 语义不变, alert.AlertRuleID 默认 nil 触发 fallback。
4. **新增 4 个 sqlmock 测试**:
   - `TestWriteNotificationTrigger_RuleEmpty_NoLogs` — 期望 0 个 INSERT INTO "notification_logs"
   - `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` — 期望 1 个 INSERT INTO "notification_logs" 仅含 chA + chB
   - `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` — 期望 INSERT 全部 3 个 channel
   - `TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` — alert.AlertRuleID == nil, 不查 rule, 期望 INSERT 全部 3 个 channel
5. **跑测试 + mutation inversion**:
   - `cd backend && go test -count=1 ./internal/service/... ./internal/notification/...` → 全绿
   - mutation inversion 3 反证 (bypass / helper 失效 / 空数组 fallback 退化) 全红 → 还原全绿
6. **写 docs**: `M82-completion-report.md` + `M82-graph-analysis.md` + `CHANGELOG.md` M82 段 + `TODO.md` G-39 完成条目 — **docs commit 2**: `docs(M82-candidate): completion + graph analysis + CHANGELOG + TODO`.
7. **commit + push** 2 commits 到 origin/main.
8. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证).

### Commit 序列

```
c3d61d1 (HEAD, M81-candidate)
   ↓
M82 commit 1: feat(M82-candidate): G-39 tickOnce 路径收口 (writeNotificationTrigger 按 rule.NotifyChannels 过滤 + 4 契约测试)
M82 commit 2: docs(M82-candidate): completion + graph analysis + CHANGELOG + TODO
```

(2 commits 即可, 沿用 M78 / M79 / M80 / M81 pattern; feat 跟 docs 拆开, 中间穿插 mutation inversion 在 commit 1 之前完成.)

## Decision gate

- **D1**: scope = **仅 backend service package** 1 文件 + 1 测试文件, 不动 bus 路径 / models / migrations ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min ✓
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4) ✓
- **D5**: mutation inversion = 3 反证 (bypass / helper 失效 / 空数组 fallback 退化) 全红 → 还原绿 ✓
- **D6**: 不写新 fact_store entry (M79 同款 advisory, fact_id = 21) ✓
- **D7**: 2 commits 即可 (沿用 M78 / M79 / M80 / M81 pattern) ✓
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证 ✓
- **D9**: 复用 `loadRuleNotifyChannelIDs` (M37-A 已 ship) 不重写 helper ✓
- **D10**: BulkAcknowledge / BulkResolve 不显式改 (走单条 Acknowledge/Resolve 路径, 自动继承) ✓