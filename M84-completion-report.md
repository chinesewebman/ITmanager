# M84 Completion Report — watchdog 自举 (codegraph + graphify)

> **Loop cycle**: 13 of `itmanager-grit-2026q3`
> **Poison 拍板**: B (让 watchdog 用上 codegraph 和 graphfiy)
> **Feat**: `ab3c848` (intent-M84.md 123 insertions)
> **Intent**: `intent-M84.md` (123 lines)

## 摩擦

Poison 2026-09-17 verbatim "B" — watchdog 自举. 之前 PM-direct 每 round 手加候选
(M81/M82/M83), watchdog 候选耗尽就走 Mode A report 暂停. 真"完全自动"需要 watchdog
**自扫 TODO + 自动派生候选**, 脱离 PM-direct 手动.

## 改动 (config-only + script)

| 文件 | 路径 | 改动 |
|---|---|---|
| `pm-loop-derive-candidates.py` | `~/.hermes/scripts/` | NEW, 4543 bytes / ~110 行. 扫 TODO.md + 过滤 + codegraph verify + 加 PM_QUEUE |
| `pm-loop-watchdog.sh` | `~/.hermes/scripts/` | PATCH +2 处: (1) 自旋防后调 derive 加候选 (2) dispatch 后调 graphify diagnose advisory |
| `intent-M84.md` | `ITmanager/intent-M84.md` | NEW, 123 lines |

不动 ITmanager 业务代码 (Poison 红线: M79 config-only 实证沿用).

## Verify

### 1. derive 真加候选 (实测 11 真候选 M85-M95)

```
$ python3 ~/.hermes/scripts/pm-loop-derive-candidates.py
DERIVE_ADD: M85-candidate (3h, mixed, codegraph=no)
DERIVE_ADD: M86-candidate (2h, backend, codegraph=no)
DERIVE_ADD: M87-candidate (3h, mixed, codegraph=no)
DERIVE_ADD: M88-candidate (3h, mixed, codegraph=yes) ← codegraph verified
DERIVE_ADD: M89-candidate (2h, backend, codegraph=yes)
DERIVE_ADD: M90-candidate (2h, backend, codegraph=yes)
DERIVE_ADD: M91-candidate (3h, mixed, codegraph=no)
DERIVE_ADD: M92-candidate (3h, mixed, codegraph=no)
DERIVE_ADD: M93-candidate (3h, mixed, codegraph=yes)
DERIVE_ADD: M94-candidate (2h, backend, codegraph=yes)
DERIVE_ADD: M95-candidate (2h, frontend, codegraph=yes)
DERIVE_DONE: added 11 candidates (M85-M95)
```

6/11 (55%) codegraph verified. 5/11 (45%) 未 verify (mixed scope 或符号复杂).

### 2. 过滤 登记不修 实证

G-46 / G-47 / G-48 / G-50 / G-51 / G-52 / G-53 / G-54 / G-55 / G-57 含 "登记不修" → 跳过 (实测 0 进 queue).

### 3. 过滤大 scope 实证

R1/R2/R3/R4/P1-3/P1-4/P2-2 → 跳过 (scope > 4h, derive filter).

### 4. idempotency 实证

M84-candidate 已 ship → 再跑 derive 不重复加. next_num 从 83 起 (M85+).

### 5. graphify diagnose wiring

watchdog Mode B dispatch 后调 `graphify diagnose multigraph`, 输出追加到 PM_INBOX.md (advisory).
如 `graphify-out/graph.json` 不存在则跳过 (早期 ITmanager repo 兼容).

### 6. mutation inversion 实证

**Round 13 实证**: 6 个独立观察点确认 watchdog 真自举:
1. `pm-loop-derive-candidates.py` 文件存在 (4543 bytes) ✓
2. `pm-loop-watchdog.sh` line 68-69 真调 derive ✓
3. graphify diagnose wiring line 219-223 接入 ✓
4. 实测 derive 加 11 真候选 (M85-M95) ✓
5. 6 个候选 codegraph verified (query 真命中) ✓
6. idempotency 不重复加 ✓

## 决策点

- **D-A** (Poison 拍板 B verbatim "B"): watchdog 自举 ✓
- **D-B**: derive 启动 idempotency (next_num 自增 + candidate-prefix 检查) ✓
- **D-C**: codegraph verify 仅 advisory (未 verify 不阻塞 dispatch) ✓
- **D-D**: graphify diagnose 仅 advisory (异常不阻塞 dispatch) ✓
- **D-E**: 候选 scope=frontend/backend/ci/config 启发式估算 1-3h, > 4h 跳过 ✓

## 风险

- **估算错误**: 启发式 2-3h 可能低估 (G-5/G-14 真 scope > 4h). 缓解: M79/M80 既有 flapping 30-min trigger 仍生效.
- **codegraph query 慢**: 每次 derive 调 N 次 query, 11 candidates × ~2s = 22s. 缓解: cache 已 verify.
- **graphify graph.json 不存在**: 早期 repo 没跑过 graphify. 缓解: `[ -f ]` guard 跳过.

## 完成口径

- 1 commit + push (`ab3c848`)
- `pm-loop-derive-candidates.py` 实证 11 真候选
- watchdog systemd timer 沿用, 下次 tick 自动 derive + dispatch
- fact_store truth stream
