package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonEmpty 去掉切分产生的空白片段（execInTx 会 TrimSpace 后跳过空串）。
func nonEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "普通多语句",
			sql:  "CREATE TABLE a(x int);\nINSERT INTO a VALUES (1);",
			want: []string{"CREATE TABLE a(x int)", "\nINSERT INTO a VALUES (1)"},
		},
		{
			name: "DO 匿名块整体不切分",
			sql:  "DO $$\nBEGIN\n    IF x THEN\n        ALTER TABLE a RENAME TO b;\n    END IF;\nEND\n$$;",
			want: []string{"DO $$\nBEGIN\n    IF x THEN\n        ALTER TABLE a RENAME TO b;\n    END IF;\nEND\n$$"},
		},
		{
			name: "带 tag 的 dollar-quoted 块",
			sql:  "DO $body$ SELECT 1; SELECT 2; $body$;\nSELECT 3;",
			want: []string{"DO $body$ SELECT 1; SELECT 2; $body$", "\nSELECT 3"},
		},
		{
			name: "单引号字符串内的分号不切分",
			sql:  "INSERT INTO t VALUES ('a;b');",
			want: []string{"INSERT INTO t VALUES ('a;b')"},
		},
		{
			name: "单引号转义 '' 内的分号不切分",
			sql:  "INSERT INTO t VALUES ('a''b;c');",
			want: []string{"INSERT INTO t VALUES ('a''b;c')"},
		},
		{
			name: "行注释里的分号不切分",
			sql:  "-- 注释; 里有分号\nSELECT 1;",
			want: []string{"-- 注释; 里有分号\nSELECT 1"},
		},
		{
			name: "块注释里的分号不切分",
			sql:  "/* a; b */ SELECT 1;",
			want: []string{"/* a; b */ SELECT 1"},
		},
		{
			name: "位置参数 $1 不被当成 dollar-quote",
			sql:  "SELECT $1; SELECT $2;",
			want: []string{"SELECT $1", " SELECT $2"},
		},
		{
			name: "未闭合 dollar-quote 不 panic，剩余整体返回",
			sql:  "DO $$ BEGIN SELECT 1;",
			want: []string{"DO $$ BEGIN SELECT 1;"},
		},
		{
			name: "空脚本",
			sql:  "",
			want: []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, nonEmpty(splitStatements(c.sql)))
		})
	}
}

func TestDollarTag(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"$$", "$$", true},
		{"$$ BEGIN", "$$", true},
		{"$body$ x", "$body$", true},
		{"$tag_1$ x", "$tag_1$", true},
		{"$1)", "", false},   // 位置参数
		{"$1$ x", "", false}, // tag 不能以数字开头
		{"$", "", false},
		{"a$", "", false},
	}
	for _, c := range cases {
		got, ok := dollarTag(c.in)
		assert.Equal(t, c.ok, ok, "dollarTag(%q) ok", c.in)
		assert.Equal(t, c.want, got, "dollarTag(%q)", c.in)
	}
}

// TestSplitStatements_真实迁移文件 用生产切分器切 backend/migrations/*.up.sql，
// 断言每个文件的 dollar-quote 配对完整（DO 块没被切碎）。
//
// 这是防「psql 跑得通、migrate.Up 跑不通」假绿的静态哨兵：
// db_smoke.sh 需要 docker，本用例只需仓库文件，普通 go test 就会跑。
func TestSplitStatements_真实迁移文件(t *testing.T) {
	dir := filepath.Join("..", "..", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "没找到 %s/*.up.sql", dir)

	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		stmts := nonEmpty(splitStatements(string(raw)))
		for _, s := range stmts {
			// 一条语句里 $$ 必须成对（0 个或 2 个）；奇数说明 DO 块被切碎
			if n := strings.Count(s, "$$"); n%2 != 0 {
				t.Errorf("%s: 语句被切碎（$$ 计数为奇数 %d）:\n%s",
					filepath.Base(f), n, firstLine(s))
			}
		}
	}
}
