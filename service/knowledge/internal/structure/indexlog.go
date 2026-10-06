package structure

import (
	"fmt"
	"strings"
	"time"
)

// This file renders the two module-level maintenance artifacts required by
// the platform Lint capability: a catalog page (index.md) and an append-only
// event log (log.md). Both are plain markdown so they stay greppable and
// diff-friendly in git.

// Log event prefixes. The set is closed so log lines stay mechanically
// parseable.
const (
	LogIngest  = "ingest"
	LogCompile = "compile"
	LogRelease = "release"
	LogLint    = "lint"
)

// PageIndex is one catalog entry of a module index page.
type PageIndex struct {
	Title   string
	Summary string
	Ref     string // doc_key or relative link
}

// singleLine 把字段压成单行:所有空白(含换行/回车/制表符)折叠为单个空格并
// 去掉首尾空白。页面字段中的换行不能伪造出第二条索引条目或日志事件。
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// escapeBrackets 转义 Markdown 链接语法定界符:标题与摘要里的方括号不能
// 注入渲染器未曾打算生成的链接结构。
func escapeBrackets(s string) string {
	s = strings.ReplaceAll(s, "[", `\[`)
	return strings.ReplaceAll(s, "]", `\]`)
}

// escapeParens 转义链接目标中的圆括号,ref 里的括号不能提前截断目标或拼出
// 新的链接。CommonMark 目标允许反斜杠转义的括号,正常 ref 不受影响。
func escapeParens(s string) string {
	s = strings.ReplaceAll(s, "(", `\(`)
	return strings.ReplaceAll(s, ")", `\)`)
}

// RenderModuleIndex renders index.md for one knowledge module. Pages render
// in the given order; the renderer never invents content. Every field is
// flattened to a single line first, and bracket/paren syntax that could
// alter the markdown structure is escaped, so one page always renders as
// exactly one entry.
func RenderModuleIndex(moduleTitle string, pages []PageIndex) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", singleLine(moduleTitle))
	if len(pages) == 0 {
		b.WriteString("_（暂无页面）_\n")
		return b.String()
	}
	for _, p := range pages {
		title := escapeBrackets(singleLine(p.Title))
		summary := escapeBrackets(singleLine(p.Summary))
		if summary == "" {
			summary = "_（无摘要）_"
		}
		ref := escapeParens(singleLine(p.Ref))
		if ref == "" {
			fmt.Fprintf(&b, "- **%s** — %s\n", title, summary)
			continue
		}
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", title, ref, summary)
	}
	return b.String()
}

// FormatLogEvent renders one append-only log line:
//
//	<RFC3339> <prefix>: <detail>
//
// detail is flattened to a single line so every event stays grep-friendly,
// and prefix must be one of the four closed-set constants; any other value
// is rejected so a caller cannot forge an extra log line through the prefix.
func FormatLogEvent(at time.Time, prefix, detail string) (string, error) {
	switch prefix {
	case LogIngest, LogCompile, LogRelease, LogLint:
	default:
		return "", fmt.Errorf("structure: log prefix %q is outside the closed set {%s, %s, %s, %s}",
			prefix, LogIngest, LogCompile, LogRelease, LogLint)
	}
	detail = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(detail))
	return fmt.Sprintf("%s %s: %s\n", at.UTC().Format(time.RFC3339), prefix, detail), nil
}
