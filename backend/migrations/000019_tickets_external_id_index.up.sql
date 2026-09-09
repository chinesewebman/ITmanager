-- W6 P16: GLPI 同步预查 tickets WHERE external_id IN (...)，external_id 无索引
-- → 每次扫全表（审计实测 152.2ms）。加索引后 1.08ms。
-- 对应 integration/service.go:230-232 的预查查询。
CREATE INDEX IF NOT EXISTS idx_tickets_external_id
    ON tickets(external_id);
