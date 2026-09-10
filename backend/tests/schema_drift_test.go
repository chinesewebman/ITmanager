// schema 漂移守门测试 —— 纯解析，不需要数据库，永远参与 `go test ./...`。
//
// 为什么需要它：生产建库走 backend/migrations/*.up.sql（cmd/server/main.go:34 注入
// MigrationsFS → database.go:72-73 执行 migrate.Up），而代码实际按 GORM 模型枚举列。
// 两边是平行宇宙，CI 里没有 Postgres，漂移长期不可见（docs/v3-架构优化需求.md §9 D-1/D-4/D-5）。
//
// 本测试把两边解析出来做集合比对：
//   - GORM 模型  → schema.Parse 得到 TableName + 每个字段的 DBName
//   - 迁移 DDL   → 解析 CREATE TABLE / ALTER TABLE ADD COLUMN / RENAME 得到 表→列集合
//
// 断言：模型用到的每一列，迁移建出来的库里必须存在。
//
// 修复方向固定为「迁移向模型对齐」，所以这里只查「模型有、迁移无」（阻断），
// 「迁移有、模型无」只打印不失败（历史遗留列，见 FIX-PLAN §6 不做 DROP）。
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"

	"network-monitor-platform/internal/models"
)

// liveModels 是「代码实际在用」的模型集合。新增模型时同步加到这里，
// 否则新表的漂移不会被本测试发现。
func liveModels() []interface{} {
	return []interface{}{
		&models.User{},
		&models.APIKey{},
		&models.AuditLog{},
		&models.Asset{},
		&models.AssetNetwork{},
		&models.Rack{},
		&models.Site{},
		&models.Alert{},
		&models.AlertRule{},
		&models.Ticket{},
		&models.TicketHistory{},
		&models.NotificationChannel{},
		&models.NotificationLog{},
		&models.AlertSuppression{},
		&models.OncallSchedule{},
		&models.OncallShift{},
		&models.EscalationPolicy{},
		&models.EscalationLevel{},
		&models.Runbook{},
		&models.MetricSnapshot{},
	}
}

// modelSchema 表名 → 模型声明的列集合
type modelSchema map[string]map[string]bool

// parseModels 用 GORM 自己的解析器拿表名与列名，避免手抄 struct tag。
func parseModels(t *testing.T) modelSchema {
	t.Helper()
	var cache sync.Map
	out := modelSchema{}
	for _, m := range liveModels() {
		s, err := schema.Parse(m, &cache, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("schema.Parse(%T): %v", m, err)
		}
		cols := out[s.Table]
		if cols == nil {
			cols = map[string]bool{}
			out[s.Table] = cols
		}
		for _, f := range s.Fields {
			if f.IgnoreMigration || f.DBName == "" || f.DBName == "-" {
				continue
			}
			// 关联字段没有自己的列（外键列由 GORM 另建为普通字段）
			if f.DataType == "" {
				continue
			}
			cols[f.DBName] = true
		}
	}
	return out
}

var (
	reCreateTable = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s*\(`)
	reAlterAdd    = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)
	reAlterRename = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s+RENAME\s+TO\s+([a-z_][a-z0-9_]*)`)
	reRenameCol   = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s+RENAME\s+COLUMN\s+"?([a-z_][a-z0-9_]*)"?\s+TO\s+"?([a-z_][a-z0-9_]*)"?`)
	reDropTable   = regexp.MustCompile(`(?is)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)
	// DROP COLUMN：不解析的话，后续迁移删掉列后，模型缺列仍会被判「存在」→ 漏报（审计 建议）
	reDropCol = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?([a-z_][a-z0-9_]*)\s+DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)
)

// stripLineComment 去掉 `-- ...` 行内注释，避免注释里的词被当成 DDL。
func stripLineComment(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// createTableBody 从 `(` 开始做括号配对，返回表体（不含最外层括号）。
// 括号内的 CHECK/UNIQUE 等约束会被原样保留，交给列解析过滤。
func createTableBody(s string, openParen int) (string, bool) {
	depth := 0
	for i := openParen; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openParen+1 : i], true
			}
		}
	}
	return "", false
}

