// Package redact 从文本里剥离凭据，供日志 / 数据库 / HTTP 响应等出口使用。
//
// 动机（TODO G-28）：URL 是凭据载体——钉钉的 access_token 在 query、飞书/Slack 的
// token 在 path、集成 URL 可能带 userinfo——而 Go 的 *url.Error 会把完整 URL 塞进
// Error()。只按参数名做黑名单必然漏，所以这里的规则是**结构性**的：形状像 URL 就
// 塌缩成 scheme://host，再做 Authorization 头与键值形态的值替换。
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

// invalidURL 解析失败 / 无 host / host 形状可疑（`https://user:`）时的占位：
// 宁可丢信息，也不回显原串。
const invalidURL = "<invalid-url>"

// URL 返回不含凭据的 URL 摘要：scheme://host（丢 userinfo、path、query）。
// userinfo 在 net/url 里是独立字段（u.User），所以 scheme://host 天然不带它。
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return invalidURL
	}
	// scheme-relative（配置里漏写 scheme 的 URL）：保留 //host 供定位，路径/query 照旧丢。
	// （net/url 只在有 authority 时给出 Host，所以这里必然来自 `//host` 形态。）
	if u.Scheme == "" {
		return "//" + u.Host
	}
	// `https://user:`（userinfo 无密码）会被 net/url 解析成 host="user:"——它不是 host 而是
	// 疑似凭据，原样返回等于回显。同理 `host:` 形态一律塌缩。
	if strings.HasSuffix(u.Host, ":") {
		return invalidURL
	}
	// M30-F3b：host 里的 `=` 同理。它虽是 RFC 3986 reg-name 的合法字符，却不是任何真实
	// 主机的形态，而是「key=value 被 URL 吞掉」的典型形状（`http://access_token=SECRET`）。
	// 这条是 Text 分段（F3）的**必要条件**：分段后 URL 段不再过规则 3，URL() 的输出必须
	// 自己保证「不含凭据形状」，否则分段会把现状遮住的串变成明文。
	if strings.Contains(u.Host, "=") {
		return invalidURL
	}
	return u.Scheme + "://" + u.Host
}

var (
	// 规则 1：任何 URL 形状的子串。路径与 query 里都可能有 token（飞书/Slack 在 path）。
	//   - 绝对形式 scheme://…
	//   - scheme-relative 形式 //host…（配置漏写 scheme 时 Go 把原串塞进 *url.Error）
	// 值类不排除 ' 与 \：它们是凭据里的常见字符，排除等于把尾部留在文本里（审计 M-3）。
	// scheme-relative 的 authority 限定「带点域名 / localhost / [IPv6]」，避免把
	// `//var/log` 这类路径当 URL 误伤（审计 M-2）。
	urlRe = regexp.MustCompile(
		`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s"<>]+` +
			`|//(?:[a-zA-Z0-9\-]+(?:\.[a-zA-Z0-9\-]+)+|localhost|\[[0-9a-fA-F:]+\])(?::\d+)?(?:/[^\s"<>]*)?`,
	)

	// 规则 2：Authorization: Bearer|Basic <cred>。值类只以空白/引号为界——值里的
	// `,`/`;` 也是凭据的一部分（审计 P1：`Bearer abc,def` 旧版只抹掉 `abc`）。
	//
	// M30-F1：起始引号并入值类。旧版要求值首字符非引号，于是**闭合引号**的凭据
	// （`Bearer "SECRET"`，从配置文件复制粘贴出来的常态）整条不匹配 = 完全不脱敏；
	// 「未闭合」与否无关，是同一个值类的两种表现。引号用 `*` 而不是 `?`：上界不是安全
	// 边界，没有理由定在 1（`Bearer ""SECRET` 同样要遮）。起始引号吃掉后不回写，
	// 故闭合引号的**尾部**会留下（`Bearer ***"`）—— 吃尾引号要多一个分支，收益只是观感。
	authHeaderRe = regexp.MustCompile(`(?i)(\bauthorization\s*:\s*(?:bearer|basic)\s+)["']*([^\s"']+)`)

	// 规则 3：键值形态（query / JSON / YAML 冒号）。名字允许 `前缀_`/`后缀` 组合
	// （smtp_password、x-webhook-secret、access_key_id），但**不收裸 key** —— 那会误伤
	// "primary key=…"；`design=`/`monkey=` 因为敏感词必须落在 `-`/`_` 分段边界上而不匹配。
	// 企微的 `?key=` 在 URL 里，由规则 1 覆盖。
	//
	// 值类只以「空白 / 引号 / query 分隔符 &,;」为界：`}` `]` `<` `>` 不收窄，否则
	// `{"password":"ab}c"}` 会漏掉尾部 `}c`、`password=}SECRET` 干脆不匹配（审计 P1）。
	//
	// M30-F2：值类的两个缺口。
	//   - **起引号**（`(["']*)`，捕获组）：旧版 `(["']?)` 只吃 0/1 个，`password=""abc`
	//     与 `password= '"SECRET'` 整条不匹配 = 完全不脱敏。捕获组必须**回写**
	//     （`${1}${2}***`）—— 写成非捕获会改掉 `password="SECRET"` → `password=***"`，
	//     破坏「引号形态保留」这个既有契约（redact_test.go 的边界锁定用例钉着）。
	//     量词用 `*` 而非 `{0,2}`：没有理由给引号定上界。
	//   - **起分隔符**（`[&,;][^\s]*`）：旧版值**以** `&`/`,`/`;` 开头时整条不匹配
	//     （`password=&SECRET` 明文）。吞到**下一个空白**而不是「只吞一个 token」——
	//     只吞 token 会把 `password=&next=SECRET` 变成 `password=&***=SECRET`：吃掉了
	//     下一个键名却把它的值留成明文，那是新引入的泄漏面。代价见 Text 的注释。
	//
	// M30-F4：分隔符补全角冒号 `：`（中文 IME 打冒号默认输出它，中文语境里手敲的
	// `token：xxx` 是常态）。代价：中文没有词间空格，`重置 password：请联系管理员`
	// 会整句被吞成 `重置 password：***`——与「吞到空白」同一类代价、同一套价值排序。
	kvSecretRe = regexp.MustCompile(`(?i)(\b(?:[a-z0-9]+[-_])*(?:access[-_]?token|access[-_]?key|token|secret|password|passwd|pwd|api[-_]?key|apikey|app[-_]?secret|user[-_]?token|sign)(?:[-_][a-z0-9]+)*["']?\s*[:=：]\s*)(["']*)(?:[&,;][^\s]*|[^\s"'&,;]+)`)
)

