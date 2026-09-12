-- 000028 down: 反转 000028 的操作.
--
-- 设计决策: 本文件实际是 noop, 只做注释占位. 原因:
--   1. 000028 的所有 ADD COLUMN 都是 IF NOT EXISTS, 与 000013 重复.
--      drop 这些列会破坏 000013 的语义 (000013 是契约, 不能因为 000028 down 而破坏).
--   2. DROP NOT NULL 在 PG 里不可逆 (PG 不记录历史 NULL-ability),
--      无法在 down 里还原. 即使能, 还原 ticket_type NOT NULL 也违反模型语义
--      (gorm:size:20, 无 not null tag).
--   3. 000013 的 down 也不删 ticket_no / creator_id 旧列 (跨版本回滚兼容),
--      本 down 保持同一约定.
--
-- 因此 down 后状态 = up 前状态 (即: ticket_type 仍允许 NULL, 其他列保持原样).
-- 这与 PG migration runner 的"down 必须能完全恢复"的习惯不完全一致,
-- 但 000013 已经建立了这个约定, 000028 沿用.

SELECT 1; -- noop
