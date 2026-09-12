-- 000028: tickets 表 schema 对齐 (M34 D-1 工单子集)
--
-- 背景: 000013 已完成 D-1 工单表 95% 的对齐 (RENAME ticket_no->ticket_number,
-- RENAME creator_id->requester_id, 14 列 ADD COLUMN IF NOT EXISTS, UNIQUE 索引).
-- 本文件不重复 000013 已做的事 (用 IF NOT EXISTS 守卫), 只补齐以下两块:
--
--   A. 模型 24 字段 vs 000013 后 tickets 列数 对齐确认 (idempotent 列表,
--      列已存在则 noop)
--   B. ticket_type 的 NOT NULL 守卫 -- 000001 创建时 ticket_type NOT NULL,
--      模型 24 字段里 ticket_type 走 gorm:size:20 (无 not null), 当 value
--      为空字符串时会被 PG 拒绝. 改为 DROP NOT NULL 与模型语义一致.
--
-- 原则 (见 docs/FIX-PLAN-D1-D2-TICKETS.md §2.1):
--   1. 非破坏 (不 DROP 任何已有列)
--   2. 幂等 (所有 DDL 都用 IF NOT EXISTS / DO $$ 守卫)
--   3. 不动 ticket_number 唯一索引 (000013 的 idx_tickets_ticket_number 已生效)

-- A. 列存在性确认 (ADD COLUMN IF NOT EXISTS = noop)
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS category        VARCHAR(50);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS tags            JSONB DEFAULT '[]';
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_name  VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_email VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS assignee_name   VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS asset_name      VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolution      TEXT;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolved_at     TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS closed_at       TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS due_date        TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS external_id     VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS source          VARCHAR(20) DEFAULT 'manual';

-- B. ticket_type NOT NULL -> 允许 NULL (模型不写 not null, 走 gorm:size:20)
ALTER TABLE tickets ALTER COLUMN ticket_type DROP NOT NULL;
