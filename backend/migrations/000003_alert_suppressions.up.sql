-- 告警抑制规则（P0-2 优化路线图）
-- 背景：同一 host 同一 metric 1 分钟内可能产生 100+ 告警
-- 抑制规则让"窗口期内的同 host 告警"只保留 1 条，避免告警风暴
CREATE TABLE IF NOT EXISTS alert_suppressions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    severity_max INTEGER DEFAULT 3,
    host_pattern TEXT NOT NULL,
    time_window_seconds INTEGER DEFAULT 300,
    ttl_seconds INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT 1,
    description TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

-- 用于 List 列表按启用状态过滤
CREATE INDEX IF NOT EXISTS idx_alert_suppressions_enabled
    ON alert_suppressions(enabled)
    WHERE enabled = 1;
