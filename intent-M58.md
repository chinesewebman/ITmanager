# intent-M58: G-Asset-BulkRetireEndpoint backend bulk 端点优化 (M51-3 解决)

## Context

- M51 G-UI-BulkAssets ship 了前端 bulkRetireMut, 用 **N 次串行 POST `/api/assets/:id/retire`**
- 100 资产时 = 100 次 HTTP round-trip → 弱网/远端场景慢
- M51-3 (🟡 P2) 标记了端点优化需求, 留 future round
- 本 round = 给 backend 加 **POST /api/assets/bulk-retire** 单端点, frontend 改调新端点

## 任务

### 1. `backend/internal/service/asset_service.go`

- 加 `BulkRetire` method:
  ```go
  func (s *assetService) BulkRetire(ctx context.Context, ids []string, reason string, userID uuid.UUID) (succeeded []string, failed map[string]string, err error)
  ```
- 内部对每个 ID 调用单条 Retire 逻辑 (复用现有 tx)
- 包在一个 DB tx (失败一个不影响其它)
- 返回成功列表 + 失败详情 (id → error message)

### 2. `backend/internal/api/handlers/asset_handler.go`

- 加 `BulkRetireAssets` handler:
  - Request: `{ ids: ["uuid", ...], reason: "..." }`
  - 单个 audit 行 (action="bulk_retire", resource 含 ID 列表摘要)
  - Response: `{ succeeded: [...], failed: { id: msg } }`

### 3. `backend/internal/api/routes.go`

- 注册 `POST /api/assets/bulk-retire` 必须**先于** `POST /api/assets/:id/retire` (Gin 静态段优先)
- `routes_integration_test.go:335` 有先例注释

### 4. Backend tests

- `asset_handler_test.go`: `BulkRetireAssets` 测试 (全成功 / 部分失败 / 空 ids / 路由顺序)
- `asset_service_test.go`: `BulkRetire` 单元测试

### 5. Frontend: 改 bulkRetireMut

- `frontend/src/services/api.ts`: `assetApi` 加 `bulkRetire(ids, reason)`
- `frontend/src/pages/Assets.tsx`: `bulkRetireMut` 改调新端点
- `Assets.test.tsx`: 更新 bulkRetireMut 测试

## Hard pass

- backend `go test -count=1 ./...`: 15 packages ok
- frontend `npx tsc --noEmit`: 0 error
- frontend Assets 测试: 全 PASS (M51 已有测试不退化)
- mutation inversion 实证
- 双轨 graphify + codegraph, 0 anomalies

## graph-tools verified (M58 起每 round 必跑)

执行时间: 2026-09-14 23:XX CST
- graphify update
- graphify diagnose (0 missing/dangling)
- codegraph sync (stale → 强 re-index)
