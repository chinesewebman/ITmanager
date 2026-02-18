# Network Monitor Platform

网络运维监控平台 - 集成 NetBox + Zabbix + GLPI

## 技术栈

- **后端**: Golang + Gin + GORM
- **前端**: React 18 + TypeScript + Ant Design
- **数据库**: PostgreSQL
- **容器**: Docker Compose

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

## 模块

- 资产管理 (NetBox 集成)
- 监控告警 (Zabbix 集成)
- 运维工单 (GLPI 集成)
- 机柜可视化

## API 文档

见 `/docs/api`
