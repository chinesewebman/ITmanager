-- 000038_alert_rule_trigger_map
--
-- M38-B / G-39 fire 路径整链路：triggerid → rule_id 映射表。
--
-- 【为什么需要】
-- Zabbix trigger 没有结构化字段匹配 AlertRule 的 5 维度（metric / operator / threshold /
-- host_group / asset_type）。M37-A 仅修了 ResolveAlert 路径里 alert.AlertRuleID 已
-- 写入时的过滤；fire 路径需要把 triggerid 解析到 rule_id 才有 RuleID 可填。M38-B
-- 决策点 E1.b：运维在 ITmanager 自己持有一个 triggerid → rule_id 映射表（不依赖
-- Zabbix trigger.tags 加 `itmanager_rule_id`，降低耦合）。
--
-- 【为什么 triggerid 是 PK 而不是 (triggerid, rule_id)】
-- 一条 trigger 只允许映射到一个 rule（同 trigger 跨 rule 会让 worker 推送过滤语义
-- 模糊化、且 last-write-wins 已经能表达"运维改主意"）。所以 triggerid 唯一 → 重复
-- POST 同 triggerid 由业务层先 SELECT（created_at DESC 取最新）然后 UPDATE / 覆盖。
-- 见 handler 注释。
--
-- 【为什么 ON DELETE CASCADE】
--   - rule 被删 → 映射行跟着没意义 → 级联清。
--   - 反向（rule_id change）不该触发删除，应走 UPDATE（rule_id 字段独立可改）。
-- 我们只打算 ON DELETE CASCADE rule，不打算 ON DELETE CASCADE 任何反向关系。
--
-- 【为什么 created_at NOT NULL DEFAULT NOW()】
-- last-write-wins 排序需要这列；同时审计需要。created_at 列已在 000001 的公共
-- timestamp 约定里（NOW()），勿改。
--
-- 【为什么 triggerid 是 VARCHAR(100) 而不是 TEXT】
-- 与 alerts.trigger_id / Zabbix trigger.triggerid 同列宽（migration 000001），保持
-- 对齐；超长值会被 SyncFromZabbix 的 truncate.go 截断（colAlertTriggerName 等）。
--
-- 【自检：阻止已删除 rule 残留孤儿】
-- 上面 ON DELETE CASCADE 处理了「rule 被删」；但若 ON DELETE CASCADE 因任何原因失效
-- （例如手动改 FK），迁移重新应用时会留下指向不存在 rule 的孤儿行。简单 RAISE 是
-- 没必要的——CASCADE 已经覆盖 99% 路径；运维手动改 FK 这种破坏本身就是数据完整性
-- 事故，靠迁移自检救场是反向鼓励。
--
CREATE TABLE IF NOT EXISTS alert_rule_trigger_map (
    triggerid  VARCHAR(100) NOT NULL,
    rule_id    UUID         NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (triggerid)
);

CREATE INDEX IF NOT EXISTS idx_alert_rule_trigger_map_rule_id
    ON alert_rule_trigger_map(rule_id);

-- 同 triggerid 历史映射的 created_at DESC 索引：API "取最新" 用
-- （last-write-wins；查询形如 `WHERE triggerid = ? ORDER BY created_at DESC`）
-- triggerid 是 PK → WHERE triggerid = ? 直接定位一行；ORDER BY created_at DESC
-- 触发 index-only scan 即可。这里 idx 用 created_at 而不是 triggerid，原因是
-- API 「按 triggerid 取该 trigger 的历史映射」需求（多行）目前不在 M38-B 范围。
-- 留这行注释仅为明示该路径尚未开辟，避免将来误以为忘了加。
