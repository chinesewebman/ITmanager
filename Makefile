# Network Monitor Platform - Makefile

.PHONY: help install build run test lint format clean docker-build docker-up docker-down dev dev-frontend dev-backend deploy deploy-min deploy-status

# 默认目标
help:
	@echo "Network Monitor Platform - Makefile"
	@echo ""
	@echo "=== 通用命令 ==="
	@echo "  make install          - 安装所有依赖"
	@echo "  make build            - 构建前后端"
	@echo "  make build-backend    - 构建后端"
	@echo "  make build-frontend  - 构建前端"
	@echo "  make clean            - 清理构建产物"
	@echo ""
	@echo "=== 开发命令 ==="
	@echo "  make dev              - 前后端同时开发"
	@echo "  make dev-backend     - 后端开发模式"
	@echo "  make dev-frontend    - 前端开发模式"
	@echo ""
	@echo "=== 数据库命令 ==="
	@echo "  make db-seed          - 初始化数据库数据"
	@echo "  make db-migrate       - 运行数据库迁移"
	@echo "  make db-reset         - 重置数据库（慎用）"
	@echo ""
	@echo "=== 测试命令 ==="
	@echo "  make test            - 运行后端测试"
	@echo "  make test-frontend   - 运行前端测试"
	@echo "  make test-coverage   - 运行测试并生成覆盖率报告"
	@echo ""
	@echo "=== Docker 命令 ==="
	@echo "  make docker-build    - 构建 Docker 镜像"
	@echo "  make docker-up       - 启动所有服务"
	@echo "  make docker-down     - 停止所有服务"
	@echo "  make docker-logs    - 查看日志"
	@echo ""
	@echo "=== 代码质量 ==="
	@echo "  make lint            - 代码检查"
	@echo "  make format          - 代码格式化"
	@echo ""
	@echo "=== 服务管理 ==="
	@echo "  make server          - 启动后端服务器"
	@echo "  make migrate-create  - 创建新的数据库迁移"
	@echo ""
	@echo "=== 一键部署 (v1.0) ==="
	@echo "  make deploy          - install + docker-up + db-migrate + db-seed (含演示数据)"
	@echo "  make deploy-min      - install + docker-up + db-migrate (无种子数据)"
	@echo "  make deploy-status   - 检查所有服务健康状态"

# 安装依赖
install:
	@echo "=== 安装依赖 ==="
	@echo "安装后端依赖..."
	cd backend && go mod download
	@echo "安装前端依赖..."
	cd frontend && npm install

# 构建
build: build-backend build-frontend

build-backend:
	@echo "=== 构建后端 ==="
	cd backend && go build -o bin/server ./cmd/server

build-frontend:
	@echo "=== 构建前端 ==="
	cd frontend && npm run build

# 开发模式
dev-backend:
	@echo "=== 后端开发模式 ==="
	cd backend && go run ./cmd/server/main.go

dev-frontend:
	@echo "=== 前端开发模式 ==="
	cd frontend && npm run dev

dev: dev-backend dev-frontend

# 服务器
server:
	@echo "=== 启动后端服务器 ==="
	cd backend && go run ./cmd/server/main.go

# 数据库
db-seed:
	@echo "=== 初始化数据库数据 ==="
	cd backend && go run ./cmd/seed/main.go

db-migrate:
	@echo "=== 运行数据库迁移 ==="
	cd backend && go run ./cmd/migrate/main.go

db-reset:
	@echo "⚠️  警告: 将重置数据库，所有数据将丢失！"
	@read -p "确认执行? (yes/no): " confirm; \
	if [ "$$confirm" = "yes" ]; then \
		cd backend && go run ./cmd/seed/main.go --reset; \
	fi

# 测试
test:
	@echo "=== 运行后端测试 ==="
	cd backend && go test -v ./...

test-frontend:
	@echo "=== 运行前端测试 ==="
	cd frontend && npm run test

test-coverage:
	@echo "=== 运行测试并生成覆盖率报告 ==="
	cd backend && go test -coverprofile=coverage.out ./...
	cd backend && go tool cover -html=coverage.out -o coverage.html

# 代码质量
lint:
	@echo "=== 代码检查 ==="
	cd backend && golangci-lint run || go vet ./...
	cd frontend && npm run lint || true

format:
	@echo "=== 代码格式化 ==="
	cd backend && go fmt ./...
	cd frontend && npm run format || true

