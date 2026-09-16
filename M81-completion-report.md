# M81 Completion Report — G-41 zabbix_truncated 跨语言裸字符串约定改类型/常量 + 加契约测试

## 摩擦
M27/B ship `zabbix_truncated` 这个 0/1 源侧截断标志时 (`backend/internal/integration/service.go:630` +
`backend/internal/api/handlers/integration_handler.go:88` 各写一遍 `results["zabbix_truncated"] = trunc` 字面量)
就**已经**留下一个跨语言裸字符串约定 — 两侧字面量各自漂移, 改名/拼写错会**静默失效**:
前端 `res?.data?.data?.synced?.zabbix_truncated ?? 0` 读不到 → `?? 0` 兜底 → UI 永远显示「未截断」,
运维错过 6000-1=5999 条丢告警的真相。比 M27/B 修之前更隐蔽 (日志里仍有「超过上限」, 只有 UI 静默).

TODO.md G-41 一直登记为「本轮取 ③: 只登记不改 (改名是低频事件、且 B 的收益不依赖它)」, 属低危挂账.
Poison 触发 watchdog Mode B 选 PM_QUEUE M81-candidate = G-41 作为 cycle 12 round, 实证跨语言常量 + 契约测试
的收口模式 (M82/M83 同族候选的范本).

## 决策 (PM-direct 拍)

**Decision 1 (选路 ①: 双侧常量, 加契约测试)**: TODO.md G-41 列了 3 个修法:
① 把两个 key 提成双侧引用的常量 (Go 侧 const + 前端 const, 仍然各一份);
② 走 openapi/`gen:api` 让类型系统兜住 (要改响应结构, 属独立改动);
③ 只登记不改.
本轮选 ① + 加 2 个跨语言契约测试. 理由: ① 与 ② 都属于「让类型/契约兜住漂移」, 但 ② 要改 openapi 响应结构
(影响 G-57 同型残留), 沿用 M33 §6 残余登记 (本轮不动); ① 是最小改动, 配 2 个跨语言契约测试就能
把「漂移」从「人盯」变成「CI 红」.

**Decision 2 (scope 仅 zabbix_truncated, 不动 glpi_skipped / *_field_truncations 同族)**: brief 明确
zabbix_truncated. 同族 (glpi_skipped / netbox / glpi / *_field_truncations 共 4 个) 仍是裸字符串,
沿用 TODO.md G-57 登记不修 (data.synced 自由形态的 schema 收口范围), 属独立 round scope.

**Decision 3 (契约测试走「真源 + 跨语言 drift 守卫」双线)**:
- 「真源」= `backend/internal/api/openapi.yaml:3771-3787` 的 SyncResult.description 手写枚举 (M78/G-15 已用此模式).
- 「跨语言 drift」= Go 测试读 TS 常量 (regex 提取字面量) + TS 测试读 Go 常量 (regex 提取字面量),
  任一侧漂离 → 双测试红.

**Decision 4 (test 设计要点 — word-boundary regex)**: 第一版用 `assert.Contains(specStr, "zabbix_truncated")`,
但 Contains 是 substring 匹配, `zabbix_truncated_v2` 也算命中 → mutation 3 (改 spec 为 `_v2`) 假绿.
改用 `\bzabbix_truncated\b` word-boundary regex 后真红.

## 关键设计变化

### backend (Go)
1. **新文件 `backend/internal/integration/sync_keys.go`** (~30 行):
   ```go
   const KeyZabbixTruncated = "zabbix_truncated"
   ```
   含 docstring 详注: 语义 (M27/D-6 + M33/D-6) / 失败分支不写 / 与 TS 常量的契约 / 契约测试位置.
2. **`backend/internal/integration/service.go:627-633`**: SyncAll 的 Zabbix 成功分支用 `results[KeyZabbixTruncated] = trunc`
   替换裸 `"zabbix_truncated"`.
3. **`backend/internal/api/handlers/integration_handler.go:88-89`**: case "zabbix" 的 map 字面量用
   `integration.KeyZabbixTruncated` 替换裸 `"zabbix_truncated"`.
4. **新文件 `backend/internal/integration/sync_keys_test.go`** (~145 行): 4 个契约测试:
   - `TestKeyZabbixTruncated_Value` — 常量 == `"zabbix_truncated"` 硬钉
   - `TestKeyZabbixTruncated_InOpenAPISync` — OpenAPI SyncResult.description 段含字面量 (word-boundary)
   - `TestKeyZabbixTruncated_NoBareStringInCode` — `service.go` + `integration_handler.go` 无 `"zabbix_truncated"` 字面量 (grep guard)
   - `TestKeyZabbixTruncated_CrossLangWithTS` — 读 `frontend/src/services/syncKeys.ts`, regex 提取 TS 常量,
     断言 == Go 常量

### frontend (TS)
1. **新文件 `frontend/src/services/syncKeys.ts`** (~25 行):
   ```typescript
   export const SYNC_KEY_ZABBIX_TRUNCATED = "zabbix_truncated";
   ```
   含 JSDoc 详注: 与 Go 端 `KeyZabbixTruncated` 对齐契约.
