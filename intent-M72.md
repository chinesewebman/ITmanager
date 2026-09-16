# M72 — G-UI-AssetIpValidatorParity-Mapped（OMH ulw-loop 第 4 cycle）

> **Loop cycle**: 4 of `itmanager-grit-2026q3`

## Goal

前端 IP validator 与 backend `net.ParseIP` 口径对齐 — 加 IPv4-mapped IPv6 形式 `::ffff:1.2.3.4`.

业务上 IPv4-mapped 是常见形式 (双栈 socket bind / `IN6_IS_ADDR_V4MAPPED` 检查 / `ip -6 addr` 输出), 业务表单里如果用户填这个值, 前端不应挡住再让 backend 收.

## Non-goals

- 不加 zone id `fe80::1%eth0` (backend `net.ParseIP` 不收, 需 `net.ParseAddr` — 是另一个口径, 留 future)
- 不改 backend service / handler 逻辑
- 不改 AssetFormModal / 其他页面

## Assumptions

- Backend `service.updateFirstNetworkIP` 用 `net.ParseIP(ip)` 解析, 通过后再走 v4/v6 分流
- `net.ParseIP("::ffff:1.2.3.4")` 返非 nil, `To4()` 也非 nil — 走 v4 分流 (M69 注释 L297 已记)
- 前端 IPV6_BODY 数组当前 10 条不含 IPv4-mapped
- IPv4-mapped 形状: `::ffff:0:0` / `::ffff:1.2.3.4` / `::ffff:ffff:ffff` (RFC 4291 §2.5.5.2)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `IPV6_PATTERN.test('::ffff:1.2.3.4')` = true | regex 加新交替式 |
| `IPV6_PATTERN.test('::ffff:0:0')` = true | 同上 |
| `IPV6_PATTERN.test('::ffff:1.2.3.4.5')` = false | 边界 |
| `IP_PATTERN.test('::ffff:1.2.3.4')` = true | 二选一通过 |
| 不影响现有测试 | M62 9 ok / 9 bad 全 PASS |
| mutation inversion 实证 | bypass 新交替式 → 测试真红 |

## Verification

- `tsc --noEmit` 0 err
- `vitest run src/utils/validators.test.ts` 全 PASS (59 baseline + M72 新 case)
- mutation: 删除新交替式 → M72 新测试红 + 现有 IPv6 测试仍绿

## Risks

- **zone id 仍拒绝**: 与 backend `net.ParseIP` 一致 (backend 也不收 zone id). 如果业务需要 zone id 走 ParseAddr, 是另一个口径.
- **IPv4-mapped 与 v4 守卫冲突**: backend M68 v4 守卫按 `asset_networks.ipv4_address` 查, `::ffff:1.2.3.4` 写了之后落归一为 v4 (`1.2.3.4`), 所以走 v4 守卫正确. 无冲突.

## Plan

1. `validators.ts` 的 `IPV6_BODIES` 数组加一条交替式:
   `::ffff:` + IPv4 (10.5.2 节, 3 种形式: hex / hex:hex / 点分十进制)
2. `validators.test.ts` 加新 describe "M72 IPv4-mapped" — 3 ok + 2 bad case
3. mutation inversion (删除新交替式) → 测试真红
4. docs commit + CHANGELOG + TODO + completion + graph analysis

## Decision gate

- **D1**: 加 IPv4-mapped, 不加 zone id ✓ (与 backend net.ParseIP 对齐)
- **D2**: 加 1 条交替式进现有数组 (不复用单行大 regex) ✓ (M62 数组策略沿用)
- **D3**: 测试不扩到 `ipRules` 文案 (文案不动, 只 regex 变) ✓
