-- W6 P14: /dashboard/kpis 的 4 条聚合（MTTR/MTTD/密度/计数）都按
-- `problem_start >= ?` 过滤时间窗，problem_start 无索引 → 每次扫全表
-- （审计实测合计 ~1.1s）。加索引后密度查询 360.6ms→12.1ms。
-- 对应 dashboard_service.go:156-207 的 4 处 `WHERE problem_start >= ?`。
CREATE INDEX IF NOT EXISTS idx_alerts_problem_start
    ON alerts(problem_start);
