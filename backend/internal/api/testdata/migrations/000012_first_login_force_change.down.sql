DROP INDEX IF EXISTS idx_users_must_change_password;
ALTER TABLE users DROP COLUMN password_set_at;
ALTER TABLE users DROP COLUMN must_change_password;