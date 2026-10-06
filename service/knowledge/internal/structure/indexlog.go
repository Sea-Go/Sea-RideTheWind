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

// RenderModuleIndex renders index.md for one knowledge module. Pages render
// in the given order; the renderer never invents content.
func RenderModuleIndex(moduleTitle string, pages []PageIndex) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", moduleTitle)
	if len(pages) == 0 {
		b.WriteString("_（暂无页面）_\n")
		return b.String()
	}
	for _, p := range pages {
		summary := strings.TrimSpace(p.Summary)
		if summary == "" {
			summary = "_（无摘要）_"
		}
		ref := strings.TrimSpace(p.Ref)
		if ref == "" {
			fmt.Fprintf(&b, "- **%s** — %s\n", p.Title, summary)
			continue
		}
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", p.Title, ref, summary)
	}
	return b.String()
}

// FormatLogEvent renders one append-only log line:
//
//	<RFC3339> <prefix>: <detail>
//
// detail is flattened to a single line so every event stays grep-friendly.
func FormatLogEvent(at time.Time, prefix, detail string) string {
	detail = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(detail))
	return fmt.Sprintf("%s %s: %s\n", at.UTC().Format(time.RFC3339), prefix, detail)
}
