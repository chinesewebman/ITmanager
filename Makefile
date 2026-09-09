# Network Monitor Platform - Makefile

# 宿主机侧命令（go run ./cmd/migrate|seed）要读 .env 里的 NMP_* secret。
#
# 不用 make 的 `include .env`：make 把值里的 `#` 当注释起点**静默截断**
# （`NMP_DATABASE_PASSWORD=ab#cd` → `ab`），而 compose 自己解析 .env 不截断 ——
# 结果是「容器密码对、宿主迁移密码错」，报错只显示 authentication failed，
# 完全不提截断。shell 的 `. ./.env` 里 `#` 只有位于词首才是注释，值完整保留。
LOAD_ENV = set -a; if [ -f .env ]; then . ./.env; fi; set +a;

.PHONY: help install build run test lint format clean docker-build docker-up docker-up-aux docker-down docker-logs dev dev-frontend dev-backend deploy deploy-min deploy-status

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
	@echo "  make db-seed          - 初始化数据库数据（⚠️ 已知密码的演示账号）"
	@echo "  make db-migrate       - 运行数据库迁移（宿主侧；容器里 api 启动时已自动迁移）"
	@echo "  make db-reset         - 回滚全部迁移 + 重新迁移 + 灌种子（慎用）"
	@echo ""
	@echo "=== 测试命令 ==="
	@echo "  make test            - 运行后端测试"
	@echo "  make test-frontend   - 运行前端测试"
	@echo "  make test-coverage   - 运行测试并生成覆盖率报告"
	@echo ""
	@echo "=== Docker 命令 ==="
	@echo "  make docker-build    - 构建 Docker 镜像"
	@echo "  make docker-up       - 启动主链 4 服务 (postgres/redis/api/web)"
	@echo "  make docker-up-aux   - 追加辅助系统 (netbox/zabbix/glpi/graylog)"
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
	@echo "  make deploy          - install + docker-up + db-seed (⚠️ 含 admin/admin123 演示账号)"
	@echo "  make deploy-min      - install + docker-up (生产路径，无种子数据；迁移由 api 启动时执行)"
	@echo "  make deploy-status   - 检查服务健康状态（主链 4 + aux）"

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
	@echo "=== 初始化演示数据（⚠️ 会创建 admin/admin123 等已知密码账号，生产勿用）==="
	@$(LOAD_ENV) cd backend && go run ./cmd/seed

db-migrate:
	@echo "=== 运行数据库迁移 ==="
	@$(LOAD_ENV) cd backend && go run ./cmd/migrate up

# db-reset: 回滚全部迁移 → 重新迁移 → 灌演示数据（cmd/migrate reset 自己会二次确认）。
# 原先调 `./cmd/seed --reset`，而 seed 根本没有 flag 解析 → 提示「数据将丢失」却什么都没删。
db-reset:
	@echo "⚠️  警告: 重置数据库 schema 并重新灌种子，所有业务数据将丢失！"
	@$(LOAD_ENV) cd backend && go run ./cmd/migrate reset
	@$(LOAD_ENV) cd backend && go run ./cmd/migrate up
	@$(LOAD_ENV) cd backend && go run ./cmd/seed

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
	docker compose build

docker-up:
	docker compose up -d --build --wait --wait-timeout 600
	@echo ""
	@echo "=== 主链已启动（postgres/redis/api/web）==="
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://127.0.0.1:8080  (仅环回)"
	@echo "辅助系统（netbox/zabbix/glpi/graylog）：make docker-up-aux"

docker-up-aux:
	docker compose --profile aux up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# 迁移
# cmd/migrate 只有 up/down/status/reset 四个子命令，没有 create（原先这里调的是
# 不存在的 `create`，必失败）。手工建迁移文件的步骤打印出来，避免误导。
migrate-create:
	@echo "❌ cmd/migrate 没有 create 子命令（现有：up / down / status / reset）"
	@echo "手工创建：backend/migrations/<6位序号>_<name>.up.sql 与同名 .down.sql"
	@echo "序号 = 现有最大序号 +1；写完跑 scripts/db_smoke.sh 验证（迁移不可重入会红）"
	@exit 1

# ==================== 一键部署 (v1.0) ====================

