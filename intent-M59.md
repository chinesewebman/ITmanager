# intent-M59: G-UI-SettingsValidators 集成 / 通知 form 字段校验 (F-1 摩擦)

## Context

审查发现 — `frontend/src/pages/Settings.tsx` 共 13 Form.Item, 16 rules (含重复计数). 9 字段只校验 required 不校验格式, 用户填错只能靠 backend 400 才知道, 反复提交才知道哪错了.

**真摩擦**:
- 3 集成 URL (Zabbix / NetBox / GLPI) 缺格式校验 — `zabbix:8080` (无协议) 通过前端
- 3 webhook URL (SMTP / 钉钉 / 企业微信 / generic webhook) 缺格式校验
- 2 邮箱字段 (`from` / `smtp_user` / `to`) 缺 email 校验
- SMTP 端口缺 1-65535 范围校验

## 任务

### 1. `frontend/src/pages/Settings.tsx`

- 提取共享 rules 到 module 顶层:
  ```ts
  const URL_PATTERN = /^https?:\/\/[^\s/$.?#].[^\s]*$/
  const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
  const urlRules = [{ required: true, message: '请输入 URL' }, { pattern: URL_PATTERN, message: 'URL 必须以 http:// 或 https:// 开头' }]
  const emailRules = [{ required: true, message: '请输入邮箱' }, { pattern: EMAIL_PATTERN, message: '邮箱格式不正确' }]
  const portRules = [{ required: true, message: '请输入端口' }, { type: 'integer', min: 1, max: 65535, message: '端口必须在 1-65535 之间' }]
  ```
- 9 字段改用共享 rules:
  - Zabbix URL / NetBox URL / GLPI URL → urlRules
  - SMTP port → portRules
  - SMTP from / SMTP user → emailRules
  - SMTP to (Select mode=tags) → 自定义 validator 每项 email
  - 钉钉 webhook_url / 企业微信 url / generic webhook URL → urlRules

### 2. `frontend/src/pages/Settings.test.tsx`

- 加 7+ 测试:
  - Zabbix URL `not-a-url` → 显示 "URL 必须以 http:// 或 https:// 开头"
  - Zabbix URL `http://zabbix:8080` → 通过校验
  - NetBox URL `https://netbox.local` → 通过
  - SMTP port 99999 → 显示 "端口必须在 1-65535 之间"
  - SMTP from `not-an-email` → 显示 "邮箱格式不正确"
  - SMTP to 加 `bad@@@` → 显示 "邮箱格式不正确"
  - 钉钉 webhook_url `ftp://...` → 显示 URL 格式错误

## Hard pass

- frontend `npx tsc --noEmit`: 0 error
- frontend Settings test: 全 PASS (含 M59 新测试)
- frontend 全量 vitest: 0 退化
- mutation inversion 实证
- 双轨 graphify + codegraph

## graph-tools verified

执行时间: 2026-09-15 00:XX
- graphify update
- graphify diagnose (0 missing/dangling)
- codegraph sync (stale → 强 re-index)
