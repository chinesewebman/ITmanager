// M62：`utils/validators` 的模块级契约。
//
// 这里**不重复** M59 的 URL / EMAIL 正负样本表（那 16 条在 `pages/Settings.test.tsx`
// 的「M59 共享 pattern 边界」里已逐条钉住，搬家后未改一字）；本文件补的是三段此前
// 没有直接断言的东西：
//   ① M62 新增的 IP pattern 边界（`256.0.0.1` 这类「形状对、地址错」的值是这次修的对象）；
//   ② IP 规则对象本身 —— 消费方（Form.Item）拿到的是规则数组，不是 pattern；
//   ③ M60 的 4 组既有规则对象（urlRules / emailRules / portRules / arrayOfPatternRules）
//      的文案与约束 —— 它们此前只在 UI 层被间接打到，模块级的文案漂移没人看得见。
import { describe, it, expect } from 'vitest'
import {
  IPV4_PATTERN,
  IPV6_PATTERN,
  IP_PATTERN,
  ipRules,
  URL_PATTERN,
  EMAIL_PATTERN,
  urlRules,
  emailRules,
  portRules,
  arrayOfPatternRules,
} from './validators'

describe('M62 IPV4_PATTERN 边界', () => {
  const ok = [
    '192.168.1.1', // 内网
    '10.0.0.1', // 内网
    '172.16.31.255', // 内网（172.16-31 段）
    '8.8.8.8', // 公网
    '255.255.255.255', // 上界
    '0.0.0.0',
  ]
  const bad = [
    '256.0.0.1', // 段越界 —— M62 前内联的 /^(\d{1,3}\.){3}\d{1,3}$/ 会放行这个
    '999.999.999.999',
    '1.2.3', // 段数不足
    '1.2.3.4.5', // 段数过多
    '192.168.1.', // 末段空
    '192.168.1.1 ', // 尾随空格
    '192.168.1.0/24', // CIDR 不在本轮范围（登记为 out of scope）
    '::1', // v4 pattern 不认 v6
    'not-an-ip',
    '',
  ]

  it.each(ok)('接受 %s', (ip) => {
    expect(IPV4_PATTERN.test(ip)).toBe(true)
  })
  it.each(bad)('拒绝 %s', (ip) => {
    expect(IPV4_PATTERN.test(ip)).toBe(false)
  })
})

describe('M62 IPV6_PATTERN 边界', () => {
  const ok = [
    '::1', // 回环
    '::',
    'fe80::', // 左侧 1 组 + 尾部 ::（单行字面量最容易漏掉 `$` 的那条）
    'fe80::1',
    '2001:db8::1',
    '2001:db8:0:0:0:0:0:1', // 8 组全写
    '2001:db8::8a2e:370:7334', // 压缩点两侧各多组
    '::1:2:3:4:5:6:7', // 左 0 组
  ]
  const bad = [
    'zz::1', // 子串匹配才会为 true（整串不是地址）
    ':::',
    'a::b::c', // 两处压缩
    '1:2:3:4:5:6:7:8:9', // 9 组
    '12345::1', // 组超 4 位
    'gggg::1', // 非 16 进制
    'fe80::1%eth0', // zone id 不在本轮范围
    '192.168.1.1', // v6 pattern 不认 v4
    '',
  ]

  it.each(ok)('接受 %s', (ip) => {
    expect(IPV6_PATTERN.test(ip)).toBe(true)
  })
  it.each(bad)('拒绝 %s', (ip) => {
    expect(IPV6_PATTERN.test(ip)).toBe(false)
  })
})

describe('M62 IP_PATTERN 同时收 v4 与 v6', () => {
  it.each(['192.168.1.1', '10.10.10.10', '::1', 'fe80::1', '2001:db8::1'])('接受 %s', (ip) => {
    expect(IP_PATTERN.test(ip)).toBe(true)
  })
  it.each(['256.0.0.1', '1.2.3', ':::', 'fe80::1%eth0', '192.168.1.0/24', 'not-an-ip', ''])(
    '拒绝 %s',
    (ip) => {
      expect(IP_PATTERN.test(ip)).toBe(false)
    },
  )
})

