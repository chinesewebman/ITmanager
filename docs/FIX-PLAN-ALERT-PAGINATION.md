# FIX-PLAN: 告警页服务端分页（M3/P5 方案 B）

**来源**：§8 台账已知阻塞「M3/P5 告警页分页」三方案待拍板 → 拍板 **方案 B**（后端 offset 分页）。
**口径**：与资产页（rev47）、工单页（rev48）统一，告警页是最后一个没跟上的列表。

## What

告警页从「前端假分页」改为「服务端 offset 分页」：

- 现状：`alertApi.list()` 不带 `page`，后端默认 `limit=100` 拉最新 100 条，`total` 被丢弃，antd `pagination.showTotal` 显示的是**当前页条数**（假数字），>100 条**静默截断**。
- 目标：前端传 `page/page_size`，后端返回「过滤后 total」，`AlertTable` 用受控分页，三页（资产/工单/告警）口径一致。

## 核心语义决策（§8「全表 stats.total vs 过滤后 total」的解法）

告警页有两处「总数」，语义不同，必须分开：

| 键 | 语义 | 现态 | 去向 |
|---|---|---|---|
| `data.stats.total` | **全表**告警总数（统计卡「总告警」） | `statsInternal` 无 WHERE（`alert_service.go:154-174`） | 不变，`AlertStatsCards` 用 |
| `data.total`（新增） | **过滤后** Count（应用 status/severity/host_id/is_false_positive） | 无 | 新增，`AlertTable` 分页器「共 X 条」用 |

规则：统计卡永远显示全库汇总（不随筛选变）；分页器 total 随筛选变。靠「返回位置 + JSON 键」区分，不做名字魔法。

## Where（涉及文件）

后端：
- `backend/internal/service/alert_service.go` — `AlertFilter` 加 `Page/PageSize`；`List` 三元→四元 `(items, stats, total, err)`，加 offset 分支。
- `backend/internal/api/handlers/alert_handler.go` — `ListAlerts` 解析 `page/page_size`，响应加 `total`。
- `backend/internal/grpcserver/alert_server.go` — 适配四元返回（`alerts, _, _, err`），cursor 逻辑不动。

前端：
- `backend/internal/api/openapi.yaml` — `/alerts` GET 加 `page/page_size`（照 `/tickets:675-684`）。
- `frontend/src/services/api.types.ts` — `gen:api` 重新生成。
- `frontend/src/pages/Alerts.tsx` — 加 `page/pageSize` state + fetcher 传参 + 解包 `total`。
- `frontend/src/components/AlertTable.tsx` — 加受控分页 `total/page/pageSize/onPageChange`。

## 拆步（3 小步，各自可独立验证）

1. **后端**：`AlertFilter` + `List` 四元 + offset 分支 + `handler` + gRPC 适配 + 单测 + 变异反证。验证 `go build ./...` + `go test ./...`。
2. **契约**：`openapi.yaml` 加参数 + `gen:api` + 前端类型。验证 `npx tsc --noEmit`。
3. **前端**：`Alerts.tsx` + `AlertTable` 受控分页 + vitest + 变异反证。验证 `tsc` + `eslint` + `vitest`。

## Risk

1. **签名改动波及面**：`List` 三元→四元，所有调用方都要改。
   - 波及清单：`alert_handler.go`（HTTP）、`alert_server.go`（gRPC）、`alert_service_test.go`（mock）、`alert_handler_test.go`（`mockAlertService`）、`alert_server_test.go`（mock）。
   - 失败模式：漏改某调用方 → 编译失败或测试红。
   - 缓解：小步 1 内 `go build ./...` + `go test ./...` 全量兜底。
2. **stats vs total 语义混淆**：两个「total」同名不同义，未来维护者把分页器绑到 `stats.Total`。
   - 失败模式：筛选后分页器显示全库数、页码错位。
   - 缓解：测试断言「筛选 status=problem 后 `total < stats.total`」钉住语义；handler JSON 键分开（`stats.total` 全表 vs `data.total` 过滤后）。
3. **offset 深分页性能**：大 offset 时 `OFFSET` 慢。
   - 失败模式：告警几十万条时翻末页慢。
   - 缓解：告警活跃量级 <1 万，offset 可接受；资产/工单页已用同一方案（一致性优先）。未来量大再演进 cursor（前端此时已无假分页，改 cursor 是独立增量）。

## How（before/after 示意）

### 小步 1 后端

`AlertFilter`（`alert_service.go:19-32`）：
```go
type AlertFilter struct {
    Status          string
    Severity        int
    HostID          string
    IsFalsePositive *bool
    Limit           int  // 上限语义（dashboard 最近告警 limit=5 用，无分页时）
    CursorTS        time.Time
    CursorID        uuid.UUID
    Page            int  // 新增：offset 分页（>0 时走 offset 路径）
    PageSize        int  // 新增：每页条数，默认 20 上限 500（对齐 asset/ticket）
    IncludeStats    bool
}
```

`List`（`alert_service.go:104-152`）三元→四元，加 offset 分支：
```go
func (s *alertService) List(ctx, f AlertFilter) ([]models.Alert, AlertStats, int64, error) {
    q := ...过滤 status/severity/host_id/is_false_positive...  // 不变
    q = q.Order("created_at DESC, id DESC")
    var items []models.Alert
    var total int64
    if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
        // cursor 路径（gRPC）：不跑 Count，total 留 0
        q.Where("(created_at, id) < (?, ?)", ...).Limit(normLimit(f.Limit)).Find(&items)
    } else if f.Page > 0 {
        // offset 路径（列表页）：过滤后 Count + Offset/Limit
        pageSize := normPageSize(f.PageSize)   // 默认 20 上限 500
        q.Count(&total)
        q.Offset((f.Page-1)*pageSize).Limit(pageSize).Find(&items)
    } else {
        // limit 上限路径（dashboard 最近告警）：现有行为
        q.Limit(normLimit(f.Limit)).Find(&items)
    }
    // 注：normLimit/normPageSize 仅为示意图 —— 实现时**内联**归一化，匹配
    // alert_service.go:120-131 现有内联风格，不引入新 helper（极简约定）。
    // limit 归一化（现有 120-131 行）与 pageSize 归一化（默认 20 上限 500，对齐
    // asset_service.go:79-85 / ticket_service.go:50-60）各自内联。
    if !f.IncludeStats { return items, AlertStats{}, total, nil }
    stats, err := s.statsInternal(ctx)
    return items, stats, total, nil
}
```

`alert_handler.ListAlerts`：解析 `page/page_size`，响应加 `total`：
```json
{ "items": [...], "stats": {全表}, "total": <过滤后> }
```

### 小步 2 契约

`openapi.yaml:328-355` 加（照 `/tickets:675-684`）：
```yaml
- name: page
  in: query
  schema: { type: integer, default: 1 }
- name: page_size
  in: query
  schema: { type: integer, default: 20 }
```
`AlertList` schema 加 `total: integer`。跑 `gen:api`。

### 小步 3 前端

`Alerts.tsx`：加 `page/pageSize` state，fetcher 传 `page/page_size/status/severity`，解包 `items/stats/total`，筛选变化重置 `page=1`；`AlertTable` 加受控分页。照 `Assets.tsx`（rev47）/`Tickets.tsx`（rev48）。