// Text 把文本里的凭据值替换为 ***：
//  1. URL 形状子串 → scheme://host（覆盖 path / query / userinfo 里的 token）
//  2. Authorization: Bearer|Basic 的值
//  3. access_token=… / "password":"…" 这类键值形态的值（保留键名，便于定位）
//
// **规则 1 与规则 2/3 不叠加（M30-F3）**：文本先按 URL 匹配切成「URL 段 / 非 URL 段」，
// URL 段只过 URL()，非 URL 段才过规则 2/3。旧版是三次 ReplaceAll 顺序执行，于是规则 3
// 会看见规则 1 的**输出**，把 `scheme://host:port` 的端口当键值吃掉
// （`http://token:8080/x` → `http://token:***`，G-35）——丢的正是规则 1 想保住的主机定位
// 信息。分段让「规则 3 知道自己在不在 URL 里」由**结构**保证，不靠模式匹配去猜：
// 用「回看 `://` 就跳过规则 3」的写法，`://access_token=SECRET` 会变成明文（见测试）。
//
// 分段依赖的不变式：**URL() 的输出不含凭据形状**。path/query/userinfo 由塌缩丢弃，
// host 里的 `=` 由 F3b 挡掉 —— 改 URL() 时若放宽这两点，分段就会退化成泄漏。
//
// 不含敏感内容时原样返回。
func Text(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range urlRe.FindAllStringIndex(s, -1) {
		b.WriteString(redactKeyValues(s[last:loc[0]]))
		b.WriteString(URL(s[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(redactKeyValues(s[last:]))
	return b.String()
}

// redactKeyValues 规则 2/3：作用于**不含 URL** 的文本段（见 Text 的分段说明）。
func redactKeyValues(s string) string {
	s = authHeaderRe.ReplaceAllString(s, "${1}***")
	s = kvSecretRe.ReplaceAllString(s, "${1}${2}***")
	return s
}

// StripControl 删除控制字符（C0：NUL/HTAB/CR/LF 等，以及 DEL）。
//
// 与 Text 的分工：Text 管「别泄漏凭据」，本函数管「别伪造行」——两者互不替代。
// 反过来也不成立：剥控制字符不能替代脱敏（只有前者时凭据照样明文）。
//
// **顺序是安全边界**：必须先 StripControl 再 Text，不能反。
// Text 的规则是「形状识别」，而控制字符会截断值类——`password=abc\nDEF` 在 Text 眼里
// 值只到 `\n` 为止（遮成 `password=***\nDEF`），此后再剥掉 `\n`，等于把未遮盖的尾部
// 接回一个已被认成凭据的串上，得到 `password=***DEF`；极端情形 `pass\nword=SECRET`
// 更是整条泄漏（Text 先看不到 `password=` 这个键）。反过来先 Strip 则拼接发生在识别
// 之前，凭据完整、遮盖完整。notification.markFailed 就曾因顺序反了而真泄漏（M29 修复）。
//
// 顺带保证输出是合法 UTF-8：strings.Map 把非法字节按 rune 迭代为 U+FFFD，
// 于是 PostgreSQL 的 22021（invalid byte sequence）那一类也一并关掉。
// 含 HTAB 是既定口径（与 notification.stripControlChars、sanitizeAuditUsername 一致）。
func StripControl(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
