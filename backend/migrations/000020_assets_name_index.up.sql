-- W6 P17: metric sync 每 5min 按 assets.name IN (...) 关联，name 无索引
-- → 每次扫全表（审计实测 75.3ms）。加索引后 1.21ms。
-- 对应 integration/metric_sync.go:154-156 的关联查询。
CREATE INDEX IF NOT EXISTS idx_assets_name
    ON assets(name);
