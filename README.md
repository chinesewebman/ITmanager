# Network Monitor Platform

网络运维监控平台 - 集成 NetBox + Zabbix + GLPI

---

> **2026-06-16 状态**: 全部 Phase 0-5 业务功能完成，Phase 6 测试 ✅ 完成 / 部署 🔴 未开始。详见 [开发计划.md](开发计划.md)。
>
> - **测试**: backend 401 passed / frontend 53 passed / 61.2% 覆盖率
> - **Bug 修复**: 17 个生产 bug 修复（handlers 8 / httpx 2 / service 17）
> - **CI 保障**: pre-commit hook (gofmt + swagger validate + .bak 拦截)
> - 详细测试现状见 [TESTING.md](TESTING.md)

## 技术栈

- **后端**: Golang + Gin + GORM
- **前端**: React 18 + TypeScript + Ant Design + Zustand
- **数据库**: PostgreSQL (生产) / SQLite (测试, in-memory)
- **容器**: Docker Compose
- **API 文档**: OpenAPI 3.0 + Swagger UI (gin-swagger)

## 快速开始

### 前置要求

- Go 1.21+
- Node.js 18+
- Docker & Docker Compose

### 安装

```bash
# 安装依赖
make install

# 运行
make run
```

### 开发

```bash
# 后端开发
make run

# 前端开发
cd frontend && npm run dev
```

### 测试

```bash
# 后端 (含 race)
cd backend && go test -race -count=2 ./...

# 前端
cd frontend && ./node_modules/.bin/vitest run

# 后端覆盖率
cd backend && go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
```

详细测试报告见 [TESTING.md](TESTING.md)，开发任务跟踪见 [TODO.md](TODO.md) / [tasks.md](tasks.md)。

## 模块

- 资产管理 (NetBox 集成)
- 监控告警 (Zabbix 集成)
- 运维工单 (GLPI 集成)
- 机柜可视化
- 用户/权限 (JWT + API Key + RBAC)
- 通知渠道 (钉钉 / 邮件 / Webhook)

## API 文档

启动后端后访问: **http://localhost:8080/swagger/index.html**

- OpenAPI 源: `backend/internal/api/openapi.yaml` (26.3K, 手写, swagger-cli validate 通过)
- Type-safe 客户端: `frontend/src/services/apiClient.ts` (基于 openapi-typescript, 3/13 endpoint 已 typed)
- 详细 API 列表见 `backend/internal/api/swagger.go`
