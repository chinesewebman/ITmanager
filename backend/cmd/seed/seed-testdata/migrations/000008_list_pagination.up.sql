-- v1.1 P2 性能优化：为 Offset+Limit 分页的列表接口加 created_at DESC 复合索引。
-- 涉及: assets, tickets, runbooks, users。
-- 背景：codex 审查识别 M3-P2 "深分页性能差"。Offset N 走全表扫 + 排序，
-- 复合索引允许 PG 用 Index Scan 替代 Sort。
-- 注意：单列 created_at 上有默认 btree 索引，但 ORDER BY ... DESC 不能直接复用升序索引。
-- 这里用 DESC 索引走 backward scan，0 额外成本。

CREATE INDEX IF NOT EXISTS idx_assets_created_at_desc
    ON assets(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tickets_created_at_desc
    ON tickets(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_runbooks_updated_at_desc
    ON runbooks(updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_users_created_at_desc
    ON users(created_at DESC);
