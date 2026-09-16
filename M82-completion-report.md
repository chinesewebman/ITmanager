# M82 Completion Report — G-39 AlertRule.NotifyChannels 真读 (tickOnce 路径收口, OMH ulw-loop 第 13 cycle)

## 摩擦

G-39 (`TODO.md:311`) 自 2026-09-09 M3 正确性审计观察项起就一直挂着, 原始症状: "`AlertRule.NotifyChannels` 只写不读, 管理员在规则里勾掉的渠道照样收告警".

M37-A (commit `b9baeaf`, 2026-09-13 ship) + M38-B (commit `ff1bced`, 2026-09-13 ship) 把**两条 bus 路径** (ResolveAlert publish + Zabbix fire path publish) 都修了 — worker.handleAlertEvent 现在按 `AlertEventPayload.NotifyChannelIDs` 过滤推送渠道, 与 rule.NotifyChannels 对齐.

但 `service.alertService.writeNotificationTrigger` (`backend/internal/service/alert_service.go:489`) 走的是**另一条路径** — `Acknowledge` + `Resolve` 调它往 `notification_logs` 写 pending log, 由 worker `tickOnce` (`worker.go:367`) 异步消费. 这个 sink **完全无视** alert 关联的 AlertRule.NotifyChannels — 直接查所有 enabled channel 落库. 与 G-39 原始症状"通知渠道只写不读"完全同形, 是 M37-A/M38-B 留下的盲点.

具体后果:
- **Acknowledge**: tickOnce 路径是**唯一**出口 (bus 没 topic `alert.acknowledged`). 管理员把某 channel 在规则里勾掉, ack 一条该规则的告警 → 该 channel 仍收到 ack 通知. 静默失效.
- **Resolve**: bus 路径 + tickOnce 路径**两条都发**. bus 路径按规则过滤 (M37-A 已修), tickOnce 路径仍全发 → 同一 resolved 告警, 被规则勾掉的 channel 收到**重复**通知 (1 条 bus 路径不该收到的 + 1 条 tickOnce 不该收到的). 静默 + 重复双 bug.

Poison 触发 watchdog Mode B 选 PM_QUEUE M82-candidate = G-39 tickOnce 路径作为 cycle 13 round, 实证「同源问题收口不全时, 沿用既修复路径 helper + 加契约测试」的低成本收口模式.

## 决策 (PM-direct 拍)

**Decision 1 (沿用 M37-A 已 ship 的 `loadRuleNotifyChannelIDs` helper)**: helper 已存在 (`alert_service.go:759`, M37-A ship), 返回 `[]string` 的契约 (nil = fallback / []string{} = 明确空 / 非空切片 = 解析成功) 已在 4 个测试里钉死. 本 round 复用, 不重写. `filterNotificationChannelsByIDs` helper 写本地 (~15 行) 是因为 service 包不依赖 notification 包 (反向依赖会破坏既有分层), 与 `notification.filterChannelsByIDs` (M37-A ship) 语义对齐 (channels 顺序保留 + 空 want 返 nil + UUID 字符串比对).

**Decision 2 (签名变更 `writeNotificationTrigger(ctx, alertID, ...) → writeNotificationTrigger(ctx, alert, ...)`)**: 调用方 `Acknowledge` + `Resolve` 都已经 `s.Get(ctx, id)` 拿到 `*models.Alert`, 改签名不引入额外 DB 读, 不动外部 API (`alertService.writeNotificationTrigger` 是 lowercase 私有方法, 改动局限在 service 包内). 既有 3 个测试 (`TestWriteNotificationTrigger_*`) 仅签名变更, alert.AlertRuleID 默认 nil 触发 fallback 全启用, 语义不变.

**Decision 3 (加 4 个 sqlmock 测试 + 2 个 helper 测试 + 1 个真 sqlite 测试 = 7 个新测试, 不写 db_smoke 真 PG 测试)**: sqlmock 测契约 (序列精确匹配), helper 白盒测顺序/语义, 真 sqlite 测「INSERT 不应发生」sqlmock 抓不到的反向断言 (M3 守卫). db_smoke 真 PG 测试留给后续 round (M37-A 既有真 PG 测试 `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter` 已覆盖 bus 路径 e2e, 沿用即可).

**Decision 4 (scope 仅 service 包, 不动 bus 路径 / models / migrations)**: bus 路径已 ship (M37-A + M38-B), 不退化 = 通过既有 M37-A/M38-B 测试全过实证. models.Alert.AlertRuleID 字段已存在 (migration 000001), 不动. 不写新 migration.

