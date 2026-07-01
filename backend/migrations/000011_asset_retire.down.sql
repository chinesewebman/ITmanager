-- B4: 回滚
DROP INDEX IF EXISTS idx_assets_retired_at;
DROP INDEX IF EXISTS idx_assets_status_retired;

ALTER TABLE assets DROP COLUMN IF EXISTS last_known_ip4;
ALTER TABLE assets DROP COLUMN IF EXISTS last_known_ip6;
ALTER TABLE assets DROP COLUMN IF EXISTS retired_at;
ALTER TABLE assets DROP COLUMN IF EXISTS retired_reason;
ALTER TABLE assets DROP COLUMN IF EXISTS retired_by;