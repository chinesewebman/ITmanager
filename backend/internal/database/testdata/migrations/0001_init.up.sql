-- M88 / G-14 数据库测试用迁移: 创建 users 表 (sqlite 方言, 整数主键自增).
-- 用于 InitWithAutoMigrate_默认true不破现有行为 + TestInitWithAutoMigrate_开关false跳过migrateUp.
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT UNIQUE
);
