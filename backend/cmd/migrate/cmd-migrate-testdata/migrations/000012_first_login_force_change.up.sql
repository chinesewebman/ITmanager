-- C7: 首次登录强改密 (SQLite cmd-migrate 镜像)
ALTER TABLE users ADD COLUMN must_change_password INTEGER DEFAULT 1;
ALTER TABLE users ADD COLUMN password_set_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_users_must_change_password ON users (must_change_password);