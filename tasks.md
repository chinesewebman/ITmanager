# 开发任务清单

> 最近更新: 2026-06-16 — 状态同步 + 新增 2026-06-16 增量项
> 详细开发计划见 [开发计划.md](开发计划.md)，测试现状见 [TESTING.md](TESTING.md)

## Phase 0: 环境准备 (Week 0)

- [x] P0.1 创建 Git 仓库，初始化项目结构
- [x] P0.2 配置开发环境 (Go 1.21+, Node 18+, Docker)
- [x] P0.3 编写 `Makefile` 构建脚本
- [x] P0.4 配置 Git hooks (pre-commit) — **2026-06-16 升级**：加 gofmt + swagger-cli validate + .bak 拦截
- [x] P0.5 创建 `docker-compose.yml` 基础配置

## Phase 1: 基础设施 (Week 1)

### Day 1-2: 后端脚手架
- [x] P1.1 初始化 Go module
- [x] P1.2 配置日志库 (zap + logrus)
- [x] P1.3 配置 Viper 配置管理
- [x] P1.4 创建 HTTP 服务器 (Gin)
- [x] P1.5 配置数据库连接 (GORM + PostgreSQL)
- [x] P1.6 创建数据库迁移脚本

### Day 3-4: 前端脚手架
- [x] P1.7 初始化 React + TypeScript 项目
- [x] P1.8 配置 Ant Design 5 UI库
- [x] P1.9 配置路由 (React Router)
- [x] P1.10 配置状态管理 (Zustand)
- [x] P1.11 配置 API 客户端 (Axios)
- [x] P1.12 创建基础布局组件

### Day 5: API 设计 + 模型
- [x] P1.13 设计 RESTful API 规范 (OpenAPI 3.0) — **2026-06-16 升级**：`/swagger/index.html` + swagger-cli CI
- [x] P1.14 创建基础数据模型 (User, Role)

## Phase 2: 核心集成 (Week 2)

### Day 1-2: NetBox 集成
- [x] P2.1 创建 NetBox API 客户端
- [x] P2.2 实现设备列表同步
- [x] P2.3 实现机柜列表同步
- [x] P2.4 实现 IP 地址同步
- [x] P2.5 封装 NetBox 服务层

### Day 3-4: Zabbix 集成
- [x] P2.6 创建 Zabbix API 客户端
- [x] P2.7 实现主机列表获取
- [x] P2.8 实现告警列表获取
- [x] P2.9 实现监控指标获取
- [x] P2.10 封装 Zabbix 服务层

### Day 5: 统一 API 网关
- [x] P2.11 创建统一的数据聚合服务
- [x] P2.12 实现资产+状态联合查询

## Phase 3: 业务开发 (Week 3)

### Day 1-2: 资产管理页面
- [x] P3.1 创建资产列表页面
- [x] P3.2 实现资产列表 API
- [x] P3.3 实现资产详情页面
- [x] P3.4 实现资产筛选/搜索功能
- [x] P3.5 实现资产导出功能

### Day 3-4: 机柜可视化
- [x] P3.6 创建机柜列表页面
- [x] P3.7 实现机柜设备可视化
- [x] P3.8 实现设备状态指示灯
- [x] P3.9 实现悬浮显示指标
- [x] P3.10 实现点击跳转详情

### Day 5: 监控仪表盘
- [x] P3.11 创建监控仪表盘页面
- [x] P3.12 实现概览统计卡片
- [x] P3.13 实现告警趋势图

## Phase 4: 工单集成 + 前端完善 (Week 4)

### Day 1-2: GLPI 集成
- [x] P4.1 创建 GLPI API 客户端
- [x] P4.2 实现工单列表获取
- [x] P4.3 实现工单详情获取
- [x] P4.4 实现告警自动创建工单

### Day 3-4: 工单页面
- [x] P4.5 创建工单列表页面
- [x] P4.6 实现工单详情页面
- [x] P4.7 实现工单创建页面

