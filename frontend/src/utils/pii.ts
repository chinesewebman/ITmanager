// PII 脱敏唯一出口（M75 / T-80）。
//
// 为什么这张模块存在：`/users` 页面把 `username` / `email` / `nickname` 直接打在表格里，
// 旁观者路过屏幕就能读到 —— 这是 admin 操作页常见反模式。需求：保留 admin 操作能力，
// 但表格里**默认脱敏**，需要原文时点一下再展开（不是 hover / tooltip —— 那种「鼠标一过就
// 显示全文」对屏幕录制、远程协助、身后看屏幕的人完全无效）。
//
// 脱敏口径（与后端契约无关）：
//   - email   `alice@example.com` → `a***@example.com`（保留首字符 + 域名）
//   - username `admin_42`         → `ad****42`（保留前 2 + 后 2，中间 len-4 星号）
//   - 短到无法两边各留 2 字符（< 5 字符）→ 全星号（不留线索）
//
// 不做：
//   - 不返回原文到 React state（防 devtools / React DevTools 误读）
//   - 不做"点一下全文高亮 5 秒"——截图就泄露，做不到
//   - 不改 API 契约（admin 该看原文，调用方拿到的是明文 —— 渲染层只决定怎么展示）
//   - 不引入第三方 mask 库（4 个 case 用不到）
//   - 不在 console.log 里打印原文（不在本模块范围内，但调用方注意）

const MASK_CHAR = '*'

/**
 * 邮箱脱敏。`alice@example.com` → `a***@example.com`.
 * 输入校验：不是合法邮箱格式 → 返回原文（**不**就地抛错 —— 渲染层无 try/catch）。
 */
export function maskEmail(email: string): string {
  if (!email) return ''
  if (!email.includes('@')) return email
  const [local, domain] = email.split('@')
  if (!local || !domain) return email
  if (local.length === 0) return email
  if (local.length === 1) return `${local}${MASK_CHAR.repeat(3)}@${domain}`
  return `${local[0]}${MASK_CHAR.repeat(3)}@${domain}`
}

/**
 * 用户名脱敏。`admin_42` → `ad****42`（前 2 + 后 2，中间 len-4 个星号）.
 * < 5 字符 → 全星号（不留线索）。
 * 输入校验：空 / null / undefined / 数字 → 空字符串（防御性）；非空字符串走脱敏。
 */
export function maskUsername(username: string): string {
  if (!username || typeof username !== 'string') return ''
  const len = username.length
  if (len < 5) return MASK_CHAR.repeat(Math.max(len, 3))
  const head = username.slice(0, 2)
  const tail = username.slice(-2)
  return `${head}${MASK_CHAR.repeat(len - 4)}${tail}`
}
