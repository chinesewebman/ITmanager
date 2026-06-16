-- 告警抑制规则（P0-2）— sqlite 兼容
CREATE TABLE IF NOT EXISTS alert_suppressions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    severity_max INTEGER DEFAULT 3,
    host_pattern TEXT NOT NULL,
    time_window_seconds INTEGER DEFAULT 300,
    ttl_seconds INTEGER DEFAULT 0,
    enabled INTEGER DEFAULT 1,
    description TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_alert_suppressions_enabled
    ON alert_suppressions(enabled);
