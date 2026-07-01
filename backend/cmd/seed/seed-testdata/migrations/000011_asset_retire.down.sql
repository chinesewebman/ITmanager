-- B4: 回滚 (SQLite 镜像)
DROP INDEX IF EXISTS idx_assets_retired_at;
DROP INDEX IF EXISTS idx_assets_status_retired;

ALTER TABLE assets DROP COLUMN retired_at;
ALTER TABLE assets DROP COLUMN retired_by;
ALTER TABLE assets DROP COLUMN retired_reason;
ALTER TABLE assets DROP COLUMN last_known_ip6;
ALTER TABLE assets DROP COLUMN last_known_ip4;