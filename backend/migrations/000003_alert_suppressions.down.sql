-- 回滚告警抑制规则
DROP INDEX IF EXISTS idx_alert_suppressions_enabled;
DROP TABLE IF EXISTS alert_suppressions;
