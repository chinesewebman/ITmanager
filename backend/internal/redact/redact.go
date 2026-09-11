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
	authHeaderRe = regexp.MustCompile(`(?i)(\bauthorization\s*:\s*(?:bearer|basic)\s+)[^\s"']+`)

	// 规则 3：键值形态（query / JSON / YAML 冒号）。名字允许 `前缀_`/`后缀` 组合
	// （smtp_password、x-webhook-secret、access_key_id），但**不收裸 key** —— 那会误伤
	// "primary key=…"；`design=`/`monkey=` 因为敏感词必须落在 `-`/`_` 分段边界上而不匹配。
	// 企微的 `?key=` 在 URL 里，由规则 1 覆盖。
	//
	// 值类只以「空白 / 引号 / query 分隔符 &,;」为界：`}` `]` `<` `>` 不收窄，否则
	// `{"password":"ab}c"}` 会漏掉尾部 `}c`、`password=}SECRET` 干脆不匹配（审计 P1）。
	// 残余：值**以** `&`/`,`/`;` 开头时不匹配（见 §6 登记）。
	kvSecretRe = regexp.MustCompile(`(?i)(\b(?:[a-z0-9]+[-_])*(?:access[-_]?token|access[-_]?key|token|secret|password|passwd|pwd|api[-_]?key|apikey|app[-_]?secret|user[-_]?token|sign)(?:[-_][a-z0-9]+)*["']?\s*[:=]\s*)(["']?)([^\s"'&,;]+)`)
)

// Text 把文本里的凭据值替换为 ***：
//  1. URL 形状子串 → scheme://host（覆盖 path / query / userinfo 里的 token）
//  2. Authorization: Bearer|Basic 的值
//  3. access_token=… / "password":"…" 这类键值形态的值（保留键名，便于定位）
//
// 不含敏感内容时原样返回。
func Text(s string) string {
	if s == "" {
		return s
	}
	s = urlRe.ReplaceAllStringFunc(s, URL)
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
