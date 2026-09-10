-- 000024_alerts_status_default 回滚
--
-- 本迁移**可逆**：只改了列默认值、没有任何数据改写，把默认值改回 000001 声明的 'firing'
-- 即可。（与 000023 不同 —— 那个是数据迁移，迁完分不清原始值，所以刻意不可逆。）
--
-- 注意：回滚后 000001 的默认值重新生效。受影响的是**绕开 GORM 模型字段表**的写入方
--（裸 SQL / db.Exec / 将来的非 GORM 导入器），它们漏设 status 会落 'firing' ——
-- 一个前端 getAlertActions 给不出按钮的状态。走 GORM 的写入方不受影响（模型 tag 自己兜）。
-- down 的存在只是为了让迁移链对称可回滚，**不代表 'firing' 是可接受的状态**。

ALTER TABLE alerts ALTER COLUMN status SET DEFAULT 'firing';
