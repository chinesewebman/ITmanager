-- 值班 + 升级策略（P1-2 优化路线图）
-- oncall_schedules: 值班组定义
-- oncall_shifts: 班次（schedule + user + 起止时间）
-- escalation_policies: 升级策略
-- escalation_levels: 升级层级（每条 policy 1-3 级）
CREATE TABLE IF NOT EXISTS oncall_schedules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    timezone TEXT DEFAULT 'Asia/Shanghai',
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS oncall_shifts (
    id TEXT PRIMARY KEY,
    schedule_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    user_name TEXT,
    starts_at DATETIME NOT NULL,
    ends_at DATETIME NOT NULL,
    created_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_oncall_shifts_schedule
    ON oncall_shifts(schedule_id);
CREATE INDEX IF NOT EXISTS idx_oncall_shifts_time
    ON oncall_shifts(starts_at, ends_at);

CREATE TABLE IF NOT EXISTS escalation_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS escalation_levels (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL,
    level INTEGER NOT NULL,
    target_type TEXT,
    target_id TEXT,
    wait_minutes INTEGER DEFAULT 5,
    notify_methods TEXT
);
CREATE INDEX IF NOT EXISTS idx_escalation_levels_policy
    ON escalation_levels(policy_id, level);
