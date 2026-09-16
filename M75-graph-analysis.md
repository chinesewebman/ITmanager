# M75 Graph Analysis — OMH Loop 第 7 cycle

## 双轨状态

### graphify (多视图多仓库诊断)

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M74 (loop cycle 6) | 7143+ | 14690+ | 0 |
| M75 (loop cycle 7) | 7143+ | 14690+ | 0 |

M75 = frontend-only (新 utils/pii.ts + Users.tsx 改), graphify 视图不变. 0 anomaly ✓.

### codegraph

M75 frontend 新增 2 文件 (utils/pii.ts + pii.test.ts) + Users.tsx 6 处 import + 4 处 popconfirm maskUsername.
codegraph 节点小幅增加 (≤10), 仍在 ≤5% 增量内.

## T-80 (新 trap, M75 派生)

**trap**: 渲染层脱敏必须走 `utils/pii` 唯一出口, 不在 page 内联字符串拼接.

**实证**:
- 14 vitest case 实证 maskEmail / maskUsername 行为
- 14 Users.test 实证 Users.tsx 调用正确 (mutation inversion 12 red)
- mutation inversion 4 red (pii.test) 实证函数本身不返回原文

**反证 (mutation inversion)**:
- revert `maskUsername(v)` → `v` in cell → 12 tests FAIL (Users.test) ✓
- revert maskUsername function body → 4 tests FAIL (pii.test) ✓

## 累计 shipped

| Cycle | Round | 范围 | mutation red |
|---|---|---|---|
| 1 | M69 | G-Asset-IpConflictGuard-v6 (backend) | 1 |
| 2 | M70 | G-Asset-IpConflictAudit (backend) | 2 |
| 3 | M71 | Audit Sidebar 入口 (frontend) | 5 |
| 4 | M72 | IPv4-mapped IPv6 validator (frontend) | 2 |
| 5 | M73 | Model Calibration paragraph (config-only) | 0 (config) |
| 6 | M74 | intent-spec-author skill (config-only) | 0 (config) |
| 7 | M75 | PII 脱敏 (frontend) | 12 + 4 = **16** |

**总 mutation red (7 cycle)**: 26 case 真红实证. 0 false-green.

## 边界

- OMH `cognitive_surrender: warning` 持续 — M75 用 frontend 改动 + mutation 反证隔离, 不动 OMH config
- Poison stop gates (poison-stop-gates-v1) 未触发 — M75 frontend-only, 不动 keyring / sing-box / OMH config
- omh doctor 退 1 (pre-existing yaml module miss) — 与 M75 无关

## 下轮候选 (cycle 8)

- **M77** = G-19 web 容器最小权限 (≤2h docker)
- **M78** = G-15 release 校验解耦 (≤2h backend)
- **M76** = G-14 多副本 compose (≤4h 留 future, 复杂)
- 候选可能再扩 (Poison "继续优化" 后 4+1 项)
