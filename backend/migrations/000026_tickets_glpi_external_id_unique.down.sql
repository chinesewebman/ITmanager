-- 000026_tickets_glpi_external_id_unique 回滚
--
-- 本迁移**可逆**：只建了一个索引，无存量数据改写。回滚即删索引。
--
-- ⚠️ 但滚掉它会**静默改变运行语义**，不只是「少个索引」：
--   service.go 的 ON CONFLICT (external_id) WHERE ... 依赖这个索引当仲裁者。
--   索引一没，PG 立刻 42P10（there is no unique or exclusion constraint matching
--   the ON CONFLICT specification）→ **每一次 GLPI 同步都 500**。
-- 也就是说：这一层滚下去，代码必须跟着回退到 000026 之前的版本，不能只滚迁移。
-- 若只是想临时关掉重复保护，正确做法是停 GLPI 同步，而不是滚这一层。
--
-- 保留 idx_tickets_external_id（000019 的普通索引）—— 它不归本迁移管，
-- 且预查查询靠它，见 up 里的实测数据。

DROP INDEX IF EXISTS uq_tickets_glpi_external_id;
