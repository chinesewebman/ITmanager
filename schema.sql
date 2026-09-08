-- ============================================================================
-- ⚠️  设计快照，**不是建库来源**
-- ============================================================================
-- 建库唯一途径: backend/migrations/（golang-migrate）
-- 本文件是早期设计 DDL 快照，已落后于 migrations（缺 alert_suppressions /
-- oncall_* / runbooks / metric_snapshots 等后增表）。
--
-- 2026-09-09 v3 整理（依据 docs/v3-架构优化需求.md R5）：
--   * 本文件只保留**有代码实现**的表（16 张）。
--   * 39 张「有表无码」的设计表已移入 docs/schema-planned.sql（保留定义，未删除）。
--   * idc 表虽无 CRUD 代码，但被 assets / racks 外键引用，故保留在此。
--
-- 已实现（16）:
--   users, roles, permissions, user_roles, api_keys, idc, racks, assets, asset_network, metrics, alert_rules, alerts, notify_channels, tickets, topology_nodes, audit_logs
-- 设计未实现（39）:
--   见 docs/schema-planned.sql
-- ============================================================================

-- ============================================================
-- 网络运维监控平台 - 完整数据库表结构
-- 版本: v1.14
-- 生成日期: 2025-02-13
-- ============================================================

-- ============================================================
-- 第一部分：用户与权限
-- ============================================================

-- 用户表
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(50) NOT NULL UNIQUE,
    password_hash   VARCHAR(255) NOT NULL,
    nickname        VARCHAR(100),
    email           VARCHAR(255),
    phone           VARCHAR(20),
    avatar          VARCHAR(500),
    
    -- 认证
    auth_type       VARCHAR(20) DEFAULT 'password',
    oauth_provider  VARCHAR(20),
    oauth_id        VARCHAR(100),
    
    -- MFA
    mfa_enabled     BOOLEAN DEFAULT FALSE,
    mfa_secret      VARCHAR(100),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',
    failed_login    INTEGER DEFAULT 0,
    locked_until    TIMESTAMP,
    last_login      TIMESTAMP,
    last_login_ip   INET,
    
    -- 部门
    department_id   UUID,
    department_name VARCHAR(100),
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_status ON users(status);


-- 角色表
CREATE TABLE roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50) NOT NULL UNIQUE,
    code            VARCHAR(50) NOT NULL UNIQUE,
    description     TEXT,
    is_system       BOOLEAN DEFAULT FALSE,
    scope           VARCHAR(20) DEFAULT 'own',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 权限表
CREATE TABLE permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource        VARCHAR(50) NOT NULL,
    action          VARCHAR(20) NOT NULL,
    scope           VARCHAR(20) DEFAULT 'own',
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 用户角色表
CREATE TABLE user_roles (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    department_id   UUID,
    expires_at      TIMESTAMP,
    PRIMARY KEY (user_id, role_id)
);


-- API Key 表
CREATE TABLE api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            VARCHAR(100) NOT NULL,
    key_hash        VARCHAR(255) NOT NULL,
    prefix          VARCHAR(20) NOT NULL,
    permissions     TEXT[],
    ip_whitelist    INET[],
    rate_limit      INTEGER DEFAULT 1000,
    expires_at      TIMESTAMP,
    last_used_at    TIMESTAMP,
    status          VARCHAR(20) DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第二部分：资产管理
-- ============================================================

-- 机房表
CREATE TABLE idc (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    code            VARCHAR(50) NOT NULL UNIQUE,
    province        VARCHAR(50),
    city            VARCHAR(50),
    address         TEXT,
    contact         VARCHAR(100),
    contact_phone   VARCHAR(50),
    tier            VARCHAR(20),
    power_capacity  INTEGER,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 机柜表
CREATE TABLE racks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idc_id          UUID NOT NULL REFERENCES idc(id) ON DELETE CASCADE,
    name            VARCHAR(50) NOT NULL,
    total_u         INTEGER DEFAULT 42,
    max_weight      INTEGER,
    floor           VARCHAR(20),
    row             VARCHAR(20),
    column          VARCHAR(20),
    status          VARCHAR(20) DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT unique_rack UNIQUE (idc_id, name)
);

CREATE INDEX idx_racks_idc ON racks(idc_id);


-- 资产表
CREATE TABLE assets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_type      VARCHAR(50) NOT NULL,
    asset_name      VARCHAR(255) NOT NULL,
    asset_tag       VARCHAR(100),
    sn              VARCHAR(100),
    brand           VARCHAR(100),
    model           VARCHAR(100),
    purchase_date   DATE,
    warranty_end    DATE,
    vendor          VARCHAR(255),
    vendor_contact  VARCHAR(255),
    status          VARCHAR(20) DEFAULT 'active',
    online_time     TIMESTAMP,
    offline_time    TIMESTAMP,
    idc_id          UUID REFERENCES idc(id),
    rack_id         UUID REFERENCES racks(id),
    rack_position   VARCHAR(50),
    discovered_from VARCHAR(100),
    source_id       VARCHAR(100),
    tags            JSONB,
    custom_fields   JSONB,
    business_unit   VARCHAR(100),
    service_name    VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id),
    CONSTRAINT unique_asset UNIQUE (asset_tag, idc_id)
);

CREATE INDEX idx_assets_type ON assets(asset_type);
CREATE INDEX idx_assets_status ON assets(status);
CREATE INDEX idx_assets_idc ON assets(idc_id);
CREATE INDEX idx_assets_sn ON assets(sn);


