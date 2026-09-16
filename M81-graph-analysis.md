# M81 Graph Analysis — G-41 zabbix_truncated 跨语言常量 + 契约测试

## 双轨状态

### graphify (多仓库多视图诊断)
- latest run: 2026-09-16 12:55
- **0 anomalies** (multigraph 综合: cycle / dead-code / god-module / cross-layer-coupling 4 维度全绿)
- node/edge 增长:
  - M80 (前): 7,143 nodes / 14,690 edges (M80 是 config-only, 无业务图变化)
  - M81 (本轮): 7,143 nodes / 14,690 edges (仍是 constants 提取, 无跨模块新边)

### codegraph (结构化查询)
- latest run: 2026-09-16 12:55
- 6,532 nodes / 16,226 edges (M80 baseline 等同)
- `codegraph query "M81 sync_keys"`:
  - 1 method: `KeyZabbixTruncated` @ `backend/internal/integration/sync_keys.go:28`
  - 3 test functions:
    - `TestKeyZabbixTruncated_Value` @ `backend/internal/integration/sync_keys_test.go:24`
    - `TestKeyZabbixTruncated_InOpenAPISync` @ `backend/internal/integration/sync_keys_test.go:29`
    - `TestKeyZabbixTruncated_NoBareStringInCode` @ `backend/internal/integration/sync_keys_test.go:64`
    - `TestKeyZabbixTruncated_CrossLangWithTS` @ `backend/internal/integration/sync_keys_test.go:127`
  - 1 module: `SYNC_KEY_ZABBIX_TRUNCATED` @ `frontend/src/services/syncKeys.ts:25`
  - 3 test cases:
    - `SYNC_KEY_ZABBIX_TRUNCATED 值等于 'zabbix_truncated'` @ `frontend/src/services/syncKeys.test.ts:18`
    - `Settings.tsx 用 SYNC_KEY_ZABBIX_TRUNCATED 常量, 不写回裸字符串` @ `frontend/src/services/syncKeys.test.ts:21`
    - `与后端 Go 常量 KeyZabbixTruncated 一致 (跨语言漂移守卫)` @ `frontend/src/services/syncKeys.test.ts:56`

## M81 涉及的图变更

### 节点 (4 新 + 1 抽象)
- `integration.KeyZabbixTruncated` (新常量节点, M27/B 时代的 `"zabbix_truncated"` 字面量 → M81 提取为包级常量)
- `services.SYNC_KEY_ZABBIX_TRUNCATED` (新模块常量节点, 前端 Settings.tsx 的 `.zabbix_truncated` access → M81 提取为 import 常量)
- `sync_keys.go` 新文件节点: 含 1 个常量 + 1 段 package doc
- `sync_keys_test.go` 新文件节点: 含 4 个测试函数
- `syncKeys.ts` 新文件节点: 含 1 个常量 + 1 段 module doc
- `syncKeys.test.ts` 新文件节点: 含 3 个测试 case

### 边 (2 改 + 4 新)
- **改** `SyncAll → results[KeyZabbixTruncated] = trunc`: 旧边 `SyncAll → "zabbix_truncated"` 字面量 → 新边 `SyncAll → KeyZabbixTruncated` 常量 (语义同, 引用关系从「裸字面量」变「命名常量」)
- **改** `case "zabbix" → results{..., integration.KeyZabbixTruncated: ...}`: 旧边同上, handler 侧从裸字面量变命名常量
- **新** `Settings.handleSyncZabbix → SYNC_KEY_ZABBIX_TRUNCATED`: import + `[SYNC_KEY_ZABBIX_TRUNCATED]` 计算属性 access
- **新** 4 个契约测试 → 跨语言 drift 守卫边: TestKeyZabbixTruncated_CrossLangWithTS ↔ SyncKeys_CrossLangWithGo (双向)
- **新** 1 个 OpenAPI 真源守卫边: TestKeyZabbixTruncated_InOpenAPISync → openapi.yaml:3776 读
- **新** 1 个 grep guard 边: TestKeyZabbixTruncated_NoBareStringInCode → service.go + integration_handler.go 字面量扫描

