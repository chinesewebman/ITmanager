# M65 — G-UI-AssetIpValidatorParity 修前端 IP regex 与 Go net.ParseIP 口径一致 (M64 派生 TODO)

## Background

M64 final report 派生 TODO G-UI-AssetIpValidatorParity:

> 前端 `ipRules` (M62 ship) 用 `[01]?\d\d?` 允许前导零 `010.1.1.1`, 但 Go `net.ParseIP("010.1.1.1")` 返回 nil (前导零是非法 IPv4 dotted-decimal, RFC 6943). 用户填 `010.1.1.1` → 前端 IP_PATTERN 通过 → 提交 → backend `net.ParseIP` 拒 → **422 误伤**.

实证 (current IP_PATTERN):
```
"1.2.3.4"    → true  ✓
"010.1.1.1"  → true  ✗ (Go 拒)
"0.0.0.0"    → true  ✓ (合法 0)
"00.0.0.0"   → true  ✗ (Go 拒, 双前导零)
"1.2.3.4.5"  → false ✓
"256.1.1.1"  → false ✓
"1.2.3"      → false ✓
```

## 范围 (≤1h PM-direct round)

### Frontend

1. **`frontend/src/utils/validators.ts`**:
   - 收紧 OCTET 为 `(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)` — 拒绝前导零 (除 `0` 本身)
   - 注释加 RFC 6943 + Go net.ParseIP 口径引用
   - 新加边界注释: "前导零 (除单 0) 非法; 与 Go `net.ParseIP` 口径一致"

2. **`frontend/src/utils/validators.test.ts`**:
   - 加新 case: `010.1.1.1` → false (前导零拒); `00.0.0.0` → false (双前导零拒); `0.0.0.0` → true (合法 0); `1.2.3.4` → true (回归)
   - 加 IPv6 回归 case 确认 0 退化

3. **`frontend/src/components/AssetFormModal.test.tsx`**:
   - 加 2 case: 输入 `010.1.1.1` → 字段红色错误 + 不调 onSubmit; 输入 `1.2.3.4` → 通过

### 不要写

- 不要改 backend (Go net.ParseIP 是正确的)
- 不要改 AssetFormModal form 字段 (只改 IP_PATTERN 来源)
- 不要写 IP 校验中间件
- 不要改其他 page 接入 utils/validators (M60 follow-up, 留 future)

## Hard pass

- frontend tsc 0
- frontend `validators.test.ts`: 新 case PASS + 老 case 不退化
- frontend `AssetFormModal.test.tsx`: 新 case PASS + 4 老 case 不退化
- mutation inversion 实证: bypass OCTET 收紧 → 2 新 case FAIL

## Commit cadence (3 commit + push)

1. `feat(M65): validators.ts OCTET 收紧为 RFC 6943 / Go net.ParseIP 口径`
2. `test(M65): validators + AssetFormModal 前导零拒测 (2 + 2 case)`
3. `docs(M65): CHANGELOG + TODO (G-UI-AssetIpValidatorParity 结案) + 双轨分析 + completion report`

完成请按 PM-direct 5-section final report 输出.
