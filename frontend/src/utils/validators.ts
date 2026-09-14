// 表单格式规则唯一出口（M60/T-71）。
//
// 此前 URL / email / port 规则住在 `pages/Settings.tsx` module 顶层：同页 10 处 Form.Item 复用
// 是没问题，但别的页面（Oncall URL / Runbook webhook / AssetForm IP）要么再抄一份，要么
// 「import 一个 page 文件里的常量」—— 后者会让路由级页面成为工具模块的依赖，改页面就动全局。
//
// 规则提到这里后：pattern 只有一份（改一处不会漏另一处），页面只 import 规则本身。

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