-- 网络接口表
CREATE TABLE asset_network (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    interface_name  VARCHAR(50) NOT NULL,
    interface_type  VARCHAR(20),
    mac_address     VARCHAR(17),
    ipv4_address    INET,
    ipv4_netmask    INET,
    ipv6_address    INET,
    speed           INTEGER,
    duplex          VARCHAR(20),
    status          VARCHAR(20),
    connected_to    UUID,
    connected_port  VARCHAR(50),
    purpose         VARCHAR(50),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT unique_asset_interface UNIQUE (asset_id, interface_name)
);

CREATE INDEX idx_network_asset ON asset_network(asset_id);
CREATE INDEX idx_network_ipv4 ON asset_network(ipv4_address);
CREATE INDEX idx_network_mac ON asset_network(mac_address);


-- ============================================================
-- 第三部分：监控指标 (TimescaleDB)
-- ============================================================

-- 监控指标表
CREATE TABLE metrics (
    time            TIMESTAMPTZ NOT NULL,
    asset_id        UUID NOT NULL,
    metric_name     VARCHAR(100) NOT NULL,
    metric_value    DOUBLE PRECISION,
    metric_unit     VARCHAR(20),
    tags            JSONB
);

SELECT create_hypertable('metrics', 'time');

CREATE INDEX idx_metrics_asset_time ON metrics(asset_id, time DESC);
CREATE INDEX idx_metrics_name_time ON metrics(metric_name, time DESC);

-- CPU 物化视图
CREATE MATERIALIZED VIEW metrics_cpu AS
SELECT time_bucket('1 minute', time) AS bucket,
       asset_id, avg(metric_value) as value
FROM metrics WHERE metric_name = 'cpu_usage'
GROUP BY bucket, asset_id;

-- 内存物化视图
CREATE MATERIALIZED VIEW metrics_memory AS
SELECT time_bucket('1 minute', time) AS bucket,
       asset_id, avg(metric_value) as value
FROM metrics WHERE metric_name = 'memory_usage'
GROUP BY bucket, asset_id;


-- ============================================================
-- 第四部分：告警管理
-- ============================================================

-- 告警规则表
CREATE TABLE alert_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    asset_type      VARCHAR(50),
    asset_ids       UUID[],
    metric_name     VARCHAR(100) NOT NULL,
    operator        VARCHAR(10) NOT NULL,
    threshold       DOUBLE PRECISION NOT NULL,
    duration        INTEGER DEFAULT 0,
    level           VARCHAR(20) NOT NULL,
    notify_channels JSONB,
    notify_users    UUID[],
    enabled         BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);


-- 告警表
CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID REFERENCES assets(id),
    alert_rule_id   UUID REFERENCES alert_rules(id),
    level           VARCHAR(20) NOT NULL,
    title           VARCHAR(255) NOT NULL,
    message         TEXT,
    metric_name     VARCHAR(100),
    metric_value    DOUBLE PRECISION,
    threshold       DOUBLE PRECISION,
    status          VARCHAR(20) DEFAULT 'firing',
    acknowledged_at TIMESTAMP,
    acknowledged_by UUID REFERENCES users(id),
    resolved_at     TIMESTAMP,
    resolved_by     UUID REFERENCES users(id),
    notified        BOOLEAN DEFAULT FALSE,
    notify_channels JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_alerts_asset ON alerts(asset_id);
CREATE INDEX idx_alerts_status ON alerts(status);
CREATE INDEX idx_alerts_level ON alerts(level);
CREATE INDEX idx_alerts_created ON alerts(created_at DESC);


-- 通知渠道表
CREATE TABLE notify_channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type    VARCHAR(20) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    config          JSONB NOT NULL,
    enabled         BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第五部分：工单管理
-- ============================================================

-- 工单表
CREATE TABLE tickets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_no       VARCHAR(50) NOT NULL UNIQUE,
    ticket_type     VARCHAR(50) NOT NULL,
    priority        VARCHAR(20) NOT NULL,
    title           VARCHAR(255) NOT NULL,
    description     TEXT,
    attachments     JSONB,
    asset_id        UUID REFERENCES assets(id),
    alert_id        UUID REFERENCES alerts(id),
    status          VARCHAR(20) DEFAULT 'created',
    progress        DECIMAL(5,2) DEFAULT 0,
    creator_id      UUID NOT NULL REFERENCES users(id),
    assignee_id     UUID REFERENCES users(id),
    reviewer_id     UUID REFERENCES users(id),
    assignee_group  VARCHAR(50),
    cc_users        UUID[],
    planned_start   TIMESTAMP,
    planned_end     TIMESTAMP,
    actual_start    TIMESTAMP,
    actual_end      TIMESTAMP,
    result          TEXT,
    satisfaction    INTEGER,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_tickets_no ON tickets(ticket_no);
CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_assignee ON tickets(assignee_id);


-- ============================================================
-- 第六部分：网络拓扑
-- ============================================================

-- 拓扑节点表
CREATE TABLE topology_nodes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID REFERENCES assets(id),
    node_type       VARCHAR(50) NOT NULL,
    label           VARCHAR(100),
    x               FLOAT,
    y               FLOAT,
    style           JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第十部分：审计日志
-- ============================================================

-- 审计日志表
CREATE TABLE audit_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_id         UUID,
    username        VARCHAR(100),
    ip_address      INET,
    user_agent      VARCHAR(500),
    event_type      VARCHAR(100) NOT NULL,
    resource        VARCHAR(50),
    resource_id     UUID,
    action          VARCHAR(50) NOT NULL,
    changes         JSONB,
    old_values      JSONB,
    new_values      JSONB,
    result          VARCHAR(20) DEFAULT 'success',
    error_message   TEXT,
    risk_level      VARCHAR(20) DEFAULT 'low'
);

CREATE INDEX idx_audit_logs_timestamp ON audit_logs(timestamp);
CREATE INDEX idx_audit_logs_user ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource, resource_id);
