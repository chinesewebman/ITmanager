# M75 — PII 脱敏（Users.tsx 表格 username/email 默认脱敏, OMH ulw-loop 第 7 cycle）

> **Loop cycle**: 7 of `itmanager-grit-2026q3`

## Goal

`/users` 页面 admin 操作页, 表格里 `username` / `email` 直接打明文. 旁观者路过屏幕 / 远程协助 / 屏幕录制就泄露 PII.
钉默认脱敏口径, admin 操作能力不受影响 (API 仍返明文, 渲染层只决定怎么展示).

## Non-goals

- 不改 API 契约 (admin 该看原文, 调用方拿到的是明文 — 渲染层只决定怎么展示)
- 不返回原文到 React state (防 devtools / React DevTools 误读)
- 不做"hover tooltip 显示全文" (那对屏幕录制 / 远程协助无效)
- 不引入第三方 mask 库 (4 case 没必要)
- 不在 console.log 打印原文 (调用方注意, 不在本模块范围)

## Assumptions

- 脱敏是**渲染层唯一掩码**, 不进 React state
- admin 通过 API 拿明文不影响 (运维处置需要原文, e.g. "禁用 alice@example.com 这个账号")
- `maskUsername` < 5 字符 → 全星号 (不留线索, 短用户名本身就稀有信号)
- `maskEmail` 单字符 local → 1 字符 + 3 星号 (保留最少可识别度)
- T-80 (新 trap): 渲染层脱敏必须走 utils/pii 唯一出口, 不在 page 内联字符串

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| 新文件 `frontend/src/utils/pii.ts` 导出 maskEmail + maskUsername | import verify |
| 新文件 `frontend/src/utils/pii.test.ts` ≥ 14 cases | 14 PASS |
| Users.tsx 表格 username 列走 maskUsername | mutation inversion 12 red |
| Users.tsx 表格 email 列走 maskEmail | mutation inversion (covered in 12 red) |
| Users.tsx Popconfirm (role/status/force-change) 走 maskUsername | mutation inversion (covered) |
| 现有 Users.test.tsx 14 cases 改用 masked 期望 | 14 PASS |
| tsc --noEmit 0 错 | verify |
| 不动 API 契约 | 验证 (没改 handlers / models) |
| fact_store fact_id=15 | shipped record |

## Verification

- `vitest run src/utils/pii.test.ts` 14/14 PASS
- `vitest run src/pages/Users.test.tsx` 14/14 PASS
- `tsc --noEmit` 0 错
- **mutation inversion red**:
  - revert maskUsername in cell → 12 tests FAIL (red)
  - revert maskUsername to return original → 4 pii.test FAIL (red)

## Risks

- **T-80 (新 trap)**: 渲染层脱敏必须走 utils/pii. 不要在 page 内联字符串拼接 (`v.slice(0,2) + '*'.repeat(...)` 是漂移源头)
- **API 不返脱敏**: 如果后续要 API 也脱敏 (e.g. 列表 endpoint), 那是 backend round, 不在本 round
- **mutation inversion 在 popconfirm 也红**: 已经验 — 12 tests fail (含 popconfirm textContent 检查)

## Plan

1. 写 `utils/pii.ts` (~2.4KB) maskEmail + maskUsername
2. 写 `utils/pii.test.ts` 14 cases (含 mutation 反证)
3. Users.tsx import maskEmail + maskUsername + 应用到表格列 (2 处) + Popconfirm (4 处)
4. Users.test.tsx 改 14 assertion 期望 (masked 文本)
5. vitest verify 28 PASS + tsc 0 错 + mutation inversion 12 red
6. fact_store fact_id=15 + docs commit

## Decision gate

- **D1**: 默认脱敏, 不暴露"展开原文"按钮 — admin 通过 API 拿明文即可 ✓
- **D2**: < 5 字符全星号, 不留线索 (短用户名本身就是稀有信号) ✓
- **D3**: email 单字符 local 1+3 星号, 保留最少可识别度 ✓
- **D4**: 不动 API 契约 (admin 该看原文) ✓
- **D5**: T-80 唯一出口 utils/pii, 不在 page 内联 ✓
