-- 告警误报标记 + ML 训练集导出（小改进 #2）
-- 背景：运维人员经常把"已知的误报"（周期性抖动、敏感阈值等）手动标记为 FP，
-- 累积后可导出 CSV 给 ML 训练做监督学习
--
-- 2026-09-09 方言修正：原 DDL 用 `DEFAULT 0` 给 BOOLEAN、用 DATETIME 类型、
-- 部分索引写 `WHERE is_false_positive = 1`，PostgreSQL 全部语法报错。
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS is_false_positive BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS marked_by VARCHAR(100);   -- 标记人 username（来自 c.GetString("username")）
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS marked_at TIMESTAMP;      -- 标记时间
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS false_positive_note TEXT; -- 标记备注（如"周期性抖动"）

-- 用于 List 列表按 FP 状态过滤 + 导出训练集
CREATE INDEX IF NOT EXISTS idx_alerts_is_false_positive
    ON alerts(is_false_positive)
    WHERE is_false_positive = TRUE;
