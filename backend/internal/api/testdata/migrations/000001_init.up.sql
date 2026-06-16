-- ============================================================
-- 集成测试 schema (sqlite 兼容版)
-- 跟 production migrations/000001_init.up.sql 镜像，但用 sqlite 类型
-- UUID 用 text + gen_random_uuid() 函数（已通过 ConnectHook 注册）
-- 时间用 DATETIME + CURRENT_TIMESTAMP
-- 数组/JSON 用 text 存（gorm 期望 text[]，sqlite 没有）
-- ============================================================

-- 11 张表，顺序按服务依赖排列

-- users
CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    nickname        TEXT,
    email           TEXT,
    phone           TEXT,
    avatar          TEXT,
    auth_type       TEXT DEFAULT 'password',
    oauth_provider  TEXT,
    oauth_id        TEXT,
    mfa_enabled     INTEGER DEFAULT 0,
    mfa_secret      TEXT,
    status          TEXT DEFAULT 'active',
    department_id   TEXT,
    role            TEXT DEFAULT 'user',
    failed_login    INTEGER DEFAULT 0,
    locked_until    DATETIME,
    last_login      DATETIME,
    last_login_ip   TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- api_keys
CREATE TABLE api_keys (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    name            TEXT NOT NULL,
    key_hash        TEXT NOT NULL,
    prefix          TEXT NOT NULL,
    permissions     TEXT,
    ip_whitelist    TEXT,
    rate_limit      INTEGER DEFAULT 1000,
    expires_at      DATETIME,
    last_used_at    DATETIME,
    status          TEXT DEFAULT 'active',
    created_at      DATETIME,
    updated_at      DATETIME
);
-- 🐛 BUG#8: api_key 同 user 不允许重名
CREATE UNIQUE INDEX idx_api_keys_user_name ON api_keys(user_id, name);

-- audit_logs
CREATE TABLE audit_logs (
    id              TEXT PRIMARY KEY,
    user_id         TEXT,
    username        TEXT,
    action          TEXT NOT NULL,
    resource        TEXT,
    resource_id     TEXT,
    method          TEXT,
    path            TEXT,
    ip              TEXT,
    user_agent      TEXT,
    status          INTEGER,
    error_msg       TEXT,
    request_id      TEXT,
    created_at      DATETIME
);

-- racks
CREATE TABLE racks (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    code            TEXT,
    site_id         TEXT,
    location        TEXT,
    height_u        INTEGER DEFAULT 42,
    width_mm        INTEGER,
    depth_mm        INTEGER,
    manufacturer    TEXT,
    model           TEXT,
    serial_number   TEXT,
    asset_tag       TEXT,
    status          TEXT DEFAULT 'active',
    install_date    DATETIME,
    warranty_until  DATETIME,
    notes           TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- sites
CREATE TABLE sites (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    code            TEXT,
    address         TEXT,
    city            TEXT,
    province        TEXT,
    country         TEXT,
    postal_code     TEXT,
    timezone        TEXT,
    contact_name    TEXT,
    contact_phone   TEXT,
    contact_email   TEXT,
    latitude        REAL,
    longitude       REAL,
    status          TEXT DEFAULT 'active',
    notes           TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- assets
CREATE TABLE assets (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    asset_tag       TEXT UNIQUE,
    asset_type      TEXT NOT NULL,
    status          TEXT DEFAULT 'in_stock',
    serial_number   TEXT,
    manufacturer    TEXT,
    model           TEXT,
    site_id         TEXT,
    rack_id         TEXT,
    rack_position   INTEGER,
    rack_size       INTEGER DEFAULT 1,
    owner_id        TEXT,
    department      TEXT,
    purchase_date   DATETIME,
    warranty_until  DATETIME,
    ip_address      TEXT,
    mac_address     TEXT,
    hostname        TEXT,
    os              TEXT,
    os_version      TEXT,
    cpu_cores       INTEGER,
    memory_gb       INTEGER,
    storage_gb      INTEGER,
    notes           TEXT,
    custom_fields   TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- asset_networks
CREATE TABLE asset_networks (
    id              TEXT PRIMARY KEY,
    asset_id        TEXT NOT NULL,
    network_type    TEXT,
    ip_address      TEXT,
    subnet_mask     TEXT,
    gateway         TEXT,
    vlan_id         INTEGER,
    mac_address     TEXT,
    is_primary      INTEGER DEFAULT 0,
    notes           TEXT,
    created_at      DATETIME,
    updated_at      DATETIME
);

-- alert_rules
CREATE TABLE alert_rules (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    resource_type   TEXT,
    metric          TEXT,
    operator        TEXT,
    threshold       REAL,
    severity        TEXT DEFAULT 'warning',
    duration        INTEGER DEFAULT 0,
    enabled         INTEGER DEFAULT 1,
    notification_channel_ids TEXT,
    asset_filter    TEXT,
    created_by      TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- alerts
CREATE TABLE alerts (
    id              TEXT PRIMARY KEY,
    rule_id         TEXT,
    asset_id        TEXT,
    severity        TEXT NOT NULL,
    status          TEXT DEFAULT 'firing',
    title           TEXT NOT NULL,
    description     TEXT,
    source          TEXT,
    metric_value    REAL,
    threshold_value REAL,
    triggered_at    DATETIME,
    acknowledged_at DATETIME,
    acknowledged_by TEXT,
    resolved_at     DATETIME,
    resolved_by     TEXT,
    notes           TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- tickets
CREATE TABLE tickets (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    description     TEXT,
    type            TEXT DEFAULT 'incident',
    priority        TEXT DEFAULT 'medium',
    status          TEXT DEFAULT 'open',
    impact          TEXT,
    urgency         TEXT,
    requester_id    TEXT,
    requester_name  TEXT,
    assignee_id     TEXT,
    assignee_name   TEXT,
    related_alert_id TEXT,
    related_asset_id TEXT,
    asset_name      TEXT,
    resolution      TEXT,
    resolved_at     DATETIME,
    closed_at       DATETIME,
    due_at          DATETIME,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- notification_channels
CREATE TABLE notification_channels (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
    config          TEXT,
    enabled         INTEGER DEFAULT 1,
    is_default      INTEGER DEFAULT 0,
    description     TEXT,
    created_at      DATETIME,
    updated_at      DATETIME,
    deleted_at      DATETIME
);

-- notification_logs
CREATE TABLE notification_logs (
    id              TEXT PRIMARY KEY,
    channel_id      TEXT,
    alert_id        TEXT,
    status          TEXT,
    error_message   TEXT,
    payload         TEXT,
    sent_at         DATETIME,
    created_at      DATETIME
);
