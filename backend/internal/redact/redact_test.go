package redact

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// ==================== URL ====================

func TestURL_只留scheme和host(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"钉钉 query token", "https://oapi.dingtalk.com/robot/send?access_token=SECRET", "https://oapi.dingtalk.com"},
		{"Slack path token", "https://hooks.slack.com/services/T000/B000/SECRETPATH", "https://hooks.slack.com"},
		{"飞书 hook token", "https://open.feishu.cn/open-apis/bot/v2/hook/abcdef-1234", "https://open.feishu.cn"},
		{"userinfo", "https://user:pw@example.com/api?token=x", "https://example.com"},
		{"带端口", "http://127.0.0.1:8080/hook", "http://127.0.0.1:8080"},
		{"IPv6 带端口", "http://[::1]:8080/hook", "http://[::1]:8080"},
		{"空串", "", invalidURL},
		{"解析失败", "http://[::1", invalidURL},
		{"无 host", "mailto:someone@example.com", invalidURL},
		// 审计 M-2：配置漏写 scheme 的 URL，Go 会把原串塞进 *url.Error → 也要塌缩。
		{"scheme-relative", "//example.com/path", "//example.com"},
		{"scheme-relative 带 token 路径", "//127.0.0.1:1/hooks/T/B/SECRET", "//127.0.0.1:1"},
		// 审计 M-3：userinfo 没有密码时 net/url 把它并进 Host，原样返回等于回显凭据。
		{"userinfo 无密码", "https://user:", invalidURL},
		{"host 尾冒号", "http://example.com:/api", invalidURL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, URL(c.in))
		})
	}
}

// ==================== Text ====================

func TestText_替换凭据值(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"URL path token（飞书/Slack 形态）",
			`Post "https://hooks.slack.com/services/T000/B000/SECRETPATH": dial tcp: refused`,
			`Post "https://hooks.slack.com": dial tcp: refused`,
		},
		{
			"URL query token（钉钉形态）",
			`Post "https://oapi.dingtalk.com/robot/send?access_token=SECRET": EOF`,
			`Post "https://oapi.dingtalk.com": EOF`,
		},
		{
			"URL userinfo",
			"zabbix: http://user:pw@example.com/api_jsonrpc.php",
			"zabbix: http://example.com",
		},
		{
			"Authorization 头",
			"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.PAYLOAD",
			"Authorization: Bearer ***",
		},
		{
			"Authorization Basic 头",
			"authorization: basic dXNlcjpwYXNz",
			"authorization: basic ***",
		},
		{
			"query 键值（保留非敏感参数）",
			"channel config: access_token=SECRET&page=2",
			"channel config: access_token=***&page=2",
		},
		{
			"JSON 形态",
			`{"smtp_password":"SECRET","host":"x"}`,
			`{"smtp_password":"***","host":"x"}`,
		},
		{
			"冒号形态",
			"api_key: SECRET",
			"api_key: ***",
		},
		{
			"大小写混合",
			"Access_Token=SECRET",
			"Access_Token=***",
		},
		{
			"值含 $ 不破坏替换",
			"token=ab$1cd",
			"token=***",
		},
		{
			"x-webhook-secret",
			"X-Webhook-Secret: SECRET",
			"X-Webhook-Secret: ***",
		},
		{
			"带前缀/后缀的组合名",
			"secret_key=SECRET&csrf_token=T2&access_key_id=AKIA",
			"secret_key=***&csrf_token=***&access_key_id=***",
		},
		{
			"scheme-relative URL（审计 M-2）",
			`Post "//127.0.0.1:1/hooks/T/B/SECRETPATH": dial tcp: refused`,
			`Post "//127.0.0.1:1": dial tcp: refused`,
		},
		{
			// M-3：旧值类把 ' 当终点 → `AB` 之后全部留在文本里。现在整串一起塌缩。
			"单引号不再是值终点（审计 M-3）",
			`Post "http://127.0.0.1:1/api?auth=AB'CDEF": dial tcp: refused`,
			`Post "http://127.0.0.1:1": dial tcp: refused`,
		},
		{
			// 审计 P1：旧值类把 } ] < > 当终点 → JSON 回显只剩 `***}c"}`。
			"值含右花括号（审计 P1）",
			`{"password":"ab}c"}`,
			`{"password":"***"}`,
		},
		{
			// 审计 P1 最坏形态：值**以**分隔符开头时旧版整条不匹配 = 完全不脱敏。
			"值以右花括号开头（审计 P1）",
			"password=}SUPERSECRET",
			"password=***",
		},
		{
			"值被尖括号包裹（审计 P1）",
			"token=<SECRET>",
			"token=***",
		},
		{
			"Bearer 值含逗号（审计 P1）",
			"Authorization: Bearer abc,def",
			"Authorization: Bearer ***",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_不误伤正常文本 — R-1 的守门用例：脱敏必须只动凭据值。
func TestText_不误伤正常文本(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"SQL 约束名", `pq: duplicate key value violates unique constraint "users_username_key"`},
		{"非敏感 query", "GET /api/assets?page=2&limit=10"},
		{"design= 不是 sign=", "design=flat"},
		{"无 = 无 : 的普通错误", "context deadline exceeded"},
		{"空串", ""},
		{"monkey= 不是 key=", "monkey=banana"},
		{"primary key= 不脱敏", "pq: duplicate key value violates unique constraint (primary key=id)"},
		// 双斜杠路径不是 URL：authority 必须是「带点域名 / localhost / [IPv6]」。
		{"双斜杠路径", "open //data/memory.db: no such file"},
		{"注释里的双斜杠", "// 这里记录重试次数"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.in, Text(c.in), "非敏感文本必须原样返回")
		})
	}
}

