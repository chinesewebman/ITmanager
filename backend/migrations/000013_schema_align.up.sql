-- 000013_schema_align: 把「迁移建出来的库」对齐到 GORM 模型（代码实际使用的 schema）。
--
-- 背景：生产建库走 migrations/*.up.sql（cmd/server/main.go:34 注入 MigrationsFS →
-- database.go:72-73 执行 migrate.Up），而代码按 models 的字段枚举列。两边长期平行演化，
-- D-1/D-4/D-5 只是其中一部分。完整漂移清单由 backend/tests/schema_drift_test.go 持续守门。
--
-- 原则（见 docs/FIX-PLAN-D1-D7.md §2）：
--   1. 非破坏：只做 RENAME / ADD COLUMN / DROP NOT NULL / 类型放宽 / 建索引
--   2. 不 DROP 任何旧列（留待 v4，需先确认零引用）
--   3. 能确定语义的旧列做数据回填
--   4. 幂等：重复执行不报错
--
-- 命名对齐策略：同一概念只是叫法不同 → RENAME（保数据、保外键）；
--               语义/类型不同 → ADD COLUMN + 放宽旧列 NOT NULL。

-- ============================================================
-- 1. 表名对齐
-- ============================================================
DO $$
BEGIN
    IF to_regclass('public.idc') IS NOT NULL AND to_regclass('public.sites') IS NULL THEN
        ALTER TABLE idc RENAME TO sites;
    END IF;
    IF to_regclass('public.asset_network') IS NOT NULL AND to_regclass('public.asset_networks') IS NULL THEN
        ALTER TABLE asset_network RENAME TO asset_networks;
    END IF;
    IF to_regclass('public.notify_channels') IS NOT NULL AND to_regclass('public.notification_channels') IS NULL THEN
        ALTER TABLE notify_channels RENAME TO notification_channels;
    END IF;
END
$$;

-- ============================================================
-- 2. 列名对齐（同一概念的叫法差异）
-- ============================================================
DO $$
BEGIN
    -- users / audit_logs
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_logs' AND column_name='timestamp')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_logs' AND column_name='created_at') THEN
        ALTER TABLE audit_logs RENAME COLUMN "timestamp" TO created_at;
    END IF;
    -- tickets
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='ticket_no')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='ticket_number') THEN
        ALTER TABLE tickets RENAME COLUMN ticket_no TO ticket_number;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='creator_id')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='requester_id') THEN
        ALTER TABLE tickets RENAME COLUMN creator_id TO requester_id;
    END IF;
    -- assets
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='asset_name')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='name') THEN
        ALTER TABLE assets RENAME COLUMN asset_name TO name;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='idc_id')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='site_id') THEN
        ALTER TABLE assets RENAME COLUMN idc_id TO site_id;
    END IF;
    -- racks
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='racks' AND column_name='idc_id')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='racks' AND column_name='site_id') THEN
        ALTER TABLE racks RENAME COLUMN idc_id TO site_id;
    END IF;
    -- alert_rules
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='alert_rules' AND column_name='enabled')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='alert_rules' AND column_name='is_enabled') THEN
        ALTER TABLE alert_rules RENAME COLUMN enabled TO is_enabled;
    END IF;
    -- notification_channels
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='channel_type')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='type') THEN
        ALTER TABLE notification_channels RENAME COLUMN channel_type TO type;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='enabled')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='is_enabled') THEN
        ALTER TABLE notification_channels RENAME COLUMN enabled TO is_enabled;
    END IF;
END
$$;

-- ============================================================
-- 3. 放宽 NOT NULL（模型不写这些旧列 → 不放宽会让 INSERT 全部失败）
-- ============================================================
ALTER TABLE tickets      ALTER COLUMN requester_id DROP NOT NULL;
ALTER TABLE alerts       ALTER COLUMN level        DROP NOT NULL;
ALTER TABLE alerts       ALTER COLUMN title        DROP NOT NULL;
ALTER TABLE alert_rules  ALTER COLUMN metric_name  DROP NOT NULL;
ALTER TABLE alert_rules  ALTER COLUMN level        DROP NOT NULL;
ALTER TABLE audit_logs   ALTER COLUMN event_type   DROP NOT NULL;

-- 审计 中-4：若某库旧列与新列并存（AutoMigrate 跑过 / 上一轮手工修过），上面的 RENAME
-- 分支会被跳过，旧列的 NOT NULL 原样保留 —— 而模型从不写旧列，INSERT 仍会失败。
-- 这里按「两列都存在」判断，补一次 DROP NOT NULL（列不存在则跳过）。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='creator_id')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='requester_id') THEN
        ALTER TABLE tickets ALTER COLUMN creator_id DROP NOT NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='asset_name')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='assets' AND column_name='name') THEN
        ALTER TABLE assets ALTER COLUMN asset_name DROP NOT NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='alert_rules' AND column_name='enabled')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='alert_rules' AND column_name='is_enabled') THEN
        ALTER TABLE alert_rules ALTER COLUMN enabled DROP NOT NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='channel_type')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='type') THEN
        ALTER TABLE notification_channels ALTER COLUMN channel_type DROP NOT NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='enabled')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='notification_channels' AND column_name='is_enabled') THEN
        ALTER TABLE notification_channels ALTER COLUMN enabled DROP NOT NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='ticket_no')
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='tickets' AND column_name='ticket_number') THEN
        ALTER TABLE tickets ALTER COLUMN ticket_no DROP NOT NULL;
    END IF;
END
$$;

-- ============================================================
-- 4. 类型对齐：JSON 字符串列（模型用 serializer:json 写 TEXT，原列是数组/jsonb → 写入必失败）
--
-- 幂等性（审计 阻断-1）：所有类型转换都按 information_schema 的**当前类型**判断，
-- 只在还是数组/jsonb/inet 时才转。裸写 `USING to_json(x)::text` 在列已是 TEXT 时
-- 会把 JSON 再套一层引号（'["read"]' → '"[""read""]"'）—— 静默数据损坏，且
-- models.StringList 解不出来 → API Key 认证全挂。触发路径是「DDL 已提交、版本未记录」
-- 的崩溃窗口（migrate.go 已改为版本记录与 DDL 同事务，见审计 中-8）。
-- ============================================================
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='api_keys' AND column_name='permissions'
                 AND data_type NOT IN ('text','character varying','character')) THEN
        ALTER TABLE api_keys ALTER COLUMN permissions TYPE TEXT
            USING COALESCE(to_json(permissions)::text, '[]');
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='api_keys' AND column_name='ip_whitelist'
                 AND data_type NOT IN ('text','character varying','character')) THEN
        ALTER TABLE api_keys ALTER COLUMN ip_whitelist TYPE TEXT
            USING COALESCE(to_json(ip_whitelist)::text, '[]');
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='alert_rules' AND column_name='notify_users'
                 AND data_type NOT IN ('text','character varying','character')) THEN
        ALTER TABLE alert_rules ALTER COLUMN notify_users TYPE TEXT
            USING COALESCE(to_json(notify_users)::text, '[]');
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='alert_rules' AND column_name='notify_channels'
                 AND data_type NOT IN ('text','character varying','character')) THEN
        ALTER TABLE alert_rules ALTER COLUMN notify_channels TYPE TEXT
            USING COALESCE(notify_channels::text, '[]');
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='notification_channels' AND column_name='config'
                 AND data_type <> 'text') THEN
        ALTER TABLE notification_channels ALTER COLUMN config TYPE TEXT USING config::text;
    END IF;
    -- inet → varchar：必须用 host() 取裸地址。直接 ::text 走 network_show，
    -- 会把 '10.1.2.3' 变成 '10.1.2.3/32'（掩码列更是变成 '255.255.255.0/32'），
    -- 与同一份迁移里 to_json(inet[]) 的裸地址写法自相矛盾（审计 中-3）。
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='users' AND column_name='last_login_ip' AND data_type='inet') THEN
        ALTER TABLE users ALTER COLUMN last_login_ip TYPE VARCHAR(50) USING host(last_login_ip);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='asset_networks' AND column_name='ipv4_address' AND data_type='inet') THEN
        ALTER TABLE asset_networks ALTER COLUMN ipv4_address TYPE VARCHAR(45) USING host(ipv4_address);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='asset_networks' AND column_name='ipv4_netmask' AND data_type='inet') THEN
        ALTER TABLE asset_networks ALTER COLUMN ipv4_netmask TYPE VARCHAR(45) USING host(ipv4_netmask);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='asset_networks' AND column_name='ipv6_address' AND data_type='inet') THEN
        ALTER TABLE asset_networks ALTER COLUMN ipv6_address TYPE VARCHAR(45) USING host(ipv6_address);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='asset_networks' AND column_name='connected_to' AND data_type='uuid') THEN
        ALTER TABLE asset_networks ALTER COLUMN connected_to TYPE VARCHAR(255) USING connected_to::text;
    END IF;
END
$$;

UPDATE api_keys SET permissions = '[]' WHERE permissions IS NULL OR permissions = '';
UPDATE api_keys SET ip_whitelist = '[]' WHERE ip_whitelist IS NULL OR ip_whitelist = '';
ALTER TABLE api_keys ALTER COLUMN permissions  SET DEFAULT '[]';
ALTER TABLE api_keys ALTER COLUMN ip_whitelist SET DEFAULT '[]';
ALTER TABLE api_keys ALTER COLUMN permissions  SET NOT NULL;
ALTER TABLE api_keys ALTER COLUMN ip_whitelist SET NOT NULL;
ALTER TABLE notification_channels ALTER COLUMN config DROP NOT NULL;

-- ============================================================
-- 5. 补齐模型列
-- ============================================================
-- users
-- 注意：role 先不带 DEFAULT 加列。PG 11+ 的 ADD COLUMN ... DEFAULT 会给既有行
-- 直接读出默认值（'user'），导致下面「只回填 NULL/空」的 UPDATE 恒不命中 ——
-- 存量部署的 admin 会被降级成 user 并被 D-6 门禁锁在门外。
-- 正确顺序：加列(无默认) → 从 user_roles 回填 → 兜底 'user' → 再设 DEFAULT。
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20);
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

-- sites（原 idc）
ALTER TABLE sites ADD COLUMN IF NOT EXISTS net_box_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_sites_net_box_id ON sites(net_box_id);

-- racks
ALTER TABLE racks ADD COLUMN IF NOT EXISTS site_name  VARCHAR(100);
ALTER TABLE racks ADD COLUMN IF NOT EXISTS net_box_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_racks_net_box_id ON racks(net_box_id);

-- assets
ALTER TABLE assets ADD COLUMN IF NOT EXISTS site_name  VARCHAR(100);
ALTER TABLE assets ADD COLUMN IF NOT EXISTS rack_name  VARCHAR(50);
ALTER TABLE assets ADD COLUMN IF NOT EXISTS net_box_id BIGINT;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS source     VARCHAR(50);
CREATE INDEX IF NOT EXISTS idx_assets_net_box_id ON assets(net_box_id);

-- alert_rules
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS condition      TEXT;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS host_group     VARCHAR(100);
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS metric         VARCHAR(100);
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS severity       BIGINT;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS severity_name  VARCHAR(20);
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS notify_enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS priority       BIGINT DEFAULT 0;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS updated_by     UUID;
CREATE INDEX IF NOT EXISTS idx_alert_rules_is_enabled ON alert_rules(is_enabled);

-- alerts
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS alert_id       VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS host_id        UUID;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS host_name      VARCHAR(255);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS host_ip        VARCHAR(45);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS trigger_name   VARCHAR(500);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS trigger_id     VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS severity       BIGINT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS severity_name  VARCHAR(20);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS problem        TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS problem_start  TIMESTAMP;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS problem_end    TIMESTAMP;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS duration       BIGINT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ack_time       TIMESTAMP;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ack_user       VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS resolve_time   TIMESTAMP;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS resolve_user   VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ticket_id      UUID;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS source         VARCHAR(20) DEFAULT 'zabbix';
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS repeat_count   BIGINT DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_alerts_alert_id ON alerts(alert_id);
CREATE INDEX IF NOT EXISTS idx_alerts_host_id  ON alerts(host_id);
CREATE INDEX IF NOT EXISTS idx_alerts_severity ON alerts(severity);
CREATE INDEX IF NOT EXISTS idx_alerts_source   ON alerts(source);

-- tickets
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS ticket_type     VARCHAR(20);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS category        VARCHAR(50);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS tags            JSONB DEFAULT '[]';
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS asset_name      VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_name  VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_email VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS assignee_name   VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolution      TEXT;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolved_at     TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS closed_at       TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS due_date        TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS external_id     VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS source          VARCHAR(20) DEFAULT 'manual';
CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_ticket_number ON tickets(ticket_number);

-- audit_logs
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS method     VARCHAR(10);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS path       VARCHAR(500);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS ip         VARCHAR(50);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS status     BIGINT;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS error_msg  VARCHAR(1000);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS request_id VARCHAR(50);

-- notification_channels
ALTER TABLE notification_channels ADD COLUMN IF NOT EXISTS is_default BOOLEAN DEFAULT FALSE;

-- ============================================================
-- 6. 数据回填（只做语义确定的）
-- ============================================================
-- users.role ← user_roles/roles（cmd/admin-bootstrap/main.go:105 会写这两张表）
-- IS DISTINCT FROM：既覆盖刚加的 NULL 列，也容忍 role 已带默认值的情况（幂等）。
UPDATE users u
SET role = r.code
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = u.id
  AND u.role IS DISTINCT FROM r.code;

-- 兜底：user_roles 里没有记录的用户按最小权限 'user'，最后才设列默认值。
UPDATE users SET role = 'user' WHERE role IS NULL OR role = '';
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'user';

-- ============================================================
-- 7. 索引补齐（模型声明的 index / uniqueIndex）
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_users_deleted_at   ON users(deleted_at);
CREATE INDEX IF NOT EXISTS idx_users_role         ON users(role);
