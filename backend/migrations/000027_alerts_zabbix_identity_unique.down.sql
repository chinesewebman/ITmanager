-- 000027_alerts_zabbix_identity_unique 回滚
--
-- 本迁移**可逆**：只建了一个索引，无存量数据改写。回滚即删索引。
--
-- ⚠️ 但滚掉它会**静默改变运行语义**，不只是「少个索引」：
--   service.go 的 ON CONFLICT (trigger_id, problem_start) WHERE ... 依赖这个索引当仲裁者。
--   索引一没，PG 立刻 42P10（there is no unique or exclusion constraint matching
--   the ON CONFLICT specification）→ **每一次 Zabbix 同步都 500**。
-- 也就是说：这一层滚下去，代码必须跟着回退到 000027 之前的版本，不能只滚迁移。
-- 若只是想临时关掉重复保护，正确做法是停 Zabbix 同步，而不是滚这一层。
--
-- 保留 idx_alerts_trigger_id（000018 的普通索引）—— 它不归本迁移管，
-- 且同步预查那条 `source='zabbix' AND trigger_id IN (...)` 靠它，见 up 里的说明。

DROP INDEX IF EXISTS uq_alerts_zabbix_identity;
