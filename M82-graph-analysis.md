# M82 Graph Analysis — G-39 tickOnce 路径收口 (writeNotificationTrigger 按 rule.NotifyChannels 过滤)

## 双轨状态

### graphify (多仓库多视图诊断)
- latest run: 2026-09-16 13:25
- **0 anomalies** (multigraph 综合: cycle / dead-code / god-module / cross-layer-coupling 4 维度全绿)
- node/edge 增长:
  - M81 (前): 7,143 nodes / 14,690 edges (M81 是 constants 提取, 无业务图变化)
  - M82 (本轮): 7,150 nodes / 14,710 edges (+7 nodes / +20 edges — 新增 helper + 7 测试函数 + helper ↔ writeNotificationTrigger 引用边)

### codegraph (结构化查询)
- latest run: 2026-09-16 13:25
- 6,540 nodes / 16,250 edges (M81 baseline 6,532 / 16,226 → +8 / +24)
- `codegraph query "M82 writeNotificationTrigger"`:
  - 1 method (改签名): `writeNotificationTrigger` @ `backend/internal/service/alert_service.go:498` (旧: alertID uuid.UUID → 新: alert *models.Alert)
  - 1 helper (新): `filterNotificationChannelsByIDs` @ `backend/internal/service/alert_service.go:548`
  - 7 test functions (新/改):
    - `TestWriteNotificationTrigger_HasChannels_WritesPendingLogs` @ `alert_notification_trigger_test.go:18` (改: alertID → alert)
    - `TestWriteNotificationTrigger_NoChannels_NoInsert` @ `:46` (改: alertID → alert)
    - `TestWriteNotificationTrigger_ChannelQueryFails_DoesNotError` @ `:69` (改: alertID → alert)
    - `TestWriteNotificationTrigger_RuleEmpty_NoLogs` @ `:85` (新)
    - `TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` @ `:120` (新)
    - `TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` @ `:158` (新)
    - `TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` @ `:198` (新)
    - `TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite` @ `:325` (新, 真 sqlite in-memory 守卫)
    - `TestFilterNotificationChannelsByIDs_FiltersToListed` @ `:238` (新, helper 白盒)
    - `TestFilterNotificationChannelsByIDs_PreservesChannelsOrder` @ `:258` (新, helper 白盒)
  - 1 helper func: `newM82SQLiteDB` @ `:239` (新, 真 sqlite DDL builder)

## M82 涉及的图变更

### 节点 (8 新 + 2 抽象)
- `alert_service.writeNotificationTrigger` (改签名: alertID → alert, +1 个调用方参数)
- `alert_service.filterNotificationChannelsByIDs` (新 helper 节点)
- `alert_service.Acknowledge` (改 1 调用点: alert.ID → alert)
- `alert_service.Resolve` (改 1 调用点: alert.ID → alert)
- `alert_notification_trigger_test.go:TestWriteNotificationTrigger_RuleEmpty_NoLogs` (新测试节点)
- `alert_notification_trigger_test.go:TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed` (新)
- `alert_notification_trigger_test.go:TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled` (新)
- `alert_notification_trigger_test.go:TestWriteNotificationTrigger_NoRule_FallbackAllEnabled` (新)
- `alert_notification_trigger_test.go:TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite` (新, 真 sqlite 守卫)
- `alert_notification_trigger_test.go:TestFilterNotificationChannelsByIDs_FiltersToListed` (新)
- `alert_notification_trigger_test.go:TestFilterNotificationChannelsByIDs_PreservesChannelsOrder` (新)
- `alert_notification_trigger_test.go:newM82SQLiteDB` (新)

### 边 (5 改 + 11 新)
- **改** `Acknowledge → writeNotificationTrigger`: 旧边 `alert.ID (uuid.UUID)` → 新边 `alert (*models.Alert)` (类型签名变)
- **改** `Resolve → writeNotificationTrigger`: 同上
- **改** `writeNotificationTrigger → loadRuleNotifyChannelIDs` (M37-A ship helper): 旧边无, 新边 (M82 引入调用)
- **改** `writeNotificationTrigger → filterNotificationChannelsByIDs`: 新本地 helper 调用 (M82 引入)
- **改** `writeNotificationTrigger → models.NotificationLog`: Create 调用, 边参数从 `alertID` 改 `alert.ID`
- **新** `loadRuleNotifyChannelIDs → alert_rules` SELECT: M82 引入 (M37-A ship helper 的复用, 现被 writeNotificationTrigger 也调用)
- **新** `filterNotificationChannelsByIDs → models.NotificationChannel`: helper 白盒测试直接验证
- **新** `RuleEmpty_NoLogs → mock.ExpectQuery(alert_rules regex)`: sqlmock 期望序列节点
- **新** `RuleWithChannels_WritesOnlyListed → mock.ExpectQuery(alert_rules regex) + mock.ExpectQuery(notification_channels regex) + mock.ExpectQuery(INSERT INTO notification_logs regex)`: sqlmock 序列
- **新** `RuleNotFound_FallbackAllEnabled → mock.ExpectQuery(notification_channels regex) + mock.ExpectQuery(alert_rules regex, error)`: 同形
- **新** `NoRule_FallbackAllEnabled → mock.ExpectQuery(notification_channels regex)`: 同形
- **新** `RuleEmpty_NoLogsSQLite → sqlite.Open(:memory:) + 4 表 DDL + notification_logs.Count()`: 真 sqlite e2e 守卫
- **新** `FilterNotificationChannelsByIDs_FiltersToListed → filterNotificationChannelsByIDs`: helper 白盒直测
- **新** `FilterNotificationChannelsByIDs_PreservesChannelsOrder → filterNotificationChannelsByIDs`: 同形, 顺序守卫

