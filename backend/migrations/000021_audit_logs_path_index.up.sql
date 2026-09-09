-- W6 P18: 审计列表 path LIKE 'x%' 前缀匹配，path 无可用索引（审计实测罕见过滤
-- 455ms）。text_pattern_ops 专为 LIKE 前缀设计——非 C collation 下默认 btree
-- opclass 不加速 LIKE。对应 service/audit_service.go:50-53。
CREATE INDEX IF NOT EXISTS idx_audit_logs_path
    ON audit_logs(path text_pattern_ops);