2. **`frontend/src/pages/Settings.tsx:6`**: 新增 `import { SYNC_KEY_ZABBIX_TRUNCATED } from '../services/syncKeys'`.
3. **`frontend/src/pages/Settings.tsx:295-296`**: 用 `res?.data?.data?.synced?.[SYNC_KEY_ZABBIX_TRUNCATED] ?? 0`
   替换裸 `.zabbix_truncated` access. (handleSyncZabbix 函数体内, Zabbix 同步提示文案分支.)
4. **新文件 `frontend/src/services/syncKeys.test.ts`** (~75 行): 3 个契约测试:
   - `SYNC_KEY_ZABBIX_TRUNCATED === 'zabbix_truncated'`
   - `SyncKeys_SettingsUsesConstant` — Settings.tsx import + 用 `[SYNC_KEY_ZABBIX_TRUNCATED]` 引用
     + 无 `['" ]zabbix_truncated['" ]` 裸 access (grep guard 3 形态)
   - `SyncKeys_CrossLangWithGo` — 读 `backend/internal/integration/sync_keys.go`, regex 提取 Go 常量,
     断言 == TS 常量

### 不动 (scope 外)
- `frontend/src/pages/Settings.test.tsx` 既有 4 处 mock 数据 (728/749/766/849) — 是测试夹具,
  模拟「旧后端」返回值, 让前端「字段缺失兜底」分支可测. 契约测试覆盖生产代码 (Settings.tsx),
  不覆盖 mock 端 (那是被模拟的后端本身).
- `frontend/src/services/api.types.ts:2212` 的 description 注释 — 是 `gen:api` 生成产物,
  契约测试覆盖 `openapi.yaml` 真源, 不重复锁生成产物.
- `backend/tests/db_smoke_test.go:2885-2887` 的列表字面量 — 同形 fixture, 同 Settings.test.tsx 沿用.

## 验证 (实证)

### backend
- `go build ./...`: 0 错 (无新增 import, 无 API 签名变更)
- `go test -count=1 ./internal/integration/...`: **ok 14.7s** (含 4 新契约测试)
- `go test -count=1 ./...`: **27 packages ok** (无回归)

### frontend
- `vitest run src/services/syncKeys.test.ts src/pages/Settings.test.tsx`:
  - syncKeys.test.ts: 3/3 PASS (7ms)
  - Settings.test.tsx: 58/58 PASS (既有 4 处 mock 数据保持原样, 不退化, 65276ms)
  - **total: 61/61 PASS**

### mutation inversion 实证 (3 / 3 反证全红 → 还原全绿)

| # | 变异 | 期望红测试 | 实证 |
|---|---|---|---|
| **M1** | Go `KeyZabbixTruncated = "zabbix_truncated"` → `"zabbix_truncated_v2"` (漂离) | `TestKeyZabbixTruncated_Value` + `TestKeyZabbixTruncated_InOpenAPISync` + `TestKeyZabbixTruncated_CrossLangWithTS` 三红 | ✓ 三红, 还原后三绿 |
| **M2** | 偷偷在 `integration_handler.go:89` 写回裸字符串 `"zabbix_truncated": truncated` (绕过常量) | `TestKeyZabbixTruncated_NoBareStringInCode` (grep guard) 红 | ✓ 红 (`["zabbix_truncated" @ ../api/handlers/integration_handler.go:89]`), 还原后绿 |
| **M3** | 改 `openapi.yaml:3776` 的 `zabbix_truncated` → `zabbix_truncated_v2` (改 spec 不改代码) | `TestKeyZabbixTruncated_InOpenAPISync` 红 (word-boundary regex 防 substring 漏判) | ✓ 红, 还原后绿 |

3 变异 → 3 类红测试 → 3 类还原绿 = 漂移守卫真工作.

### 双轨分析
- **graphify multigraph 0 anomalies** (latest run)
- **codegraph** nodes/edges 与 M80 baseline 等同 (M81 是 constants 提取, 无新跨模块边)
- 累计 mutation red: M18=17 / M79=18 / M80=19 / **M81=20**

## commit
3 commits (沿用 M78 / M79 / M80 pattern):
1. `bb4a9e1` feat(M81-candidate): intent spec (omh-plan 8 节骨架, G-41 跨语言常量 + 契约测试) — 1 file / +182
2. `a18bfd8` feat(M81-candidate): G-41 zabbix_truncated 跨语言常量 + 契约测试 — 7 files / +288 / -4
   - `backend/internal/integration/sync_keys.go` 新建 (~30 行)
   - `backend/internal/integration/sync_keys_test.go` 新建 (~145 行, 4 测试)
   - `backend/internal/integration/service.go` 改 SyncAll Zabbix 分支用 KeyZabbixTruncated
   - `backend/internal/api/handlers/integration_handler.go` 改 case "zabbix" 用 integration.KeyZabbixTruncated
   - `frontend/src/services/syncKeys.ts` 新建 (~25 行)
   - `frontend/src/services/syncKeys.test.ts` 新建 (~75 行, 3 测试)
   - `frontend/src/pages/Settings.tsx` 改 handleSyncZabbix 用 [SYNC_KEY_ZABBIX_TRUNCATED]
