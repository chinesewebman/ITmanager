-- 告警抑制规则（P0-2 优化路线图）
-- 背景：同一 host 同一 metric 1 分钟内可能产生 100+ 告警
-- 抑制规则让"窗口期内的同 host 告警"只保留 1 条，避免告警风暴
--
-- 2026-09-09 方言修正：原 DDL 为 SQLite 方言（TEXT 主键 / DATETIME / BOOLEAN DEFAULT 1 /
-- 部分索引 WHERE enabled = 1），PostgreSQL 语法直接报错，migrate.Up 会停在 000003。
-- 改为与 models.AlertSuppression 对齐的 PG DDL（UUID / TIMESTAMP / BOOLEAN）。
CREATE TABLE IF NOT EXISTS alert_suppressions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(100) NOT NULL,
    severity_max        INTEGER DEFAULT 3,
    host_pattern        VARCHAR(255),
    time_window_seconds INTEGER DEFAULT 300,
    ttl_seconds         INTEGER DEFAULT 0,
    enabled             BOOLEAN DEFAULT TRUE,
    description         TEXT,
    created_at          TIMESTAMP DEFAULT NOW(),
    updated_at          TIMESTAMP DEFAULT NOW()
);

-- 用于 List 列表按启用状态过滤
CREATE INDEX IF NOT EXISTS idx_alert_suppressions_enabled
    ON alert_suppressions(enabled)
    WHERE enabled = TRUE;
