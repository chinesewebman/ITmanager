// Package logger 结构化日志（C-P8）。
//
// 输出格式：
//   - 终端（isatty）：彩色 + 人类可读
//   - 管道/文件：JSON（每行一条，含 timestamp/level/msg/fields）
//
// 设计原则：
//   - 零依赖（lumberjack 已在外）
//   - 字段用 kv 列表：logger.Info("user login", "user_id", 42, "ip", "1.2.3.4")
//   - 同步写：lumberjack 自身线程安全，无需额外锁
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"network-monitor-platform/internal/config"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	mu      sync.Mutex
	level   = "INFO" // 全局日志级别，由 Init 设置
	writers = map[string]io.Writer{}
	isatty  = isTerminalFn
	nowFn   = time.Now
)

const (
	timestampFormat = "2006-01-02T15:04:05.000Z07:00" // RFC3339 + millis
)

// Init 初始化 logger（应在 main 启动时调用一次）。
// 根据 cfg.Output 选择输出目标：file → lumberjack 滚动；stdout → os.Stdout。
func Init(cfg *config.LogConfig) {
	mu.Lock()
	defer mu.Unlock()
	level = cfg.Level

	if cfg.Output == "file" {
		dir := filepath.Dir(cfg.File.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "logger: 创建日志目录失败: %v\n", err)
		}
		w := &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSize,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     30,
			Compress:   true,
		}
		for _, lvl := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
			writers[lvl] = w
		}
	} else {
		for _, lvl := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
			writers[lvl] = os.Stdout
		}
	}
}

// Log 通用记录函数（level: DEBUG/INFO/WARN/ERROR；msg + kv 字段）。
func Log(levelStr, msg string, kv ...any) {
	mu.Lock()
	currentLevel := level
	w, ok := writers[levelStr]
	mu.Unlock()
	if !ok {
		return
	}
	if !shouldLog(currentLevel, levelStr) {
		return
	}

	// 拼装 fields：kv 必须偶数长度；odd 长度最后一个 key 也记录，值=空字符串
	fields := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = fmt.Sprintf("%v", kv[i])
		}
		var v any
		if i+1 < len(kv) {
			v = kv[i+1]
		} else {
			v = "" // odd 长度兜底
		}
		fields[k] = v
	}

	entry := struct {
		Timestamp string         `json:"timestamp"`
		Level     string         `json:"level"`
		Message   string         `json:"message"`
		Fields    map[string]any `json:"fields,omitempty"`
	}{
		Timestamp: nowFn().UTC().Format(timestampFormat),
		Level:     levelStr,
		Message:   msg,
		Fields:    fields,
	}

	if isatty(w) {
		// 人类可读：单行带 ANSI 色
		fmt.Fprintf(w, "%s %s%-5s%s %s %v\n",
			entry.Timestamp,
			colorFor(levelStr), levelStr, "\033[0m",
			entry.Message, entry.Fields)
		return
	}
	// JSON 行（生产/Pipe 友好）
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(entry)
}

// Debug / Info / Warn / Error / Fatal 便捷 API（无结构化字段；用 %s/%d 等占位）
func Debug(format string, args ...any) { Log("DEBUG", fmt.Sprintf(format, args...)) }
func Info(format string, args ...any)  { Log("INFO", fmt.Sprintf(format, args...)) }
func Warn(format string, args ...any)  { Log("WARN", fmt.Sprintf(format, args...)) }
func Error(format string, args ...any) { Log("ERROR", fmt.Sprintf(format, args...)) }
func Fatal(format string, args ...any) { Log("ERROR", fmt.Sprintf(format, args...)); os.Exit(1) }

// Debugf / Infof / Warnf / Errorf 带 kv 字段的结构化版本（C-P8 推荐用法）。
// msg 为消息文本，kv 为偶数长度的 key/value 列表；odd 长度最后一个 key 配空字符串。
func Debugf(msg string, kv ...any) { Log("DEBUG", msg, kv...) }
func Infof(msg string, kv ...any)  { Log("INFO", msg, kv...) }
func Warnf(msg string, kv ...any)  { Log("WARN", msg, kv...) }
func Errorf(msg string, kv ...any) { Log("ERROR", msg, kv...) }

// --- helpers ---

func shouldLog(currentLevel, msgLevel string) bool {
	order := map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}
	return order[msgLevel] >= order[currentLevel]
}

func colorFor(level string) string {
	switch level {
	case "DEBUG":
		return "\033[36m"
	case "INFO":
		return "\033[32m"
	case "WARN":
		return "\033[33m"
	case "ERROR":
		return "\033[31m"
	default:
		return "\033[0m"
	}
}

// isTerminalFn 真实检查 stdout 是否 TTY。
// 默认实现：linux/darwin 都返回 false（生产环境默认 JSON）。
// 调试时可由调用方覆盖：`logger.IsTerminal = func(io.Writer) bool { return true }`。
var isTerminalFn = func(w io.Writer) bool { return false }
