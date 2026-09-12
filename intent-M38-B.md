---
id: INTENT-M38-B-FirePathE2E
title: G-39 fire 路径整链路修复（TopicAlertCreated publish + triggerid→rule_id 映射 + 60s dedup + NotifyUsers）
status: draft
author: hermes@local (PM)
created: 2026-09-12
round: M38-B
supersedes: TODO.md G-39 (剩余 fire 路径)
prerequisite: INTENT-M37-A-NotifyChannelsWorker (M37-A 已 ship, 0b9baeaf)
target_files:
  - backend/migrations/000038_g39_fire_mapping.up.sql
  - backend/migrations/000038_g39_fire_mapping.down.sql
  - backend/internal/models/alert.go
  - backend/internal/models/alert_rule.go
  - backend/internal/integration/service.go
  - backend/internal/integration/alert_rule_mapper.go
  - backend/internal/integration/alert_rule_mapper_test.go
  - backend/internal/notification/worker.go
  - backend/internal/notification/worker_test.go
  - backend/internal/notification/firededup.go
  - backend/internal/notification/firededup_test.go
  - backend/internal/notification/notification_test.go
  - backend/internal/service/alert_service.go
  - backend/internal/service/alert_service_test.go
  - backend/internal/api/handlers/alert_handler.go
  - backend/internal/api/handlers/alert_handler_test.go
  - backend/internal/eventbus/eventbus_test.go
  - backend/tests/db_smoke_test.go
  - scripts/db_smoke.sh
  - CHANGELOG.md
outcomes:
  - Zabbix fire 新增 alert 后 publish TopicAlertCreated；worker 收到事件后按 AlertRule.NotifyChannels 过滤推送
  - 运维在 ITmanager UI 配置「triggerid → rule_id」映射表，新 alert 自动 inherit 该 rule（包括 rule_id 与 notify_channels/notify_users）
  - 同一 trigger + problem_start 在 60s 窗口内不重复 fire 通知（worker 端 dedup，不依赖 caller）
  - AlertRule.NotifyUsers 同样按 rule_id 过滤推送（与 NotifyChannels 平级处理）
  - 真 PG 端到端：构造 alert → ingestion 模拟 fire → worker 收事件 → 按 rule 配置的 channel + user 推送
acceptance:
  - id: AC-M38-B-1
    given: DB 中一条 enabled AlertRule，notify_channels=["ch-A"]+notify_users=["user-1"]；Zabbix sync 触发的 alert 通过 triggerid→rule_id 映射到该 rule
    when: Zabbix sync 完成 publish TopicAlertCreated 事件
    then: worker.handleAlertEvent 收到后，按 rule.NotifyChannels 推送到 ch-A；用户 user-1 也收到通知（NotifyUsers 走相同 snapshot 通道）
  - id: AC-M38-B-2
    given: 同一 trigger + problem_start 在 60s 内被多次 publish（如 Zabbix sync + 手工 sync 并发）
    when: worker 收到第二条同 trigger_id + problem_start 的事件
    then: 第二条**不**触发 sender.Send（60s dedup 窗口期内）
  - id: AC-M38-B-3
    given: 一条 alert 没有匹配的 rule（triggerid 没在映射表里）
    when: publish TopicAlertCreated
    then: worker 走 fallback：推全启用 channels + 不带 NotifyUsers（不漏告警，与 ResolveAlert 路径一致）
  - id: AC-M38-B-4
    given: DB 新表 alert_rule_trigger_map (triggerid PK + rule_id FK + created_at) 已建
    when: 运维通过 POST /api/alert-rules/:id/triggers 加映射
    then: 同一 triggerid 被多个 rule 引用时按 created_at DESC 取最新（last-write-wins）
  - id: AC-M38-B-5
    given: GLPI 路径（暂未 insert alert）+ 手动建 alert + ticket→alert 路径
    when: 这三个路径 alert 入库后
    then: 这些路径暂不 publish TopicAlertCreated（GLPI 不入 alerts / 手动路径 out of scope / ticket→alert 走 ResolveAlert 路径已有 publish）；M38-B 仅修 Zabbix fire 路径
  - id: AC-M38-B-6
    given: 完整 M38-B 实施
    when: go build + go vet + go test -count=1 + 真 PG db_smoke
    then: 全部 exit 0；新增 ≥3 个 worker test + 2 个 mapper test + 2 个 dedup test + 1 个 真 PG test；旧测试 0 回归；migration 升/降级双守门
