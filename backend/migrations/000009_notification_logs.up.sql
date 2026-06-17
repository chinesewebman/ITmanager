-- v1.1 P2: 通知日志表 (notification trigger 落点)
-- 背景：codex 审查识别 M3-P2-4 "告警状态变更无通知 trigger"。
-- 当 alert 从 problem → acknowledged/resolved 时，写一行 pending 状态 log。
-- 实际发送 (dingtalk/email) 是 v1.2 的事；这里只做 trigger + 落库。
-- 表结构对齐 testdata/internal/api/testdata/migrations/000001_init.up.sql。

CREATE TABLE IF NOT EXISTS notification_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id        UUID,
    channel_id      UUID,
    channel_name    VARCHAR(100),
    recipient       VARCHAR(255),
    content         TEXT,
    status          VARCHAR(20) DEFAULT 'pending', -- pending / success / failed
    error_msg       VARCHAR(500),
    sent_at         TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 查询索引：按 alert_id 拉历史 / 按 status 找失败
CREATE INDEX IF NOT EXISTS idx_notification_logs_alert_id
    ON notification_logs(alert_id);
CREATE INDEX IF NOT EXISTS idx_notification_logs_status
    ON notification_logs(status)
    WHERE status = 'failed';
