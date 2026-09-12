---
id: INTENT-M37-A-NotifyChannelsWorker
title: AlertRule.NotifyChannels 写不读 — worker 过滤 + AlertEventPayload 携带 RuleID（运维 ResolveAlert 路径）
status: draft
author: hermes@local (PM)
created: 2026-09-12
round: M37-A
supersedes: TODO.md G-39 (partially — 仅覆盖 ResolveAlert 路径)
target_files:
  - backend/internal/models/alert.go
  - backend/internal/notification/worker.go
  - backend/internal/service/alert_service.go
  - backend/internal/notification/notification_test.go
  - backend/tests/db_smoke_test.go
  - backend/migrations/000037_g39_worker_filter.up.sql   # 可选, 见 Operational constraints
outcomes:
  - 运维在 AlertRule 编辑页勾 4 个 channel，ResolveAlert 触发时只推送被勾的 channel，未勾的不再推送
  - AlertEventPayload 携带 RuleID + 解析后的 NotifyChannels 列表，worker 不再需要二次 DB 读
  - 旧 alerts（alert_rule_id=NULL）行为兼容：worker fallback 推全启用 channels，与改动前完全一致
  - models.Alert 结构体新增 AlertRuleID 字段，DB 已有的 alert_rule_id 列从此能被 GORM 读写
  - 真 PG 集成测试覆盖三场景：rule 有 2 个 channel（应发 2）+ rule 有 0 个 channel（应发 0）+ rule 不存在 / payload 没 RuleID（应发全启用的）
acceptance:
  - id: AC-M37-A-1
    given: 一条 enabled AlertRule，notify_channels=JSON(["ch-A","ch-B"])；DB 中存在 ch-A、ch-B、ch-C 三条 enabled notification_channels；一条 alert 对应该 rule
    when: alert_service.ResolveAlert 被调用，publish AlertEventPayload 到 event bus
    then: worker.handleAlertEvent 收到后，sender.Send 仅被调用 2 次（ch-A、ch-B），ch-C 一次都不发
  - id: AC-M37-A-2
    given: 同上，但 alert.alert_rule_id=NULL（旧数据），eventbus payload 没 RuleID 字段
    when: alert_service.ResolveAlert 被调用
    then: worker 走 fallback "推所有 enabled channels"，发 3 次（ch-A/ch-B/ch-C）—— 与改动前一致
  - id: AC-M37-A-3
    given: AlertRule 存在但 notify_channels=JSON([])（运维明确清空）
    when: alert 触发 ResolveAlert
    then: worker 推 0 次（即便 DB 有 enabled channels 也跳过）
  - id: AC-M37-A-4
    given: DB 中 alerts 表已有 alert_rule_id 列（migration 000001 创建）；models.Alert 结构体原本没 AlertRuleID 字段
    when: M37-A 完成后
    then: models.Alert.AlertRuleID *uuid.UUID 字段存在；GORM 用它能 SELECT/INSERT 正常；真 PG 集成测试可 INSERT (alert with AlertRuleID=…) 并回读
  - id: AC-M37-A-5
    given: handler/service/worker 路径改动
    when: go build ./... + go vet ./... + go test -count=1 ./internal/... ./tests/... 跑完
    then: 全部 exit 0；新 ≥3 个 worker 测试 + 1 个真 PG db_smoke 测试加入；旧测试 0 回归
edges:
  - NotifyChannels JSON 解析失败（malformed JSON）→ fallback 推全启用 channels（与现状一致，不静默丢告警）
  - NotifyChannels 解析成功但里面 UUID 在 DB 找不到对应 channel → 推 0 次（运维配错 channel 就不发，避免发到不存在的目标）
  - AlertRule.NotifyChannels=NULL（字段未写过）→ 当 JSON([]) 处理 → 推 0 次
  - 同一 alert 多次 ResolveAlert（幂等 / 并发）→ 各次 publish 各自带 RuleID，worker 端靠 eventbus 自身的 publish-once 语义保证不重
  - AlertRule 删了但 alert 还有旧 rule_id 引用 → worker 拿 rule 失败 → fallback 全发（不漏告警）
