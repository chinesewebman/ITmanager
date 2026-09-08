-- 值班 + 升级策略（P1-2 优化路线图）
-- oncall_schedules: 值班组定义
-- oncall_shifts: 班次（schedule + user + 起止时间）
-- escalation_policies: 升级策略
-- escalation_levels: 升级层级（每条 policy 1-3 级）
--
-- 2026-09-09 方言修正：原 DDL 为 SQLite 方言（TEXT 主键 / DATETIME / BOOLEAN DEFAULT 1），
-- PostgreSQL 语法直接报错。改为与 models.Oncall* / Escalation* 对齐的 PG DDL。
CREATE TABLE IF NOT EXISTS oncall_schedules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    timezone    VARCHAR(50) DEFAULT 'Asia/Shanghai',
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMP DEFAULT NOW(),
    updated_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oncall_shifts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id UUID NOT NULL,
    user_id     UUID NOT NULL,
    user_name   VARCHAR(100),
    starts_at   TIMESTAMP NOT NULL,
    ends_at     TIMESTAMP NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oncall_shifts_schedule
    ON oncall_shifts(schedule_id);
CREATE INDEX IF NOT EXISTS idx_oncall_shifts_time
    ON oncall_shifts(starts_at, ends_at);

CREATE TABLE IF NOT EXISTS escalation_policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMP DEFAULT NOW(),
    updated_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS escalation_levels (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id      UUID NOT NULL,
    level          INTEGER NOT NULL,
    target_type    VARCHAR(20),
    target_id      VARCHAR(100),
    wait_minutes   INTEGER DEFAULT 5,
    notify_methods VARCHAR(255)
);
CREATE INDEX IF NOT EXISTS idx_escalation_levels_policy
    ON escalation_levels(policy_id, level);
