-- ============================================================================
-- 设计未实现的表（design-only / planned）
-- ============================================================================
-- 来源: 从 schema.sql 拆分（2026-09-09 v3 整理，docs/v3-架构优化需求.md R5）
-- 状态: 全部**零代码引用**（0 行 Go 实现、0 端点、0 页面）
-- 保留原因: 可追溯设计意图；**不删除**。
--
-- ⚠️ 本文件**不是可执行建库脚本**：其中多张表通过外键引用 schema.sql 中的
--    已实现表，单独执行会因缺少被引用表而失败。仅作设计归档。
--
-- 清单（39）:
--   role_permissions, user_sessions, line_types, lines, line_changes, line_monitors, asset_history, asset_hardware, asset_software, collect_tasks, collectors, discovery_tasks, discovery_results, snmp_credential_compliance, snmp_devices, alert_notifications, ticket_steps, step_progress_history, ticket_progress, ticket_templates, ticket_workflows, ticket_approvals, ticket_sla, ticket_segments, ticket_handoffs, topology_links, vmware_connections, vmware_hosts, vmware_vms, vmware_sync_history, migration_tasks, migration_node_status, migration_history, llm_providers, kb_documents, ai_conversations, ai_messages, system_config, scheduled_tasks
-- ============================================================================


-- 角色权限表
CREATE TABLE role_permissions (
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id   UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);


-- 用户会话表
CREATE TABLE user_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token           VARCHAR(500) NOT NULL UNIQUE,
    refresh_token   VARCHAR(500),
    expires_at      TIMESTAMP NOT NULL,
    ip_address     INET,
    user_agent      VARCHAR(500),
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_sessions_token ON user_sessions(token);
CREATE INDEX idx_sessions_user ON user_sessions(user_id);


-- 专线类型表
CREATE TABLE line_types (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            VARCHAR(20) NOT NULL UNIQUE,
    name            VARCHAR(50) NOT NULL,
    description     TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 专线信息表
CREATE TABLE lines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    line_no         VARCHAR(50) NOT NULL UNIQUE,
    line_type_id    UUID REFERENCES line_types(id),
    
    -- 运营商信息
    carrier         VARCHAR(50) NOT NULL,
    contract_no     VARCHAR(50),
    contract_start  DATE,
    contract_end    DATE,
    monthly_fee     DECIMAL(10,2),
    contact_person  VARCHAR(50),
    contact_phone   VARCHAR(20),
    fault_phone     VARCHAR(20),
    
    -- 带宽信息
    bandwidth       VARCHAR(30),
    bandwidth_mbps  INTEGER,
    
    -- 端点A
    endpoint_a_idc_id UUID REFERENCES idc(id),
    endpoint_a_rack_id UUID REFERENCES racks(id),
    endpoint_a_device VARCHAR(255),
    endpoint_a_interface VARCHAR(50),
    endpoint_a_vlan   INTEGER,
    
    -- 端点B
    endpoint_b_idc_id UUID REFERENCES idc(id),
    endpoint_b_rack_id UUID REFERENCES racks(id),
    endpoint_b_device VARCHAR(255),
    endpoint_b_interface VARCHAR(50),
    endpoint_b_vlan   INTEGER,
    
    -- 业务信息
    purpose         VARCHAR(200),
    business_unit   VARCHAR(50),
    is_critical     BOOLEAN DEFAULT FALSE,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',
    install_date    DATE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_lines_no ON lines(line_no);
CREATE INDEX idx_lines_type ON lines(line_type_id);
CREATE INDEX idx_lines_carrier ON lines(carrier);
CREATE INDEX idx_lines_status ON lines(status);


-- 专线变更记录
CREATE TABLE line_changes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    line_id         UUID NOT NULL REFERENCES lines(id),
    change_type     VARCHAR(20) NOT NULL,
    change_field    VARCHAR(50),
    old_value       TEXT,
    new_value       TEXT,
    reason          TEXT,
    operator_id     UUID REFERENCES users(id),
    changed_at      TIMESTAMP DEFAULT NOW()
);


