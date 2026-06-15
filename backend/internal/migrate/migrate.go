// Package migrate 提供零依赖的轻量级 SQL migration runner。
// 约定：migrations/ 目录下 *.up.sql 按文件名升序执行；*.down.sql 反向回滚。
// 状态存到 schema_migrations(version BIGINT PK, applied_at TIMESTAMPTZ)。
package migrate

import (
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
var FS embed.FS

// 确保 schema_migrations 表存在
func ensureTable(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error
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
		if err := runInTx(db, m.upSQL); err != nil {
			return fmt.Errorf("apply %d_%s: %w", m.version, m.name, err)
		}
		if err := db.Exec("INSERT INTO schema_migrations(version) VALUES (?)", m.version).Error; err != nil {
			return fmt.Errorf("record %d: %w", m.version, err)
		}
		log.Printf("✓ applied %d_%s in %s", m.version, m.name, time.Since(start))
	}
	return nil
}

// Down 回滚最后一个已应用的 migration
func Down(db *gorm.DB) error {
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
	if err := runInTx(db, mig.downSQL); err != nil {
		return fmt.Errorf("rollback %d: %w", mig.version, err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", mig.version).Error; err != nil {
		return err
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
func runInTx(gdb *gorm.DB, sqlText string) error {
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	return execInTx(sqlDB, sqlText)
}

func execInTx(db *sql.DB, sqlText string) error {
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
	return tx.Commit()
}

// splitStatements 简单分号切分（不处理 $$...$$ 函数体；本项目 schema 不涉及）
func splitStatements(sqlText string) []string {
	// 去掉 -- 注释行
	var cleaned strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteString("\n")
	}
	return strings.Split(cleaned.String(), ";")
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}
