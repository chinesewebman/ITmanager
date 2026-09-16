# M75 Completion Report — PII 脱敏 Users.tsx

> **Loop cycle**: 7 of `itmanager-grit-2026q3`
> **Feat**: (commit pending)
> **Intent**: `intent-M75.md` (in same commit)

## 摩擦

`/users` admin 页表格里 `username` / `email` 直接打明文. 旁观者路过屏幕 / 远程协助 /
屏幕录制就泄露 PII — admin 操作页常见反模式. M61 TODO L21 已登记.

## 改动 (frontend-only, ≤2h)

| 文件 | 改动 |
|---|---|
| `frontend/src/utils/pii.ts` | **新建** 2.4KB, 导出 `maskEmail` + `maskUsername` |
| `frontend/src/utils/pii.test.ts` | **新建** 14 cases |
| `frontend/src/pages/Users.tsx` | import + 表格列 (2 处) + Popconfirm (4 处) 全部走脱敏 |
| `frontend/src/pages/Users.test.tsx` | 14 assertion 改 masked 期望 |

## 脱敏口径

- **email** `alice@example.com` → `a***@example.com` (保留首字符 + 域名)
- **username** `admin_42` (len 8) → `ad****42` (前 2 + 后 2 + len-4 星号)
- **短到无法两边各留 2 字符 (< 5 字符)** → 全星号 (不留线索)

## 不做

- **不改 API 契约**: admin 该看原文, 调用方拿到的是明文 — 渲染层只决定怎么展示
- **不返回原文到 React state**: 防 devtools / React DevTools 误读
- **不做"hover tooltip 显示全文"**: 那对屏幕录制 / 远程协助无效
- **不引入第三方 mask 库**: 4 case 没必要
- **不在 console.log 打印原文**: 调用方注意, 不在本模块范围

## Verify

- `vitest run src/utils/pii.test.ts` **14/14 PASS** ✓
- `vitest run src/pages/Users.test.tsx` **14/14 PASS** ✓
- `tsc --noEmit` **0 错** ✓
- **mutation inversion 12 red**: revert maskUsername in cell → 12 tests FAIL ✓
- **mutation inversion 4 red**: revert maskUsername function → 4 pii tests FAIL ✓

## 决策点

- **D1**: 默认脱敏, 不暴露"展开原文"按钮 — admin 通过 API 拿明文即可 ✓
- **D2**: < 5 字符全星号, 不留线索 ✓
- **D3**: email 单字符 local 1+3 星号 ✓
- **D4**: 不动 API 契约 ✓
- **D5**: T-80 唯一出口 utils/pii, 不在 page 内联 ✓

## T-80 (新 trap, M75 派生)

渲染层脱敏必须走 `utils/pii` 唯一出口. 不要在 page 内联字符串拼接
(`v.slice(0,2) + '*'.repeat(...)` 是漂移源头 — 后续若调口径, 散在 page 里改不全).