-- 专线监控配置
CREATE TABLE line_monitors (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    line_id         UUID NOT NULL REFERENCES lines(id),
    monitor_type    VARCHAR(20),
    target          VARCHAR(100),
    interval        INTEGER DEFAULT 60,
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 资产变更历史
CREATE TABLE asset_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    field_name      VARCHAR(50) NOT NULL,
    old_value       TEXT,
    new_value       TEXT,
    change_reason   TEXT,
    operator_id     UUID REFERENCES users(id),
    operator_name   VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_asset_history_asset ON asset_history(asset_id);


-- 硬件配置表
CREATE TABLE asset_hardware (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    cpu_model       VARCHAR(255),
    cpu_cores       INTEGER,
    cpu_physical    INTEGER,
    cpu_threads     INTEGER,
    cpu_speed       VARCHAR(50),
    memory_total    BIGINT,
    memory_slots    INTEGER,
    memory_used     BIGINT,
    disk_total      BIGINT,
    disk_type       VARCHAR(20),
    disk_count      INTEGER,
    raid_config     VARCHAR(50),
    power_count     INTEGER,
    power_status    VARCHAR(20),
    fan_count       INTEGER,
    fan_status      VARCHAR(20),
    mgmt_ip         INET,
    mgmt_type       VARCHAR(20),
    mgmt_username   VARCHAR(100),
    mgmt_password   VARCHAR(255),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 软件信息表
CREATE TABLE asset_software (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    software_type   VARCHAR(50) NOT NULL,
    software_name   VARCHAR(100) NOT NULL,
    version         VARCHAR(50),
    install_path    VARCHAR(255),
    port            INTEGER,
    process_name    VARCHAR(100),
    status          VARCHAR(20),
    kernel_version  VARCHAR(50),
    os_version      VARCHAR(100),
    arch            VARCHAR(20),
    ssh_version     VARCHAR(50),
    ssl_enabled     BOOLEAN,
    ssl_expiry      DATE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_software_asset ON asset_software(asset_id);


-- 采集任务表
CREATE TABLE collect_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    target_type     VARCHAR(20) NOT NULL,
    target_config   JSONB NOT NULL,
    metric_names    TEXT[],
    interval        INTEGER DEFAULT 60,
    timeout         INTEGER DEFAULT 30,
    status          VARCHAR(20) DEFAULT 'active',
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 采集器配置表
CREATE TABLE collectors (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    collector_type  VARCHAR(20) NOT NULL,
    config          JSONB NOT NULL,
    status          VARCHAR(20) DEFAULT 'active',
    last_heartbeat  TIMESTAMP,
    metrics_collected BIGINT DEFAULT 0,
    errors_count    INTEGER DEFAULT 0,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 自动发现任务表
CREATE TABLE discovery_tasks (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(100) NOT NULL,
    description         TEXT,
    
    -- 发现范围
    target_network      CIDR NOT NULL,          -- 如 192.168.1.0/24
    target_ports        INTEGER[],              -- SNMP端口，默认 [161]
    
    -- SNMP 配置（支持多个通讯字）
    snmp_versions       VARCHAR(10)[],          -- ['v2c', 'v3'] 或 ['v2c']
    community_strings   VARCHAR(100)[],         -- 最多5个通讯字尝试，如 ['public', 'private', 'community']
    
    -- SNMP v3 配置（可选）
    security_level      VARCHAR(20),            -- noAuthNoPriv/authPriv/authNoPriv
    auth_protocol       VARCHAR(20),            -- MD5/SHA
    auth_password       VARCHAR(100),
    priv_protocol       VARCHAR(20),            -- DES/AES
    priv_password       VARCHAR(100),
    
    -- 发现选项
    discover_methods    VARCHAR(20)[],          -- ['snmp', 'ping', 'arp']
    ping_timeout        INTEGER DEFAULT 3000,   -- ping超时(毫秒)
    snmp_timeout        INTEGER DEFAULT 5000,  -- SNMP超时(毫秒)
    snmp_retries        INTEGER DEFAULT 3,     -- SNMP重试次数
    
    -- 采集选项
    collect_metrics     BOOLEAN DEFAULT TRUE,   -- 发现后自动采集指标
    oid_list            TEXT[],                 -- 自定义OID列表
    
    -- 调度
    schedule_type       VARCHAR(20),            -- manual/periodic/onetime
    schedule_cron       VARCHAR(50),            -- cron表达式
    schedule_interval   INTEGER,                -- 间隔(秒)，periodic用
    
    -- 状态
    status              VARCHAR(20) DEFAULT 'pending',  -- pending/running/paused/completed/failed
    progress            INTEGER DEFAULT 0,      -- 进度百分比
    total_ips           INTEGER,                 -- 扫描IP总数
    discovered_ips      INTEGER DEFAULT 0,       -- 已发现IP数
    
    -- 结果
    discovered_count    INTEGER DEFAULT 0,       -- 发现设备数
    added_assets_count  INTEGER DEFAULT 0,       -- 入库资产数
    
    created_by          UUID REFERENCES users(id),
    started_at          TIMESTAMP,
    completed_at        TIMESTAMP,
    created_at          TIMESTAMP DEFAULT NOW(),
    updated_at          TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_discovery_tasks_status ON discovery_tasks(status);
CREATE INDEX idx_discovery_tasks_network ON discovery_tasks(target_network);


-- 发现结果表（每个发现的IP）
CREATE TABLE discovery_results (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id             UUID REFERENCES discovery_tasks(id) ON DELETE CASCADE,
    
    -- IP信息
    ip                  INET NOT NULL,
    port                INTEGER,
    mac_address         VARCHAR(17),
    
    -- 识别信息
    hostname            VARCHAR(255),
    vendor              VARCHAR(100),
    device_type         VARCHAR(50),             -- router/switch/server/workstation/printer
    model               VARCHAR(100),
    sys_descr           TEXT,
    sys_objectid        VARCHAR(255),
    
    -- SNMP信息（成功通讯字）
    snmp_version        VARCHAR(10),
    snmp_community      VARCHAR(100),           -- 成功的通讯字
    snmp_timeout        INTEGER,
    
    -- 采集的指标
    metrics             JSONB,
    
    -- 关联资产
    asset_id            UUID REFERENCES assets(id),
    merge_action        VARCHAR(20),            -- new/update/exists/ignored
    
    -- 状态
    status              VARCHAR(20) DEFAULT 'pending',  -- pending/confirmed/merged/ignored
    error_message       TEXT,
    
    created_at          TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_discovery_results_task ON discovery_results(task_id);
CREATE INDEX idx_discovery_results_ip ON discovery_results(ip);
CREATE INDEX idx_discovery_results_asset ON discovery_results(asset_id);


-- SNMP通讯字合规记录表
CREATE TABLE snmp_credential_compliance (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id            UUID REFERENCES assets(id),
    snmp_device_id      UUID REFERENCES snmp_devices(id),
    
    host                INET NOT NULL,            -- 设备IP
    port                INTEGER DEFAULT 161,     -- SNMP端口
    snmp_version        VARCHAR(10),              -- v1/v2c/v3
    
    -- 通讯字（加密存储）
    community_encrypted VARCHAR(255),
    
    -- 合规检查结果
    is_compliant        BOOLEAN DEFAULT FALSE,
    compliance_level    VARCHAR(20) DEFAULT 'non_compliant',  -- compliant/warning/non_compliant
    check_result        TEXT,                   -- 检查详情
    used_community      VARCHAR(100),           -- 实际使用的通讯字（脱敏后显示，如 p***c）
    
    -- 检查信息
    discovered_at      TIMESTAMP DEFAULT NOW(), -- 发现时间
    last_checked_at    TIMESTAMP,               -- 最近检查时间
    check_count        INTEGER DEFAULT 1,       -- 检查次数
    
    -- 整改跟踪
    ticket_id          UUID,                    -- 关联整改工单
    is_rectified       BOOLEAN DEFAULT FALSE,   -- 是否已整改
    rectified_at       TIMESTAMP,               -- 整改时间
    rectification_note TEXT,                   -- 整改说明
    
    created_at          TIMESTAMP DEFAULT NOW(),
    updated_at          TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_snmp_compliance_asset ON snmp_credential_compliance(asset_id);
CREATE INDEX idx_snmp_compliance_host ON snmp_credential_compliance(host);
CREATE INDEX idx_snmp_compliance_level ON snmp_credential_compliance(compliance_level);
CREATE INDEX idx_snmp_compliance_rectified ON snmp_credential_compliance(is_rectified);


-- SNMP 设备配置表
CREATE TABLE snmp_devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID REFERENCES assets(id),
    host            INET NOT NULL,
    port            INTEGER DEFAULT 161,
    version         VARCHAR(10) DEFAULT 'v2c',
    community       VARCHAR(100),
    security_level  VARCHAR(20),
    auth_protocol   VARCHAR(20),
    auth_password   VARCHAR(100),
    priv_protocol   VARCHAR(20),
    priv_password   VARCHAR(100),
    oid_list        TEXT[],
    timeout         INTEGER DEFAULT 5000,
    retries         INTEGER DEFAULT 3,
    status          VARCHAR(20) DEFAULT 'active',
    last_polled     TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_snmp_devices_host ON snmp_devices(host);


-- 告警通知记录表
CREATE TABLE alert_notifications (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id        UUID NOT NULL REFERENCES alerts(id),
    channel_id      UUID NOT NULL REFERENCES notify_channels(id),
    status          VARCHAR(20) DEFAULT 'pending',
    response        TEXT,
    sent_at         TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 工单步骤表
CREATE TABLE ticket_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    step_order      INTEGER NOT NULL,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    weight          DECIMAL(3,1) DEFAULT 1.0,
    estimated_time  INTEGER,
    input_type      VARCHAR(20) NOT NULL,
    target_value    TEXT,
    target_count    INTEGER,
    completed_value TEXT,
    completed_count INTEGER,
    progress_percent DECIMAL(5,2) DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'pending',
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 步骤进度历史
CREATE TABLE step_progress_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    step_id         UUID NOT NULL REFERENCES ticket_steps(id),
    submitted_value TEXT,
    submitted_count INTEGER,
    progress_percent DECIMAL(5,2),
    audit_status    VARCHAR(20),
    audit_comment   TEXT,
    audited_by      UUID REFERENCES users(id),
    audited_at      TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 工单进度汇总
CREATE TABLE ticket_progress (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    total_weight    DECIMAL(5,2) NOT NULL,
    completed_weight DECIMAL(5,2) DEFAULT 0,
    progress_percent DECIMAL(5,2) DEFAULT 0,
    updated_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE (ticket_id)
);


-- 工单模板表
CREATE TABLE ticket_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    category        VARCHAR(50),
    description     TEXT,
    steps           JSONB NOT NULL,
    keywords        TEXT[],
    is_default      BOOLEAN DEFAULT FALSE,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);


-- 工单流程表
CREATE TABLE ticket_workflows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    step_name       VARCHAR(100) NOT NULL,
    step_type       VARCHAR(20) NOT NULL,
    handler_id      UUID REFERENCES users(id),
    handler_group   VARCHAR(50),
    remark          TEXT,
    attachment      VARCHAR(255),
    status          VARCHAR(20) DEFAULT 'pending',
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 工单审批表
CREATE TABLE ticket_approvals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    approver_id     UUID NOT NULL REFERENCES users(id),
    approval_type   VARCHAR(50),
    decision        VARCHAR(20),
    comment         TEXT,
    valid_from      TIMESTAMP,
    valid_to        TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- SLA配置表
CREATE TABLE ticket_sla (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_type     VARCHAR(50) NOT NULL,
    priority        VARCHAR(20) NOT NULL,
    response_time   INTEGER NOT NULL,
    resolve_time    INTEGER NOT NULL,
    escalation_time INTEGER,
    escalate_to     UUID REFERENCES users(id),
    enabled         BOOLEAN DEFAULT TRUE,
    UNIQUE (ticket_type, priority)
);


-- 工单分片表
CREATE TABLE ticket_segments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    segment_name    VARCHAR(100) NOT NULL,
    assignees       UUID[],
    target_ips      TEXT[],
    target_count   INTEGER,
    task_config    JSONB,
    progress        DECIMAL(5,2) DEFAULT 0,
    completed_count INTEGER DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'pending',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 班次交接表
CREATE TABLE ticket_handoffs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    handoff_no      VARCHAR(50) NOT NULL UNIQUE,
    shift_type      VARCHAR(20) NOT NULL,
    shift_date      DATE NOT NULL,
    handover_from   UUID NOT NULL REFERENCES users(id),
    handover_to     UUID NOT NULL REFERENCES users(id),
    handover_time   TIMESTAMP NOT NULL,
    overall_progress DECIMAL(5,2) DEFAULT 0,
    completed_items TEXT[],
    pending_items   TEXT[],
    next_steps      JSONB,
    precautions     JSONB,
    status          VARCHAR(20) DEFAULT 'pending',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 拓扑链路表
CREATE TABLE topology_links (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_node_id  UUID NOT NULL REFERENCES topology_nodes(id),
    target_node_id  UUID NOT NULL REFERENCES topology_nodes(id),
    link_type       VARCHAR(50),
    bandwidth       VARCHAR(50),
    label           VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第七部分：VMware 集成
-- ============================================================

-- VMware 连接表
CREATE TABLE vmware_connections (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    host            VARCHAR(255) NOT NULL,
    port            INTEGER DEFAULT 443,
    username        VARCHAR(255) NOT NULL,
    password        VARCHAR(255) NOT NULL,
    auth_method     VARCHAR(20) DEFAULT 'userpass',
    ssl_verify      BOOLEAN DEFAULT TRUE,
    status          VARCHAR(20) DEFAULT 'active',
    last_sync       TIMESTAMP,
    sync_error      TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- ESXi 主机表
CREATE TABLE vmware_hosts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    moref           VARCHAR(50) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    vendor          VARCHAR(100),
    model           VARCHAR(100),
    uuid            VARCHAR(100),
    serial_number   VARCHAR(100),
    esxi_version    VARCHAR(50),
    esxi_build      VARCHAR(50),
    cpu_cores       INTEGER,
    cpu_threads     INTEGER,
    memory_total_mb BIGINT,
    cluster_name    VARCHAR(100),
    status          VARCHAR(20),
    power_state     VARCHAR(20),
    vms_count       INTEGER,
    asset_id        UUID REFERENCES assets(id),
    last_sync       TIMESTAMP DEFAULT NOW(),
    UNIQUE (connection_id, moref)
);


-- 虚拟机表
CREATE TABLE vmware_vms (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    moref           VARCHAR(50) NOT NULL,
    instance_uuid   VARCHAR(100) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    host_id         UUID REFERENCES vmware_hosts(id),
    cluster_name    VARCHAR(100),
    guest_full_name VARCHAR(255),
    cpu_cores       INTEGER,
    memory_mb       BIGINT,
    power_state     VARCHAR(20),
    ip_addresses    TEXT[],
    mac_addresses  TEXT[],
    cpu_usage       INTEGER,
    memory_usage   INTEGER,
    annotation      TEXT,
    asset_id        UUID REFERENCES assets(id),
    is_template     BOOLEAN DEFAULT FALSE,
    last_sync       TIMESTAMP DEFAULT NOW(),
    UNIQUE (connection_id, instance_uuid)
);


-- 同步历史表
CREATE TABLE vmware_sync_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    sync_type       VARCHAR(20) NOT NULL,
    hosts_added     INTEGER DEFAULT 0,
    hosts_updated   INTEGER DEFAULT 0,
    vms_added       INTEGER DEFAULT 0,
    vms_updated     INTEGER DEFAULT 0,
    errors          INTEGER DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'running',
    started_at      TIMESTAMP DEFAULT NOW(),
    completed_at    TIMESTAMP,
    duration_ms     INTEGER
);


-- ============================================================
-- 第八部分：SolarWinds 迁移
-- ============================================================

-- 迁移任务表
CREATE TABLE migration_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_name       VARCHAR(100) NOT NULL,
    source_system   VARCHAR(50) NOT NULL DEFAULT 'solarwinds',
    export_config   JSONB NOT NULL,
    mapping_config  JSONB NOT NULL,
    merge_strategy  JSONB NOT NULL,
    status          VARCHAR(20) DEFAULT 'pending',
    progress        INTEGER DEFAULT 0,
    total_nodes     INTEGER DEFAULT 0,
    imported_nodes  INTEGER DEFAULT 0,
    merged_nodes    INTEGER DEFAULT 0,
    skipped_nodes   INTEGER DEFAULT 0,
    failed_nodes    INTEGER DEFAULT 0,
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    error_message   TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);


-- 迁移节点状态表
CREATE TABLE migration_node_status (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES migration_tasks(id),
    source_id       VARCHAR(100) NOT NULL,
    source_ip       INET,
    source_name     VARCHAR(255),
    match_type      VARCHAR(20),
    matched_asset_id UUID REFERENCES assets(id),
    status          VARCHAR(20) DEFAULT 'pending',
    asset_id        UUID REFERENCES assets(id),
    error_message   TEXT,
    processed_at    TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- 迁移历史表
CREATE TABLE migration_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES migration_tasks(id),
    operation       VARCHAR(50) NOT NULL,
    entity_type     VARCHAR(50),
    source_data     JSONB,
    target_data     JSONB,
    success         BOOLEAN,
    error_message   TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第九部分：AI 模块
-- ============================================================

-- LLM 提供商表
CREATE TABLE llm_providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    provider_type   VARCHAR(50) NOT NULL,
    api_base        VARCHAR(255),
    api_key         VARCHAR(255),
    model           VARCHAR(100),
    status          VARCHAR(20) DEFAULT 'active',
    is_default      BOOLEAN DEFAULT FALSE,
    config          JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- 知识库文档表
CREATE TABLE kb_documents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           VARCHAR(255) NOT NULL,
    content         TEXT NOT NULL,
    category        VARCHAR(50),
    tags            TEXT[],
    embedding       JSONB,
    source          VARCHAR(100),
    author          VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- AI 会话表
CREATE TABLE ai_conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id),
    session_type    VARCHAR(50) NOT NULL,
    title           VARCHAR(255),
    context         JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);


-- AI 消息表
CREATE TABLE ai_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES ai_conversations(id),
    role            VARCHAR(20) NOT NULL,
    content         TEXT NOT NULL,
    tokens          INTEGER,
    created_at      TIMESTAMP DEFAULT NOW()
);


-- ============================================================
-- 第十一部分：系统配置
-- ============================================================

-- 系统配置表
CREATE TABLE system_config (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    config_key      VARCHAR(100) NOT NULL UNIQUE,
    config_value    JSONB NOT NULL,
    category        VARCHAR(50),
    description     TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    updated_by      UUID REFERENCES users(id)
);


-- 定时任务表
CREATE TABLE scheduled_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    task_type       VARCHAR(50) NOT NULL,
    cron_expression VARCHAR(100),
    interval_ms     BIGINT,
    task_config     JSONB NOT NULL,
    status          VARCHAR(20) DEFAULT 'active',
    last_run_at     TIMESTAMP,
    next_run_at     TIMESTAMP,
    run_count       BIGINT DEFAULT 0,
    error_count     BIGINT DEFAULT 0,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- ============================================================
-- 初始化数据
-- ============================================================

-- 初始化角色
INSERT INTO roles (name, code, description, is_system) VALUES 
('超级管理员', 'admin', '系统超级管理员，拥有所有权限', TRUE),
('运维管理员', 'ops_admin', '运维管理人员', FALSE),
('运维人员', 'ops_user', '普通运维人员', FALSE),
('只读用户', 'readonly', '只读用户', FALSE),
('审计员', 'auditor', '审计人员，只有读取权限', FALSE);

-- 初始化权限
INSERT INTO permissions (resource, action, scope) VALUES 
-- 资产权限
('assets', 'create', 'all'),
('assets', 'read', 'own'),
('assets', 'update', 'own'),
('assets', 'delete', 'all'),
-- 告警权限
('alerts', 'create', 'all'),
('alerts', 'read', 'own'),
('alerts', 'update', 'all'),
-- 工单权限
('tickets', 'create', 'own'),
('tickets', 'read', 'own'),
('tickets', 'update', 'own'),
('tickets', 'approve', 'all'),
-- 监控权限
('metrics', 'read', 'own'),
-- 用户权限
('users', 'create', 'all'),
('users', 'read', 'all'),
('users', 'update', 'all'),
('users', 'delete', 'all'),
-- 审计权限
('audit', 'read', 'all');

-- 给管理员角色分配所有权限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p WHERE r.code = 'admin';

-- 给审计员只读权限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p 
WHERE r.code = 'auditor' AND p.action = 'read';

-- 创建默认管理员用户 (密码: admin123, 需要在生产环境修改)
INSERT INTO users (username, password_hash, nickname, email, status) VALUES 
('admin', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', '管理员', 'admin@company.com', 'active');

-- 关联管理员和角色
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u, roles r WHERE u.username = 'admin' AND r.code = 'admin';

-- 初始化工单模板
INSERT INTO ticket_templates (name, category, description, keywords, steps) VALUES
('操作系统安全基线配置', 'maintenance', '对服务器进行安全基线配置', ARRAY['安全基线', '加固', 'security'],
'[
  {"name": "安全基线检查", "weight": 3, "input_type": "cidr", "description": "检查服务器安全基线"},
  {"name": "账户策略配置", "weight": 2, "input_type": "ip_list", "description": "配置账户策略"},
  {"name": "密码策略配置", "weight": 2, "input_type": "ip_list", "description": "配置密码策略"},
  {"name": "审计策略配置", "weight": 2, "input_type": "ip_list", "description": "配置审计日志"},
  {"name": "验证确认", "weight": 1, "input_type": "boolean", "description": "验证配置生效"}
]'),

('安装虚拟机', 'maintenance', '创建并配置新虚拟机', ARRAY['vm', '虚拟机', '安装'],
'[
  {"name": "创建VM", "weight": 2, "input_type": "text", "description": "创建虚拟机"},
  {"name": "配置资源", "weight": 1, "input_type": "text", "description": "配置CPU/内存/磁盘"},
  {"name": "安装OS", "weight": 3, "input_type": "text", "description": "安装操作系统"},
  {"name": "配置网络", "weight": 2, "input_type": "ip_list", "description": "配置网络"},
  {"name": "验收", "weight": 1, "input_type": "boolean", "description": "最终验收"}
]'),

('服务器上架', 'maintenance', '新服务器物理上架', ARRAY['服务器', '上架', '物理'],
'[
  {"name": "规划位置", "weight": 1, "input_type": "text", "description": "规划机柜位置"},
  {"name": "物理安装", "weight": 3, "input_type": "text", "description": "物理安装服务器"},
  {"name": "接线", "weight": 2, "input_type": "text", "description": "连接网线/电源"},
  {"name": "通电测试", "weight": 1, "input_type": "boolean", "description": "通电并测试"},
  {"name": "验收", "weight": 1, "input_type": "boolean", "description": "最终验收"}
]');

-- 初始化默认系统配置
INSERT INTO system_config (config_key, config_value, category, description) VALUES
('system.name', '"网络监控系统"', 'system', '系统名称'),
('system.version', '"1.0.0"', 'system', '系统版本'),
('session.timeout', '3600', 'security', '会话超时时间(秒)'),
('password.min_length', '8', 'security', '密码最小长度'),
('password.require_mfa', 'false', 'security', '是否强制MFA'),
('alert.max_per_hour', '100', 'alert', '每小时最大告警数'),
('metrics.retention_days', '90', 'metrics', '指标保留天数');

-- ============================================================
-- 表结构完成
-- 共 45+ 张表
-- ============================================================
