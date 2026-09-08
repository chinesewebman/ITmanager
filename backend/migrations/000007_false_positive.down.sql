-- 回滚告警误报标记
DROP INDEX IF EXISTS idx_alerts_is_false_positive;
ALTER TABLE alerts DROP COLUMN IF EXISTS false_positive_note;
ALTER TABLE alerts DROP COLUMN IF EXISTS marked_at;
ALTER TABLE alerts DROP COLUMN IF EXISTS marked_by;
ALTER TABLE alerts DROP COLUMN IF EXISTS is_false_positive;