// ==================== M30：值边界与相互咬合的不变量 ====================
//
// 本组是「先固化不变量、再改规则」的前半（M28/F 的交接前置，见
// docs/FIX-PLAN-REDACT-BOUNDARY.md §5.1）：断言的全部是**现状即绿**的行为，
// 目的是在动 redact.go 之前把不许改坏的东西钉死。
// 「漏」的修复项用例（改前应当红）随实现一起提交。

// TestText_边界锁定 — §1.4：修 G-35（URL 端口不再被规则 3 吃掉）时，这些输出一个字都不许动。
func TestText_边界锁定(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 引号形态必须**回写**（${1}${2}***）：F2 的引号组一旦写成非捕获，前三条立刻红。
		{"未闭合引号值", `token="S`, `token="***`},
		{"JSON 引号值", `password="SECRET"`, `password="***"`},
		{"JSON 花括号值（审计 P1：} ] 不收窄）", `{"password":"ab}c"}`, `{"password":"***"}`},
		{"冒号键名", `X-Api-Key: SECRET`, `X-Api-Key: ***`},
		{"连字符键名", `api-key=SECRET`, `api-key=***`},
		{"值含右花括号", `password=ab}c`, `password=***`},
		{"值被方括号包裹", `password=[SECRET]`, `password=***`},
		{"值被圆括号包裹", `password=(SECRET)`, `password=***`},
		{"方括号值后跟裸字符", `password=[SECRET]x`, `password=***`},
		{"值尾的 & 不吃（非敏感参数保留）", `password=SECRET&x=1`, `password=***&x=1`},
		{"空 Bearer 值原样（§6 接受）", `Authorization: Bearer ""`, `Authorization: Bearer ""`},
		{"scheme-relative 且 host 不带点（§6 接受）", `//token:8080/x`, `//token:***`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_回归不变量 — §3.1「回归」行里现状即绿的部分。
// 前三条是**方案 A（回看 `://` 就跳过规则 3）被否决的依据**：它们现状是遮住的，
// 回看方案会把它们变成明文。谁要改回看，先看这三条。
func TestText_回归不变量(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"协议残缺的键值（方案 A 反例 1）", `://access_token=SECRET`, `://access_token=***`},
		{"协议残缺的键值（方案 A 反例 2）", `foo:://password=SECRET`, `foo:://password=***`},
		{"协议残缺的键值（方案 A 反例 3）", `x=://access_token=SECRET`, `x=://access_token=***`},
		{"query 里的 access_token", `?a=1&access_token=X`, `?a=1&access_token=***`},
		{"Authorization Basic", `Authorization: Basic dXNlcjpwYXNz`, `Authorization: Basic ***`},
		{"Bearer 值以分隔符开头", `Authorization: Bearer &SECRET`, `Authorization: Bearer ***`},
		{"普通 host 带端口（对照：不该脱敏）", `http://host:8080/x`, `http://host:8080`},
		{"敏感词后跟裸字母（对照：不该脱敏）", `http://tokens:8080/x`, `http://tokens:8080`},
		{"userinfo 整体丢弃", `http://user:pass@host/x`, `http://host`},
		{"中文与 URL 混排不切坏字符", `中文前缀 http://host:8080/x 中文后缀 password=SECRET`,
			`中文前缀 http://host:8080 中文后缀 password=***`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_已知残余_钉住 — §6 登记的**未修**残余。
//
// 这些断言写的是**已知泄漏/已知不命中**，不是「期望行为」。钉住它们的目的：
// ① 让残余可见（不会因为「没人知道」而被当成已修）；② 谁把它们修了，这里会红，
// 从而必须同步 TODO / TRAPS（结构化的那条对应 TODO **G-46**）。
func TestText_已知残余_钉住(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			// G-46：嵌套值需要嵌套匹配，属「不换状态机」的范围（§6）。对象形态
			// `{"password":{"a":"SECRET"}}` 同样漏，此处只钉数组形态作代表。
			"结构化值内的元素不遮盖（G-46）",
			`{"password":["SECRET"]}`,
			`{"password":***"SECRET"]}`,
		},
		{
			// 值**中间**的引号把值类截断，尾巴留明文。收窄「引号只在值首起作用」
			// 会与 §1.4 的「引号形态回写」冲突，故登记不修。
			"值中间的引号截断",
			`password=SEC"RET`,
			`password=***"RET`,
		},
		{
			// 键词必须落在 `-`/`_` 分段边界上（与注释里的 design=/monkey= 同一取舍）。
			// 同族的还有 `_password=SECRET`、`1password=SECRET`（实测同样不命中）。
			"键名不落在 -/_ 边界（clientsecret）",
			`clientsecret=SECRET`,
			`clientsecret=SECRET`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_修复项_引号与分隔符边界 — M30 §1.1 的 L1–L12（F1 / F2 / F4）。
//
// 这些用例在改 redact.go 之前**全是红的**（§5.1 的「用例真的钉住了缺陷」证明）。
func TestText_修复项_引号与分隔符边界(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// F1：规则 2 的值类补「任意个起始引号」。L1 是本次最重要的修正 ——
		// 闭合引号的凭据（从配置文件复制粘贴出来的常态）此前整条明文。
		{"L1 闭合双引号", `Authorization: Bearer "SECRET"`, `Authorization: Bearer ***"`},
		{"L2 小写 bearer", `authorization: bearer "SECRET"`, `authorization: bearer ***"`},
		{"L3 两个空格", "Authorization: Bearer  \"SECRET\"", "Authorization: Bearer  ***\""},
		{"L4 Tab 分隔", "Authorization: Bearer\t\"SECRET\"", "Authorization: Bearer\t***\""},
		{"L5 未闭合双引号", `Authorization: Bearer "SECRET`, `Authorization: Bearer ***`},
		{"L6 未闭合单引号", `Authorization: Bearer 'SECRET`, `Authorization: Bearer ***`},
		{"两个引号（量词用 * 而非 ?）", `Authorization: Bearer ""SECRET`, `Authorization: Bearer ***`},
		// 引号内含空格：现状整条明文，修后至少遮住首段（尾部残留见 §6 残余表）。
		{"引号内含空格", `Authorization: Bearer "SECRET VALUE"`, `Authorization: Bearer *** VALUE"`},
		// F2：规则 3 的值类补「起引号」（捕获回写）与「起分隔符」（吞到空白）。
		{"L7 值以 & 开头", `password=&SECRET`, `password=***`},
		{"L8 值以 ; 开头", `password=;SECRET`, `password=***`},
		{"L9 值以 , 开头", `password=,SECRET`, `password=***`},
		{"L10 两个起引号", `password=""abc`, `password=""***`},
		{"L11 空格后单双引号", `password= '"SECRET'`, `password= '"***'`},
		{"三个起引号", `password='''SECRET`, `password='''***`},
		{"空值也遮（无泄漏）", `password=&`, `password=***`},
		// 「吞到空白」而不是「只吞一个 token」：否则会吃掉下一个键名却留下它的值。
		{"吞到空白，不产生 =SECRET 明文", `password=&next=SECRET`, `password=***`},
		{"行为突变：值以分隔符开头时吃掉后续参数", `?a=1&password=&b=2`, `?a=1&password=***`},
		// F4：全角冒号（中文 IME 的默认冒号）。
		{"L12 全角冒号", `password：SECRET`, `password：***`},
		{"全角冒号 token", `token：SECRET`, `token：***`},
		// 全角等号与全角冒号同源（中文输入法全角模式），收一个不收另一个是自相矛盾：
		// 若只收 `：`，`password＝SECRET` 就是同一句话换个字符的明文。
		{"全角等号", `password＝SECRET`, `password＝***`},
		{"全角等号 token", `token＝SECRET`, `token＝***`},
		// F4b：连续分隔符。只收一个分隔符时，多出来的那个落进值类把引号组挤空 —— 与 F2 的
		// `password=""abc`（L10）是同一族的漏，F2 修了那个却留着这个就是自相矛盾。
		// 差分跑 11520 组才现形：`password=="SECRET"` 此前 → `password=***"SECRET"`（明文）。
		{"重复分隔符 + 引号形态", `password=="SECRET"`, `password=="***"`},
		{"重复分隔符 + 分隔符起头", `password==&SECRET`, `password==***`},
		// 行为突变（已告知）：键名后保留整段分隔符，此前是 `password=***`。遮盖性不变。
		{"行为突变：连续分隔符整体保留", `password==SECRET`, `password==***`},
		// F4 的代价（§6 行为突变②）：中文无词间空格，整句被吞。这条是**接受**的，写出来防止被当成 bug 又改回去。
		{"行为突变：中文整句被吞（接受）", `重置 password：请联系管理员`, `重置 password：***`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_修复项_URL端口保留与host凭据 — M30 §1.2 的 G-35 全谱（F3 + F3b）。
//
// F3：URL 段只过 URL()，规则 3 不再把 `host:port` 的端口当键值吃掉。
// F3b：分段的必要条件是「URL() 的输出不含凭据形状」—— host 里的 `=` 是缺口，
// 不加 F3b 就是净回归（现状遮住 → 分段后明文）。
func TestText_修复项_URL端口保留与host凭据(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"敏感词当 host：端口保留", `http://token:8080/x`, `http://token:8080`},
		{"无路径也保留端口", `http://secret:8080`, `http://secret:8080`},
		{"带连字符前缀的 host", `http://my-token:8080/x`, `http://my-token:8080`},
		{"非 http scheme", `redis://pwd:6379/0`, `redis://pwd:6379`},
		{"下划线组合的 host", `http://access_token:8080/x`, `http://access_token:8080`},
		{"query 里的 token 随塌缩一起丢", `https://pwd:443/a?token=abc`, `https://pwd:443`},
		{"端口与 query 同时存在", `http://token:8080/x?password=SECRET`, `http://token:8080`},
		{"URL 段与非 URL 段并存", `http://token:8080/x password=abc&y=1`, `http://token:8080 password=***&y=1`},
		{"中文与 URL 混排", `中文前缀 http://token:8080/x 中文后缀 password=SECRET`,
			`中文前缀 http://token:8080 中文后缀 password=***`},
		// F3b：host 本身是 `key=value` 形态 → 不是真实主机，按 invalidURL 处置
		//（与既有的 `host:` 尾冒号判据同构：宁可丢信息，也不回显原串）。
		{"host 含 = 的绝对 URL", `http://access_token=SECRET`, `<invalid-url>`},
		{"host 含 = 且带端口", `http://access_token=SECRET:8080/x`, `<invalid-url>`},
		{"host 含 = 的非 http scheme", `redis://access_token=SECRET/0`, `<invalid-url>`},
		{"err.Error() 里的 host 含 =", `Get "http://access_token=SECRET": dial tcp: timeout`,
			`Get "<invalid-url>": dial tcp: timeout`},
		// F3b 的拒绝集必须与规则 3 的分隔符类**同集**（F4 收全角，这里就得跟着收）：
		// 只挡半角 `=` 的话，`http://access_token：SECRET/x` 这种「F4 会在纯文本里遮住、
		// 却由 URL() 原样回显」的形状就留下来了 —— URL() 输出的形状必须自己就可信。
		{"host 含全角冒号", `http://access_token：SECRET/x`, `<invalid-url>`},
		{"host 含全角等号", `http://access_token＝SECRET/x`, `<invalid-url>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Text(c.in))
		})
	}
}

// TestText_修复项_值本身是一个URL — F3 分段的**净回归**修复（M30 审计两路独立报出）。
//
// 缺陷机制：分段把 URL 从规则 3 的视野里拿走。旧版是三次 ReplaceAll 顺序执行，规则 3
// 看得见规则 1 的**输出**（一个塌缩后的 URL），于是 `<敏感键>=<URL>` 的值被整段吞成 `***`；
// 分段后非 URL 段只剩悬空的 `password=`（无值 → 不匹配），URL 段又只过 URL()，
// 这个值就退回明文：`password=https://example.com` → `password=https://example.com`。
//
// 修法不是「让规则 3 再看 URL」，而是**这种形态不切分**：段尾停在凭据前缀上时，
// 紧跟的 URL 就是那个值，让它留在非 URL 段里被规则 2/3 连值一起吞（见 danglingCredRe）。
// 所以这组用例同时钉住两件事：值被整体遮盖、且遮盖后**不留 host 残影**。
func TestText_修复项_值本身是一个URL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"键值形态的值是 https URL", `password=https://example.com`, `password=***`},
		{"键值形态的值是 http URL 带端口", `token=http://token:8080/x`, `token=***`},
		{"JSON 里的 URL 值", `{"password":"https://s.com","user":"a"}`, `{"password":"***","user":"a"}`},
		{"值带 query 的 URL", `access_token=https://example.com/x?token=SECRET`, `access_token=***`},
		{"URL 带 userinfo", `password: https://user:pw@host/x`, `password: ***`},
		{"scheme-relative URL 值", `secret=//example.com/x`, `secret=***`},
		{"值被引号包裹的 URL", `password="http://x.com"`, `password="***"`},
		{"值以分隔符开头的 URL", `password=&http://x.com`, `password=***`},
		{"Bearer 的值是 URL", `Authorization: Bearer http://SECRET/x`, `Authorization: Bearer ***`},
		// 下面三行是**第二族**净回归（差分跑 11520 组才现形，第一版修法漏了它们）：
		// 重复分隔符与「值中间没有空白」时，前缀模式与规则 3 的值类不同集 → 退回明文。
		{"重复分隔符", `password==http://SECRET/x`, `password==***`},
		// `=` 落在值类里（值类只排除空白/引号/&,;），所以它跟着值一起被替换掉。
		{"冒号加等号", `token:=http://SECRET/x`, `token:=***`},
		{"值中间无空白", `password=abc$http://SECRET/x`, `password=***`},
		// 反向守门：前缀不在段尾（后面还有别的字符）时**不该**因此改变既有行为。
		{"值不是 URL 时不受影响", `password=abc http://host:8080/x`, `password=*** http://host:8080`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Text(c.in)
			assert.Equal(t, c.want, out)
			// 无论哪种形态，值里的凭据都不许以任何残片形式留下。
			assert.NotContains(t, out, "SECRET", "值里的凭据不得留残影")
		})
	}
}

// TestText_URL段不参与规则23 — §5.2 的守门用例（防回归，独立于修复项）。
//
// 若有人把 Text 改回「三次 ReplaceAll 顺序执行」，端口会重新消失；
// 若有人改用「回看 :// 就跳过规则 3」，后两条会变成明文。两种退化都在这里红。
func TestText_URL段不参与规则23(t *testing.T) {
	// 三段拼接：URL 段（含敏感词 host）、协议残缺段、host 含 = 段。
	out := Text(`http://token:8080/x | ://access_token=SECRET | http://access_token=SECRET`)
	assert.Contains(t, out, `http://token:8080`, "URL 段的端口必须保留（规则 3 不得看见 URL）")
	assert.Contains(t, out, `://access_token=***`, "协议残缺段不是 URL，规则 3 必须照常遮盖")
	assert.NotContains(t, out, "SECRET", "host 含 = 的形态不得回显原串")

	// userinfo 由塌缩直接丢弃，不依赖规则 3。
	assert.Equal(t, `http://host`, Text(`http://user:pass@host/x`))
}

// ==================== StripControl ====================

func TestStripControl_删控制字符保留可打印(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"CRLF 伪造行", "a\r\n[FAKE] forged", "a[FAKE] forged"},
		{"NUL 与 DEL", "a\x00b\x7fc", "abc"},
		{"HTAB 也删（既定口径，见函数注释）", "a\tb", "ab"},
		{"其余 C0", "\x01\x02\x1f", ""},
		{"保留可打印 ASCII", "GET /api/assets?page=2", "GET /api/assets?page=2"},
		{"保留 CJK", "资产 中文名", "资产 中文名"},
		{"保留 emoji", "🚨 告警", "🚨 告警"},
		{"空串", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, StripControl(c.in))
		})
	}
}