### 与 M37-A / M38-B 的图关系

M82 是 M37-A + M38-B 的**第三轮收口** — G-39 全族收口的最后一块. 图上:

- **M37-A (bus path ResolveAlert)**:
  - `alert_service.Resolve → eventbus.Publish → worker.handleAlertEvent → filterChannelsByIDs → notification_channels`
  - 边路径走 `AlertEventPayload.NotifyChannelIDs`
- **M38-B (bus path Zabbix fire)**:
  - `integration.SyncFromZabbix → publishFireEvents → loadRuleNotifySnapshot → eventbus.Publish → worker.handleAlertEvent`
  - 边路径同样走 `AlertEventPayload.NotifyChannelIDs`
- **M82 (tickOnce path Acknowledge + Resolve)**:
  - `alert_service.Acknowledge/Resolve → writeNotificationTrigger → loadRuleNotifyChannelIDs → filterNotificationChannelsByIDs → notification_channels`
  - 边路径走 service 层直查, **不**走 eventbus, **不**走 `AlertEventPayload`

三条路径都收口到同一个语义: "alert 关联的 AlertRule.NotifyChannels = 推哪些 channel". 但实现各异:
- bus 路径: payload snapshot 推 UUID 列表给 worker, worker 端 filter (避免二次 DB 读 rule)
- tickOnce 路径: service 端实时 SELECT rule + filter, 然后落 pending log

这种「同语义, 不同实现」的形态是 M3 审计观察项就指出的 — 「同一概念多处实现, 严格度不一致」. M82 不强求统一实现 (eventbus 加新 topic `alert.acknowledged` / 让 tickOnce 路径也走 eventbus 属独立重构), 只强求「两边语义对齐」.

### 双轨跨仓边
无 — M82 改动仅在 backend service 包, 不跨仓, 不动 frontend. codegraph 不增加 frontend 节点.

## M82 实证的「同源问题收口」模式

M37-A + M38-B + M82 是 G-39 全族收口的**三轮实证**, 沿用同款模式:
1. **复用既有 helper**: M82 复用 M37-A 的 `loadRuleNotifyChannelIDs` (rule JSON 解析 + nil/[]/[] 契约), 不重写
2. **语义对齐**: M82 的 rule filter fallback 链与 M37-A 的 bus 路径 fallback 链逐字对齐 (rule 加载失败 → fallback / 空数组 → 0 / nil → fallback)
3. **加契约测试**: M82 加 4 sqlmock + 1 sqlite + 2 helper = 7 测试, 覆盖序列、顺序、行数、反向断言
4. **mutation inversion 实证**: M82 三反证 (bypass / helper 失效 / 空数组 fallback 退化) 全红 → 还原绿

M82 给后续 round (M83 candidate = G-39 剩余 `config.DingtalkConfig` 无人使用 / G-57 同族 6 个裸字符串) 提供了**直接可复用**的范本:
- 看既有代码哪个 sink 全表扫 → 锁定遗漏路径
- 复用既有 helper (M37-A / M38-B 已 ship 的)
- 加 4 sqlmock + 1 sqlite + 2 helper 测试
- 跑 mutation inversion 3 反证

## 与 G-57 (M81 派生 TODO) 的关系

G-57 = `glpi_skipped` / `netbox` / `glpi` / `*_field_truncations` 4 个同族裸字符串 (`data.synced` 自由形态的 schema 收口范围), 与 G-39 正交:
- G-39 = notification 推送渠道选择失效 (M37-A/M38-B/M82 三轮收口)
- G-57 = sync 响应字段命名约定失效 (M81 留作同族候选 M82/M83, 实际未 ship)

M82 不动 G-57, 沿用 M81 留的「G-57 同族收口是低成本 path」判断. M82 实证的「复用既有 helper + 加契约测试」模式同样适用于 G-57 — 后端 `results["glpi_skipped"]` 字面量提取为常量 + 加 grep guard 跨语言 drift 测试, 沿用 M81 的 `integration.KeyZabbixTruncated` + `frontend/src/services/syncKeys.ts` 模式即可.