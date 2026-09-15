# M65 完成报告 — G-UI-AssetIpValidatorParity

## Round 信息
- **ID**: M65
- **标题**: 前端 IP regex 与 Go `net.ParseIP` 口径一致
- **类型**: PM-direct ≤1h (≤1h 极小 round 不浪费 omp dispatch 成本)
- **派生**: M64 (TODO G-UI-AssetIpValidatorParity 前导零口径)
- **日期**: 2026-09-15 周二 09:25-17:55 CST

## 1. 摩擦 (M64 派生)

M64 把 IP 走通了写入路径 —— POST /assets 含 `ip_address` 现在真落 `asset_networks` (v4 → `ipv4_address`、v6 → `ipv6_address`), 后端用 `net.ParseIP` 兜底校验 (失败返 422 `validation_failed`).

但前端 `IP_PATTERN` (M62 ship) 的 IPv4 段是 `[01]?\d\d?`, 允许 `010.1.1.1` / `00.0.0.0` / `192.168.001.1`. Go `net.ParseIP` 按 RFC 6943 **拒绝前导零** (除单 0).

**用户路径**: 填 `010.1.1.1` → 前端放行 → 提交 → backend 422, 文案是通用「创建失败」 → **UX 卡墙**.

## 2. 决定 (PM-direct ≤1h 自拍)

- **本轮只修「前端放行、后端拒绝」这一支**: 前导零 (已知产品口径: 写错, 拒)
- **不修「后端放行、前端拒绝」这一支** (IPv4-mapped `::ffff:1.2.3.4` / zone id `fe80::1%eth0`): 需产品口径, 留 `G-UI-AssetIpValidatorParity-Mapped` 跟进
- **修法**: 收紧 `OCTET` 正则, 与 Go `net.ParseIP` 行为一致
- **不写新 helper**: 改 1 行正则字面量, 加注释引用 RFC 6943

## 3. 改动

### 3.1 `frontend/src/utils/validators.ts`
```diff
- /** 一段 IPv4：0-255（25x / 2[0-4]x / 0-199）。 */
- const OCTET = '(?:25[0-5]|2[0-4]\\d|[01]?\\d\\d?)'
+ /**
+  * 一段 IPv4：0-255（25x / 2[0-4]x / 1\d\d / [1-9]?\d）。
+  *
+  * 与 RFC 6943 + Go `net.ParseIP` 口径一致：**拒绝前导零**（除 `0` 本身）。
+  * 例如 `010.1.1.1` / `00.0.0.0` 在前端视为非法（Go `net.ParseIP` 拒），
+  * 避免「前端 IP_PATTERN 通过 → 提交 → backend 422 误伤」（M65 / M64 派生 TODO G-UI-AssetIpValidatorParity）。
+  */
+ const OCTET = '(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)'
```

### 3.2 `frontend/src/utils/validators.test.ts`
```diff
+    // M65: 前导零（除单 0）按 RFC 6943 + Go net.ParseIP 口径拒；M64 422 误伤的根因
+    '010.1.1.1', // 单前导零 —— Go net.ParseIP 拒
+    '00.0.0.0', // 双前导零 —— Go net.ParseIP 拒
+    '192.168.001.1', // 中段前导零 —— Go net.ParseIP 拒
+    '001.002.003.004',
```

### 3.3 `frontend/src/components/AssetFormModal.test.tsx`
```diff
+  // M65: 前导零（除单 0）按 RFC 6943 + Go net.ParseIP 口径拒；M64 422 误伤的根因
+  it('IPv4 010.1.1.1 → 前导零（与 net.ParseIP 口径一致）显示格式错误且不提交', async () => {
+    await fillForm('010.1.1.1')
+    expect(await screen.findByText(IP_ERROR)).toBeInTheDocument()
+    expect(onSubmit).not.toHaveBeenCalled()
+  })
+
+  it('IPv4 00.0.0.0 → 双前导零（与 net.ParseIP 口径一致）显示格式错误且不提交', async () => {
+    await fillForm('00.0.0.0')
+    expect(await screen.findByText(IP_ERROR)).toBeInTheDocument()
+    expect(onSubmit).not.toHaveBeenCalled()
+  })
```

