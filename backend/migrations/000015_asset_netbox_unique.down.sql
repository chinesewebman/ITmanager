-- 000015_asset_netbox_unique 回滚：唯一索引退回普通索引。
--
-- 回滚后 SyncFromNetBox 会重新失败（42P10）—— 这是「回到 000014 的旧行为」的应有含义，
-- 不是回滚本身的缺陷：upsert 依赖的唯一性保证随索引一起被撤销。
--
-- 不删索引（000013 就存在的普通索引要保留，否则 net_box_id 的查询退化成全表扫描）：
-- 先 DROP 再按原形态重建。down 与 up 一样在单事务里执行（execInTx），失败整体回滚。

DROP INDEX IF EXISTS idx_assets_net_box_id;
CREATE INDEX IF NOT EXISTS idx_assets_net_box_id ON assets(net_box_id);
