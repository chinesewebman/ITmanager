-- 复合索引迁移（C-P2 性能优化）
-- 背景：codex 审查识别 P1 项，外键 + 复合查询缺配套索引
-- 单条简单 WHERE 已有单列索引；这里专门补复合/覆盖/降序索引

-- assets: 机柜内设备按 rack_position 升序排（GetRackDevices 用）
CREATE INDEX IF NOT EXISTS idx_assets_rack_position
    ON assets(rack_id, rack_position)
    WHERE rack_position IS NOT NULL;

-- alerts: 工单创建时按 status + created_at 倒序（Stats/List 用）
CREATE INDEX IF NOT EXISTS idx_alerts_status_created
    ON alerts(status, created_at DESC);

-- alerts: 按 asset + status 查工单（GetRackDevices alert count 用）
CREATE INDEX IF NOT EXISTS idx_alerts_asset_status
    ON alerts(asset_id, status);

-- tickets: 工单列表按 status 过滤
CREATE INDEX IF NOT EXISTS idx_tickets_status
    ON tickets(status);

-- tickets: 工单按 assignee 查询（运维工作台）
CREATE INDEX IF NOT EXISTS idx_tickets_assignee
    ON tickets(assignee_id)
    WHERE assignee_id IS NOT NULL;

-- alert_rules: 规则启用状态过滤（ListRules）
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled
    ON alert_rules(is_enabled)
    WHERE is_enabled = true;

-- api_keys: 按 hash 查询 + 状态过滤（AuthMiddleware 用）
CREATE INDEX IF NOT EXISTS idx_api_keys_hash_status
    ON api_keys(key_hash, status);

-- users: 登录时按 username 查（用户量上来后避免全表扫）
CREATE INDEX IF NOT EXISTS idx_users_username_active
    ON users(username)
    WHERE status = 'active';
