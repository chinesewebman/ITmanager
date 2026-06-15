package network_monitor_platform

import "embed"

// MigrationsFS 把 backend/migrations/ 目录打包进二进制。
// cmd/server 和 cmd/migrate 启动时调用 database.SetMigrationsFS(MigrationsFS) 注入。
//
//go:embed all:migrations
var MigrationsFS embed.FS