not_goals:
  - 修 fire 路径（ingestion/service.go publish TopicAlertCreated）—— 这是 M38-B 的事
  - alert ↔ rule 匹配逻辑（triggerid → rule_id 映射表）—— M38-B 决策点 1 的 E1.b
  - fire 去重策略 —— M38-B 决策点 2 的 E2.a
  - AlertRule.NotifyUsers 处理（同 NotifyChannels 一并留到 M38-B 整链路）
  - OpenAPI spec 更新（key 字段是 internal payload，不进 public API）
  - migration 改动 DB schema（DB 已有 alert_rule_id 列，0 DDL）
operational_constraints:
  - models.NotificationChannel / models.AlertRule 表结构零改动
  - notification_channels 表 0 改动
  - alert_rules 表 0 改动（NotifyChannels 字段已存在）
  - DB schema 0 改动（不动 migration 目录，除非真 PG 测试需要 additive index；如需要另开 migration 000037）
  - 旧 alerts（alert_rule_id=NULL）的行为不能变 —— 走 fallback
  - 既有 worker 测试（notification_test.go 7+ 个 TestHandleAlertEvent_*）0 回归
  - 既有的 sender / Resolver / RegisterSender 接口零改动
  - eventbus.Publish/Subscribe 接口零改动
evidence:
  - "TODO.md G-39: AlertRule.NotifyChannels 写不读"
  - "backend/migrations/000001_init.up.sql:651 — alerts.alert_rule_id UUID REFERENCES alert_rules(id) 已建"
  - "backend/internal/notification/worker.go:122 — db.Where(\"is_enabled = ?\", true).Find(&channels) 是 bug 现场"
  - "backend/internal/service/alert_service.go:355 — ResolveAlert publish 是当前唯一 publish 路径，payload 应加 RuleID"
  - "Poison 用户拍板 (2026-09-12 23:35): 走 Option E = M37-A + M38-B 两轮；E1.b / E2.a 决策同意推荐"
risk_decisions_resolved:
  - 决策点 1 (alert↔rule 匹配) — 留 M38-B 走 E1.b (triggerid→rule_id 映射表)
  - 决策点 2 (fire 去重) — 留 M38-B 走 E2.a (trigger_id + problem_start 60s 窗口 dedup)
  - M37-A 仅修 ResolveAlert 路径（运维主动操作），fire 路径不动
test_strategy:
  unit:
    - notification_test.go: 新 3 个 TestHandleAlertEvent_*_按Rule过滤 — mock db 返回 channels + mock sender，断言 send 次数
    - alert_service_test.go (如不存在则新增): 1 个 TestPublishOnResolve_携带RuleID — 验证 publish payload 含 RuleID
  integration_real_pg:
    - db_smoke_test.go: 新 1 个 TestResolveAlert_按RuleNotifyChannels过滤 — 真 PG 构造 rule+channels+alert+ResolveAlert+eventbus→worker，断言 alert_notifications 表里只有被勾 channel 的记录
  regression:
    - go test -count=1 ./... 必须 0 回归
    - frontend vitest 必须 0 回归（不碰前端）
    - gorm migration must NOT auto-create alert_rule_id column (已存在, 防误判)
round_plan:
  commit_1: feat(M37-A): models.Alert.AlertRuleID 字段 + AlertEventPayload.RuleID 字段
  commit_2: feat(M37-A): worker.handleAlertEvent 按 Rule.NotifyChannels 过滤 (含 fallback)
  commit_3: feat(M37-A): alert_service.ResolveAlert publish 时快照 RuleID 到 payload
  commit_4: test(M37-A): worker 3 unit test + alert_service 1 unit test
  commit_5: test(M37-A): 真 PG db_smoke TestResolveAlert_按RuleNotifyChannels过滤
  commit_6: docs(M37-A): CHANGELOG + intent-spec-author artifact + completion report
