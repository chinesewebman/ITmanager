-- 000026_tickets_glpi_external_id_unique
--
-- M26/D-4：ADR-0004 Risk-3 承诺的 external_id 幂等索引，从未落地。
--
-- 【为什么需要】SyncFromGLPI 靠一次 `WHERE external_id IN (...)` 的预查做幂等，
-- 那是 TOCTOU：预查之后、插入之前若有并发同步（或人工 curl）插了同一 external_id，
-- CreateInBatches 整批原子 → 撞唯一约束 → **整批新票一起回滚**，同步全失败。
-- 有索引 + ON CONFLICT DO NOTHING 之后，该场景退化成「跳过那一行」，其余照常入库。
--
-- 【为什么是**部分**唯一索引，而不是全表唯一】tickets.external_id 对 manual/email/api
-- 来源是自由文本，空串与重复都合法（人工建单不填 external_id → 全是 ''）。
-- 谓词 source='glpi' AND external_id <> '' 把约束收窄到「GLPI 导入的票」，
-- 与 service.go 的 ON CONFLICT TargetWhere 逐字一致 —— 两边必须同改，否则 PG 42P10。
--
-- 【保留 000019 的普通索引 idx_tickets_external_id】它服务于同步预查那条
-- `WHERE external_id IN (...)`，而该查询既推不出 external_id <> '' 也推不出
-- source='glpi'，部分索引的两段谓词都不可被蕴含 —— 即**无法替代**。
-- 真 PG 18 实测（5 万行 / 200 个 ID 的字面量 IN 列表）：
--   两索引并存           → Bitmap Index Scan on idx_tickets_external_id，buffers hit=91
--   删掉普通索引只留部分 → Seq Scan，Rows Removed by Filter: 49800，buffers hit=375
-- 所以这**不是**一次「用部分索引换掉普通索引」的优化。
--
-- 【锁语义】CREATE INDEX 取 SHARE 锁、阻塞 tickets 写入；迁移框架在事务内执行
-- （migrate.go execInTx），故不能用 CONCURRENTLY。大表走维护窗口。
--
-- 【自检】把「无 DETAIL 的 23505」变成带样本的异常。
-- 迁移 DDL 与版本记录同事务，失败会导致版本不落、每次重启重放、服务持续不可用；
-- 而 PG 默认的唯一索引冲突日志不含是哪几行（见 TODO.md G-22）。
-- 前置自检把「哪几个 external_id 重了」直接写进异常文本，运维一次就能定位。

DO $$ DECLARE n int; s text;
BEGIN
  SELECT count(*), string_agg(external_id, ', ') INTO n, s FROM (
    SELECT external_id FROM tickets
    WHERE source = 'glpi' AND external_id <> ''
    GROUP BY external_id HAVING count(*) > 1
    ORDER BY external_id LIMIT 5
  ) x;
  IF n > 0 THEN
    RAISE EXCEPTION '存在重复的 glpi external_id（最多列 5 个）：%，请先人工清理后重跑迁移', s;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_glpi_external_id
    ON tickets(external_id) WHERE source = 'glpi' AND external_id <> '';