### Day 5: 用户权限页面
- [x] P4.8 创建登录页面
- [x] P4.9 ~~实现 LDAP 认证~~ (改为 JWT 认证)
- [x] P4.10 创建用户管理页面

## Phase 5: 告警通知 (Week 5)

### Day 1-2: 告警管理
- [x] P5.1 创建告警列表页面
- [x] P5.2 实现告警详情页面
- [x] P5.3 实现告警确认功能
- [x] P5.4 实现告警解决功能

### Day 3-4: 通知配置
- [x] P5.5 创建通知渠道配置页面
- [x] P5.6 实现钉钉 webhook 配置
- [x] P5.7 实现邮件通知配置

### Day 5: 告警规则
- [x] P5.8 对接 Zabbix 告警规则
- [x] P5.9 创建告警统计页面

## Phase 6: 测试 + 部署 (Week 6)

### Day 1-2: 单元测试 — **2026-06-16 全部完成**
- [x] P6.1 后端单元测试 (覆盖率 > 60%) — **实测 61.2%** ✅
- [x] P6.2 前端组件测试 — **53 tests pass** ✅
- [x] P6.3 API 集成测试 — **21 routes + 50 handler tests** ✅

### Day 3-4: 部署配置
- [ ] P6.4 配置生产环境 docker-compose
- [ ] P6.5 配置 Nginx 反向代理
- [ ] P6.6 配置 HTTPS (Let's Encrypt)
- [ ] P6.7 配置日志收集 (Filebeat)

### Day 5: 部署验证
- [ ] P6.8 生产环境部署
- [ ] P6.9 冒烟测试

## Phase 7: 交付验收 (Week 7-8)

- [ ] P7.1 编写用户手册
- [ ] P7.2 编写运维手册
- [x] P7.3 编写 API 文档 — Swagger UI (`/swagger/index.html`)
- [ ] P7.4 用户培训
- [ ] P7.5 验收测试
- [ ] P7.6 上线切换

---

## 2026-06-16 增量项 (新)

### 代码质量
- [x] 修复 handlers 8 个 bug (rate_limit 越界、IP 越界、uuid 静默、type=garbage、Name UNIQUE、登录锁定 race、弱密码、改回旧密码)
- [x] 修复 httpx 2 个 bug (熔断器 half-open race、ctx 取消误触熔断)
- [x] 修复 service 17 个 bug (Severity int、Limit=0 拆分、Bulk 1000 上限、重复 First、User 分页、Dashboard 单条 SQL 等)
- [x] 修复 cmd + 前端 + 中间件 4 个 bug (admin role 检测、testdata schema 漂移、asset_networks ipv6_address、localStorage 缺失)

### 测试覆盖
- [x] 4 个 0 测试 handler 完整覆盖 (50 new tests)
- [x] routes integration 测试 (21 tests)
- [x] cmd/admin-bootstrap (17 tests) + cmd/seed (12 tests) + cmd/migrate (10 tests)
- [x] 中间件链路测试 (28 tests)
- [x] model BeforeCreate hook 测试 (13 tests)
- [x] 前端 hooks + stores 测试 (8 + 31 tests)
- [x] apierr / apikey / config 完整覆盖 (19+17+30 tests)

### 工具链
- [x] Swagger UI 集成 + swagger-cli CI validate
- [x] type-safe API client (3/13 服务方法已 typed)
- [x] pre-commit hook 升级 (gofmt + swagger + .bak 拦截)
- [x] TESTING.md (4.7K) 现状报告
- [x] 文档状态同步 (TODO.md / tasks.md / 12-优化建议.md / 开发计划.md)

### 待跟进
- [ ] CI 升级：加 `go test -race` + frontend vitest 步骤
- [ ] CI 升级：加 coverage 阈值门禁
- [ ] 覆盖盲区：internal/database 0% / internal/middleware 36.5% / internal/integration 39.5%
- [ ] type-safe 推进：10/13 服务方法仍 `data: any`
- [ ] 前端 coverage 工具未启用
- [ ] 5 service 方法 type-safe 渐进 3/13
