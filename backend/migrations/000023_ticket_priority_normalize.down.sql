-- 000023_ticket_priority_normalize 回滚
--
-- **本迁移不可逆**，这里刻意不做任何数据改写。
--
-- 原因：迁移后无法区分「原本就是 normal」的行与「从 medium 迁过来的」行。
-- 写 UPDATE tickets SET priority='medium' WHERE priority='normal' 会把本来正确的行
-- 一起改坏——用一个静默的数据损坏去「回滚」另一个，比不回滚更糟。
-- 真正的回滚走备份恢复（上线前 pg_dump）。
--
-- 保留一条 no-op 语句，让 down 链可执行：migrate.Down 按版本号顺序回滚，
-- 而 splitStatements 会把注释原样累积成一条「语句」交给驱动执行，注释-only 文件
-- 因此依赖驱动对「纯注释查询」的容忍度——没必要引入这个边缘依赖。

SELECT 1;
