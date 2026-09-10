-- 000023_ticket_priority_normalize
--
-- M16：tickets.priority 用**两套词**表示同一个「普通」——契约（openapi.yaml 的
-- Ticket.priority enum）与手工建单表单用 normal，GLPI 同步（integration/glpi.go 的 ConvertToTicket）
-- 与告警一键建单（service/ticket_service.go 的 priorityFromSeverity）用 medium。
-- 该列没有 CHECK 约束，两套值都能落库。后果：工单页按「普通」筛选
-- （WHERE priority='normal'）**查不到告警派生出来的票**。
--
-- 本迁移以契约为准，把存量数据收敛到 normal。只命中 medium 这一个**已知同义词**：
-- 不对未知值做「一律改成 normal」的归一——那是凭空定义语义，会把
-- 'urgent' / '' 之类的值悄悄变成「普通」，比留着不动更难查。
--
-- 幂等：重复执行第二次命中 0 行。
-- 不可逆：迁移后无法区分「原本就是 normal」与「从 medium 迁来的」行，见同名 .down.sql。
--
-- 序号取 000023 而非 000022：000022 被 pg_trgm（docs/FIX-PLAN-UI-PERF.md P20）在计划里预占
-- （盘上没有该文件）。让号而不是抢号 —— 迁移执行器 internal/migrate 的 Load() 曾经按版本号
-- 归并、同号会**静默覆盖**其一（本轮已改为撞号即报错，见 docs/FIX-PLAN-M16-PRIORITY.md §7）。

UPDATE tickets SET priority = 'normal' WHERE priority = 'medium';