edges:
  - 映射表 triggerid 不存在 → worker fallback 推全启用 channels，不带 NotifyUsers（与 M37-A 一致）
  - 映射表的 rule 已被删 → 走 fallback（不漏告警）
  - 同一 triggerid 被映射到 rule，但 rule_id 与 alert.alert_rule_id 不一致 → 以 alert.alert_rule_id 为准（M37-A 已经写了 alert.AlertRuleID）
  - 60s dedup 窗口期内 worker 重启 → 重启后 dedup 状态丢失，新事件正常推送（best-effort，不强一致）
  - 并发 Zabbix sync 同一 triggerid+problem_start → ON CONFLICT 部分索引（已有 000027）保证 alerts 表幂等；publish 走 transaction AFTER insert；dedup 在 worker 端再次保险
  - NotifyUsers 是 user_id (UUID) 列表，worker 需要从 users 表拿到 contact info (email/phone) 推送；当前 users 表是否有 contact 字段需 grep 确认
not_goals:
  - GLPI 路径 publish TopicAlertCreated（GLPI 当前不入 alerts 表）
  - 手动建 alert publish 路径（前端操作员路径不在本 round）
  - ticket→alert 路径 publish（已通过 ResolveAlert 路径 publish）
  - 多 worker 实例横向扩展的 dedup 同步（仅单实例 in-memory dedup）
  - NotifyUsers 推送方式本身（email/phone/webhook 三选一由前端 user_handler 配置决定，worker 仅负责按 user_id 找到对应 channel）
  - OpenAPI 改 schema（本 round 内部 contract，public API 0 改）
  - Zabbix trigger tags 加 `itmanager_rule_id`（E1.a 走法不在本 round）
operational_constraints:
  - backend/migrations/000001~000037 0 改动
  - alert_rules 表结构 0 改动（NotifyChannels/NotifyUsers 字段已存在）
  - alerts 表结构 0 改动（alert_rule_id 列已存在，migration 000001 已建）
  - notification_channels 表 0 改动
  - users 表 0 改动
  - eventbus.Publish/Subscribe 接口 0 改动（TopicAlertCreated 已存在 eventbus.TopicAlertCreated 常量）
  - notification.Sender / Resolver / RegisterSender 接口 0 改动
  - 既有 worker 单元测试 0 回归（M37-A 加的 7 个 + 旧的 25+ 个）
  - 既有 service 单元测试 0 回归
  - 既有 db_smoke 真 PG 测试 0 回归
  - per-commit-push cadence 维持：每完成一个组件就 commit + push
  - Author 一律 hermes@local
evidence:
  - "TODO.md G-39 仍开放 (fire 路径)"
  - "M37-A 已 ship: b9baeaf — worker 已能按 Rule.NotifyChannels 过滤 (ResolveAlert 路径)"
  - "intent-M37-A.md 决策记录: E1.b (triggerid→rule_id 映射表) + E2.a (trigger_id+problem_start 60s 窗口 dedup)"
  - "Poison 拍板 (2026-09-12 23:35): Option E + E1.b + E2.a"
  - "backend/internal/notification/worker.go:94 Subscribe TopicAlertCreated (但无生产 publisher) — M38-B 修复此洞"
  - "backend/internal/integration/service.go:251 zabbix sync insert alert 处 — M38-B 在此 publish"
