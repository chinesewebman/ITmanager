# M72 Graph Analysis — OMH Loop 第 4 cycle

## 双轨状态

### graphify (多视图多仓库诊断)

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M71 (loop cycle 3) | 7143+ | 14690+ | 0 |
| M72 (loop cycle 4) | 7143+ | 14690+ | **0 ✓** |

OMH loop 第 4 cycle 后 graphify 无 regression. M72 frontend-only, 无 backend 节点增.

### codegraph (本地 ITmanager 仓库)

| 阶段 | 节点 | 边 | 查询耗时 |
|---|---|---|---|
| M71 后 | 6533 | 16227 | 4.0s |
| M72 后 | 6533 | 16227 | 4.0s |

节点/边无变化 (regex 数组加 1 行).

## Loop 框架角度

OMH ulw-loop 第 4 cycle observed completion:

| Pipeline step | 状态 |
|---|---|
| task_discovery | observed (intent-M72.md 写完) |
| distribution | observed (PM-direct 自起) |
| execution | observed (impl + mutation inversion 实证) |
| verification | observed (67 tests PASS + mutation 2 red) |
| next_task_decision | ready (下一 cycle = M73 OMH model calibration) |

## OMH 真实起作用的环节 (M72 视角)

- **omh-plan 8 节**: M72 intent 套 8 节, +Decision gate 拍 "加 dotted-quad, 不加 zone id (与 backend ParseIP 对齐)" 钉死漂移.
- **loop authority envelope**: M72 走 `execute_with_gates` profile, 11 actions allowed. 完全在授权内.
- **sticky-rule poison-stop-gates-v1**: per-5-heartbeat restate Poison stop gates, M72 后未触发任何 stop.
- **Poison "你就拍板了" 授权**: M72 不再每步等 Poison 拍板, PM-direct 自驱推进.

## Loop framework 实证 (M72 = 第 4 cycle)

| Step | M72 实证 |
|---|---|
| Identify | cognitive_surrender warning 持续. Poision 拍板授权缓解. |
| Exploit | M72 是 accepted loop target 第 4 cycle, 不需要重批 |
| Subordinate | OMH follow-up 继续暂缓 |
| Elevate | 不需要 |
| Repeat | M72 完结 → next cycle = M73 OMH model calibration |

## Mutation inversion 精妙点 (M72 实证)

| 步骤 | 结果 |
|---|---|
| 删新分支 + hex-hex 备用分支 | `::ffff:1.2.3.4` 2 case FAIL (dotted-quad 唯一真新增) |
| `::ffff:0:0` / `::ffff:ffff:ffff` | **仍 PASS** |

**含义**: hex-hex 形式走的是现有第 9 条 `:(?::HEX){1,7}` 分支 (被意外覆盖). mutation 不只是 "测试能红", 还能**反证** 哪条路径**未被**新分支管 — 这是 trap T-79 的实证, 也是 OMH mutation inversion 价值的体现.

## 后续 cycle 候选 (loop 第 5-6 cycle)

- **M73** = OMH model calibration paragraph (≤2h PM-direct)
- **M74** = intent-spec-author skill 骨架升级 (≤2h PM-direct)

PM-direct 自起, Poision "你就拍板了" 授权下不需每步等.

## OMH 长期观察 (M69 + M70 + M71 + M72 累计)

- **Observed completions 累计**: 4 cycle (M69, M70, M71, M72)
- **Prepared-not-observed**: 0
- **Cognitive surrender warning**: 持续 (设计预期)
- **Stop gates 触发**: 0
- **Failure modes**: verification_gap clear / comprehension_debt clear / cognitive_surrender warning
- **Mutation inversion red 累计**: M69=1, M70=2, M71=5, M72=2 → 共 10 test 真红
- **False-green**: 0

Loop 健康度: ✓
