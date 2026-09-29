package command_review_svc

import "regexp"

// redactRule 把命中的密钥值替换成 ***。规则按顺序执行，前面的规则可能改变后面规则看到的文本。
type redactRule struct {
	re   *regexp.Regexp
	repl string
}

// quotedOrBare 匹配一个参数值：单引号、双引号，或到空白 / shell 分隔符为止的裸值。
const quotedOrBare = `('[^']*'|"[^"]*"|[^\s;&|]+)`

var redactRules = []redactRule{
	// 私钥内容整体替换。
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), `-----BEGIN PRIVATE KEY-----***-----END PRIVATE KEY-----`},
	// 网址里的 用户名:密码@。
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://[^\s:/@'"]+:)([^\s@/'"]+)@`), `${1}***@`},
	// 请求头。
	{regexp.MustCompile(`(?i)(authorization:\s*(?:bearer\s+|basic\s+|token\s+)?)([^\s'"]+)`), `${1}***`},
	{regexp.MustCompile(`(?i)((?:x-api-key|x-auth-token|api-key):\s*)([^\s'"]+)`), `${1}***`},
	// JSON 字段。
	{regexp.MustCompile(`(?i)("[a-z0-9_]*(?:password|passwd|pwd|token|secret|api_?key|access_?key)[a-z0-9_]*"\s*:\s*)"[^"]*"`), `${1}"***"`},
	// SQL 里设置密码。
	{regexp.MustCompile(`(?i)(IDENTIFIED\s+(?:WITH\s+\S+\s+)?BY\s+)('[^']*'|"[^"]*"|\S+)`), `${1}'***'`},
	// --password <值>（值不以 - 开头，避免把下一个参数当成密码）；必须在下一条之前。
	{regexp.MustCompile(`(?i)(--pass(?:word)?\s+)([^\s\-]\S*)`), `${1}***`},
	{regexp.MustCompile(`(?i)(\bPASSWORD\s+)('[^']*'|"[^"]*")`), `${1}'***'`},
	// NAME=值：变量名里带 password / token / secret 等，也覆盖 --password=值 和网址参数。
	{regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:password|passwd|pwd|token|secret|api_?key|access_?key|credentials?)[a-z0-9_]*)=` + quotedOrBare), `${1}=***`},
	// MySQL 家族紧跟在 -p 后面的密码；单独的 -p（交互输入）不动。
	{regexp.MustCompile(`(?i)(\b(?:mysql|mysqldump|mysqladmin|mariadb|mariadb-dump)\b[^;&|\n]*?\s-p)([^\s;&|]+)`), `${1}***`},
	{regexp.MustCompile(`(\bsshpass\s+-p\s*)` + quotedOrBare), `${1}***`},
	{regexp.MustCompile(`(\bredis-cli\b[^;&|\n]*?\s-a\s+)` + quotedOrBare), `${1}***`},
	// curl -u 用户:密码（值里带 / 的是网址，不处理）。
	{regexp.MustCompile(`(\s(?:-u|--user)\s+)([^\s:'"/]+):([^\s'"@/]+)(\s|$)`), `${1}${2}:***${4}`},
	// Redis AUTH [用户名] 密码。
	{regexp.MustCompile(`(?im)^(\s*AUTH\s+).+$`), `${1}***`},
	// 常见的密钥格式。
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), `AKIA***`},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`), `gh***`},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{16,}`), `sk-***`},
}

// RedactSecrets 把命令里的密码、token、私钥等替换成 ***。命令会发给外部模型审核，
// 这里宁可多替换，也不能漏掉明显的密钥写法。
func RedactSecrets(command string) string {
	for _, r := range redactRules {
		command = r.re.ReplaceAllString(command, r.repl)
	}
	return command
}
