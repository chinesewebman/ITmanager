# Network Monitor Platform

[![Release](https://img.shields.io/github/v/release/chinesewebman/ITmanager)](https://github.com/chinesewebman/ITmanager/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/chinesewebman/ITmanager/ci.yml?branch=main)](https://github.com/chinesewebman/ITmanager/actions)
[![License](https://img.shields.io/github/license/chinesewebman/ITmanager)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](backend/go.mod)
[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)](frontend/package.json)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](docker-compose.yml)

网络运维监控平台 - 集成 NetBox + Zabbix + GLPI

---

> **当前状态 (2026-09-09)**: 真实基线 `git describe` = **v2.1.2-12-gdad2b6f**，最后提交 **2026-07-01**。
>
> - **最新 tag = v2.1.2**（2026-06-27）；**v2.2 / v2.3 系列（B/C 系列）工作已在 v2.1.2 之后合入**，尚未打 tag —— 见 [CHANGELOG.md](CHANGELOG.md) 「未发布」节
> - **下一步：v3 架构优化** —— 收敛为「人的操作台」，见 [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md)
> - 详细变更见 [CHANGELOG.md](CHANGELOG.md)，开发任务见 [TODO.md](TODO.md) / [tasks.md](tasks.md)
>
> **历史状态 (2026-06-17)**: v1.4.0 已发布 — 38 commits, 8 tags. service 覆盖率 64.3%→84.1%, backend 779 / frontend 147 / tsc 0.
>
> - **v1.4.0**: rate limit + 通知 worker + LRU 缓存 + 审计日志 + database 测试 + service 覆盖率
> - **v2.0 规划**: [ADR-0002](docs/adr/0002-v2-scope.md) — cursor 分页 + event bus + gRPC (估 10-15h 实测)
> - v2.0 路线图见 [13-实施规划.md §21](13-实施规划.md)

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

> **AI 能力（v3 起）**: 本项目**不再自建 AI 问答 / 知识库**，改为**对接 HolmesGPT** —— ITmanager 作为其数据源（暴露诊断时间线、Runbook 推荐等 toolset 端点），AI 分析层对本项目**一律只读**。定位与分工见 [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md) §2 / R1。

## API 文档

启动后端后访问: **http://localhost:8080/swagger/index.html**

- OpenAPI 源: `backend/internal/api/openapi.yaml` (26.3K, 手写, swagger-cli validate 通过)
- Type-safe 客户端: `frontend/src/services/apiClient.ts` (基于 openapi-typescript, 3/13 endpoint 已 typed)
- 详细 API 列表见 `backend/internal/api/swagger.go`

## 相关文档

- [docs/v3-架构优化需求.md](docs/v3-架构优化需求.md) — v3 架构优化需求（R1–R6，当前改造依据）
- [CHANGELOG.md](CHANGELOG.md) / [TODO.md](TODO.md) / [TESTING.md](TESTING.md)
