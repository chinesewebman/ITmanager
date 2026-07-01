-- B4: 回滚 (SQLite 镜像)
-- 重要: 先 DROP INDEX (避免 DROP COLUMN 时索引引用不存在的列)
-- 注意: sqlite < 3.35 不支持 DROP COLUMN IF EXISTS, 3.35+ 支持; 直接 DROP 即可
DROP INDEX IF EXISTS idx_assets_retired_at;
DROP INDEX IF EXISTS idx_assets_status_retired;

ALTER TABLE assets DROP COLUMN retired_at;
ALTER TABLE assets DROP COLUMN retired_by;
ALTER TABLE assets DROP COLUMN retired_reason;
ALTER TABLE assets DROP COLUMN last_known_ip6;
ALTER TABLE assets DROP COLUMN last_known_ip4;