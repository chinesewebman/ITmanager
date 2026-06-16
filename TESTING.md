# 测试现状报告

**最后更新**: 2026-06-16
**HEAD**: `bcb406d`
**状态**: ✅ 401 backend + 53 frontend = **454 tests 全过**，无 race warning

---

## 📊 总览

| 维度 | 数值 | 备注 |
|---|---|---|
| Backend 测试数 | **401** | 20 packages, `-race` 验证无 race |
| Frontend 测试数 | **53** | 8 files, vitest 1.6.1 |
| Backend 覆盖率 | **61.2%** | 超过 `开发计划.md` Phase 6 目标 (60%) |
| Frontend 覆盖率 | 未测 | vitest 配 `--coverage` 未启用 |
| 平均测试密度 | ~6.5 tests/pkg | service/apikey 100% 覆盖 |
| Race-free | ✅ | 102/102 race tests pass |

---

## 🏆 Backend 覆盖率 (按 package 排序)

| Package | Coverage | Tests | 备注 |
|---|---|---:|---|
| `internal/apikey` | **100.0%** | 17 | hash + pepper + prefix |
| `internal/config` | **94.3%** | 30 | Viper + validate |
| `internal/metrics` | **90.2%** | 9 | prometheus + handler |
| `internal/apierr` | **88.5%** | 19 | Conflict/TooManyItems/All 错误码 |
| `internal/httpx` | **87.8%** | 4 | 熔断器 + 重试 + context |
| `internal/api` | **85.5%** | 49+28+21 | handler + middleware chain + routes |
| `internal/cmd/seed` | **73.7%** | 12 | seedData 全链路 |
| `internal/migrate` | **72.9%** | 7+10 | migrate runner + cmd/migrate |
| `internal/cmd/admin-bootstrap` | **66.1%** | 17 | env 校验 + runWithDeps |
| `internal/pkg/logger` | **58.9%** | 7 | zap + structured |
| `internal/cmd/migrate` | **54.3%** | 10 | runWithDeps + migrateReset |
| `internal/service` | **43.4%** | 28+5+17=~50 | mock-based, 多 service 补测 |
| `internal/integration` | **39.5%** | 13 | E2E with mock server |
| `internal/middleware` | **36.5%** | 10 | CORS + auth + metrics |
| `internal/models` | **36.1%** | 13 | User + Ticket BeforeCreate hook |
| **TOTAL** | **61.2%** | **401** | - |

---

## 🖥️ Frontend 覆盖率

| File | Tests | 覆盖 |
|---|---:|---|
| `pages/Login.test.tsx` | 4 | ✅ |
| `pages/Dashboard.test.tsx` | 2 | ✅ |
| `pages/Assets.test.tsx` | 2 | ✅ |
| `pages/Racks.test.tsx` | 2 | ✅ |
| `pages/Alerts.test.tsx` | 2 | ✅ |
| `pages/Tickets.test.tsx` | 2 | ✅ |
| `hooks/useApiQuery.test.ts` | 8 | ✅ |
| `stores/index.test.ts` | 31 | ✅ (8 zustand store) |
| **TOTAL** | **53** | 8 files |

---

## 🐛 已修复的 Bug（按 commit）

### `97a5c46` — handlers 8 bug
1. **FailedLogin race** (auth_handler) — `gorm.Expr("failed_login+1")` 原子 + re-fetch
2. **弱密码** — 加 8 字符+字母数字校验
3. **改回旧密码** — `CompareHashAndPassword(new)` 拦截
4. **rate_limit 越界** — `[1, 100000]` 校验
5. **IP whitelist 越界** — `net.ParseIP/CIDR` 校验
6. **uuid parse 静默** — 返 401 不吞
7. **type=garbage 接受** — switch 严格
8. **Name 重名** — UNIQUE index + 409 Conflict

### `6c047d8` — httpx 2 bug
9. **half-open race** — atomic CAS 移入 Mutex
10. **ctx 取消触发熔断** — 4xx/cancel 不记 failure

### `5830d8d` — service 17 bug
11. #13 `AlertFilter.Severity` string → int
12. #14 `Limit=0` 默认 100
13. #15 #24 #25 `Update/UpdateRule` 重复 First/Get
14. #17 `BulkDelete/Ack/Resolve` 1000 上限 + `ErrTooManyItems`
15. #18 `idToIndex` 死代码
16. #19 `usedMap` 键匹配
17. #26 `User.List()` 加分页
18. #27 #29 Dashboard 单条 SQL 聚合 + Machines/Networks 区分

### `bcb406d` — cmd + 前端 + 中间件 + hooks
19. **admin role 检测** — `Scan` → `Row().Scan` (cmd/admin-bootstrap)
20. **testdata schema 漂移** — sites/racks 字段名跟 models 对齐
21. **asset_networks.ipv_address** → `ipv6_address`

---

## 🚀 跑测试

```bash
# 后端
cd backend
go test ./... -count=1                    # 401 tests
go test -race ./... -count=1              # race 验证
go test -cover ./... -coverprofile=cov.out  # 覆盖率
go tool cover -html=cov.out -o cov.html   # HTML 报告

# 前端
cd frontend
./node_modules/.bin/vitest run            # 53 tests
./node_modules/.bin/vitest run --coverage # 覆盖率（待配置）

# 预提交（已配 pre-commit hook）
git commit -m "..."                       # 自动跑 gofmt + swagger validate + .bak 拦截
```

---

## 📋 CI 现状

`.github/workflows/ci.yml` 当前只跑：
- backend: `go vet` + `go test` + `go build`
- 不跑 `-race` / `frontend` / 覆盖率

**待办**:
- 加 `go test -race` 步骤
- 加 frontend vitest 步骤
- 加 coverage badge

---

## 🔜 下一步方向

1. **migrate to pg_test**: integration e2e 用真 PG test 替 sqlite (提升真实性)
2. **frontend coverage 启用**: vitest 配 `@vitest/coverage-v8`
3. **service 43% → 70%**: 增 alert/rack/ticket handler-level coverage
4. **CI 加速**: cache go modules + parallel jobs
5. **mutation testing**: go-mutants 验证测试质量
