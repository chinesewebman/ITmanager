-- v2.0.0 rollback
DROP INDEX IF EXISTS idx_audit_logs_created_id;
DROP INDEX IF EXISTS idx_tickets_created_id;
DROP INDEX IF EXISTS idx_alerts_created_id;
DROP INDEX IF EXISTS idx_event_dlq_created;
DROP INDEX IF EXISTS idx_event_dlq_topic;
DROP TABLE IF EXISTS event_dlq;
