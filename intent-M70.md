# M70 — G-Asset-IpConflictAudit 跨资产同 IP 巡检（OMH ulw-loop 第 2 cycle）

> **Loop cycle**: 2 of `itmanager-grit-2026q3`

## Goal

一次性巡检工具, SELECT 跨资产同 `ipv4_address` / `ipv6_address`, 输出冲突报告. **不自动修**.

## Non-goals

- 不动 M68/M69 守卫逻辑
- 不入 cmd/service tree (一次性脚本, 不污染 repo) — 但核心 SQL 抽到 `service.AuditIPConflicts` 供测试
- 不写前端 (纯 backend)

## Assumptions

- 守卫只在写入时检查 (M68/M69). 历史数据可能存在跨资产同 IP, 需要巡检.
- 操作者需要看报告后**手动拍决策** (删除 IP / 修改 IP / 接受冲突).
- Poision 偏好 keyring/libsecret; DSN 通过 env 注入.

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| 抽到 service 层可测试 | `AuditIPConflicts` 在 `backend/internal/service/asset_audit.go` |
| 真 sqlite 测试覆盖 | 4 case (核心冲突/空表/全唯一/空串) |
| mutation inversion red | bypass `HAVING COUNT(*) >= 2` → 2 test FAIL → restore → 全绿 ✓ |
| 单文件一次性 CLI | `/tmp/m70-audit/main.go` (不进 repo) |
| 报告含 IP/Kind/Count/AssetIDs/Networks | `IPConflictRow` struct |

## Verification

- `go build ./...` 0 err ✓
- `go test -count=1 ./internal/service/... -run TestM70_` 4/4 PASS ✓
- mutation inversion: bypass HAVING → 2 test 真红 (TestM70_查跨资产同IP_返所有冲突 + TestM70_全唯一IP_返空切片) ✓

## Risks

- **SQLite vs Postgres 差异**: 测试用 sqlite, 生产用 postgres. SQL 用标准 GROUP BY/HAVING/ORDER BY, 无 cast, 无 JSON path, 跨方言安全.
- **大表性能**: `asset_networks.ipv4_address` 是 plain index (M68 决策: 不加 unique). 大表 GROUP BY 全表扫可能慢. 接受 (一次性脚本, 可放 cron 跑夜间).
- **DSN 暴露**: `ITMANAGER_DSN` env 不入库, 用 libsecret 走也行. CLI 默认 DSN 仅 dev 用.

## Plan

1. `service.AuditIPConflicts(ctx) ([]IPConflictRow, error)` — 抽核心 SQL 到 service 层
2. 4 case 真 sqlite 测试
3. mutation inversion (HAVING bypass)
4. CLI wrapper `/tmp/m70-audit/main.go` — 不入 repo, 一次性
5. docs (本文件 + completion + graph analysis)

## Decision gate

- **D1**: 巡检报告不动库, 让操作者拍 ✓ (业务决策, 不是技术决策)
- **D2**: 核心 SQL 抽到 service 层可测试, 不放 cmd tree ✓ (测试驱动 vs 一次性脚本 trade-off)
- **D3**: 不加 unique constraint ✓ (M68 决策沿用: 同资产多网卡允许)