**Decision 5 (mutation inversion 3 反证全红 → 还原全绿)**: M1 bypass rule filter / M2 bypass helper filter / M3 改 `!=nil` 为 `len>0` 丢「明确空」语义 — 3 类反证, 实证 3/3 红 → 还原绿.

## 关键设计变化

### backend (Go)

1. **`backend/internal/service/alert_service.go:498`**: `writeNotificationTrigger` 签名变更:
   - 旧: `(ctx context.Context, alertID uuid.UUID, newStatus, userID string) error`
   - 新: `(ctx context.Context, alert *models.Alert, newStatus, userID string) error`
   - 把 alert.AlertRuleID 透出, 调用方无需再 SELECT.

2. **`backend/internal/service/alert_service.go:498-541`**: 在加载 channels 后插入 rule filter 块 (与 bus 路径 handleAlertEvent:161-173 语义完全对齐):
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
   完整 fallback 链:
   - `alert.AlertRuleID == nil` (历史 alert) → 不查 rule, 推全启用 channels
   - rule 加载失败 / parse 失败 → `loadRuleNotifyChannelIDs` 返 nil → 不过滤, 推全启用
   - rule.NotifyChannels 显式空 → `loadRuleNotifyChannelIDs` 返 `[]string{}` → filter 后 0 channel → 推 0 次 (运维主动清空语义)
   - 解析成功但 UUID 在 DB 找不到 → filter 后 0 channel → 推 0 次

3. **`backend/internal/service/alert_service.go:327 + 381`**: 调用方更新:
   - `Acknowledge`: `return s.writeNotificationTrigger(ctx, alert, "acknowledged", userID)`
   - `Resolve`: `return s.writeNotificationTrigger(ctx, alert, "resolved", userID)`

4. **`backend/internal/service/alert_service.go:548-564`**: 新增 `filterNotificationChannelsByIDs` helper (~17 行):
   - 与 `notification.filterChannelsByIDs` (M37-A ship) 语义对齐 (channels 顺序保留 / 空 want 返 nil / UUID 字符串比对).
   - 不复用 `notification.filterChannelsByIDs` 的理由: 通知包不能反向依赖 service 包 (既有分层), 重复 ~15 行 helper 是合理的隔离代价.

### 测试 (10 个新/改 sqlmock + sqlite)

5. **`backend/internal/service/alert_notification_trigger_test.go`**: 既有 3 个测试签名更新 + 7 个新测试:
   - **既有 3 个 (签名更新, 逻辑不变)**:
     - `TestWriteNotificationTrigger_HasChannels_WritesPendingLogs`
     - `TestWriteNotificationTrigger_NoChannels_NoInsert`
     - `TestWriteNotificationTrigger_ChannelQueryFails_DoesNotError`
   - **新增 4 个 sqlmock 契约测试 (M82 AC)**:
     - `TestWriteNotificationTrigger_RuleEmpty_NoLogs` — alert 有 rule, rule.NotifyChannels="" → 序列精确匹配: rule SELECT → no channels filter → no INSERT (sqlmock 测「序列匹配」, 反向靠 TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite 守)
     - `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` — alert 有 rule, rule 勾 [chA, chB], DB 有 3 启用 channel → 序列: channels SELECT → rule SELECT → INSERT (chC 不在 INSERT 序列里, sqlmock 守「序列精确」)
     - `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` — alert 有 rule, rule 查询 gorm.ErrRecordNotFound → 序列: channels SELECT → rule SELECT (error) → INSERT 3 channels (fallback 全启用, 与改动前兼容)
     - `TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` — alert.AlertRuleID nil → 序列: channels SELECT → INSERT 3 channels (fallback, 历史 alert 兼容)
   - **新增 1 个真 sqlite 测试 (M3 守卫, sqlmock 抓不到「INSERT 不应发生」的反向断言)**:
     - `TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite` — 真 sqlite in-memory, notification_channels + alert_rules + alerts + notification_logs 4 表手写 DDL, alert 有 rule (notify_channels=""), 3 启用 channel → 实跑 writeNotificationTrigger → 直查 notification_logs count 必须 0. M3 mutation (len(notifyIDs) > 0) 会让代码 INSERT 3 行, count != 0 → 红
   - **新增 2 个 helper 白盒测试**:
     - `TestFilterNotificationChannelsByIDs_FiltersToListed` — 3 channels 输入, want [chA, chB] → 2 channels 输出 (chC 不在). 同时测 nil want / []string{} want → 返 nil (空数组语义)
     - `TestFilterNotificationChannelsByIDs_PreservesChannelsOrder` — 输入 channels [chA, chB, chC], want [chC, chA] → 输出 [chA, chC] (按 channels 顺序, 不是 want 顺序). M2 mutation (bypass filter 让 helper 返回 channels 全部) → 测试红 (期望 2 个, 实际 3 个)

