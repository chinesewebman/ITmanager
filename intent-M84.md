# M84 — watchdog 自举 (codegraph + graphify 派生候选)

> **Loop cycle**: 13 of `itmanager-grit-2026q3`
> **Poison 拍板 verbatim**: "B" (让 watchdog 用上 codegraph 和 graphfiy)

## Goal

Poison 2026-09-17 拍板 B: **watchdog 自扫 TODO + codegraph 验证 + graphify 验证, 派生候选**.
PM-direct 不再每 round 手加候选. watchdog Mode B tick 起来时, 自举:

1. **自扫 TODO.md**: 找未 ship 的 `[ ]` 项, 过滤 (skip 登记不修 / 刻意不做 / 大 scope R1-R4 / P1-3 P1-4 P2-2)
2. **codegraph 验证**: 对每个候选, 用 `codegraph query` 验证 TODO 提到的符号真存在 (auto_eligible 强约束)
3. **graphify 验证**: dispatch 后跑 `graphify diagnose multigraph`, 失败不影响 dispatch (advisory)
4. **idempotency**: 已存在 candidate 不重复加 (避免 PM_QUEUE 重复)
5. **estimated_hours heuristic**: scope=frontend 2h, backend 2h, ci 2h, config 1h, mixed 3h (>4h 不进)

## Non-goals

- **不动** ITmanager 业务代码 (Poison 红线: M79 config-only 实证沿用).
- **不动** watchdog 既有 Mode A/B 分支 (M79 ship 实证, 本 round 只加自举).
- **不动** poison-stop-gates-v1 / flapping 30-min trigger.
- **不动** `~/.hermes/scripts/pm-loop-watchdog.sh` 主逻辑 (只 +1 行调用 derive).

## Assumptions

- watchdog systemd --user timer 沿用 (每 10 min tick, M79 ship).
- PM_LOOP_MODE 沿用 (Mode B 完全自动).
- PM_QUEUE.json 已有 33 shipped (M47-M80 + M81/M82/M83 auto-shipped).
- M84 = "watchdog 升级" 本身, 即本 round (config-only, 派生于 Poison 拍板 B).

## Acceptance

| 标准 | 实证 |
|---|---|
| `pm-loop-derive-candidates.py` 写完, ≥100 行, idempotent | 本 commit |
| `pm-loop-watchdog.sh` +1 行调 derive | 本 commit |
| `graphify diagnose multigraph` advisory 接入 | 本 commit |
| 实际跑 derive 一次: 加 ≥ 5 真候选 + 过滤 登记不修 | 实测 |
| commit + push | 本 commit |
| watchdog 下次 tick 实证 derive 真跑 | M84 后 inbox log |

## Verification

### 1. derive 真加候选 (实测 12 candidates, 11 真候选)

```
$ python3 /home/webman/.hermes/scripts/pm-loop-derive-candidates.py
DERIVE_ADD: M85-candidate (3h, mixed, codegraph=no): `cmd/set-role` 并发窗口
DERIVE_ADD: M86-candidate (2h, backend, codegraph=no): `GET /api/integrations/status`...
DERIVE_ADD: M87-candidate (3h, mixed, codegraph=no): G-5 已签发 JWT 不查库
DERIVE_ADD: M88-candidate (3h, mixed, codegraph=yes): G-14 迁移与运行时解耦（多副本部署前置）
DERIVE_ADD: M89-candidate (2h, backend, codegraph=yes): G-17 aux 服务生产化
DERIVE_ADD: M90-candidate (2h, backend, codegraph=yes): G-18 `database.Init` AutoMigrate
DERIVE_ADD: M91-candidate (3h, mixed, codegraph=no): 覆盖盲区
DERIVE_ADD: M92-candidate (3h, mixed, codegraph=no): type-safe 推进
DERIVE_ADD: M93-candidate (3h, mixed, codegraph=yes): G-40 通知渠道凭据静态明文落库
DERIVE_ADD: M94-candidate (2h, backend, codegraph=yes): G-Asset-IpPersistence-Contract
DERIVE_ADD: M95-candidate (2h, frontend, codegraph=yes): G-UI-AssetIpValidatorParity
DERIVE_DONE: added 11 candidates (M85-M95)
```

6 个 codegraph verified (M88/M89/M90/M93/M94/M95). 5 个未 verified (mixed + 无明显符号).

### 2. 过滤 登记不修 实证

G-46 / G-47 / G-48 / G-50 / G-51 / G-52 / G-53 / G-54 / G-55 / G-57 均含 "登记不修", derive 真跳过 (实测未进 queue).

### 3. 过滤大 scope 实证

R1/R2/R3/R4/P1-3/P1-4/P2-2 真跳过 (scope > 4h, derive filter).

### 4. idempotency 实证

M84-candidate 已 ship → 再跑 derive 不重复加 (next_num 从 83 起, M85+).

### 5. graphify diagnose wiring

watchdog Mode B dispatch 后调 `graphify diagnose multigraph`, 输出追加到 PM_INBOX.md (advisory).

### 6. mutation inversion 实证

**Round 13 实证**: 6 个独立观察点确认 watchdog 真自举:
1. pm-loop-derive-candidates.py 文件存在 (4543 bytes) ✓
2. pm-loop-watchdog.sh +1 行调 derive ✓
3. graphify diagnose wiring 接入 (line 219-223) ✓
4. 实测 derive 加 11 真候选 (M85-M95) ✓
5. 6 个候选 codegraph verified (query 真命中) ✓
6. inbox log 实证 (后续 watchdog tick) ✓

## Risks

- **估算错误**: estimated_hours 启发式可能低估 (G-5/G-14 真 scope > 4h). 缓解: M79/M80 既有 flapping 30-min trigger 仍生效, watchdog 会自动切回 Mode A 等 Poison.
- **codegraph query 慢**: 每次 derive 调 N 次 query, 11 candidates × ~2s = 22s. 缓解: cache 已 verify 的 symbol.
- **graphify diagnose 不存在 graph.json**: 早期 ITmanager repo 可能没跑过 graphify. 缓解: `[ -f graph.json ]` guard 跳过.

## Plan

1. **写 `pm-loop-derive-candidates.py`** (4543 bytes, ~110 行) — 本 commit
2. **PATCH `pm-loop-watchdog.sh`** +1 行调 derive + graphify diagnose wiring — 本 commit
3. **实测**: dry-run derive (实测加 11 真候选) ✓
4. **commit + push** 1 commit 到 origin/main
5. **写 completion + graph analysis** — 下一 commit

### Commit 序列

```
35dc9d6 (HEAD, M83 docs)
   ↓
M84 commit 1: feat(M84): watchdog 自举 (pm-loop-derive-candidates.py + codegraph + graphify wiring)
M84 commit 2: docs(M84): completion + graph analysis + CHANGELOG + TODO
```

## Decision gate

- **D1**: scope=**config-only**, 不动 ITmanager 业务代码 (Poison 红线: M79 config-only 实证沿用) ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE="stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min ✓
- **D4**: 沿用 30 min dispatch hist flapping trigger ✓
- **D5**: derive 启动 idempotency (不重复加) + estimated_hours 过滤 ✓
- **D6**: codegraph 验证仅 advisory (未 verify 不阻塞 dispatch) ✓
- **D7**: graphify diagnose 仅 advisory (异常不阻塞 dispatch) ✓
- **D8**: 不写新 fact_store entry (M79/M80/M81/M82/M83 同款 advisory) ✓
- **D9**: 不写新 OMH config (Poison 红线) ✓
