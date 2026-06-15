-- 回滚 000002_indexes 复合索引
DROP INDEX IF EXISTS idx_assets_rack_position;
DROP INDEX IF EXISTS idx_alerts_status_created;
DROP INDEX IF EXISTS idx_alerts_asset_status;
DROP INDEX IF EXISTS idx_tickets_status;
DROP INDEX IF EXISTS idx_tickets_assignee;
DROP INDEX IF EXISTS idx_alert_rules_enabled;
DROP INDEX IF EXISTS idx_api_keys_hash_status;
DROP INDEX IF EXISTS idx_users_username_active;
