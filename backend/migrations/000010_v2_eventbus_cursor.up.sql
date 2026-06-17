-- v2.0.0: event bus 死信队列表 + cursor 分页联合索引
-- 详见 ADR-0002 (docs/adr/0002-v2-scope.md)

-- 1. event_dlq: 死信队列 (handler 失败 3 次后的事件, 可人工补)
CREATE TABLE IF NOT EXISTS event_dlq (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    payload BLOB,
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
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_id ON audit_logs (created_at DESC, id DESC);
