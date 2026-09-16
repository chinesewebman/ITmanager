# M72 Completion Report — IPv4-mapped IPv6 前端口径对齐

> **Loop cycle**: 4 of `itmanager-grit-2026q3`
> **Feat**: `6aa9559`
> **Intent**: `intent-M72.md` (committed in `6aa9559`)

## 摩擦

backend `service.updateFirstNetworkIP` 用 `net.ParseIP(ip)` 解析, 收 IPv4-mapped 形式 `::ffff:1.2.3.4`. 前端 `IPV6_PATTERN` 当前 10 条交替式不含 IPv4-mapped dotted-quad 形式, **业务表单填 `::ffff:1.2.3.4` 会前端红 → 后端通 = 假阳性** (M65 派生 TODO G-UI-AssetIpValidatorParity-Mapped).

## 改动

### Frontend

- **`frontend/src/utils/validators.ts`** (1 行新增):
  - `IPV6_BODIES` 数组加 1 条交替式 `::ffff:${IPV4_BODY}` 锚 dotted-quad 形式
  - 注释里说明 hex-hex 形式 (`::ffff:0:0` / `::ffff:ffff:ffff`) 已被现有第 9 条 `:(?::HEX){1,7}` 意外覆盖
- **`frontend/src/utils/validators.test.ts`** (1 describe 加 8 case):
  - M62 IP_PATTERN 既有 2 case 不变
  - 新 describe "M72 IPv4-mapped IPv6":
    - 3 ok case: `::ffff:1.2.3.4` / `::ffff:0:0` / `::ffff:ffff:ffff`
    - 4 bad case: `::ffff:1.2.3.4.5` / `::ffff:1.2.3` / `::ffff:1.2.3.x` / `::ffff:12345`
    - 1 IP_PATTERN 二选一 case: `::ffff:1.2.3.4` 也走 IP_PATTERN 通过

### Backend

不动. `net.ParseIP` 已是 source of truth (M62 backend 实证).

## 测试

| Test | 钉的口径 |
|---|---|
| `M72 IPV6_PATTERN 接受 ::ffff:1.2.3.4` | dotted-quad 形式 |
| `M72 IPV6_PATTERN 接受 ::ffff:0:0` | hex-hex 形式 (现有分支已覆盖, mutation 反证) |
| `M72 IPV6_PATTERN 接受 ::ffff:ffff:ffff` | hex-hex 形式 (同上) |
| `M72 IPV6_PATTERN 拒绝 ::ffff:1.2.3.4.5` | 5 段 |
| `M72 IPV6_PATTERN 拒绝 ::ffff:1.2.3` | 3 段 |
| `M72 IPV6_PATTERN 拒绝 ::ffff:1.2.3.x` | 非数字 |
| `M72 IPV6_PATTERN 拒绝 ::ffff:12345` | 5 位 16 进制 |
| `M72 IP_PATTERN 也接受 ::ffff:1.2.3.4` | 二选一通过 |

67/67 PASS (M62 baseline 59 + M72 new 8). AssetFormModal 6/6 PASS 不受影响.

## Mutation inversion 实证

| 步骤 | 结果 |
|---|---|
| 删除新分支 + hex-hex 备用分支 | `::ffff:1.2.3.4` 2 个 case FAIL ✓ (dotted-quad 唯一真新增) |
| hex-hex 形式 (`::ffff:0:0` 等) | **仍 PASS** ✓ (证明走的是现有第 9 条 `:(?::HEX){1,7}`, 意外覆盖) |
| 还原 | 67/67 PASS ✓ |

**Mutation 设计精妙点**: 删两条分支后只有 dotted-quad 真红, hex-hex 不红 → 这恰好**反证**了 "hex-hex 已被现有分支意外覆盖" 的认知. mutation 不只是 "测试能红", 还能验证哪些路径**未被**这条分支管.

## 双轨 graph verify

- graphify 0 anomalies (frontend-only, 不增 backend 节点)
- codegraph 增量 = 0 (regex 数组加 1 行, 无新 method / 无新 component)

## Acceptance criteria (loop LC001) ✓

| 标准 | 实证 |
|---|---|
| `IPV6_PATTERN.test('::ffff:1.2.3.4')` = true | 新 describe PASS |
| `IPV6_PATTERN.test('::ffff:0:0')` = true | 新 describe PASS (走现有分支) |
| `IPV6_PATTERN.test('::ffff:1.2.3.4.5')` = false | 新 describe PASS |
| `IP_PATTERN.test('::ffff:1.2.3.4')` = true | 二选一 PASS |
| 不影响现有测试 | M62 baseline 59 全 PASS |
| mutation inversion 实证 | 2 red ✓ |

## Loop framework 实证 (cycle 4)

| Step | M72 实证 |
|---|---|
| Identify | cognitive_surrender warning 持续, Poision "你就拍板了" 授权 |
| Exploit | M72 是 accepted loop target cycle 4, 不需要重批 |
| Subordinate | OMH follow-up 继续暂缓 |
| Elevate | 不需要 |
| Repeat | M72 完结 → next cycle = M73 OMH model calibration |

## Trap 落档 (新)

**T-79** (M72 派生): IPv6 正则的"左 0 组"分支 `:(?::HEX){1,7}` **意外覆盖** hex-hex 形式的 IPv4-mapped (`::ffff:0:0` / `::ffff:ffff:ffff`). 任何收紧"左 0 组"那条 (例如拒绝 `::1:2:3:4:5:6:7`) 都会同时把 hex-hex IPv4-mapped 也拒绝. 这是真耦合, 必须**同时改** (或同时加新分支锚 hex-hex 形式). M72 注释里已写明.
