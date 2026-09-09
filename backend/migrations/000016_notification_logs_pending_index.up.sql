-- W6 P13: 通知 worker 每 5s 轮询 status='pending'，但唯一索引是
-- `WHERE status='failed'` 的部分索引（000009 建的 idx_notification_logs_status），
-- 轮询查询每次全表扫 + Sort（审计实测 286.6ms）。加 pending 部分索引后 0.346ms。
-- 与 000009 的 failed 索引互补，覆盖 worker.go:198-206 的轮询热点。
CREATE INDEX IF NOT EXISTS idx_notification_logs_pending
    ON notification_logs(sent_at)
    WHERE status = 'pending';