// tableColumns 从 CREATE TABLE 表体里抽出列名。
// 只取「行首标识符 + 后面不是约束关键字」的行，跳过 PRIMARY KEY / UNIQUE / CONSTRAINT /
// FOREIGN KEY / CHECK / LIKE 这些非列定义行。
var constraintKeywords = map[string]bool{
	"primary": true, "unique": true, "constraint": true,
	"foreign": true, "check": true, "like": true, "exclude": true,
}

func tableColumns(body string) []string {
	var cols []string
	for _, raw := range strings.Split(body, ",") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// 跨行的约束定义（如 CONSTRAINT x UNIQUE (...)) 在按逗号切分后可能被拆开，
		// 这里只认「第一个 token 是普通标识符且不是约束关键字」的片段。
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// 列名可能被引号包住（保留字，如 racks."column"）
		name := strings.ToLower(strings.Trim(fields[0], `"`))
		if constraintKeywords[name] {
			continue
		}
		if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(name) {
			continue
		}
		cols = append(cols, name)
	}
	return cols
}

// parseMigrations 解析 migrations 目录下所有 *.up.sql，得到「按顺序执行完」后的 表→列。
func parseMigrations(t *testing.T, dir string) map[string]map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到迁移文件于 %s: %v", dir, err)
	}
	sort.Strings(files) // 文件名升序 = runner 的执行顺序

	tables := map[string]map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读迁移文件 %s: %v", f, err)
		}
		s := stripLineComment(string(raw))

		// 同一文件内的 DDL 必须按出现顺序执行（例如先 RENAME 表、再 RENAME 该表的列），
		// 否则「先按类型批量处理」会得到错误的最终态。
		type op struct {
			pos  int
			kind string
			m    []string
			body string
		}
		var ops []op
		for _, m := range reCreateTable.FindAllStringSubmatchIndex(s, -1) {
			name := s[m[2]:m[3]]
			body, ok := createTableBody(s, m[1]-1)
			if !ok {
				t.Fatalf("%s: CREATE TABLE %s 括号不配对", f, name)
			}
			ops = append(ops, op{pos: m[0], kind: "create", m: []string{name}, body: body})
		}
		for _, m := range reAlterAdd.FindAllStringSubmatchIndex(s, -1) {
			ops = append(ops, op{pos: m[0], kind: "addcol", m: []string{s[m[2]:m[3]], s[m[4]:m[5]]}})
		}
		for _, m := range reRenameCol.FindAllStringSubmatchIndex(s, -1) {
			ops = append(ops, op{pos: m[0], kind: "renamecol", m: []string{s[m[2]:m[3]], s[m[4]:m[5]], s[m[6]:m[7]]}})
		}
		for _, m := range reAlterRename.FindAllStringSubmatchIndex(s, -1) {
			ops = append(ops, op{pos: m[0], kind: "renametbl", m: []string{s[m[2]:m[3]], s[m[4]:m[5]]}})
		}
		for _, m := range reDropTable.FindAllStringSubmatchIndex(s, -1) {
			ops = append(ops, op{pos: m[0], kind: "droptbl", m: []string{s[m[2]:m[3]]}})
		}
		for _, m := range reDropCol.FindAllStringSubmatchIndex(s, -1) {
			ops = append(ops, op{pos: m[0], kind: "dropcol", m: []string{s[m[2]:m[3]], s[m[4]:m[5]]}})
		}
		sort.SliceStable(ops, func(i, j int) bool { return ops[i].pos < ops[j].pos })

		for _, o := range ops {
			switch o.kind {
			case "create":
				if tables[o.m[0]] == nil {
					tables[o.m[0]] = map[string]bool{}
				}
				for _, c := range tableColumns(o.body) {
					tables[o.m[0]][c] = true
				}
			case "addcol":
				if tables[o.m[0]] == nil {
					tables[o.m[0]] = map[string]bool{}
				}
				tables[o.m[0]][o.m[1]] = true
			case "renamecol":
				if cols, ok := tables[o.m[0]]; ok && cols[o.m[1]] {
					delete(cols, o.m[1])
					cols[o.m[2]] = true
				}
			case "renametbl":
				if src, ok := tables[o.m[0]]; ok {
					if tables[o.m[1]] == nil {
						tables[o.m[1]] = map[string]bool{}
					}
					for c := range src {
						tables[o.m[1]][c] = true
					}
					delete(tables, o.m[0])
				}
			case "droptbl":
				delete(tables, o.m[0])
			case "dropcol":
				if cols, ok := tables[o.m[0]]; ok {
					delete(cols, o.m[1])
				}
			}
		}
	}
	return tables
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestSchemaDrift_ModelColumnsExistInMigrations(t *testing.T) {
	models_ := parseModels(t)
	migs := parseMigrations(t, filepath.Join("..", "migrations"))

	var missingTables, missingCols []string
	tables := make([]string, 0, len(models_))
	for k := range models_ {
		tables = append(tables, k)
	}
	sort.Strings(tables)

	for _, table := range tables {
		cols, ok := migs[table]
		if !ok {
			missingTables = append(missingTables, table)
			continue
		}
		for _, col := range sortedKeys(models_[table]) {
			if !cols[col] {
				missingCols = append(missingCols, fmt.Sprintf("%s.%s", table, col))
			}
		}
	}

	if len(missingTables) > 0 {
		t.Errorf("迁移里没有建这些表（模型在用，查询会直接 relation does not exist）：\n  %s",
			strings.Join(missingTables, "\n  "))
	}
	if len(missingCols) > 0 {
		t.Errorf("迁移里缺这些列（模型在用，INSERT/SELECT 会报 column does not exist）：\n  %s",
			strings.Join(missingCols, "\n  "))
	}

	// 反向漂移（迁移有、模型无）只打印，不失败：历史遗留列，本轮不做 DROP。
	var extra []string
	for _, table := range sortedKeys(migs) {
		cols, ok := models_[table]
		if !ok {
			continue // 迁移建了但本轮不用的表（ticket_* / ai_* / vmware_* 等）
		}
		for _, col := range sortedKeys(migs[table]) {
			if !cols[col] {
				extra = append(extra, fmt.Sprintf("%s.%s", table, col))
			}
		}
	}
	if len(extra) > 0 {
		t.Logf("反向漂移（迁移有、模型无，本轮保留不 DROP）%d 处：\n  %s",
			len(extra), strings.Join(extra, "\n  "))
	}
}

