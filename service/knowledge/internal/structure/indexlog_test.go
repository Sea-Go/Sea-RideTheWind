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

func TestFormatLogEvent(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 30, 0, 0, time.FixedZone("CST", 8*3600))
	got := FormatLogEvent(at, LogRelease, "module ml 发布 r3\n跨行内容")
	if got != "2026-10-06T04:30:00Z release: module ml 发布 r3 跨行内容\n" {
		t.Fatalf("log line = %q", got)
	}
	for _, prefix := range []string{LogIngest, LogCompile, LogRelease, LogLint} {
		line := FormatLogEvent(at, prefix, "x")
		if !strings.HasSuffix(line, " "+prefix+": x\n") {
			t.Fatalf("prefix %s not rendered: %q", prefix, line)
		}
	}
}