3. `docs(M81-candidate): completion + graph analysis + CHANGELOG + TODO` — 4 files

## 派生 TODO (留 future)

- **G-57 同族收口 (M81 同族候选 M82/M83)**: `glpi_skipped` / `netbox` / `glpi` / `*_field_truncations` 4 个
  仍是裸字符串, 沿用 TODO.md G-57 登记不修 (data.synced 自由形态的 schema 收口范围). M81 实证的
  模式 (常量 + grep guard + 跨语言 drift 测试) 可直接复用 — M81-candidate 是范本, 后续 round 顺手收
  同族是低成本 path.
- **gen:api 漂移守卫**: M33/D-8 未落地 — openapi description 未补 4 个新键与「标志 vs 计数」的区分,
  `frontend/src/services/api.types.ts` 未重生成. CI 有硬门禁 `.github/workflows/ci.yml:197-198`
  (生成物**禁手改**), 仍可作下一轮候选.

## OMH workflow shape 实证 (M67 沿用)

M81 intent-M81-candidate.md 用 omh-plan 8 节骨架:
1. **Non-goals 节** 钉死 scope 仅 zabbix_truncated, 同族 (4 个) 与 api.types.ts / Settings.test.tsx mock
   都明确「不动」, 避免 scope creep.
2. **Decision gate 节** 钉死 8 个 D (D1 scope / D2 poison-stop-gates / D3 自旋防 / D4 30-min hist /
   D5 mutation inversion / D6 fact_store advisory / D7 2-3 commits / D8 PM_LAST_DISPATCH_RESULT).
3. **Verification 节** 钉 3 mutation 反证 (M1 漂离 / M2 写回裸字符串 / M3 spec 漂离) —
   实证跑了, 真红 → 还原绿, 不是空话.

OMH 没自动接管 PM-direct 流程, 但 omh-plan 骨架帮我在写 intent 阶段就把「契约测试要测什么」「mutation
要变异什么」钉死. mutation inversion 不再是「跑跑看」, 是「按表 1.1 跑这条, 期望这条红, 跑这条, 期望这条红
...」的清单执行.

## 注意事项

1. **Go test 运行时 cwd = `backend/internal/integration/`, 不是 `backend/`**: 所有相对路径要走 `../api/openapi.yaml` 等.
   `glpi_e2e_test.go` 已有先例 (`../api/openapi.yaml`), 沿用.
2. **vitest 运行时 cwd = `frontend/`** (vitest 默认工作目录): 跨仓路径走
   `../../../backend/internal/integration/sync_keys.go` (从 `frontend/src/services/` 出发).
3. **word-boundary regex 防 substring 漏判**: `assert.Contains(specStr, "zabbix_truncated")` 不抓 `_v2` 后缀,
   M3 mutation 第一版假绿暴露此点. 改用 `regexp.MustCompile(\`\bzabbix_truncated\b\`)` 后真红.
4. **regex 对声明形式敏感**: TS 端的 `SYNC_KEY_ZABBIX_TRUNCATED\s*=\s*['"]([^'"]+)['"]` 与 Go 端的
   `KeyZabbixTruncated\s*=\s*"([^"]+)"` 对空白宽容 (等号前后空格 / tab), 但对引号与等号严格.
   改声明风格 (e.g. 改成 `const X: string = "..."` TS 类型注解) 需同步更新 regex (1 行改动).
5. **`grep guard` 不覆盖注释**: `TestKeyZabbixTruncated_NoBareStringInCode` 抓所有 `"zabbix_truncated"` 字面量
   (含注释), 但本 round 把 service.go 和 handler.go 里的注释保留 (讲解语义), 故 service.go 还有 1 行注释
   含 `zabbix_truncated` (M27/D-6 原文), handler.go 还有 1 行注释 (G-41/M81 新加的指引).
   测试只检查 service.go / handler.go 的字面量, 不包括 sync_keys.go (那是常量定义本身).

## 后续推荐
- **G-57 同族收口**: M82/M83 candidate 都可走 M81 同款模式 (后端常量 + 契约测试), 是低成本路径.
  但 G-57 范围比 G-41 大 (4 个键 + openapi description 收口 + 重生成 api.types.ts), 不属本轮 scope.
- **gen:api description 补全 (M33/D-8)**: 沿用 M81 实证的 OpenAPI 真源模式, 补 SyncResult.description
  4 个新键 + 「标志 vs 计数」区分 + 重生成 `frontend/src/services/api.types.ts` (CI 硬门禁会守住).
- **fact_store fact_id = 20**: 沿用 M18 = 17 / M79 = 18 / M80 = 19 / M81 = 20 advisory 序列,
  未实际落库 (M79 同款 "fact_store truth stream" 是 advisory).
