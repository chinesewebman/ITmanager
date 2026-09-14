# intent-M62: G-UI-AssetIpValidator + 其他页接入 utils/validators (F-1 follow-up + 新发现 IP 摩擦)

## Context

M60 提取 `frontend/src/utils/validators.ts` 后, 留下:
- 其他页 (Oncall/Runbook/AssetFormModal 等) 还没接入
- 多角度审查新发现: `AssetFormModal.tsx:91` 有 `ip_address` 字段, 只校验 required, **无 IP 格式校验**

## 任务

### 1. `frontend/src/utils/validators.ts` (新增 IP)

- `IPV4_PATTERN` 严格 (0-255 各段)
- `IPV6_PATTERN` 简版 (8 组 16 进制, 允许 ::)
- `IP_PATTERN` 二选一
- `ipRules` (required + pattern)

### 2. `frontend/src/utils/validators.test.ts` (新建)

- IPV4: `192.168.1.1` / `10.0.0.1` / `255.255.255.255` PASS
- IPV4: `256.0.0.1` / `1.2.3` FAIL
- IPV6: `::1` / `fe80::1` PASS
- IP_PATTERN: 同时支持 v4 + v6
- urlRules / emailRules / portRules / arrayOfPatternRules snapshot

### 3. `frontend/src/components/AssetFormModal.tsx`

- ip_address 改用 `ipRules`

### 4. `frontend/src/components/AssetFormModal.test.tsx` (新建)

- 填 `192.168.1.1` → 通过
- 填 `256.0.0.1` → 显示 IP 格式错误
- 填 `::1` → 通过

## Hard pass

- frontend tsc 0
- validators.test.ts PASS
- AssetFormModal.test.tsx PASS
- 全量 frontend 无退化
- mutation inversion 实证
- 双轨 graphify + codegraph