## 4. 验证 (PM 独立)

### Hard pass
| 维度 | 结果 |
|---|---|
| frontend `npx tsc --noEmit` | 0 error |
| frontend `validators.test.ts` | **59/59 PASS** (基线 55 + M65 4 新 case) |
| frontend `AssetFormModal.test.tsx` | **6/6 PASS** (基线 4 + M65 2 新 UI case) |
| 合计 M65 覆盖 | **65/65 PASS** |
| mutation inversion 实证 | 2 层 (validators 4 FAIL + AssetFormModal 2 FAIL), 还原后全绿 |
| graphify 0 anomalies | ✓ |

### Mutation inversion 实证
1. **bypass validators OCTET 收紧**: 还原 `[01]?\d\d?` → `validators.test.ts` **4 failed** (`010.1.1.1` / `00.0.0.0` / `192.168.001.1` / `001.002.003.004` 全被放行)
2. **bypass AssetFormModal 共享的 IP_PATTERN**: 同一 bypass → `AssetFormModal.test.tsx` **2 failed** (`findByText(IP_ERROR)` timeout —— 旧 pattern 放行, 没显示错误直接 onSubmit, **正是 M64 422 误伤的 UI 路径**)
3. **还原**: 65/65 PASS

## 5. 关键实现亮点

1. **OCTET 收紧**: `(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)` — 拒前导零 (除单 0), 与 Go `net.ParseIP` 行为一致
2. **注释引用 RFC 6943 + Go 口径**: 防下轮被无脑「放宽」回去
3. **UI 端测试覆盖**: bypass 同时验证了 validators 和 AssetFormModal 两层 (M62 retro 实证: 跨层 mutation 必须跨层 verify)

## 6. 背离与诚实登记

1. **半结案**: 「后端放行、前端拒绝」这一支 (IPv4-mapped + zone id) 留 `G-UI-AssetIpValidatorParity-Mapped` 跟进
2. **0 业务行为改**: 所有合法 IP (`0.0.0.0` / `255.255.255.255` / `192.168.1.1` / `::1` 等) 继续放行, 只收紧「形状对但地址错」边界
3. **mutation inversion 实证**: 跨 2 层 (validators + AssetFormModal) 实证有效, 不只 module 级

## 7. 残余 (如实登记)

- 真 PG 上的 010.1.1.1 422 往返实测: 未做 (本机无 PG 服务端)
- IPv4-mapped (`::ffff:1.2.3.4`) 后端 `To4()` 归一但前端拒: **未修**, 需产品口径
- zone id (`fe80::1%eth0`): 前后端都拒, **未修**, 需产品口径 (表单要不要允许?)

## 8. Commit

- `a197699 docs(M65): ... intent`
- `a52a660 feat(M65): validators.ts OCTET 收紧为 RFC 6943 / Go net.ParseIP 口径 (拒前导零)`
- `(this commit) docs(M65): CHANGELOG + TODO + 双轨分析 + completion report`

## 9. 文件

- `frontend/src/utils/validators.ts`: OCTET 字面量 + 注释
- `frontend/src/utils/validators.test.ts`: +4 case (010.1.1.1 / 00.0.0.0 / 192.168.001.1 / 001.002.003.004)
- `frontend/src/components/AssetFormModal.test.tsx`: +2 UI case
- `CHANGELOG.md`: M65 section
- `TODO.md`: G-UI-AssetIpValidatorParity 半结案 + 派生 G-UI-AssetIpValidatorParity-Mapped
- `M65-graph-analysis.md`: 双轨分析
- `M65-completion-report.md`: 本报告
