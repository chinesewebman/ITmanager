-- 000027_alerts_zabbix_identity_unique
--
-- M27/D-4：(trigger_id, problem_start) 被声明为「一次故障发生的身份」。
-- M26 §2.3 把 problem_start 改成 Zabbix 的 lastchange 之后，它才第一次成为
-- 源侧派生、随故障发生而变的键 —— 在此之前它恒为零值，这个索引本就没有意义。
--
-- 【为什么需要】SyncFromZabbix 靠一次 `WHERE source='zabbix' AND trigger_id IN (...)`
-- 的预查做去重，那是 TOCTOU：预查之后、插入之前若有并发同步（或人工 curl）插了同一
-- 身份，CreateInBatches 整批原子（gorm finisher_api.go）→ 撞索引 → **整批新告警一起
-- 回滚**，同步全失败。有索引 + ON CONFLICT DO NOTHING 之后，该场景退化成
-- 「跳过那一行」，其余照常入库。与 GLPI 侧 000026 完全同形。
--
-- 【为什么是**部分**唯一索引，而不是全表唯一】谓词把约束收窄到「Zabbix 导入的告警」：
--   · trigger_id <> ''：手工告警不带 trigger_id（models.Alert.TriggerID 的非测试写入点
--     只有 Zabbix 同步与 cmd/seed），不该被这条约束管住；
--   · source = 'zabbix'：000013 给这一列留的库级默认值就是 'zabbix'，故将来若有第二个
--     写 trigger_id 的来源，不带 source 收窄会让两边 (trigger_id, problem_start) 相撞
--     → ON CONFLICT 把**真实的 Zabbix 告警静默跳过**（无日志、无返回差异）；
--     带上之后，同类碰撞退化成可见的重复行。
--   两段谓词必须与 service.go 的 ON CONFLICT TargetWhere **逐字一致**，差一个字符 →
--   PG 42P10 → 每一次 Zabbix 同步 500。（sqlite 的仲裁者匹配是解析树比较，比这宽松；
--   PG 才是判据。）
--
-- 【保留 000018 的普通索引 idx_alerts_trigger_id】同步预查是
-- `WHERE source = 'zabbix' AND trigger_id IN (...)`，它推不出 source='zabbix'，
-- 部分索引**无法替代**它（与 000026 保留 idx_tickets_external_id 同理）。
--
-- 【锁语义】CREATE INDEX 取 SHARE 锁、阻塞 alerts 写入；迁移框架在事务内执行
-- （migrate.go execInTx），故不能用 CONCURRENTLY。大表走维护窗口。
--
-- 【自检】把「无 DETAIL 的 23505」变成带样本的异常（同 000026、TODO G-22）。
-- 三个必须写清的细节：
--   · problem_start IS NOT NULL 要显式写 —— PG 唯一索引视 NULL 互不相等，同 trigger
--     的多个 NULL 行**建索引时并不冲突**；自检若把它们算成重复，迁移会被拒且
--     **永远无法满足**（只能靠删数据过关）。
--   · 但**零值行会命中自检，这是预期**：pre-M26 的 Zabbix 行 problem_start 落的是零值
--     哨兵 0001-01-01（**非 NULL** —— 非指针 time.Time 写不出 NULL，M26 §1.6 真 PG 实测），
--     同 trigger 被同步过两次就是真重复，自检报出来正是它的职责。
--   · trigger_id <> '' 本身已排除 NULL（NULL <> '' 求值为 NULL），仍显式写 IS NOT NULL
--     让谓词与下面的索引定义字面可比。

DO $$ DECLARE n int; s text;
BEGIN
  SELECT count(*), string_agg(trigger_id || '@' || problem_start::text, ', ') INTO n, s FROM (
    SELECT trigger_id, problem_start FROM alerts
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''
      AND problem_start IS NOT NULL
    GROUP BY trigger_id, problem_start HAVING count(*) > 1
    ORDER BY trigger_id LIMIT 5
  ) x;
  IF n > 0 THEN
    RAISE EXCEPTION '存在重复的 zabbix 告警身份 (trigger_id, problem_start)（最多列 5 个）：%，请先人工清理后重跑迁移', s;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_zabbix_identity
    ON alerts(trigger_id, problem_start)
    WHERE source = 'zabbix' AND trigger_id IS NOT NULL AND trigger_id <> '';
