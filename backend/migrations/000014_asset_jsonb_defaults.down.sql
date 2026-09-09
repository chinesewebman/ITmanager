-- 000014_asset_jsonb_defaults 回滚：只撤 schema 默认值，**不动数据**。
--
-- 回填不可逆：无法知道哪些行原本是 NULL（NULL 与 []/{} 对现有消费方等价，
-- 见 docs/FIX-PLAN-ASSET-JSONB.md §1.5），所以 down 不尝试还原。
--
-- 注意回滚后的**组合状态**：down 000014 + 新二进制时，裸 SQL / 受限 Select 插入
-- 会静默落 NULL（既不报错、也不再有 22P02）。排查时别误读成「G-20 修复没生效」——
-- 应用层钩子（models.Asset.BeforeSave）仍在，gorm 写入路径不受影响。

ALTER TABLE assets ALTER COLUMN tags          DROP DEFAULT;
ALTER TABLE assets ALTER COLUMN custom_fields DROP DEFAULT;
