-- B4: 软退役 + IP 释放
-- 详见 docs/TRAPS.md T-* 未来 trap + §6 主人 7/01 insight: 软退役 ≠ 数据永占位
--   退役 = 释放 IP (AssetNetwork.IPv4Address/IPv6Address = NULL),
--          保留数据 (last_known_ip + retired_at 存档)
-- 后续新设备可用相同 IP 不冲突, 历史按 hostid 仍可查

ALTER TABLE assets ADD COLUMN IF NOT EXISTS last_known_ip4 VARCHAR(45);
ALTER TABLE assets ADD COLUMN IF NOT EXISTS last_known_ip6 VARCHAR(45);
ALTER TABLE assets ADD COLUMN IF NOT EXISTS retired_at TIMESTAMP;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS retired_reason TEXT;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS retired_by TEXT;  -- UUID, soft FK users(id) (no constraint to avoid migration coupling)

-- 索引加速: 按退役状态过滤 + 按时间排序恢复列表
CREATE INDEX IF NOT EXISTS idx_assets_retired_at ON assets (retired_at DESC) WHERE retired_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_assets_status_retired ON assets (status, retired_at DESC);

-- 部分唯一约束: 防止已退役设备的 IP 仍被新设备用 — UNIQUE 不行 (历史 IP 可能多个) 但加注释提示上层
-- 不加 unique 约束, 因为 last_known_ip 是 *历史快照*, 新设备占用相同 IP 时 IP 在 AssetNetwork 才是 active