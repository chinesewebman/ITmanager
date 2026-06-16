-- 回滚告警误报标记
DROP INDEX IF EXISTS idx_alerts_is_false_positive;
-- SQLite 不支持 DROP COLUMN，需手动 ALTER TABLE / 新表迁移（应用启动时会 auto-migrate）
-- 这里只回滚索引 + 应用层忽略列；新部署不会创建该列
