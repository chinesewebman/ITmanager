-- W6 P15: Zabbix 同步预查 alerts WHERE trigger_id IN (...) AND status='problem'，
-- trigger_id 无索引 → 每次扫全表（审计实测 294.8ms）。加索引后 1.26ms。
-- 对应 integration/service.go:166-170 的预查查询。
CREATE INDEX IF NOT EXISTS idx_alerts_trigger_id
    ON alerts(trigger_id);
