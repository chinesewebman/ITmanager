# M65 双轨分析 — G-UI-AssetIpValidatorParity

## Round 信息
- **ID**: M65
- **标题**: 前端 IP regex 与 Go `net.ParseIP` 口径一致
- **类型**: PM-direct ≤1h
- **派生**: M64 (TODO G-UI-AssetIpValidatorParity)
- **日期**: 2026-09-15

## 范围
- `frontend/src/utils/validators.ts`: OCTET 收紧为 `(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)`
- `frontend/src/utils/validators.test.ts`: +4 case (前导零拒)
- `frontend/src/components/AssetFormModal.test.tsx`: +2 UI case

## graphify 状态

### pre-flight
```
graphify update . --force
```
诊断: 0 anomalies / 6843 nodes (继承 M64 verify 时点, M65 0 业务行为改)

### 本 round 改动的图节点
- `validators.ts:const OCTET` 字面量 (图节点 ID 不变, 字面量内嵌, AST 不变)
- `validators.test.ts:describe('M62 IPV4_PATTERN 边界')` 数组字面量扩展 (图节点 ID 不变)
- `AssetFormModal.test.tsx:` 新增 `it('IPv4 010.1.1.1 → ...')` + `it('IPv4 00.0.0.0 → ...')` (新图节点 +2)

### 改动后 verify
```
graphify diagnose multigraph  # 0 anomalies
```

### path check
- `validators.ts:OCTET` → `validators.test.ts:IPV4_PATTERN.test('010.1.1.1')`: 现在 false (M65 前 true)
- `validators.ts:OCTET` → `validators.test.ts:IPV4_PATTERN.test('0.0.0.0')`: true (保留)

### 跨页 wiring
- `AssetFormModal.tsx:Form.Item name="ip_address" rules={ipRules}` → `validators.ts:ipRules`: 不变
- `Assets.test.tsx`: 不变 (M62 已 ship ipRules, mock 不涉及 IP)

## codegraph 状态

### pre-flight
```
codegraph sync
```
诊断: 309 files / 6809 nodes / 15218 edges (继承 M64 verify 时点)

### 本 round 改动的图节点
- `frontend/src/utils/validators.ts` OCTET 节点 literal 内容更新 (无新节点)
- `frontend/src/utils/validators.test.ts`:
  - `bad` 数组 literal 扩展 (`'010.1.1.1'` / `'00.0.0.0'` / `'192.168.001.1'` / `'001.002.003.004'`) — 4 新字面量节点
- `frontend/src/components/AssetFormModal.test.tsx`:
  - `it()` 新增 2 个 — 2 新 describe 节点

### 改动后 verify
```
codegraph index .  # 强 re-index 同步 (sync 自报 "already up to date" 但 const arrow symbols 不索引)
```

### callers
- `IP_PATTERN` callers (M65 后): `AssetFormModal.tsx:Form.Item rules={ipRules}` 1 个, `AssetFormModal.test.tsx` 3 case
- `OCTET` callers: `IPV4_PATTERN` + `IP_PATTERN` 2 个
- `ipRules` callers: `AssetFormModal.tsx` 1 个

### query
```
codegraph query "frontend IP validation"
```
返回: `validators.ts` (IP_PATTERN / IPV4_PATTERN / IPV6_PATTERN) + `AssetFormModal.tsx:ip_address rules={ipRules}`

## mutation inversion 实证

| 层 | bypass | 期望 fail | 实测 fail |
|---|---|---|---|
| validators.test.ts | 还原旧 OCTET `[01]?\d\d?` | 4 (010/00/192.168.001/001.002) | **4 ✓** |
| AssetFormModal.test.tsx | 还原旧 OCTET | 2 (010.1.1.1 / 00.0.0.0 UI 不显示 IP_ERROR) | **2 ✓** |

还原后 validators 59/59 PASS + AssetFormModal 6/6 PASS = **65/65 PASS**.

## T 索引

本 round **无新 trap** (M65 是 M64 派生 TODO 的直接 fix, 已知 T-52 家族 (同一判据两份实现)). 
但暴露了**新子问题**: IPv4-mapped (`::ffff:1.2.3.4`) + zone id (`fe80::1%eth0`) 这支 (后端放行/前端拒绝) 留 `G-UI-AssetIpValidatorParity-Mapped` 跟进.

## 累计状态
- PM_QUEUE: M65 ship 后 = 14 shipped
- graphify nodes: 6843 (M65 0 业务行为改)
- codegraph nodes: 6809 + 6 字面量节点 + 2 describe 节点 ≈ **6817** (估算, 需 re-index verify)
