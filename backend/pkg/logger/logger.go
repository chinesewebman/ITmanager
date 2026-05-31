package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"network-monitor-platform/internal/config"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

var (
	debugLogger *Logger
	infoLogger  *Logger
	warnLogger  *Logger
	errorLogger *Logger
)

type Logger struct {
	name  string
	level string
}

func Init(cfg *config.LogConfig) {
	// 创建日志目录
	if cfg.Output == "file" {
		dir := filepath.Dir(cfg.File.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("创建日志目录失败: %v\n", err)
		}
	}

	// 设置日志级别
	debugLogger = &Logger{name: "DEBUG", level: cfg.Level}
	infoLogger = &Logger{name: "INFO", level: cfg.Level}
	warnLogger = &Logger{name: "WARN", level: cfg.Level}
	errorLogger = &Logger{name: "ERROR", level: cfg.Level}
}

func getWriter(cfg *config.LogConfig) io.Writer {
	if cfg.Output == "file" {
		return &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSize,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     30,
			Compress:   true,
		}
	}
	return os.Stdout
}

func (l *Logger) log(level, format string, args ...interface{}) {
	// 级别过滤
	levelMap := map[string]int{
		"DEBUG": 0,
		"INFO":  1,
		"WARN":  2,
		"ERROR": 3,
	}

	currentLevel := levelMap[l.level]
	msgLevel := levelMap[level]

	if msgLevel < currentLevel {
		return
	}

	// 格式化输出
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	// 彩色输出 (终端)
	if isTerminal() {
		color := getColor(level)
		fmt.Printf("%s[%s]%s %s\n", color, timestamp, "\033[0m", message)
	} else {
		// JSON 格式
		logEntry := map[string]interface{}{
			"timestamp": timestamp,
			"level":     level,
			"message":   message,
		}
		jsonBytes, _ := yaml.Marshal(logEntry)
		fmt.Println(string(jsonBytes))
	}
}

func Debug(format string, args ...interface{}) {
	debugLogger.log("DEBUG", format, args...)
}

func Info(format string, args ...interface{}) {
	infoLogger.log("INFO", format, args...)
}

func Warn(format string, args ...interface{}) {
	warnLogger.log("WARN", format, args...)
}

func Error(format string, args ...interface{}) {
	errorLogger.log("ERROR", format, args...)
}

// Fatal 记录错误并退出
func Fatal(format string, args ...interface{}) {
	errorLogger.log("ERROR", format, args...)
	os.Exit(1)
}

func isTerminal() bool {
	return false // 生产环境默认关闭彩色输出
}

func getColor(level string) string {
	switch level {
	case "DEBUG":
		return "\033[36m" // 青色
	case "INFO":
		return "\033[32m" // 绿色
	case "WARN":
		return "\033[33m" // 黄色
	case "ERROR":
		return "\033[31m" // 红色
	default:
		return "\033[0m"
	}
}