estimated_effort: "3-4h (PM-direct, 不 spawn omp)"
estimated_commits: 6
```

---

## Context

`TODO.md` 登记的 G-39「AlertRule.NotifyChannels 写不读」是 ITmanager 当前最大的静默 bug 之一。

实际严重度比 TODO 字面更深：DB schema 已经建了 `alerts.alert_rule_id UUID REFERENCES alert_rules(id)`（migration 000001），但 `models.Alert` 结构体**没有** `AlertRuleID` 字段——GORM 模型↔DB 漂移导致这条 FK 列在 GORM 视角下是死的。

更深一层：production 全仓库**没有 `TopicAlertCreated` 的 publisher**（仅 eventbus_test 与 worker subscribe 出现），worker 订阅了 `TopicAlertCreated` 但永远收不到 fire 事件——意味着当前**任何 alert 新增都不会自动通知**，只有运维手动操作 ResolveAlert 才通知。

M37-A 不一次修全链路（避免 15h+ 单 round + 高风险）。本 round 只修**运维主动 ResolveAlert 这条路径**：让 worker 在收到 ResolveAlert 事件时按 Rule 配置的 NotifyChannels 过滤推送。fire 路径（ingestion publish）整链路修复留给 M38-B。

Poison 已拍板：走 Option E（分两轮）+ 决策点 E1.b（triggerid→rule_id 映射表）+ 决策点 E2.a（trigger_id + problem_start 60s dedup）。决策点的实装在 M38-B；M37-A 只接 ResolveAlert 的 RuleID 传递通道。

---

## Outcomes

### Outcome 1：ResolveAlert 按 rule 过滤推送
用户在 AlertRule 编辑页勾 4 个 channel 只留 2 个时，运维点 alert 的"解决"按钮，webhook 仅发到这 2 个被勾的 channel；未勾的 channel 一次都不发。

### Outcome 2：AlertEventPayload 携带 RuleID + 解析后的 NotifyChannels
worker 不再需要二次 DB 读 rule；payload 快照后，worker 只用 payload 数据就能完成过滤（payload 是 fire→resolve 间的一致快照）。

### Outcome 3：旧 alerts（alert_rule_id=NULL）兼容
historical alerts 的行为完全不变——worker 拿不到 RuleID 时 fallback 推全启用 channels，与现状一致。

### Outcome 4：GORM 模型↔DB 漂移修复
`models.Alert` 加 `AlertRuleID *uuid.UUID` 字段，从此 GORM 能读写 `alert_rule_id` 列。

### Outcome 5：真 PG 集成测试覆盖
三场景真 PG 跑通：rule 有 2 channels（应发 2）+ rule 有 0 channels（应发 0）+ rule 不存在（fallback 全发）。

---

## Acceptance Criteria

### AC-M37-A-1: rule 有 2 channels（应发 2）

**Given**: 一条 enabled AlertRule，`notify_channels=JSON(["ch-A","ch-B"])`；DB 中存在 ch-A、ch-B、ch-C 三条 enabled `notification_channels`；一条 alert 对应该 rule。

**When**: `alert_service.ResolveAlert` 被调用，publish `AlertEventPayload` 到 event bus。

**Then**: `worker.handleAlertEvent` 收到后，`sender.Send` 仅被调用 2 次（ch-A、ch-B），ch-C 一次都不发。

### AC-M37-A-2: 旧 alert（alert_rule_id=NULL）— fallback 全发

**Given**: 同上，但 `alert.alert_rule_id=NULL`（旧数据），eventbus payload 没 RuleID 字段。

**When**: `alert_service.ResolveAlert` 被调用。

**Then**: worker 走 fallback "推所有 enabled channels"，发 3 次（ch-A/ch-B/ch-C）—— 与改动前一致。

### AC-M37-A-3: rule 显式空（notify_channels=[]）— 推 0 次

**Given**: AlertRule 存在但 `notify_channels=JSON([])`（运维明确清空）。

**When**: alert 触发 ResolveAlert。

**Then**: worker 推 0 次（即便 DB 有 enabled channels 也跳过）。

### AC-M37-A-4: GORM 模型 ↔ DB 漂移修复

**Given**: DB 中 alerts 表已有 `alert_rule_id` 列（migration 000001 创建）；`models.Alert` 结构体原本没 `AlertRuleID` 字段。

**When**: M37-A 完成后。

**Then**: `models.Alert.AlertRuleID *uuid.UUID` 字段存在；GORM 用它能 SELECT/INSERT 正常；真 PG 集成测试可 INSERT `(alert with AlertRuleID=…)` 并回读。

### AC-M37-A-5: 全部 gate exit 0

**Given**: handler/service/worker 路径改动。

**When**: `go build ./...` + `go vet ./...` + `go test -count=1 ./internal/... ./tests/...` 跑完。

**Then**: 全部 exit 0；新 ≥3 个 worker 测试 + 1 个真 PG db_smoke 测试加入；旧测试 0 回归。

---

## Edge cases

| Case | Behavior |
|------|----------|
| NotifyChannels JSON 解析失败（malformed JSON） | fallback 推全启用 channels（不静默丢告警） |
| NotifyChannels 解析成功但里面 UUID 在 DB 找不到对应 channel | 推 0 次（避免发到不存在的目标；运维配错 channel 就不发） |
| AlertRule.NotifyChannels=NULL（字段未写过） | 当 JSON([]) 处理 → 推 0 次 |
| 同一 alert 多次 ResolveAlert（幂等 / 并发） | 各次 publish 各自带 RuleID，worker 端靠 eventbus 自身的 publish-once 语义保证不重 |
| AlertRule 删了但 alert 还有旧 rule_id 引用 | worker 拿 rule 失败 → fallback 全发（不漏告警） |

---

## Operational constraints

- `models.NotificationChannel` / `models.AlertRule` 表结构零改动
- `notification_channels` 表 0 改动
- `alert_rules` 表 0 改动（NotifyChannels 字段已存在）
- DB schema 0 改动（不动 migration 目录，除非真 PG 测试需要 additive index；如需要另开 migration 000037）
- 旧 alerts（`alert_rule_id=NULL`）的行为不能变 —— 走 fallback
- 既有 worker 测试（`notification_test.go` 7+ 个 `TestHandleAlertEvent_*`）0 回归
- 既有的 sender / Resolver / RegisterSender 接口零改动
- eventbus.Publish/Subscribe 接口零改动

---

## Evidence

| Outcome | Anchor |
|---------|--------|
| 1, 2 | `backend/internal/notification/worker.go:122` — `db.Where("is_enabled = ?", true).Find(&channels)` 是 bug 现场 |
| 2, 3 | `backend/internal/service/alert_service.go:355` — ResolveAlert publish 是当前唯一 publish 路径 |
| 4 | `backend/migrations/000001_init.up.sql:651` — `alerts.alert_rule_id UUID REFERENCES alert_rules(id)` 已建 |
| 0 (写不读) | `TODO.md G-39` 登记 |
| 拍板 | Poison 用户 2026-09-12 23:35 拍 Option E + E1.b + E2.a |

---

## M38-B 衔接

M37-A 仅解决 ResolveAlert 路径。fire 路径需 M38-B 整链路修复：

1. ingestion 路径 publish TopicAlertCreated（修复 D — fire 不通知）
2. alert ↔ rule 匹配逻辑（E1.b — triggerid→rule_id 映射表，新 migration）
3. fire 去重（E2.a — trigger_id + problem_start 60s 窗口 dedup）
4. NotifyUsers 字段同样处理
5. 多 worker 实例横向扩展（如未来部署）

M37-A 必须 ship 后再做 M38-B，因为 M37-A 建立的 RuleID 传递通道是 M38-B 的基础。
