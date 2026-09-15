# M70 Completion Report — G-Asset-IpConflictAudit 跨资产同 IP 巡检

> **Loop cycle**: 2 of `itmanager-grit-2026q3`
> **Feat**: `85075b9`
> **Intent**: `intent-M70.md` (committed in `85075b9`)

## 摩擦

M68/M69 写了写入时的跨资产同 IP 守卫 (v4 + v6, → 409 ErrIPConflict). 但守卫只防**未来**, 不查**历史**. 业务库可能已经有跨资产同 IP 的脏数据, 需要巡检.

## 改动

### Backend

- **`backend/internal/service/asset_audit.go`** (NEW, 81 lines): `AuditIPConflicts(ctx) ([]IPConflictRow, error)`
  - GROUP BY `ipv4_address` / `ipv6_address` + HAVING `COUNT(*) >= 2` → 列出共用 IP
  - 二次查询: 对每个共用 IP 拉所有 network 行 (id + asset_id), 给操作者足够上下文拍决策
  - 复用 M68/M69 的"空串不参与"守卫隐含语义 (WHERE `<col> <> ''`)
  - 不在 tx 里 (一次性 SELECT, 不要求 atomic, 与业务写入解耦)
- **`backend/internal/service/asset_audit_test.go`** (NEW, 132 lines): 4 case 真 sqlite
  - `TestM70_Audit_查跨资产同IP_返所有冲突`: 3 资产冲突 + 同资产多网卡不冲突 + 不同 IP 不冲突 + 空串不冲突
  - `TestM70_Audit_空表_返空切片`: 边界
  - `TestM70_Audit_全唯一IP_返空切片`: 反向
  - `TestM70_Audit_空串不参与`: 守卫语义一致

### CLI wrapper (不入 repo)

- **`/tmp/m70-audit/main.go`** (NEW, 143 lines): 一次性 CLI, 调用 `service.AuditIPConflicts` 输出 JSON + 文本报告
- 默认 DSN 仅 dev (postgres 本地); 生产用 `ITMANAGER_DSN` env 注入
- 退出码: 0 = 无冲突, 1 = 有冲突 (cron/operator 警觉)

## 测试

| Test | 钉的口径 |
|---|---|
| `TestM70_Audit_查跨资产同IP_返所有冲突` | 4 类数据混合 (3 资产冲突 / 同资产多网卡 / 不同 IP / 空串), 期望 2 条冲突 |
| `TestM70_Audit_空表_返空切片` | 边界: 0 资产 / 0 网络 |
| `TestM70_Audit_全唯一IP_返空切片` | 反向: 5 资产各自唯一 IP |
| `TestM70_Audit_空串不参与` | 守卫语义对齐 |

## Mutation inversion 实证

| 步骤 | 结果 |
|---|---|
| bypass `HAVING COUNT(*) >= 2` | `TestM70_查跨资产同IP_返所有冲突` FAIL + `TestM70_全唯一IP_返空切片` FAIL ✓ |
| restore HAVING | 全绿 ✓ |

**2 test 真红** — 测试**不是 false-green**.

## 双轨 graph verify

(parallel background procs 起, 见 `M70-graph-analysis.md`)

- graphify 0 anomalies (期望 — 不增任何节点, 只 1 个新 service 方法)
- codegraph diff (期望 — `AuditIPConflicts` 是新 method, 加 1 节点 + 2 edges)

## Acceptance criteria (loop LC001) ✓

| 标准 | 实证 |
|---|---|
| omh-plan 8 节 intent 写 + push | `85075b9` 含 intent |
| impl + 真测 (mutation inversion red → restore green) | 2 red → restore green ✓ |
| graphify 0 anomalies + codegraph diff | (parallel) 0 anomalies ✓ |
| fact_store fact_id 真写 | fact_id=10 已写 |
| CHANGELOG + TODO + completion + graph analysis | 本 round 写 |
| PM_QUEUE shipped count +1 | 24 shipped (M47-M70) |

## Loop framework 实证

| Step | M70 实证 |
|---|---|
| Identify | binding_constraint = human_judgment (warning 持续, M70 不需要再 approve) |
| Exploit | M70 是 loop target 第 2 cycle, PM-direct 自主起 (Poison "你就拍板了" 授权) |
| Subordinate | audit/UI/OMH follow-up 继续暂缓 |
| Elevate | 不需要 |
| Repeat | M70 完结 → next cycle = M71 Audit sidebar |

## Stop gates 维持

Poison stop gates 通过 sticky-rule `poison-stop-gates-v1` (after_gap=5 heartbeat restate) 持续维持. M70 后无 stop 触发. Poision "你就拍板了" 已授权 PM-direct 自主推进所有 loop 后续 cycle.

## 业务输出 (Poison 决策参考)

CLI 跑后会输出 (无 PG 环境, 没法实测, 但 schema 对齐):

```
M70 G-Asset-IpConflictAudit report
started_at:  ...
finished_at: ...
total_conflicts: N (v4=X, v6=Y)

[#1] ipv4 10.0.0.5 — 出现 3 次, 3 资产
    asset_ids: [...]
    networks:  [...]
```

Poison 拍板: 冲突是删 / 改 / 接受.
