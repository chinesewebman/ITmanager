// Package migrate 提供零依赖的轻量级 SQL migration runner。
// 约定：migrations/ 目录下 *.up.sql 按文件名升序执行；*.down.sql 反向回滚。
// 状态存到 schema_migrations(version BIGINT PK, applied_at TIMESTAMPTZ)。
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// FS 注入：调用方用 embed.FS 把 migrations/ 目录打包进二进制
// 接受 fs.FS 接口（embed.FS 实现了 fs.FS）方便测试注入
var FS fs.FS = embed.FS{}

// ensureTable 确保 schema_migrations 表存在
func ensureTable(db *gorm.DB) error {
	// SQLite 没有 TIMESTAMPTZ，用 DATETIME 替代
	colType := "TIMESTAMPTZ"
	defaultExpr := "NOW()"
	if db.Dialector.Name() == "sqlite" {
		colType = "DATETIME"
		// SQLite 不支持 NOW()（Postgres 关键字），用 CURRENT_TIMESTAMP
		defaultExpr = "CURRENT_TIMESTAMP"
	}
	stmt := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at ` + colType + ` NOT NULL DEFAULT ` + defaultExpr + `
	)`
	return db.Exec(stmt).Error
}

// advisoryLockKey 全局 migration 互斥锁的 key（C-F13）
// 选 'MIGRO' + 'TION!' = 0x4D49_4752_4F54_494F_4E21
// 必须全 8 字节非 0；选 ASCII 字符串 'MIGRATE!' 的 bigint 表示
const advisoryLockKey int64 = 0x4D49_4752_4154_4521 // 'MIGRATE!'

// migrationLock 持有独占的 *sql.Conn：pg_advisory_lock 是**会话级**锁，
// 加锁与解锁必须落在同一条连接上。早先用 db.Raw/db.Exec 各取一次连接，
// 池里换一条连接就解不掉，锁会一直挂到进程退出（审计 中-7）。
type migrationLock struct{ conn *sql.Conn }

// acquireLock 拿全局 migration 互斥锁（C-F13）。
// 非阻塞：已被别的进程持锁时立即返回错误，由 caller 决定重试。
func acquireLock(db *gorm.DB) (*migrationLock, error) {
	// SQLite 没有 advisory lock 概念（pg_try_advisory_lock 会报 no such function）
	// 测试 + 本地开发用 sqlite，无需互斥；生产用 postgres 走真实锁
	if db.Dialector.Name() == "sqlite" {
		return &migrationLock{}, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	var got bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&got); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !got {
		_ = conn.Close()
		return nil, fmt.Errorf("migration lock held by another process, retry later")
	}
	return &migrationLock{conn: conn}, nil
}

// release 解锁并归还连接（忽略错误：unlock 失败不阻塞 caller）
func (l *migrationLock) release() {
	if l == nil || l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	_ = l.conn.Close()
}

type migration struct {
	version int64
	name    string
	upSQL   string
	downSQL string
}

// Load 解析 embed.FS 中的所有 migration（按版本号排序）
func Load() ([]migration, error) {
	entries, err := fs.ReadDir(FS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	byVer := make(map[int64]*migration)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// 形如 000001_init.up.sql / 000001_init.down.sql
		base := e.Name()
		idx := strings.Index(base, "_")
		if idx < 0 {
			continue
		}
		verStr := base[:idx]
		ver, err := strconv.ParseInt(verStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration filename %q: %w", base, err)
		}
		m, ok := byVer[ver]
		if !ok {
			m = &migration{version: ver}
			byVer[ver] = m
		}
		content, err := fs.ReadFile(FS, "migrations/"+base)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", base, err)
		}
		if strings.HasSuffix(base, ".up.sql") {
			m.upSQL = string(content)
			m.name = strings.TrimSuffix(strings.TrimSuffix(base, ".up.sql"), strconv.FormatInt(ver, 10)+"_")
		} else if strings.HasSuffix(base, ".down.sql") {
			m.downSQL = string(content)
		}
	}

	migs := make([]migration, 0, len(byVer))
	for _, m := range byVer {
		migs = append(migs, *m)
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

// Status 打印已应用和待应用的 migration
func Status(db *gorm.DB) error {
	if err := ensureTable(db); err != nil {
		return err
	}
	migs, err := Load()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}
	appliedSet := make(map[int64]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}
	fmt.Printf("%-10s %-30s %s\n", "VERSION", "NAME", "STATUS")
	for _, m := range migs {
		status := "pending"
		if appliedSet[m.version] {
			status = "applied"
		}
		fmt.Printf("%-10d %-30s %s\n", m.version, m.name, status)
	}
	return nil
}

// Up 应用所有未执行的 migration
func Up(db *gorm.DB) error {
	// C-F13: 进程间互斥锁，防止两个 Up 并发跑同 version migration
	lock, err := acquireLock(db)
	if err != nil {
		return err
	}
	defer lock.release()

	if err := ensureTable(db); err != nil {
		return err
	}
	migs, err := Load()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}
	appliedSet := make(map[int64]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}
	for _, m := range migs {
		if appliedSet[m.version] {
			continue
		}
		if m.upSQL == "" {
			return fmt.Errorf("migration %d has no .up.sql", m.version)
		}
		log.Printf("⏫ applying %d_%s ...", m.version, m.name)
		start := time.Now()
		// 版本记录必须在同一个事务里（审计 中-8）：否则 DDL 提交、版本未记录时进程被杀，
		// 下次启动会重放该迁移 —— 而 000013 的类型转换/000001 的 ADD CONSTRAINT 都不幂等。
		record := fmt.Sprintf("INSERT INTO schema_migrations(version) VALUES (%d)", m.version)
		if err := runInTx(db, m.upSQL, record); err != nil {
			return fmt.Errorf("apply %d_%s: %w", m.version, m.name, err)
		}
		log.Printf("✓ applied %d_%s in %s", m.version, m.name, time.Since(start))
	}
	return nil
}

// Down 回滚最后一个已应用的 migration
func Down(db *gorm.DB) error {
	// C-F13: 进程间互斥锁（跟 Up 用同一把锁）
	lock, err := acquireLock(db)
	if err != nil {
		return err
	}
	defer lock.release()

	if err := ensureTable(db); err != nil {
		return err
	}
	migs, err := Load()
	if err != nil {
		return err
	}
	if len(migs) == 0 {
		return nil
	}
	// 找最新已应用的
	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		log.Println("no migrations to rollback")
		return nil
	}
	latest := applied[len(applied)-1]
	var mig *migration
	for i := range migs {
		if migs[i].version == latest {
			mig = &migs[i]
			break
		}
	}
	if mig == nil {
		return fmt.Errorf("applied version %d not found in migrations/", latest)
	}
	if mig.downSQL == "" {
		return fmt.Errorf("migration %d has no .down.sql", mig.version)
	}
	log.Printf("⏬ rolling back %d_%s ...", mig.version, mig.name)
	record := fmt.Sprintf("DELETE FROM schema_migrations WHERE version = %d", mig.version)
	if err := runInTx(db, mig.downSQL, record); err != nil {
		return fmt.Errorf("rollback %d: %w", mig.version, err)
	}
	log.Printf("✓ rolled back %d_%s", mig.version, mig.name)
	return nil
}

func appliedVersions(db *gorm.DB) ([]int64, error) {
	rows, err := db.Raw("SELECT version FROM schema_migrations ORDER BY version").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// runInTx 在事务中执行 SQL 脚本（每条 ; 分隔的语句独立 exec）
// 注：gorm 的 transaction 没有原生 multi-statement 支持，降到 sql.DB 自己跑
//
// tailSQL 是脚本跑完后在**同一个事务**里执行的收尾语句（版本记录），
// 让「DDL + 版本记录」原子化，消除「DDL 已提交、版本未记录」的重放窗口。
func runInTx(gdb *gorm.DB, sqlText, tailSQL string) error {
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	return execInTx(sqlDB, sqlText, tailSQL)
}

func execInTx(db *sql.DB, sqlText, tailSQL string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	for _, stmt := range splitStatements(sqlText) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec %q: %w", firstLine(stmt), err)
		}
	}
	if strings.TrimSpace(tailSQL) != "" {
		if _, err := tx.Exec(tailSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec %q: %w", firstLine(tailSQL), err)
		}
	}
	return tx.Commit()
}

// splitStatements 按分号切分 SQL 脚本，但跳过：
//   - 单引号字符串（” 转义）
//   - dollar-quoted 块（$$ ... $$ / $tag$ ... $tag$）—— DO 匿名块、函数体
//   - 行注释 -- 与块注释 /* */
//
// 旧实现是裸 strings.Split(s, ";")，会把 `DO $$ BEGIN ... ; ... END $$;`
// 切成语法碎片（000001 的 TimescaleDB 探测、000013 的表/列重命名都是这种块），
// 于是 psql 能跑通的迁移在 migrate.Up 里必失败 —— 冒烟必须走本函数。
func splitStatements(sqlText string) []string {
	var (
		out []string
		cur strings.Builder
		n   = len(sqlText)
	)
	for i := 0; i < n; {
		switch c := sqlText[i]; {
		case c == '-' && i+1 < n && sqlText[i+1] == '-':
			j := strings.IndexByte(sqlText[i:], '\n')
			if j < 0 {
				j = n - i
			}
			cur.WriteString(sqlText[i : i+j])
			i += j
		case c == '/' && i+1 < n && sqlText[i+1] == '*':
			j := strings.Index(sqlText[i+2:], "*/")
			if j < 0 {
				cur.WriteString(sqlText[i:])
				i = n
				break
			}
			cur.WriteString(sqlText[i : i+2+j+2])
			i += 2 + j + 2
		case c == '\'':
			j := i + 1
			for j < n {
				if sqlText[j] == '\'' {
					if j+1 < n && sqlText[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			cur.WriteString(sqlText[i:j])
			i = j
		case c == '$':
			tag, ok := dollarTag(sqlText[i:])
			if !ok {
				cur.WriteByte(c)
				i++
				break
			}
			end := strings.Index(sqlText[i+len(tag):], tag)
			if end < 0 {
				cur.WriteString(sqlText[i:])
				i = n
				break
			}
			end = i + len(tag) + end + len(tag)
			cur.WriteString(sqlText[i:end])
			i = end
		case c == ';':
			out = append(out, cur.String())
			cur.Reset()
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	out = append(out, cur.String())
	return out
}

// dollarTag 判断 s 是否以 dollar-quote 起始符开头（$$ 或 $tag$），返回该起始符。
// tag 规则同 PG：字母/下划线开头，可含数字，不能以数字开头（否则 $1 占位符会被误判）。
func dollarTag(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '$' {
			return s[:i+1], true
		}
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 1)
		if !ok {
			return "", false
		}
	}
	return "", false
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}