round_plan:
  commit_1: feat(M38-B): migration 000038 建 alert_rule_trigger_map + down mirror
  commit_2: feat(M38-B): models.AlertRuleTriggerMap + AlertRule.TriggerMappings 关联 (optional)
  commit_3: feat(M38-B): AlertRuleService 增 ListMappings / CreateMapping / DeleteMapping 接口
  commit_4: feat(M38-B): AlertHandler 增 POST /api/alert-rules/:id/triggers 与 GET/DELETE
  commit_5: feat(M38-B): integration.SyncFromZabbix insert 后按 triggerid 匹配 rule + publish TopicAlertCreated
  commit_6: feat(M38-B): worker handleAlertEvent 适配 AlertRuleID lookup + NotifyUsers snapshot
  commit_7: feat(M38-B): worker firededup 60s 窗口去重 (sync.Map + expiry)
  commit_8: test(M38-B): mapper 单元测试 (命中/未命中/rule 已删)
  commit_9: test(M38-B): firededup 单元测试 (窗口内去重 / 窗口外重发 / 并发)
  commit_10: test(M38-B): worker NotifyUsers 处理 + 真 PG TestDBSmoke_M38B_FirePathEnd2End
  commit_11: docs(M38-B): CHANGELOG + intent-spec + completion report
estimated_effort: "12-16h (omp subagent)"
estimated_commits: 11
spawn_meta:
  executor: omp (v18.1.18) via delegate_task
  model: deepseek-flash (default per user preference)
  brief_template: /home/webman/.hermes/state/OMP_BRIEF_TEMPLATE.md
  preflight_must_pass:
    - secret-tool lookup provider deepseek 返回非空
    - omp --version 18.1.18+
    - git status clean
    - 既有真 PG db_smoke 全绿 baseline 41 cases (现在 42 with M37-A)
  per_commit_push: true
  commit_author: "git -c user.email=hermes@local -c user.name=hermes commit"
