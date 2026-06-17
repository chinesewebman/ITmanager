# Network Monitor Platform

网络运维监控平台 - 集成 NetBox + Zabbix + GLPI

---

> **2026-06-17 状态**: v1.0.1 已发布 — 10 PR 落地 (P0-P2 + S 级 3 + A 级 3) + 一键部署
>
> - **测试**: backend 603 passed / frontend 117 passed / tsc 24 = baseline / 61.2% 覆盖率
> - **一键部署**: `make deploy` (install + docker-up + db-migrate + db-seed)
> - **健康检查**: `make deploy-status` (8 服务: PG/Redis/API/Web/NetBox/Zabbix/GLPI/Graylog)
> - 详细变更见 [CHANGELOG.md](CHANGELOG.md)，开发任务见 [TODO.md](TODO.md) / [tasks.md](tasks.md)

## 快速开始

### 前置要求

- Go 1.21+ / Node.js 18+ / Docker & Docker Compose

### 一键部署（首次推荐）

```bash
git clone <repo> && cd ITmanager
make deploy          # install + docker-up + db-migrate + db-seed
                     # 首次 5-10min (拉镜像)，后续 1-2min (缓存)
```

完成后访问：

- 前端: http://localhost:3000
- 后端 API: http://localhost:8080
- Swagger: http://localhost:8080/swagger/index.html
- 默认账号: `admin / admin123`

```bash
make deploy-status   # 检查 8 服务健康
make docker-logs     # 跟踪日志
```

### 生产部署（无种子数据）

```bash
make deploy-min      # install + docker-up + db-migrate（无 seed）
```

### 本地开发（不跑 NetBox/Zabbix/GLPI/Graylog）

```bash
make install
make dev             # 启 api (8080) + web (3000)，需自带 PG/Redis
```

### 测试

```bash
make test            # 后端 603 tests
make test-frontend   # 前端 117 tests
make test-coverage   # 生成 coverage.html
```

详细测试报告见 [TESTING.md](TESTING.md)。

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