### 双轨跨仓边 (M81 引入的新形态)
- `backend/internal/integration/sync_keys_test.go → frontend/src/services/syncKeys.ts`: Go 测试读 TS 文件 regex 提取字面量
- `frontend/src/services/syncKeys.test.ts → backend/internal/integration/sync_keys.go`: TS 测试读 Go 文件 regex 提取字面量
- 这是 M81 引入的**新形态** — 跨语言契约测试此前零先例 (M33 §6 残留 / M36 G-55/D-6 都没做跨语言守卫).
  M82/M83 candidate 沿用即可.

## 跨模块漂移面 (M81 前后对比)

### M81 前
- 后端 `integration/service.go` 的 `results["zabbix_truncated"]` 与 `integration_handler.go` 的
  `{"zabbix", "zabbix_truncated", ...}` 字面量是**两份独立副本**
- 前端 `Settings.tsx` 的 `res?.data?.data?.synced?.zabbix_truncated` 字面量是**第三份独立副本**
- 三份副本, 无任何编译/类型关联, 改名漂移静默失效 (前端 `?? 0` 兜底 → UI 永远显示「未截断」)
- OpenAPI SyncResult.description 手写枚举是「文档侧」第四份副本 — 与代码不同步

### M81 后
- 后端 `integration.KeyZabbixTruncated` 是**单一事实来源** (service.go 与 handler.go 都引用它)
- 前端 `services.SYNC_KEY_ZABBIX_TRUNCATED` 是前端单一事实来源 (Settings.tsx 引用它)
- 后端↔前端 跨语言契约测试双向守 (Go 端读 TS 字面量, TS 端读 Go 字面量, 漂移即双红)
- OpenAPI SyncResult.description 仍是手写副本, 但有 TestKeyZabbixTruncated_InOpenAPISync 守
  (字面量必须出现在 spec 描述段, drift 即红)

漂移面收口: **3 副本 → 2 常量 + 4 契约测试**. 任一侧漂离 CI 即红.

## 与 M27/B / M33 / M36 / M57 的关系

- **M27/B (shipped)**: `zabbix_truncated` 0/1 标志的设计与 ship — 后端 SyncFromZabbix 返回值 +1,
  handler 透出到 `data.synced` 字典. M81 是 M27/B 的「跨语言契约补完」, 不改 0/1 语义.
- **M33 (G-45/G-55)**: `*_field_truncations` 计数键引入, 与 `zabbix_truncated` 是不同语义键
  (字段级处数 vs 源侧 0/1 标志). M81 不动 `*_field_truncations` (沿用 G-57 同族登记不修).
- **M36 (G-55 + D-6)**: audit_logs.resource 三处副本漂移收口, 与 G-41 同族 (「跨多处的同一字面量」).
  M36 走的是「model tag + 常量 + 反射 + 真 PG」4 处对齐, G-41 是「裸字符串 → 常量 + 跨语言测试」.
  M81 走 G-41 的更轻量路径 (因为只是 1 个键名, 不是 1 个列宽).
- **G-57 (TODO.md 登记不修)**: `synced` 是自由 map, 键名是跨语言裸字符串约定, 类型系统兜不住.
  G-57 范围比 G-41 大 (4 个键 + openapi description 收口 + 重生成 api.types.ts), M81 是 G-57 的
  范本 — 后续 round 沿用 M81 模式低成本收 G-57 同族.

## codegraph 推荐追踪的问题 (本轮新增)

无新问题. M81 是「单一事实来源 + 跨语言契约测试」的收口, 没引入新的低内聚或异常跨边.
跨仓读文件 (Go 读 TS / TS 读 Go) 是新形态, 但都是测试代码内部读源文件, 不参与运行时路径,
codegraph 不会把它们当作运行时跨层耦合.

## 与 PM_QUEUE 候选清单对照

M81 = G-41 zabbix_truncated 跨语言常量 (M27/B 派生 TODO), 已 ship. 候选清单剩余:
- **G-39** AlertRule.NotifyChannels 真读 (worker.handleAlertEvent 改走规则选择, ≤2h backend)
- **M83** CI 升级 (.github/workflows 加 -race + vitest)
- **G-4** / **P1-3-MIB** (cross-stack, >4h, PM-direct only)

下次 round 候选: G-39 (≤2h backend-only) 或 G-57 同族收口 (沿用 M81 模式).
