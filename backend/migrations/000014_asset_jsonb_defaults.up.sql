-- 000014_asset_jsonb_defaults: 给 assets.tags / assets.custom_fields 补列默认值并回填历史 NULL。
--
-- 背景（TODO G-20）：两列是 jsonb，而模型字段是 Go string，零值 "" 被 gorm 原样写进
-- INSERT → PG 报 invalid input syntax for type json (22P02)：cmd/seed 的资产全建不出来
-- （且失败时退出码仍是 0），POST /api/assets 不带 custom_fields 直接 500。
--
-- 应用侧的功能保证在 models.Asset.BeforeSave（把空值归一为 '[]'/'{}'）；
-- 本迁移是 schema 层兜底，覆盖非 gorm 写入方与受限 Select 插入（省略该列时拿到合法 JSON）。
--
-- 语句顺序很关键：execInTx 把整个文件放在一个事务里（internal/migrate/migrate.go:309-325），
-- ALTER TABLE ... SET DEFAULT 取 ACCESS EXCLUSIVE 且持有到 COMMIT（PG 11+ 虽是纯元数据、
-- 不重写表）。所以先回填（RowExclusive，不阻塞读写）再改默认值（短锁窗口）；
-- 反过来写会让回填期间 assets 全表不可读写。
-- 实测（300k 行 / 14MB 表）：UPDATE 在前时并发 SELECT/UPDATE 正常，
-- ACCESS EXCLUSIVE 只出现在两条 ALTER 的 ~3.4ms 窗口内。
--
-- 幂等：SET DEFAULT 重复执行无害；回填只动 IS NULL 行（第二次 UPDATE 0 行）。

-- 抢不到锁就失败退出（事务回滚、api 启动报错），不要无限期挂住启动。
SET lock_timeout = '5s';

-- 回填存量 NULL 行。NULL 只可能来自非 gorm 写入（模型此前写 '' 会直接失败）。
-- 含 retired 行（assets 无软删除，退役行仍在表内）：NULL 与 []/{} 对全部消费方等价
-- （Go/前端均无读侧，见 docs/FIX-PLAN-ASSET-JSONB.md §1.5），语义无损。
UPDATE assets SET tags          = '[]'::jsonb WHERE tags IS NULL;
UPDATE assets SET custom_fields = '{}'::jsonb WHERE custom_fields IS NULL;

-- 列默认值：tags 用数组（与 seed 的 ["web","production"] 一致），custom_fields 用对象。
-- 不加 NOT NULL —— 那需要全表校验 + 重写（长 ACCESS EXCLUSIVE 锁），
-- 且应用层仍可被 PUT {"tags": null} 绕过（见方案 §2.2）。
ALTER TABLE assets ALTER COLUMN tags          SET DEFAULT '[]'::jsonb;
ALTER TABLE assets ALTER COLUMN custom_fields SET DEFAULT '{}'::jsonb;
