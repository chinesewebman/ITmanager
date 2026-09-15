# M69 Completion Report — G-Asset-IpConflictGuard-v6 跨资产同 IPv6 守卫

> **Loop cycle**: 1 of `itmanager-grit-2026q3`
> **Intent**: `/home/webman/Projects/ITmanager/intent-M69.md` (commit `384b5f2`)
> **Feat**: `6fb1092`

## 摩擦

M68 拍了 v4 守卫 (`asset_networks.ipv4_address` 同 IP 跨资产 → 409), 但 v6 没查。
业务上同 IPv6 跨资产也是真摩擦:

- **link-local** `fe80::...` — 同一 LAN 段两台设备都用 `fe80::1` 是常见配错
- **唯一本地** `fc00::.../7` — 内网规划重复
- **全局单播** `2001:db8::...` / 业务规划重复

M68 决策 (intent-M68 Decision 3): "v6 留 future, 理由见 intent-M68.md Decision 3" — 这是 M69 拍板的依据.

## 改动

### Backend (`backend/internal/service/asset_service.go`)

`updateFirstNetworkIP` 在 `parsed.To4() == nil` 分支加 v6 守卫:

```go
} else {
    // M69：v6 业务冲突守卫，复用 M68 v4 SELECT pattern。
    v6Str := parsed.String()
    var taken struct { ID uuid.UUID }
    err := tx.Raw(
        `SELECT id FROM asset_networks WHERE ipv6_address = ? AND asset_id <> ? AND ipv6_address <> '' LIMIT 1`,
        v6Str, assetID,
    ).Scan(&taken).Error
    if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
        return err
    }
    if taken.ID != uuid.Nil {
        return ErrIPConflict
    }
}
```

复用 M68 sentinel `ErrIPConflict`, handler 不变 (M68 映 409 通用, 不分 v4/v6).

## 测试

### 新增 (1 case)

| Test | 钉的口径 |
|---|---|
| `TestM69_AssetService_v6_被其他资产占用_返ErrIPConflict` | 真 v6 `2001:db8::1` 被其他资产占用 → 服务返 ErrIPConflict; 守卫 SELECT 期望 1 行, 命中后 ExpectRollback 不进 SELECT 网卡 |

### 更新 (1 case)

| Test | 旧口径 → 新口径 |
|---|---|
| `TestM68_AssetService_v6_不参与v4校验` | 旧: v6 跳过守卫 (无 guard SELECT 期望); 新: v6 走自己的守卫 SELECT (期望走 `ipv6_address` 字段, 不走 `ipv4_address` 字段) |

旧用例改写不是回归, 是**口径修正**: 旧用例钉的是 M68 时的 "v6 留 future" decision, M69 拍板后这条 decision 已废, 必须 update.

## Mutation inversion 实证

按 task-completion-protocol 要求, **mutation 必须真的让测试红**:

| 步骤 | 结果 |
|---|---|
| bypass v6 guard (`else` 分支空注释) | `TestM69_AssetService_v6_被其他资产占用_返ErrIPConflict` **FAIL** ✓ |
| restore guard | **PASS** ✓ |

证明测试**真红** — 不是 false-green.

## 双轨 graph verify

(parallel background procs 起, 见 `M69-graph-analysis.md`)

- `graphify diagnose multigraph` → 0 anomalies
- `codegraph index .` → 6,532 nodes / 16,226 edges (M68 → M69 没增加节点, 只改 1 个分支)
- `codegraph query "ErrIPConflict"` → 命中 sentinel + 4 caller

## Acceptance criteria (loop LC001) ✓

| 标准 | 实证 |
|---|---|
| omh-plan 8 节 intent 写 + push | `384b5f2` intent + `intent-M69.md` 82 lines |
| impl + 真测 (mutation inversion red → restore green) | mutation red ✓ → restore green ✓ |
| graphify 0 anomalies + codegraph diff | (parallel) 0 anomalies ✓ |
| fact_store fact_id 真写 | fact_id=8 已写 ("OMH ulw-loop activated") |
| CHANGELOG + TODO + completion + graph analysis | 本 round 写 |
| PM_QUEUE shipped count +1 | 23 shipped (M47-M69) |

## Stop gates 维持

Poison 拍板的 4 个 stop gates 通过 sticky-rule `poison-stop-gates-v1` (after_gap=5 heartbeat restate) 持续维持. M69 后无 stop 触发.