## 验证 (实证)

### backend
- `go build ./...`: 0 错 (无新增 import, 无外部 API 变更)
- `go test -count=1 ./internal/service/...`: **ok 0.449s** (含 10 M82 测试: 7 sqlmock + 1 sqlite + 2 helper)
- `go test -count=1 ./internal/notification/...`: **ok 0.693s** (bus 路径 M37-A/M38-B 测试不退化)
- `go test -count=1 ./...`: **24 packages ok** (无回归, 0 退化)

### mutation inversion 实证 (3 / 3 反证全红 → 还原全绿)

| # | 变异 | 期望红测试 | 实证 |
|---|---|---|---|
| **M1** | `writeNotificationTrigger` 删掉 `if alert.AlertRuleID != nil { ... }` 整段 (bypass rule filter) | `TestWriteNotificationTrigger_RuleEmpty_NoLogs` + `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` + `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` 三红 (序列断言破: 期望 rule SELECT 发生, 实际被 bypass 跳过) | ✓ 三红, 还原后三绿 |
| **M2** | `filterNotificationChannelsByIDs` 把 `if _, ok := wantSet[ch.ID.String()]; ok` 改成无条件 `out = append(out, *ch)` (等同返回 channels) | `TestFilterNotificationChannelsByIDs_FiltersToListed` + `TestFilterNotificationChannelsByIDs_PreservesChannelsOrder` 两红 (期望 2 个, 实际 3 个) | ✓ 两红, 还原后两绿 |
| **M3** | `if notifyIDs != nil` 改成 `if len(notifyIDs) > 0` (空数组走 fallback 全启用, 失去「明确空」语义) | `TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite` 红 (真 sqlite count, 期望 0, 实际 3) | ✓ 红, 还原后绿 |

3 变异 → 3 类红测试 → 3 类还原绿 = rule filter 守卫真工作.

**关键设计要点**: M3 mutation 是**反向断言** (期望 INSERT **不**发生), sqlmock 默认对 extra queries 不报错 — 必须用真 sqlite in-memory 直接 count 表. 这与 M81 的 substring vs word-boundary 同款教训: 「写是写了, 写错了列/写错了行」正是 sqlmock 手写期望盖不住的缺陷.

### bus 路径不退化 (M37-A + M38-B)
- `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter`: 真 PG, 4 场景 (rule 显式空 → 0 / rule 勾 2 → 2 / 无 RuleID fallback 3 / AlertRuleID 字段回读非 nil) — M82 不改 db_smoke_test.go, 既有测试沿用
- `TestDBSmoke_M38B_FirePathEnd2End`: 真 PG, fire 路径 e2e — 沿用
- 单元测试 `notification_test.go` M37-A + M38-B 既有 case 全过 (worker handleAlertEvent 的 NotifyChannelIDs 过滤路径)

## commit

3 commits (沿用 M78 / M79 / M80 / M81 pattern):
1. `5114bdf` feat(M82-candidate): intent spec (omh-plan 8 节骨架, G-39 tickOnce 路径收口) — 1 file / +197
2. `8ece311` feat(M82-candidate): G-39 tickOnce 路径收口 (writeNotificationTrigger 按 rule.NotifyChannels 过滤 + 4 契约测试 + 2 helper 测试) — 2 files / +378 / -12
   - `backend/internal/service/alert_service.go` 改 writeNotificationTrigger 签名 + body + 新增 filterNotificationChannelsByIDs helper (~66 行净增)
   - `backend/internal/service/alert_notification_trigger_test.go` 既有 3 测试签名更新 + 7 新测试 (4 sqlmock + 1 sqlite + 2 helper)
3. `docs(M82-candidate): completion + graph analysis + CHANGELOG + TODO` — 4 files (本 commit)

## 派生 TODO (留 future)

- **G-39 同族 — `config.DingtalkConfig` 无人使用 (TODO.md:311 同形残留)**: 本 round 仅收口 `writeNotificationTrigger` 路径, 仍有 `internal/config/config.go:120` 的 `DingtalkConfig` 字段无人读. M82 沿用 TODO.md G-39 登记不修 (这是 config 加载路径, 与 notification 路径正交), 后续 round 单独收口.
- **db_smoke 真 PG 测试覆盖 tickOnce 路径**: M82 没写真 PG db_smoke 测试 — sqlmock 序列 + 真 sqlite in-memory 覆盖了单元边界, 但真 PG 的并发 / 事务边界没测. M82 沿用 M37-A 既有真 PG 测试 (覆盖 bus 路径), 后续 round 可加 tickOnce 路径的真 PG 测试.
- **G-39 整族收口**: M82 把「通知渠道选择实际不生效」的核心症状收口 (alert_service.go:489 + bus 路径 worker.go:121 都按 rule 过滤), 但 TODO.md G-39 还有同形残留 (`config.DingtalkConfig` 无人使用). 后续 round 可走 M82 同款模式 (helper 复用 + 加契约测试), 但属独立 scope.

