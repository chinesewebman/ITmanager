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

- Go 1.25+（见 `backend/go.mod`）/ Node.js 22+ / Docker & Docker Compose

### 首次部署

compose 的 secret 全部走 `${VAR:?}`（缺值直接报错，不再有 `nmp123` 这类硬编码凭据），
**必须先准备 `.env`**：

```bash
git clone <repo> && cd ITmanager
cp .env.example .env && chmod 600 .env
# 三个 secret 各生成一份，不要复用：
#   NMP_DATABASE_PASSWORD / NMP_AUTH_JWT_SECRET / NMP_AUTH_API_KEY_PEPPER
#   sed -i "s/^NMP_DATABASE_PASSWORD=.*/NMP_DATABASE_PASSWORD=$(openssl rand -hex 32)/" .env  （其余同理）
```

生产（无演示数据）：

```bash
make deploy-min      # install + docker-up（主链 4 服务；迁移由 api 启动时自动执行）
                     # 首次 5-10min (拉镜像 + build)，后续 1-2min (缓存)
# 创建首个管理员（密码自定，>=12 位）：
docker compose exec api env FIRST_ADMIN_USERNAME=admin \
  FIRST_ADMIN_PASSWORD='<强密码>' ./admin-bootstrap
```

> ⚠️ **生产上线前必做两件事**（默认值只面向本地开发）：
> 1. `.env` 里设 `NMP_SERVER_MODE=release` —— debug 下登录 cookie 不带 `Secure`、
>    且弱集成凭据（zabbix 默认 Admin/zabbix 等）不会被启动期拒绝。
> 2. 在 web 前面做 TLS 1.2+ 终结（`frontend/nginx-tls.conf.example` 或外部 LB）——
>    `web` 默认 `3000:80` 是**明文 HTTP**，`make deploy-min` 不会替你加密。
>    详见 [08-部署运维.md](08-部署运维.md) §8.2.2 / §8.3。

演示环境（⚠️ 会写入 `admin/admin123` 等已知密码的演示账号，勿用于生产）：

```bash
make deploy          # install + docker-up + db-seed
```

完成后访问：

- 前端: http://localhost:3000
- 后端 API: http://127.0.0.1:8080（仅环回，唯一对外入口是 web）
- Swagger: http://127.0.0.1:8080/swagger/index.html

```bash
make deploy-status   # 检查服务健康（主链 4 + aux 未启动时显示 ⏸️）
make docker-logs     # 跟踪日志
```

> 辅助系统（NetBox / Zabbix / GLPI / Graylog）在 `aux` profile 下，默认不启动：
> `make docker-up-aux`。它们目前是占位凭据、独立库未初始化，生产化见 TODO G-17。

### 本地开发（不跑 NetBox/Zabbix/GLPI/Graylog）

```bash
make install
make dev             # 启 api (8080) + web (3000)，需自带 PG/Redis
```

### 测试

```bash
make test            # 后端全量单测
make test-frontend   # 前端单测 + 类型检查
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
