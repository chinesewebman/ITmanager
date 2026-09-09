package handlers_test

// testUsersDDL 是 users 表的**唯一**测试 DDL。
//
// 为什么必须只有一份：本包所有测试共用同一个 sqlite 内存库
// （`file::memory:?cache=shared`，同进程同 DSN 即同库），而各 setup 用的是
// `CREATE TABLE IF NOT EXISTS` —— 先建者胜，后者的约束定义被静默吞掉。
// 曾经 api_key_handler_test.go 与 auth_handler_test.go 各写一份且约束不一致，
// 导致 `-shuffle=on` 下偶发 `NOT NULL constraint failed`（审计中-2）。
//
// 列与约束对齐迁移 000001（username/password_hash NOT NULL）+ 000012
// （must_change_password）+ 000013（role）+ models.User 的软删列。
const testUsersDDL = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    nickname TEXT,
    email TEXT,
    phone TEXT,
    avatar TEXT,
    department_id TEXT,
    role TEXT DEFAULT 'user',
    status TEXT DEFAULT 'active',
    failed_login INTEGER DEFAULT 0,
    locked_until DATETIME,
    last_login DATETIME,
    last_login_ip TEXT,
    must_change_password INTEGER DEFAULT 1,
    password_set_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME
);
`
