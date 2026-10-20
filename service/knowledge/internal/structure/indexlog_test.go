package structure

import (
	"strings"
	"testing"
	"time"
)

func TestRenderModuleIndex(t *testing.T) {
	out := RenderModuleIndex("机器学习入门", []PageIndex{
		{Title: "梯度下降", Summary: "一阶迭代优化的基础方法。", Ref: "page:grad@r3"},
		{Title: "反向传播", Summary: "", Ref: ""},
	})
	want := "- [梯度下降](page:grad@r3) — 一阶迭代优化的基础方法。\n- **反向传播** — _（无摘要）_\n"
	if !strings.Contains(out, want) {
		t.Fatalf("index markdown mismatch:\n%q", out)
	}
	if !strings.HasPrefix(out, "# 机器学习入门\n") {
		t.Fatalf("missing module title heading: %q", out)
	}
	if empty := RenderModuleIndex("空模块", nil); !strings.Contains(empty, "_（暂无页面）_") {
		t.Fatalf("empty index = %q", empty)
	}
}

func TestRenderModuleIndexFlattensAndEscapesFields(t *testing.T) {
	out := RenderModuleIndex("模块\n标题", []PageIndex{
		{Title: "First\n- [Second](other)", Summary: "摘\n要\t一行", Ref: "page:11111111-2222-3333-4444-555555555555@r1\n"},
	})
	if !strings.HasPrefix(out, "# 模块 标题\n") {
		t.Fatalf("module title not flattened: %q", out)
	}
	// 标题行 + 空行 + 恰好一条条目行:字段里的换行不能伪造第二条目。
	if got := strings.Count(out, "\n"); got != 3 {
		t.Fatalf("index should render 3 lines, got %d:\n%q", got, out)
	}
	want := "- [First - \\[Second\\](other)](page:11111111-2222-3333-4444-555555555555@r1) — 摘 要 一行\n"
	if !strings.Contains(out, want) {
		t.Fatalf("entry mismatch:\n got %q\nwant %q", out, want)
	}
}

func TestFormatLogEvent(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 30, 0, 0, time.FixedZone("CST", 8*3600))
	got, err := FormatLogEvent(at, LogRelease, "module ml 发布 r3\n跨行内容")
	if err != nil {
		t.Fatalf("FormatLogEvent: %v", err)
	}
	if got != "2026-10-06T04:30:00Z release: module ml 发布 r3 跨行内容\n" {
		t.Fatalf("log line = %q", got)
	}
	for _, prefix := range []string{LogIngest, LogCompile, LogRelease, LogLint} {
		line, err := FormatLogEvent(at, prefix, "x")
		if err != nil {
			t.Fatalf("prefix %s: %v", prefix, err)
		}
		if !strings.HasSuffix(line, " "+prefix+": x\n") {
			t.Fatalf("prefix %s not rendered: %q", prefix, line)
		}
	}
}

func TestFormatLogEventRejectsUnknownPrefix(t *testing.T) {
	if _, err := FormatLogEvent(time.Now(), "evil\n2026-01-01T00:00:00Z ingest", "x"); err == nil {
		t.Fatal("expected prefix outside the closed set to be rejected")
	}
}
