-- 000025_ticket_history
--
-- M25：工单「经手历史」—— 谁在何时改了这张票的哪个字段。
--
-- 【为什么必须建，而不是顺手清个字段】燕如拍板 resolved_at 走「当前状态」语义
-- （重开清空，与 M24 的 closed_at 同一套：closed_at 非空 ⟺ status='closed'），
-- 但要求「之前被谁经手的历史保留完整记录」。而全仓**领域级留痕为零**：
--   · audit_logs 中间件（middleware/audit.go）只记 user/method/path/status/IP/UA，
--     **不记改了什么** —— 审计行只说「某人 PUT 过这张工单」；
--   · audit_logs 表 DDL 里的 changes/old_values/new_values 三列，
--     models.AuditLog 里**没有**对应字段 → GORM 永不写；
--   · asset_history / step_progress_history（000001_init.up.sql:280,743）是
--     **零 Go 代码引用**的死表。
-- 所以「清空 resolved_at」若不与建表同轮，就是主动丢数据。两者同轮落地。
--
-- 【形状】沿用 asset_history（本仓为「谁改了什么」设计过、但从未启用的形状）：
-- 一行一个字段变更，field_name/old_value/new_value/actor_id/actor_name/created_at。
-- 三点有意差异：
--   · batch_id —— 一次请求 = 一个批次，同一次操作的 N 行共享。没有它，UI 只能靠
--     「created_at 相同」猜分组，同秒的两次操作会并成一次。
--   · kind —— created | updated。asset_history 没有「出生」概念，工单有（建单即一次经手）。
--   · actor_id **刻意不建外键** —— 这是唯一现实的「历史插入失败」来源，而失败策略是
--     「与 UPDATE 同事务、失败即整单回滚」（宁可不改也不留假记录）。JWT 路径只信 claims、
--     不复查用户是否仍存在（middleware/auth.go），带外删号后未过期 token 仍带 user_id
--     → 若建 FK，该用户此后每次改工单都 500。去掉 FK 后插入在构造上不可能失败。
--     同 audit_logs.resource_id（裸 UUID、无 FK，000001_init.up.sql:1089）。
--
-- 【append-only 不变式】代码层零 UPDATE/DELETE 路径（唯一删除路径是 down 迁移本身）。
--
-- 【已知取舍】ticket_id 用 ON DELETE CASCADE：当前全仓**没有**工单删除端点
-- （真 grep：零 Unscoped()、零 Delete(&models.Ticket)），故不可达。将来若新增工单删除端点，
-- 必须重新评估这里 —— 备选是学 audit_logs 用裸 UUID 不建 FK（删工单留历史），
-- 代价是历史行失去 join 目标、读端点 /tickets/:id/history 也不可达。
-- 见 models/ticket_history.go 的注释与 docs/FIX-PLAN-TICKET-HISTORY.md §2.1。
--
-- 【时区】created_at 由 GORM 的 NowFunc(time.Now().UTC()) 填，DEFAULT NOW() 仅兜底
-- （绕开 GORM 的写入方：裸 SQL / db.Exec）。与 tickets/asset_history 的 TIMESTAMP 取舍一致。

CREATE TABLE IF NOT EXISTS ticket_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    batch_id    UUID NOT NULL,
    kind        VARCHAR(20) NOT NULL,
    field_name  VARCHAR(50),
    old_value   TEXT,
    new_value   TEXT,
    actor_id    UUID,
    actor_name  VARCHAR(100),
    source      VARCHAR(20),
    request_id  VARCHAR(50),
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

-- 读路径固定是「某张票的历史、最新在前」，与 alert/ticket 的 (created_at DESC, id DESC)
-- 全序约定一致（T-45：无 ORDER BY 的「最新一条」没有定义）。
CREATE INDEX IF NOT EXISTS idx_ticket_history_ticket
    ON ticket_history(ticket_id, created_at DESC, id DESC);
