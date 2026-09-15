// 表单格式规则唯一出口（M60/T-71）。
//
// 此前 URL / email / port 规则住在 `pages/Settings.tsx` module 顶层：同页 10 处 Form.Item 复用
// 是没问题，但别的页面（Oncall URL / Runbook webhook / AssetForm IP）要么再抄一份，要么
// 「import 一个 page 文件里的常量」—— 后者会让路由级页面成为工具模块的依赖，改页面就动全局。
//
// 规则提到这里后：pattern 只有一份（改一处不会漏另一处），页面只 import 规则本身。
//
// M62 追加 IP 规则：`AssetFormModal` 此前内联 `/^(\d{1,3}\.){3}\d{1,3}$/` —— 那是**形状**检查
// 不是**地址**检查（`256.0.0.1` / `999.999.999.999` 一路放行），而 IP 是运维拿去 ping / 连 SNMP
// 的定位键，填错的代价是「现场找不到设备」而不是「存不下」。

// URL_PATTERN 刻意允许**无 TLD 的内网地址**（`http://zabbix:8080`）：集成目标通常是内网
// 主机名或 IP，用「必须有 TLD」的写法会把合法配置挡在门外 —— 那比「少校验一点」严重得多。
// 只要求 scheme ∈ {http, https} + 非空无空白的剩余部分。
export const URL_PATTERN = /^https?:\/\/[^\s/$.?#].[^\s]*$/
export const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export const urlRules = [
  { required: true, message: '请输入 URL' },
  { pattern: URL_PATTERN, message: 'URL 必须以 http:// 或 https:// 开头' },
]
export const emailRules = [
  { required: true, message: '请输入邮箱' },
  { pattern: EMAIL_PATTERN, message: '邮箱格式不正确' },
]
export const portRules = [
  { required: true, message: '请输入端口' },
  { type: 'integer' as const, min: 1, max: 65535, message: '端口必须在 1-65535 之间' },
]

// ---------------------------------------------------------------- IP（M62）

/** 一段 IPv4：0-255（25x / 2[0-4]x / 0-199）。 */
const OCTET = '(?:25[0-5]|2[0-4]\\d|[01]?\\d\\d?)'
/** 一组 IPv6 16 进制（1-4 位）。 */
const HEX_GROUP = '[0-9a-fA-F]{1,4}'

const IPV4_BODY = `(?:${OCTET}\\.){3}${OCTET}`

/**
 * IPv6 的合法形状：全写 1 种 + 带 `::` 的 9 种（RFC 4291 §2.2，「压缩点两侧各有几组」）。
 *
 * 写成数组而不是一个 500 字符的单行交替式 —— 单行版每加一条（比如将来的 IPv4-mapped
 * `::ffff:1.2.3.4`）都要先肉眼解码整串，而这个族里最容易改错的恰是「压缩点两侧的组数上界」。
 *
 * 每条**不带**自己的 `^` / `$`，边界由外层统一的 `^(?:…)$` 兜住：把 10 条交替式压成单行字面量时，
 * 少写一个 `^` 就会让整个 pattern 在 standalone 使用下退化成**子串**匹配 —— 实测单行版
 * `IPV6_PATTERN.test('zz::1')` / `':::'` 均为 true（其中两条交替式缺 `^`/`$`）。
 */
const IPV6_BODIES = [
  `(?:${HEX_GROUP}:){7}${HEX_GROUP}`, // 8 组全写：2001:db8:0:0:0:0:0:1
  `(?:${HEX_GROUP}:){1,7}:`, // 左 N 组 + 尾部 ::：fe80::
  `(?:${HEX_GROUP}:){1,6}:${HEX_GROUP}`, // 左 N 组 + :: + 右 1 组：fe80::1
  `(?:${HEX_GROUP}:){1,5}(?::${HEX_GROUP}){1,2}`,
  `(?:${HEX_GROUP}:){1,4}(?::${HEX_GROUP}){1,3}`,
  `(?:${HEX_GROUP}:){1,3}(?::${HEX_GROUP}){1,4}`,
  `(?:${HEX_GROUP}:){1,2}(?::${HEX_GROUP}){1,5}`,
  `${HEX_GROUP}:(?::${HEX_GROUP}){1,6}`, // 左 1 组 + :: + 右 N 组
  `:(?::${HEX_GROUP}){1,7}`, // 左 0 组：::1 / ::1:2
  '::', // 全压缩
]
const IPV6_BODY = `(?:${IPV6_BODIES.join('|')})`

/** IPv4 严格：四段 0-255（内网 10./172.16-31./192.168. 天然在其值域内）。 */
export const IPV4_PATTERN = new RegExp(`^${IPV4_BODY}$`)
/** IPv6 简版：8 组 16 进制 + `::` 压缩。不含 zone id（`fe80::1%eth0`）与 CIDR。 */
export const IPV6_PATTERN = new RegExp(`^${IPV6_BODY}$`)
/** 二选一（资产 IP 允许 v4 或 v6）。 */
export const IP_PATTERN = new RegExp(`^(?:${IPV4_BODY}|${IPV6_BODY})$`)

export const ipRules = [
  { required: true, message: '请输入 IP 地址' },
  { pattern: IP_PATTERN, message: 'IP 地址格式不正确 (IPv4: 192.168.1.1, IPv6: ::1)' },
]

/**
 * tags 数组（`Select mode="tags"`）专用的逐项校验（T-71）。
 *
 * antd 的 `{ pattern }` 规则只作用于**字符串**值，对数组**静默不生效** —— 规则看着在、
 * 永不触发，无报错无警告。这类「写了但不起作用」的校验比「没写」更难发现，故必须自定义 validator。
 *
 * - 非数组值直接放行：空值由 `required` 规则负责（它认得「空数组 = 空」）。
 * - `requiredMessage` 可覆盖必填文案（如收件人字段要显示「请输入收件人」，而格式错误仍说「邮箱」）。
 */
export const arrayOfPatternRules = (
  pattern: RegExp,
  label: string,
  requiredMessage = `请输入${label}`,
) => [
  { required: true, message: requiredMessage },
  {
    validator: (_: unknown, value: unknown) => {
      if (!Array.isArray(value)) return Promise.resolve()
      return value.some((v) => !pattern.test(String(v).trim()))
        ? Promise.reject(new Error(`${label}格式不正确`))
        : Promise.resolve()
    },
  },
]
