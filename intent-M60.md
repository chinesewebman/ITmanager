# intent-M60: G-Utils-ValidatorsShared + G-BE-HttpsWhitelist (T-56/T-71 follow-up)

## Context

M59 follow-up 留下的两项技术债:
- **T-56**: gin `binding:"url"` 不强制 http(s), 接受 `ftp://`
- **T-71**: 前端 URL/EMAIL patterns 在 Settings.tsx, 应提 utils 复用

## 任务

### 1. `frontend/src/utils/validators.ts` (新建)

- `URL_PATTERN` / `EMAIL_PATTERN` / `urlRules` / `emailRules` / `portRules`
- `arrayOfPatternRules(pattern, label)` helper (T-71 修)

### 2. `frontend/src/pages/Settings.tsx`

- 删除 module 顶层 pattern/rules 定义
- 改 import from `../utils/validators`

### 3. `frontend/src/pages/Settings.test.tsx`

- import 路径迁移
- 58 测试不退化

### 4. `backend/internal/api/handlers/integration_handler.go`

- 加 `requireHTTPScheme` helper (只 http/https)
- 3 个 Update* handler 入口加 scheme 校验

### 5. `backend/internal/api/handlers/integration_handler_test.go`

- 表驱动测试 9 sub-cases: 3 endpoint × 3 bad scheme (ftp/file/ssh)

## Hard pass

- frontend tsc 0 + Settings 58/58 PASS
- backend 27 packages ok
- mutation inversion 实证 (bypass scheme check → 9 fail)
- 双轨 graphify + codegraph
