-- C7: 首次登录强改密 + 可跳过
-- 设计:
--   must_change_password = TRUE  → 强制要求改密
--   password_set_at = TIMESTAMP   → 记录"首次设置密码/跳过"时间 (NULL = 还没设置过)
--   触发场景:
--     - 新部署 seed admin  →  must_change_password=TRUE, password_set_at=NULL
--     - 用户走 /auth/change-password →  must_change_password=FALSE, password_set_at=NOW()
--     - 用户走 /auth/skip-password-change →  must_change_password=FALSE, password_set_at=NOW()
--   审计: skip 也写 audit_logs (action=skip_password_change)
--   可重入: 二次登录若 must_change_password=FALSE → 不再返 flag → 走正常 dashboard

ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN DEFAULT TRUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_set_at TIMESTAMP;

-- 索引: 部署后扫表"哪些用户还在必须改密状态" (admin 排查用)
CREATE INDEX IF NOT EXISTS idx_users_must_change_password ON users (must_change_password) WHERE must_change_password = TRUE;