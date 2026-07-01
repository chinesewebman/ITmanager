-- Trap 30: 先 DROP INDEX 再 DROP COLUMN (sqlite 索引引用列会悬空)
DROP INDEX IF EXISTS idx_users_must_change_password;
ALTER TABLE users DROP COLUMN IF EXISTS password_set_at;
ALTER TABLE users DROP COLUMN IF EXISTS must_change_password;