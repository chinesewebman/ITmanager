-- v1.1 P2: 通知日志表 (notification trigger 落点) — SQLite 测试版本

CREATE TABLE IF NOT EXISTS notification_logs (
    id              TEXT PRIMARY KEY,
    alert_id        TEXT,
    channel_id      TEXT,
    channel_name    TEXT,
    recipient       TEXT,
    content         TEXT,
    status          TEXT DEFAULT 'pending',
    error_msg       TEXT,
    sent_at         DATETIME,
    created_at      DATETIME
);

CREATE INDEX IF NOT EXISTS idx_notification_logs_alert_id
    ON notification_logs(alert_id);
CREATE INDEX IF NOT EXISTS idx_notification_logs_status
    ON notification_logs(status)
    WHERE status = 'failed';