## OMH workflow shape 实证 (M67 沿用)

M82 intent-M82-candidate.md 用 omh-plan 8 节骨架:
1. **Goal 节** 钉死本 round 收口的**剩余 tickOnce 路径** (非 bus 路径 — bus 路径 M37-A + M38-B 已 ship), 不混淆 G-39 全族收口与本 round 局部收口.
2. **Non-goals 节** 钉死不动 `MarkFalsePositive` / `BulkAcknowledge` / `BulkResolve` / bus 路径 / models / migrations — 避免 scope creep.
3. **Decision gate 节** 钉死 10 个 D (D1 scope 仅 service / D2 poison-stop-gates / D3 自旋防 / D4 30-min hist / D5 mutation 3 反证 / D6 fact_store advisory / D7 2 commits / D8 PM_LAST_DISPATCH_RESULT / D9 复用 loadRuleNotifyChannelIDs helper / D10 Bulk 不显式改, 单条 Acknowledge/Resolve 路径自动继承).

mutation inversion 在本 round 起新增**「反向断言用真 sqlite」**模式 (M3 守卫). sqlmock 默认对 extra queries 不报错, 「INSERT 不应发生」这类反向断言必须用真 DB 直接 count 表. 这是 M81 word-boundary regex 教训的延伸 — 单跑绿测试抓不到测试设计漏洞, 必须靠 mutation inversion 反证才能暴露.

## 注意事项

1. **sqlmock QueryMatcherRegexp 注册顺序敏感**: sqlmock 期望按注册顺序匹配, actual SQL 必须**严格**按代码调用顺序触发. M82 的 4 个 sqlmock 测试都按「channels SELECT → rule SELECT → INSERT」顺序注册 — 顺序写反会让 sqlmock 报「could not match actual sql ... with expected regexp ...」. 调试时看 sqlmock 的 [rows:X] 标记行, 一一对应.
2. **真 sqlite 测试需要手写 DDL**: `models.X.ID` 带 `default:gen_random_uuid()`, sqlite 没该函数, AutoMigrate 必炸 — 沿用 `asset_audit_test.go` / `channel_service_test.go` 既有 `newXSQLiteDB` 模式, 手写 4 表 DDL (notification_channels + alert_rules + alerts + notification_logs). DDL 必须包含模型所有字段 (含 `recipient` / `error_msg` 这种容易被遗漏的), 否则 INSERT 会报「no column named X」.
3. **gorm batch INSERT sqlmock 抓不到行数**: `INSERT INTO notification_logs VALUES (?, ?, ...), (?, ?, ...)` 形式, sqlmock 默认只校验「INSERT 发生过」, 不校验行数. 真要校验 INSERT 行数, 必须用真 DB (sqlite / 真 PG). M82 沿用「sqlmock 测序列 + 真 sqlite 测 count」双层守卫.
4. **signature 变更局限**: `writeNotificationTrigger` 是 lowercase 私有方法, 改动仅限 service 包. AlertService interface (line 61) 不暴露, 调用方仅 `Acknowledge` + `Resolve` 两处, 风险低.
5. **loadRuleNotifyChannelIDs 4 类返回值 (M37-A 已 ship)**:
   - rule 不存在 → `nil` (worker fallback 全启用, 不漏告警)
   - rule.NotifyChannels 为空字符串 → `[]string{}` (明确空, 推 0 次)
   - rule.NotifyChannels JSON parse 失败 → `nil` (worker fallback 全启用, 不漏告警)
   - 解析成功 → `[]string{...}` (按 channels 顺序过滤)
   M82 复用此契约, 不重写.

## 后续推荐

- **G-39 `config.DingtalkConfig` 无人使用**: M83 candidate 可走 M82 同款模式 (helper 复用 + 加契约测试), 沿用 TODO.md G-39 登记路径.
- **fact_store fact_id = 21**: 沿用 M18 = 17 / M79 = 18 / M80 = 19 / M81 = 20 / **M82 = 21** advisory 序列, 未实际落库 (M79 同款 "fact_store truth stream" 是 advisory).