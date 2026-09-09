package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"network-monitor-platform/internal/config"
)

// makeTestEnv 重置全局 level + writers 到测试 buffer。
func makeTestEnv(lvl string) *threadSafeBuf {
	mu.Lock()
	defer mu.Unlock()
	level = lvl
	buf := &threadSafeBuf{}
	for _, l := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		writers[l] = buf
	}
	isatty = func(io.Writer) bool { return false }
	return buf
}

func TestShouldLog(t *testing.T) {
	cases := []struct {
		current, msg string
		want         bool
	}{
		{"DEBUG", "DEBUG", true},
		{"DEBUG", "INFO", true},
		{"INFO", "DEBUG", false},
		{"INFO", "INFO", true},
		{"WARN", "INFO", false},
		{"ERROR", "ERROR", true},
		{"WARN", "ERROR", true},
		// 未知/空级别必须按 INFO（fail-closed）。查表 miss 的零值等价 DEBUG，
		// 会把拼错的级别（"warning"）静默变成最啰嗦档 —— 与 gorm 侧相反。
		{"WARNING", "DEBUG", false},
		{"WARNING", "INFO", true},
		{"", "DEBUG", false},
	}
	for _, c := range cases {
		if got := shouldLog(c.current, c.msg); got != c.want {
			t.Errorf("shouldLog(%s,%s)=%v, want %v", c.current, c.msg, got, c.want)
		}
	}
}

func TestLog_JSONOutput(t *testing.T) {
	buf := makeTestEnv("INFO")

	Infof("user login", "user_id", 42, "ip", "1.2.3.4")
	Warnf("slow query", "ms", 1500)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	var e1 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatalf("line 1 not JSON: %v / %s", err, lines[0])
	}
	if e1["level"] != "INFO" || e1["message"] != "user login" {
		t.Errorf("line 1 content wrong: %v", e1)
	}
	fields, _ := e1["fields"].(map[string]any)
	if fields["user_id"] != float64(42) || fields["ip"] != "1.2.3.4" {
		t.Errorf("fields wrong: %v", fields)
	}
	if _, ok := e1["timestamp"].(string); !ok {
		t.Errorf("timestamp missing or wrong type: %v", e1["timestamp"])
	}

	var e2 map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &e2)
	if e2["level"] != "WARN" {
		t.Errorf("line 2 level: %v", e2["level"])
	}
}

func TestLog_LevelFilter(t *testing.T) {
	buf := makeTestEnv("WARN")

	Debugf("debug 1") // filtered
	Infof("info 1")   // filtered
	Warnf("warn 1")
	Errorf("error 1")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after filter, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"level":"WARN"`) || !strings.Contains(lines[1], `"level":"ERROR"`) {
		t.Errorf("levels wrong: %s / %s", lines[0], lines[1])
	}
}

func TestLog_ODDKV(t *testing.T) {
	buf := makeTestEnv("DEBUG")
	Infof("test", "k1", "v1", "k2") // 奇数 kv：最后一个 key 配 ""

	line := strings.TrimRight(buf.String(), "\n")
	var e map[string]any
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("not JSON: %v / %s", err, line)
	}
	f := e["fields"].(map[string]any)
	if f["k1"] != "v1" {
		t.Errorf("k1: %v", f["k1"])
	}
	if v, ok := f["k2"]; !ok || v != "" {
		t.Errorf("k2 should be empty string for odd kv, got %v", v)
	}
}

func TestLog_TimestampRFC3339(t *testing.T) {
	makeTestEnv("INFO")
	nowFn = func() time.Time {
		return time.Date(2026, 6, 15, 12, 34, 56, 789_000_000, time.UTC)
	}
	defer func() { nowFn = time.Now }()

	// 由于现在无法取 buf 引用（makeTestEnv 已重置 writers），
	// 重新配置：直接覆盖
	mu.Lock()
	buf := &threadSafeBuf{}
	for _, l := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		writers[l] = buf
	}
	isatty = func(io.Writer) bool { return false }
	mu.Unlock()

	Infof("test")
	line := strings.TrimRight(buf.String(), "\n")
	var e map[string]any
	_ = json.Unmarshal([]byte(line), &e)
	ts, _ := e["timestamp"].(string)
	if !strings.HasPrefix(ts, "2026-06-15T12:34:56") {
		t.Errorf("timestamp not RFC3339: %q", ts)
	}
}

func TestLog_TextOutput_TTY(t *testing.T) {
	mu.Lock()
	level = "INFO"
	buf := &threadSafeBuf{}
	for _, l := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		writers[l] = buf
	}
	isatty = func(io.Writer) bool { return true }
	mu.Unlock()

	Infof("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "INFO") || !strings.Contains(out, "hello") {
		t.Errorf("text format wrong: %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("TTY should be text, got JSON: %q", out)
	}
}

func TestInfo_Debug_Variants(t *testing.T) {
	buf := makeTestEnv("DEBUG")
	Info("user %s", "alice")
	Debug("debug msg")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "user alice") {
		t.Errorf("format string not applied: %s", lines[0])
	}
	if !strings.Contains(lines[1], "debug msg") {
		t.Errorf("Debug not working: %s", lines[1])
	}
}

// snapshotGlobal 保存全局 level/writers，cleanup 时恢复（Init 会覆盖它们）。
func snapshotGlobal(t *testing.T) {
	t.Helper()
	mu.Lock()
	oldLevel := level
	oldWriters := map[string]io.Writer{}
	for k, v := range writers {
		oldWriters[k] = v
	}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		level = oldLevel
		for k, v := range oldWriters {
			writers[k] = v
		}
		mu.Unlock()
	})
}

// TestInit_级别归一化 钉住 G-16：shouldLog 的级别表只有大写键，而 config.yaml 写的是
// 小写 "info" —— 原样存会让 order["info"] 取到零值（等价 DEBUG），级别过滤完全失效。
func TestInit_级别归一化(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"info", "INFO"}, {"INFO", "INFO"}, {" Info ", "INFO"},
		{"debug", "DEBUG"}, {"warn", "WARN"}, {"error", "ERROR"}, {"", "INFO"},
	} {
		t.Run("in="+c.in, func(t *testing.T) {
			snapshotGlobal(t)
			Init(&config.LogConfig{Level: c.in})
			mu.Lock()
			got := level
			mu.Unlock()
			if got != c.want {
				t.Errorf("Init(level=%q) → %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestInit_小写info真的过滤DEBUG 断言行为而不只是字段：没做归一化时 Debug 行会照打。
func TestInit_小写info真的过滤DEBUG(t *testing.T) {
	snapshotGlobal(t)
	Init(&config.LogConfig{Level: "info"})

	mu.Lock()
	buf := &threadSafeBuf{}
	for _, l := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		writers[l] = buf
	}
	isatty = func(io.Writer) bool { return false }
	mu.Unlock()

	Debug("不该出现的 debug 行")
	Info("该出现的 info 行")

	out := buf.String()
	if strings.Contains(out, "不该出现的 debug 行") {
		t.Errorf("level=info 时 DEBUG 行必须被过滤，实际输出: %s", out)
	}
	if !strings.Contains(out, "该出现的 info 行") {
		t.Errorf("INFO 行必须保留，实际输出: %s", out)
	}
}

// 线程安全 buffer
type threadSafeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *threadSafeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