# deploy: 演示环境全自动部署链（⚠️ 含 db-seed，会写入已知密码的演示账号）
#   1) install      - 装 go mod + npm 依赖
#   2) docker-up    - 启主链 4 服务 (postgres/redis/api/web)，`--wait` 已等到 healthcheck 通过
#   3) db-seed      - 演示数据 (admin/admin123 等已知凭据)
#
# 迁移不在这里单独跑：api 启动时 `database.Init` 已无条件执行 `migrate.Up`，
# `--wait` 返回时迁移必然已完成。再跑一遍宿主 db-migrate 是空转，且 api 若此刻
# 因重启持锁，非阻塞的 pg_try_advisory_lock 会让这一步直接失败（审计 F5）。
#
# 生产部署请用 `make deploy-min` + `admin-bootstrap`（见下），不要用本目标。
#
# 预期耗时:
#   - 首次:  5-10min (拉镜像 + build)
#   - 后续:  1-2min  (缓存命中)
#
# 失败处理: 任意一步失败立即停下，make 不吞错
#
# 注意：这里**没有** _wait_for_pg 前置。docker-up 用 `--wait` 等 healthcheck，
# 而 make 按顺序执行前置，原先放在 docker-up 之前的等待目标在干净机器上必然先超时失败。
deploy: install docker-up db-seed
	@echo ""
	@echo "✅ 演示环境部署完成"
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://127.0.0.1:8080  (仅环回)"
	@echo "Swagger:  http://127.0.0.1:8080/swagger/index.html"
	@echo ""
	@echo "演示账号: admin / admin123  ⚠️ 仅演示环境，生产禁用"

# deploy-min: 无种子的部署（生产环境首次部署用）
deploy-min: install docker-up
	@echo ""
	@echo "✅ 部署完成（主链 4 服务，无种子数据）"
	@echo "前端:     http://localhost:3000"
	@echo "后端 API: http://127.0.0.1:8080  (仅环回)"
	@echo "Swagger:  http://127.0.0.1:8080/swagger/index.html"
	@echo ""
	@echo "下一步：创建首个管理员（密码自定，>=12 位，勿用弱口令）"
	@echo "  docker compose exec api env FIRST_ADMIN_USERNAME=admin FIRST_ADMIN_PASSWORD='<强密码>' ./admin-bootstrap"
	@echo "辅助系统（netbox/zabbix/glpi/graylog）：make docker-up-aux"

# deploy-status: 检查服务健康状态
#   主链 4 服务恒检查；aux 服务未启动时显示 ⏸️（而非 ❌，它们默认就在 aux profile 下）
deploy-status:
	@echo "=== 服务健康检查 ==="
	@echo ""
	@printf "%-15s %-12s %s\n" "Service" "Status" "Endpoint"
	@printf "%-15s %-12s %s\n" "-------" "------" "--------"
	@printf "%-15s %-12s %s\n" "PostgreSQL" "$$(docker compose exec -T postgres pg_isready -U nmp >/dev/null 2>&1 && echo '✅ UP' || echo '❌ DOWN')" "127.0.0.1:5432"
	@printf "%-15s %-12s %s\n" "Redis" "$$(docker compose exec -T redis redis-cli ping >/dev/null 2>&1 && echo '✅ UP' || echo '❌ DOWN')" "容器内 6379（未发布）"
	@printf "%-15s %-12s %s\n" "API" "$$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/healthz | grep -q 200 && echo '✅ UP' || echo '❌ DOWN')" "127.0.0.1:8080"
	@printf "%-15s %-12s %s\n" "Web" "$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000 | grep -qE '200|301|302' && echo '✅ UP' || echo '❌ DOWN')" "localhost:3000"
	@for svc in netbox zabbix glpi graylog; do \
		port=$$(case $$svc in netbox) echo 8000;; zabbix) echo 8081;; glpi) echo 8001;; graylog) echo 9000;; esac); \
		if [ -z "$$(docker compose ps -q $$svc 2>/dev/null)" ]; then \
			state="⏸️  未启动"; \
		elif curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:$$port | grep -qE '200|302'; then \
			state="✅ UP"; \
		else \
			state="❌ DOWN"; \
		fi; \
		printf "%-15s %-12s %s\n" "$$svc" "$$state" "127.0.0.1:$$port"; \
	done