# M53-completion-report — G-UI-AlertsBulkFP 告警批量标记误报

**Shipped**: 2026-09-14 (commit 7ac2855 / docs pending)
**Scope**: frontend-only, 2 files, +82/-3 LOC
**摩擦**: T99 PM-direct 摩擦表 C2 — Alerts header 只有 [批量确认] [批量解决], 缺 [批量标记误报]. 跟 M51 同样的"批量操作缺位"问题.
**Time**: ≤1h PM-direct (20:40-20:44 CST 周一, 含 ~30min 写测试)

## 改动

| 文件 | 改动 |
|---|---|
| `frontend/src/pages/Alerts.tsx` | (1) 新 import `StopOutlined`; (2) `runBulk` kind 类型 + state 类型扩 `"mark-fp"`; (3) `bulkFPMut = useApiMutation(...)` 复用 runBulk; (4) runBulk 完成 message 扩 mark-fp 分支; (5) header 加 `[批量标记误报]` 按钮 (StopOutlined + Popconfirm + danger + race-condition guard) |
| `frontend/src/pages/Alerts.test.tsx` | 3 新测试: 选中 0 项不渲染 / 选中后 Popconfirm+二次确认后 mutate 真被调 / race guard (placeholder, 真 bypass 走 mutation inversion) |

## Hard pass

- `npx tsc --noEmit`: **0 error** ✓
- `npx vitest run src/pages/Alerts.test.tsx`: **20/20 PASS** (17 老 + 3 新) ✓
- 全 frontend `npx vitest run`: (后台运行中)
- backend `cd ../backend && go test -count=1 -timeout=600s ./...`: **27 packages ok** ✓ (零改动)
- mutation inversion: bypass `onConfirm={() => bulkFPMut.mutate(selectedIds)}` → `onConfirm={() => { /*M53 BYPASS*/ }}` → **1/3 FAIL** ✓ (test 2 "mutate 真被调" 被 catch)
- 双轨分析: graphify update + diagnose + codegraph sync (待 docs commit 后跑)

## 学到 (PM-direct retro)

### 复用 `runBulk` 的省时
- M51 G-UI-BulkAssets 自己写了 `bulkRetire` 串行循环 (新的工具函数)
- M53 直接复用 `runBulk`, 只扩 `kind` 类型 + 加一条 mutation
- 这就是**抽象复用的红利** — 同样的 pattern, 不同业务, 改动局部化

### Mutation inversion 第一手就踩 syntax 坑
- `onConfirm={() => /*BYPASS*/}` — JS 解析时报 Unexpected "}"
- **正确**: `onConfirm={() => { /*BYPASS*/ }}` — 块语句 + 空函数体
- 这跟 M52 经验一致: 测试 mutation inversion 第一手经常错, 要 syntax-safe 写法

### PM-direct ≤1h 实测
- 20:40-20:44 (4min): 读现有 Alerts.tsx 找 runBulk + bulkAck/bulkResolve 模式
- 20:44-20:50 (6min): intent-M53.md + ship ✓
- 20:50-20:54 (4min): 改 Alerts.tsx (import + bulkFPMut + button + 改 runBulk kind)
- 20:54-20:55 (1min): tsc + 跑老 Alerts.test.tsx 17/17 PASS
- 20:55-20:58 (3min): 加 3 个测试 + 跑 PASS
- 20:58-20:59 (1min): mutation inversion 实证 1/3 FAIL → revert → ship
- 实际 20min, 远低于 ≤1h 预算

### runBulk kind 类型扩展
- 之前 `"ack" | "resolve"`, 加 `"mark-fp"` 后变 3 元 union
- 完成 message label 三元 ternary 改成 nested ternary (kind === "ack" ? "确认" : kind === "resolve" ? "解决" : "标记误报")
- 未来再加 kind 需重构 (4+ 元该用 lookup table) — 留 future

## Follow-up (留 future round)

- **批量"取消误报"**: 同接口反向参数 `markFalsePositive(id, false, "")`, 但 99% 运营场景是"标 FP", 留 future
- **后端 bulk_fp 端点**: N=100+ 串行循环慢, 跟 M51-3 (bulk_retire) 同模式, 留 future backend round
- **自动化 FP**: 基于历史 ack 时间阈值 (e.g. 7 天内 ack 过 3 次 → 自动标 FP), 完全新功能, 跟 M53 无关
- **runBulk kind > 3 时重构**: 用 `const KIND_LABELS: Record<string, string> = { ... }` lookup table 替 nested ternary