describe('M62 ipRules', () => {
  it('必填文案与格式文案各一条，顺序是 required 在前', () => {
    expect(ipRules).toHaveLength(2)
    expect(ipRules[0]).toMatchObject({ required: true, message: '请输入 IP 地址' })
    expect(ipRules[1].message).toBe('IP 地址格式不正确 (IPv4: 192.168.1.1, IPv6: ::1)')
  })

  it('格式规则吃的是导出的 IP_PATTERN（不是页面里另抄一份）', () => {
    const format = ipRules[1]
    if (!('pattern' in format) || !format.pattern) throw new Error('ipRules[1] 应当是带 pattern 的规则')
    expect(format.pattern).toBe(IP_PATTERN)
  })

  it('规则本身能挡住「形状对、地址错」的值', () => {
    const format = ipRules[1]
    if (!('pattern' in format) || !format.pattern) throw new Error('ipRules[1] 应当是带 pattern 的规则')
    expect(format.pattern.test('256.0.0.1')).toBe(false)
    expect(format.pattern.test('192.168.1.1')).toBe(true)
    expect(format.pattern.test('::1')).toBe(true)
  })
})

// M60 的 4 组规则对象：页面只 import 规则本身，故「规则长什么样」就是这一层的公开契约。
// 断的是文案 / 约束 / pattern 身份 —— 改一处（比如把 65535 写成 6553）必须在这里红。
describe('M60 既有规则集回归', () => {
  it('urlRules 必填 + http(s) pattern', () => {
    expect(urlRules).toHaveLength(2)
    expect(urlRules[0]).toMatchObject({ required: true, message: '请输入 URL' })
    expect(urlRules[1].message).toBe('URL 必须以 http:// 或 https:// 开头')
    const format = urlRules[1]
    if (!('pattern' in format) || !format.pattern) throw new Error('urlRules[1] 应当是带 pattern 的规则')
    expect(format.pattern).toBe(URL_PATTERN)
    expect(format.pattern.test('http://zabbix:8080')).toBe(true) // 内网无 TLD 必须继续放行
  })

  it('emailRules 必填 + email pattern', () => {
    expect(emailRules).toHaveLength(2)
    expect(emailRules[0]).toMatchObject({ required: true, message: '请输入邮箱' })
    expect(emailRules[1].message).toBe('邮箱格式不正确')
    const format = emailRules[1]
    if (!('pattern' in format) || !format.pattern) throw new Error('emailRules[1] 应当是带 pattern 的规则')
    expect(format.pattern).toBe(EMAIL_PATTERN)
    expect(format.pattern.test('nmp@example.com')).toBe(true)
  })

  it('portRules 是 1-65535 的整数（不是字符串长度之类的形状检查）', () => {
    expect(portRules).toHaveLength(2)
    expect(portRules[0]).toMatchObject({ required: true, message: '请输入端口' })
    expect(portRules[1]).toMatchObject({
      type: 'integer',
      min: 1,
      max: 65535,
      message: '端口必须在 1-65535 之间',
    })
  })

  describe('arrayOfPatternRules（数组字段专用，antd 的 pattern 规则对数组静默不生效 → T-71）', () => {
    const rules = arrayOfPatternRules(EMAIL_PATTERN, '邮箱')
    const required = rules[0]
    const item = rules[1]
    if (!('validator' in item) || !item.validator) throw new Error('第 2 条必须是自定义 validator')
    const run = (value: unknown) => item.validator({}, value) as Promise<void>

    it('必填文案默认是「请输入{label}」，可被第三参覆盖（收件人字段）', () => {
      expect(required.message).toBe('请输入邮箱')
      expect(arrayOfPatternRules(EMAIL_PATTERN, '邮箱', '请输入收件人')[0].message).toBe('请输入收件人')
    })

    it('数组里有一项不合法就拒绝，文案是「{label}格式不正确」', async () => {
      await expect(run(['nmp@example.com', 'not-an-email'])).rejects.toThrow('邮箱格式不正确')
    })

    it('全部合法（含首尾空白）时通过 —— 逐项 trim 后才判定', async () => {
      await expect(run(['nmp@example.com', '  ops@sub.example.co  '])).resolves.toBeUndefined()
    })

    it('非数组值直接放行（空值归 required 管）', async () => {
      await expect(run(undefined)).resolves.toBeUndefined()
      await expect(run('nmp@example.com')).resolves.toBeUndefined()
      await expect(run([])).resolves.toBeUndefined()
    })
  })
})
