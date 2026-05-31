# 网络运维监控平台 - 开发待办

## 当前状态 (2026-02-15)

### 已完成
- [x] 后端项目初始化 (Go + Gin)
- [x] 前端项目初始化 (React + Ant Design)
- [x] 配置文件结构
- [x] 数据库模型定义
- [x] API 路由和基础 handler
- [x] 前端页面组件 (Dashboard, Assets, Alerts, Racks, Tickets, Settings)
- [x] Docker 配置
- [x] 前端开发服务器运行 (localhost:5173)

### 待完成

## 1. 数据库初始化 (优先级: 高)
- [x] 运行 `go run ./cmd/seed/main.go` 创建初始数据
- [x] 重启后端服务 `go run ./cmd/server`

## 2. 完善认证功能 (优先级: 高)
- [x] 实现 JWT Token 生成和验证中间件
- [x] 完善 Login/Logout 接口
- [x] 添加默认管理员用户 (admin/admin123)
- [x] 添加 API Key 认证支持

## 3. 完善后端 API (优先级: 高)
- [x] 资产 CRUD - 完整实现
- [x] 告警确认/解决 - 完善逻辑
- [x] 工单管理 - 本地 CRUD 完成 (GLPI 集成待完成)

## 4. 第三方集成 (优先级: 中)
- [x] NetBox 集成 - 资产同步
- [x] Zabbix 集成 - 告警获取
- [x] GLPI 集成 - 工单同步
- [x] 集成 API 端点

## 5. 前端完善 (优先级: 中)
- [x] 登录页面
- [x] 实时数据对接
- [ ] 机柜可视化增强

## 启动命令

```bash
# 1. 启动 PostgreSQL (如果未运行)
docker run -d --name nmp-postgres \
  -e POSTGRES_USER=nmp \
  -e POSTGRES_PASSWORD=nmp123 \
  -e POSTGRES_DB=network_monitor \
  -p 5432:5432 timescale/timescaledb:latest-pg16

# 2. 初始化数据库数据
cd backend && go run ./cmd/seed/main.go

# 3. 启动后端
cd backend && go run ./cmd/server

# 4. 启动前端 (另一个终端)
cd frontend && npm run dev
```

## 访问地址
- 前端: http://localhost:5173
- 后端 API: http://localhost:8080
- API 文档: http://localhost:8080/swagger/index.html (需要安装 swagger)
