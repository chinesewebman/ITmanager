-- 000013_schema_align 回滚
--
-- 注意：本迁移**不 DROP 旧列、不删除数据**，所以回滚也只撤销「新增的列与索引」，
-- 不把 RENAME 倒回去（倒回会让已写入模型列的数据与旧列名脱节）。
-- 真正的回滚应走备份恢复；这里只保证重复执行 Up 是幂等的。
-- 注意：不要 DROP tickets.ticket_type —— 它在 000001 就存在（up 里的 ADD COLUMN 是 no-op），
-- 删掉它等于丢列丢数据，且 down 后 schema 既不满足 000001 也不满足模型（审计 阻断-2）。

DROP INDEX IF EXISTS idx_users_role;
DROP INDEX IF EXISTS idx_users_deleted_at;

DROP INDEX IF EXISTS idx_tickets_ticket_number;
ALTER TABLE tickets DROP COLUMN IF EXISTS source;
ALTER TABLE tickets DROP COLUMN IF EXISTS external_id;
ALTER TABLE tickets DROP COLUMN IF EXISTS due_date;
ALTER TABLE tickets DROP COLUMN IF EXISTS closed_at;
ALTER TABLE tickets DROP COLUMN IF EXISTS resolved_at;
ALTER TABLE tickets DROP COLUMN IF EXISTS resolution;
ALTER TABLE tickets DROP COLUMN IF EXISTS assignee_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS requester_email;
ALTER TABLE tickets DROP COLUMN IF EXISTS requester_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS asset_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS tags;
ALTER TABLE tickets DROP COLUMN IF EXISTS category;

DROP INDEX IF EXISTS idx_alerts_source;
DROP INDEX IF EXISTS idx_alerts_severity;
DROP INDEX IF EXISTS idx_alerts_host_id;
DROP INDEX IF EXISTS idx_alerts_alert_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS repeat_count;
ALTER TABLE alerts DROP COLUMN IF EXISTS source;
ALTER TABLE alerts DROP COLUMN IF EXISTS ticket_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS resolve_user;
ALTER TABLE alerts DROP COLUMN IF EXISTS resolve_time;
ALTER TABLE alerts DROP COLUMN IF EXISTS ack_user;
ALTER TABLE alerts DROP COLUMN IF EXISTS ack_time;
ALTER TABLE alerts DROP COLUMN IF EXISTS duration;
ALTER TABLE alerts DROP COLUMN IF EXISTS problem_end;
ALTER TABLE alerts DROP COLUMN IF EXISTS problem_start;
ALTER TABLE alerts DROP COLUMN IF EXISTS problem;
ALTER TABLE alerts DROP COLUMN IF EXISTS severity_name;
ALTER TABLE alerts DROP COLUMN IF EXISTS severity;
ALTER TABLE alerts DROP COLUMN IF EXISTS trigger_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS trigger_name;
ALTER TABLE alerts DROP COLUMN IF EXISTS host_ip;
ALTER TABLE alerts DROP COLUMN IF EXISTS host_name;
ALTER TABLE alerts DROP COLUMN IF EXISTS host_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS alert_id;

DROP INDEX IF EXISTS idx_alert_rules_is_enabled;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS updated_by;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS priority;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS notify_enabled;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS severity_name;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS severity;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS metric;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS host_group;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS condition;

DROP INDEX IF EXISTS idx_assets_net_box_id;
ALTER TABLE assets DROP COLUMN IF EXISTS source;
ALTER TABLE assets DROP COLUMN IF EXISTS net_box_id;
ALTER TABLE assets DROP COLUMN IF EXISTS rack_name;
ALTER TABLE assets DROP COLUMN IF EXISTS site_name;

DROP INDEX IF EXISTS idx_racks_net_box_id;
ALTER TABLE racks DROP COLUMN IF EXISTS net_box_id;
ALTER TABLE racks DROP COLUMN IF EXISTS site_name;

DROP INDEX IF EXISTS idx_sites_net_box_id;
ALTER TABLE sites DROP COLUMN IF EXISTS net_box_id;

ALTER TABLE notification_channels DROP COLUMN IF EXISTS is_default;

ALTER TABLE audit_logs DROP COLUMN IF EXISTS request_id;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS error_msg;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS status;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS ip;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS path;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS method;

ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE users DROP COLUMN IF EXISTS role;
