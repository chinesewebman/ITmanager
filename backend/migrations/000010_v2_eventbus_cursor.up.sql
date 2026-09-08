-- v2.0.0: event bus 死信队列表 + cursor 分页联合索引
-- 详见 ADR-0002 (docs/adr/0002-v2-scope.md)

-- 1. event_dlq: 死信队列 (handler 失败 3 次后的事件, 可人工补)
-- 2026-09-09 方言修正：payload 原为 SQLite 的 BLOB，PostgreSQL 用 BYTEA。
CREATE TABLE IF NOT EXISTS event_dlq (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    payload BYTEA,
    error_msg TEXT,
    attempts INT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_event_dlq_topic ON event_dlq (topic);
CREATE INDEX IF NOT EXISTS idx_event_dlq_created ON event_dlq (created_at DESC);

-- 2. alerts: cursor 分页 (created_at DESC, id DESC) 联合索引
-- 替换 v1.x 单列 created_at 索引, 走 (a, b) < (?, ?) 二元组比较
CREATE INDEX IF NOT EXISTS idx_alerts_created_id ON alerts (created_at DESC, id DESC);

-- 3. tickets: 同上
CREATE INDEX IF NOT EXISTS idx_tickets_created_id ON tickets (created_at DESC, id DESC);

-- 4. audit_logs: 同上 (list 端点新加)
-- 注：000001 建的时间列叫 `timestamp`（模型叫 created_at，列漂移见 docs/FIX-PLAN）。
-- 这里索引已存在的列；000013 会把它 RENAME 成 created_at，索引随之自动跟随。
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_id ON audit_logs ("timestamp" DESC, id DESC);