```

---

## Context

M37-A 已 ship (commit `b9baeaf`)，ResolveAlert 路径的 AlertRule.NotifyChannels 过滤已闭环。但 fire 路径仍有两个独立缺陷：

1. **fire 不通知**：`backend/internal/integration/service.go:251` Zabbix sync 插入 alert 后**不 publish 任何事件**。`worker.go:94` 虽然 subscribe 了 `TopicAlertCreated`，但全仓库零生产 publisher——worker 订阅形同虚设。当前 alert 新增 → 运维无感知。

2. **alert ↔ rule 匹配缺失**：当前 `models.Alert.AlertRuleID` 在 Zabbix sync 路径从不写入（代码 grep 0 写入点），所以即使 worker 收到 fire 事件，payload 也没 RuleID，无法过滤。M37-A 已修好 resolve 路径的 RuleID 传递通道，fire 路径需要补这一段。

Poison 拍板决策点：E1.b（triggerid → rule_id 映射表）+ E2.a（trigger_id + problem_start 60s 窗口 dedup）。

M38-B 修上述两个缺陷 + 顺手把 M37-A 遗漏的 NotifyUsers 也处理掉。

---

## Outcomes

### Outcome 1：Zabbix fire publish 整链路
Zabbix sync 完成后，sync 末 publish `TopicAlertCreated` 给 event bus。worker.handleAlertEvent 收到后按 AlertRule.NotifyChannels 过滤推送。

### Outcome 2：triggerid → rule_id 映射
新表 `alert_rule_trigger_map`（triggerid 主键 + rule_id FK + created_at）由 ITmanager 自己持有（不依赖 Zabbix trigger.tags），运维通过 API 加/查/删。Zabbix sync 期间按 triggerid 查映射，命中就写 `alert.AlertRuleID`。

### Outcome 3：60s dedup
worker 端用 sync.Map 维护 `trigger_id+problem_start → lastFireTime`。60s 窗口内同 key 不重复 Send。Worker 重启状态丢失，best-effort 不强一致。

### Outcome 4：NotifyUsers 一并支持
worker.handleAlertEvent payload 加 `NotifyUserIDs []string`，从 rule.NotifyUsers 解析。worker 按 user_id 找到对应 channel（email/phone/webhook）推送——但具体推送逻辑由前端 user_handler 配置决定，worker 仅透传 user_id 列表。

### Outcome 5：真 PG 端到端
`TestDBSmoke_M38B_FirePathEnd2End`：构造 rule + mapping + alert → ingestion 模拟 fire → worker 收事件 → 按 rule 配置推送 ch-A + user-1。

---

## Acceptance Criteria

### AC-M38-B-1：fire 路径按 rule 推送

**Given**: enabled AlertRule，notify_channels=["ch-A"] + notify_users=["user-1"]；triggerid 在映射表里指向该 rule。

**When**: Zabbix sync 完成 publish `TopicAlertCreated`。

**Then**: worker 仅推送 ch-A（不含 ch-C）+ user-1 收到通知（NotifyUsers 走相同 snapshot 通道）。

### AC-M38-B-2：60s dedup

**Given**: 同 trigger + problem_start 在 60s 内被 publish 两次。

**When**: worker 收到第二条事件。

**Then**: 第二条不触发 sender.Send（dedup 命中）。

### AC-M38-B-3：fallback 兼容

**Given**: alert 没有匹配的 rule（triggerid 没在映射表里）。

**When**: publish `TopicAlertCreated`。

**Then**: worker 推全启用 channels（不带 NotifyUsers），与 ResolveAlert 路径行为一致。

### AC-M38-B-4：API + last-write-wins

**Given**: 运维 POST 加映射。

**When**: 同一 triggerid 被映射到多 rule。

**Then**: 按 created_at DESC 取最新（last-write-wins）。

### AC-M38-B-5：scope 限定

**Given**: GLPI/手动/ticket→alert 路径。

**When**: alert 入库。

**Then**: 这三条路径不 publish（仅 Zabbix fire 路径在 M38-B 范围）。

### AC-M38-B-6：全部 gate exit 0

`go build` + `go vet` + `go test -count=1` + 真 PG db_smoke：全 exit 0；新增 ≥8 个 test（mapper × 2 + dedup × 2 + worker × 3 + 真 PG × 1）；migration 升/降双守门。

---

## Edge cases

| Case | Behavior |
|------|----------|
| 映射表 triggerid 不存在 | worker fallback 推全启用 channels，不带 NotifyUsers |
| 映射表的 rule 已被删 | worker fallback（db lookup rule 失败 → 全启用） |
| 同 triggerid 映射到 rule，但 alert.AlertRuleID 与映射不一致 | 以 alert.AlertRuleID 为准（M37-A 已修 alert.AlertRuleID 字段可读写） |
| 60s dedup 窗口期内 worker 重启 | 重启后 dedup 状态丢失，新事件正常推送 |
| 并发 Zabbix sync 同 triggerid+problem_start | 000027 部分唯一索引保 alerts 表幂等；publish 走 transaction AFTER insert；dedup worker 端再保险 |
| NotifyUsers 是 user_id 列表，需 users 表 contact 字段 | subagent 需 grep users 表确认是否有 email/phone/webhook_url 字段；若无则 NotifyUsers 仅 log 警告 + 不发 |

---

## Operational constraints

- `backend/migrations/000001~000037` 0 改动
- `alert_rules` / `alerts` / `notification_channels` / `users` 表结构 0 改动
- `eventbus.Publish/Subscribe` 接口 0 改动
- `notification.Sender / Resolver / RegisterSender` 接口 0 改动
- 既有 worker / service / db_smoke 测试 0 回归
- per-commit-push cadence 维持
- Author 一律 hermes@local

---

## Evidence

| Outcome | Anchor |
|---------|--------|
| 1, 2 | `backend/internal/integration/service.go:251` zabbix sync insert alert |
| 1, 3 | `backend/internal/notification/worker.go:94` Subscribe TopicAlertCreated（无生产 publisher） |
| 2 | `models.Alert.AlertRuleID` (M37-A ship) |
| 0 (决策) | Poison 拍板 2026-09-12 23:35 |
| 4 (前序) | M37-A intent 决策点 E1.b / E2.a |

---

## M37-A 衔接

M38-B 建立的 triggerid → rule_id 映射表，是 fire 路径整链路的核心。Zabbix sync 期间查映射表 → 写 alert.AlertRuleID → publish TopicAlertCreated 带 RuleID → worker 端 M37-A 的过滤逻辑直接复用。