// TestSchemaDrift_DumpModelTypes 打印模型在 PostgreSQL 下期望的列类型。
// 默认跳过；DRIFT_DUMP=1 go test -run DumpModelTypes -v 时输出，
// 供人工与 information_schema 核对（生成/校验 000013 用）。
func TestSchemaDrift_DumpModelTypes(t *testing.T) {
	if os.Getenv("DRIFT_DUMP") == "" {
		t.Skip("设 DRIFT_DUMP=1 才打印")
	}
	var cache sync.Map
	d := postgres.Dialector{}
	for _, m := range liveModels() {
		s, err := schema.Parse(m, &cache, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("schema.Parse(%T): %v", m, err)
		}
		for _, f := range s.Fields {
			if f.IgnoreMigration || f.DBName == "" || f.DBName == "-" || f.DataType == "" {
				continue
			}
			notNull := ""
			if f.NotNull {
				notNull = " NOT NULL"
			}
			fmt.Printf("%s|%s|%s%s\n", s.Table, f.DBName, d.DataTypeOf(f), notNull)
		}
	}
}

// TestSchemaDrift_解析器_DROP与RENAME 用合成迁移守住解析器本身的行为：
// 顺序敏感（先 RENAME 表再 RENAME 该表的列）、DROP COLUMN 必须真的把列移除。
// 解析器漏报 = 守门失效，所以它自己也要有测试（审计 建议）。
func TestSchemaDrift_解析器_DROP与RENAME(t *testing.T) {
	dir := t.TempDir()
	sql := `CREATE TABLE idc (id UUID, name TEXT, legacy TEXT);
ALTER TABLE idc RENAME TO sites;
ALTER TABLE sites RENAME COLUMN name TO site_name;
ALTER TABLE sites DROP COLUMN legacy;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS region VARCHAR(50);
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000001_synthetic.up.sql"), []byte(sql), 0o600))

	got := parseMigrations(t, dir)
	require.Contains(t, got, "sites", "表应被 RENAME 成 sites")
	require.NotContains(t, got, "idc", "旧表名应消失")
	assert.True(t, got["sites"]["site_name"], "列应被 RENAME 成 site_name")
	assert.False(t, got["sites"]["name"], "旧列名应消失")
	assert.False(t, got["sites"]["legacy"], "DROP COLUMN 的列必须被移除")
	assert.True(t, got["sites"]["region"], "ADD COLUMN 的列应在")
}
