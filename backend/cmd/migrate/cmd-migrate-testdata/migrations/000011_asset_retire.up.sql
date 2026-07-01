-- B4: 软退役 + IP 释放 (SQLite 镜像)
-- SQLite 测试库用 TEXT/DATETIME, gorm 通过 ConnectHook 注册的 gen_random_uuid() 函数
ALTER TABLE assets ADD COLUMN last_known_ip4 TEXT;
ALTER TABLE assets ADD COLUMN last_known_ip6 TEXT;
ALTER TABLE assets ADD COLUMN retired_at DATETIME;
ALTER TABLE assets ADD COLUMN retired_reason TEXT;
ALTER TABLE assets ADD COLUMN retired_by TEXT;

CREATE INDEX IF NOT EXISTS idx_assets_retired_at ON assets (retired_at DESC);
CREATE INDEX IF NOT EXISTS idx_assets_status_retired ON assets (status, retired_at DESC);