# 清理
clean:
	@echo "=== 清理构建产物 ==="
	rm -rf backend/bin/
	rm -rf frontend/dist/
	rm -f backend/coverage.out backend/coverage.html
	cd frontend && rm -rf node_modules/

# Docker
docker-build:
	docker-compose build

docker-up:
	docker-compose up -d
	@echo ""
	@echo "=== 服务已启动 ==="
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://localhost:8080"
	@echo "NetBox:   http://localhost:8000"
	@echo "Zabbix:   http://localhost:8081"
	@echo "GLPI:     http://localhost:8001"
	@echo "Graylog:  http://localhost:9000"

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f

# 迁移
migrate-create:
	@echo "=== 创建数据库迁移 ==="
	@read -p "输入迁移名称: " name; \
	cd backend && go run ./cmd/migrate/main.go create $$name

# ==================== 一键部署 (v1.0) ====================

# deploy: 全自动部署链
#   1) install      - 装 go mod + npm 依赖
#   2) docker-up    - 启 8 服务 (PG/Redis/api/web/NetBox/Zabbix/GLPI/Graylog+ES+Mongo)
#   3) db-migrate   - 等 PG ready 后跑 schema 迁移
#   4) db-seed      - 种子数据 (演示账号/资产/告警)
#
# 预期耗时:
#   - 首次:  5-10min (拉镜像 + build)
#   - 后续:  1-2min  (缓存命中)
#
# 失败处理: 任意一步失败立即停下，make 不吞错
deploy: install _wait_for_pg docker-up db-migrate db-seed
	@echo ""
	@echo "✅ 部署完成"
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://localhost:8080"
	@echo "Swagger:  http://localhost:8080/swagger/index.html"
	@echo "NetBox:   http://localhost:8000  (admin / admin)"
	@echo "Zabbix:   http://localhost:8081  (Admin / zabbix)"
	@echo "GLPI:     http://localhost:8001  (glpi / glpi)"
	@echo "Graylog:  http://localhost:9000  (admin / admin)"
	@echo ""
	@echo "默认账号: admin / admin123"

# deploy-min: 无种子的部署（生产环境首次部署用）
deploy-min: install _wait_for_pg docker-up db-migrate
	@echo ""
	@echo "✅ 部署完成（无种子数据）"
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://localhost:8080"
	@echo "Swagger:  http://localhost:8080/swagger/index.html"

# _wait_for_pg: 等 PG 健康检查通过（最多 60s）
#   私有 target（前置下划线）— 不暴露给用户
_wait_for_pg:
	@echo "=== 等待 PostgreSQL 就绪 (max 60s) ==="
	@for i in $$(seq 1 30); do \
		if docker exec nmp-postgres pg_isready -U nmp >/dev/null 2>&1; then \
			echo "✅ PG ready ($${i}*2s)"; \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "❌ PG 在 60s 内未就绪"; \
	exit 1

# deploy-status: 检查所有服务健康状态
deploy-status:
	@echo "=== 服务健康检查 ==="
	@echo ""
	@printf "%-15s %-10s %s\n" "Service" "Status" "Endpoint"
	@printf "%-15s %-10s %s\n" "-------" "------" "--------"
	@printf "%-15s %-10s %s\n" "PostgreSQL" "$$(docker exec nmp-postgres pg_isready -U nmp >/dev/null 2>&1 && echo '✅ UP' || echo '❌ DOWN')" "5432"
	@printf "%-15s %-10s %s\n" "Redis" "$$(docker exec nmp-redis redis-cli ping >/dev/null 2>&1 && echo '✅ UP' || echo '❌ DOWN')" "6379"
	@printf "%-15s %-10s %s\n" "API" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/health | grep -q 200 && echo '✅ UP' || echo '❌ DOWN')" "8080"
	@printf "%-15s %-10s %s\n" "Web" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000 | grep -qE '200|301|302' && echo '✅ UP' || echo '❌ DOWN')" "3000"
	@printf "%-15s %-10s %s\n" "NetBox" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8000 | grep -qE '200|302' && echo '✅ UP' || echo '❌ DOWN')" "8000"
	@printf "%-15s %-10s %s\n" "Zabbix" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8081 | grep -qE '200|302' && echo '✅ UP' || echo '❌ DOWN')" "8081"
	@printf "%-15s %-10s %s\n" "GLPI" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8001 | grep -qE '200|302' && echo '✅ UP' || echo '❌ DOWN')" "8001"
	@printf "%-15s %-10s %s\n" "Graylog" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:9000 | grep -qE '200|302' && echo '✅ UP' || echo '❌ DOWN')" "9000"