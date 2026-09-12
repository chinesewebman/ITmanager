-- 000038_alert_rule_trigger_map 回滚
--
-- 本迁移**可逆**：只建了一个表 + 一个普通索引。
-- 回滚先删索引（依赖关系），再删表。
--
-- ⚠️ 滚下去会**静默改变运行语义**（同 000027 的 down 注释模式）：
--   integration.SyncFromZabbix 的 triggerid → rule_id 查询依赖本表。
--   表一没，所有 fire 路径都退化到 fallback（推全启用 channels，
--   不带 NotifyUsers）—— 这是**安全退化**（不漏告警），但运维失去过滤能力。
--   同步代码同时需回退到 M38-B 之前的版本（即不写 alert.AlertRuleID 的形态），
--   才能保证「迁移滚了 + 代码还在」时不抛 NPE —— 否则 sync 路径仍会读本表 → 42P01。
--
DROP INDEX IF EXISTS idx_alert_rule_trigger_map_rule_id;

DROP TABLE IF EXISTS alert_rule_trigger_map;