// TestStripControl_输出必为合法UTF8 — 这是「关掉 PG 22021」的依据：
// 非法字节经 rune 迭代变成 U+FFFD，落库不再被拒。
func TestStripControl_输出必为合法UTF8(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"孤立高位字节", "abc\xff\xfe"},
		{"被切断的多字节字符", "中"[0:1] + "文"},
		{"截断的 emoji", "🚨"[0:2]},
		{"纯非法字节", "\x80\x81"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := StripControl(c.in)
			assert.True(t, utf8.ValidString(out), "输出必须是合法 UTF-8，实际 %q", out)
		})
	}
}

// TestStripControl_必须先Strip再Text — M29 §1.4 的顺序反例。
//
// 这不是「风格」问题：控制字符会截断 Text 的值类，先 Text 后 Strip 等于把被切开的
// 凭据尾部接回去。此用例把两个方向都钉住，防止后人「顺手合并成一行」时改回错误顺序
// （notification.markFailed 就是这么漏的）。
func TestStripControl_必须先Strip再Text(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		leaked string // 错误顺序（Text→Strip）下会明文出现的尾巴
	}{
		{"值被换行切开", "password=YWJjZGVmZ2hp\namtsbW5vcHFy", "amtsbW5vcHFy"},
		{"键被换行切开", "pass\nword=SECRET", "SECRET"},
		{"Authorization 被切开", "Authorization: Bearer SEC\nRET", "RET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 正确顺序：先剥控制字符，凭据完整 → 完整遮盖。
			right := Text(StripControl(c.in))
			assert.NotContains(t, right, c.leaked, "先 Strip 再 Text 不得残留凭据尾巴")

			// 错误顺序确实会泄漏 —— 若这条不再成立，说明 Text 的规则变了，
			// 需要重新评估函数注释里的「顺序是安全边界」是否还准确。
			wrong := StripControl(Text(c.in))
			assert.Contains(t, wrong, c.leaked,
				"反例失效：Text 已不再被控制字符截断，请复核 StripControl 的注释")
			assert.NotEqual(t, right, wrong, "两种顺序结果不同即证明顺序是语义的一部分")
		})
	}
}
