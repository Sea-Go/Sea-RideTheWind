package contentsecurityservicelogic

import (
	"strings"
	"testing"
)

// G5 测试补底：security 域首个测试——内容净化纯函数（XSS 防线的行为锁定）。

func TestSanitizeHTMLStripsScript(t *testing.T) {
	out := sanitizeHTML(`<p onclick="evil()">hi</p><script>alert(1)</script>`, []string{"p"})
	if strings.Contains(strings.ToLower(out), "<script") {
		t.Fatalf("script 标签必须被剥离: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "onclick") {
		t.Fatalf("事件属性必须被剥离: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("正文应保留: %q", out)
	}
}

func TestSanitizeHTMLAllowsWhitelistedTags(t *testing.T) {
	out := sanitizeHTML(`<b class="x">bold</b><em>it</em>`, []string{"b", "i", "em"})
	if !strings.Contains(out, "<b") || !strings.Contains(out, "bold") {
		t.Fatalf("白名单 b 应保留: %q", out)
	}
	if !strings.Contains(out, "<em>") {
		t.Fatalf("白名单 em 应保留: %q", out)
	}
}

func TestSanitizeHTMLStripsNonWhitelistedTags(t *testing.T) {
	out := sanitizeHTML(`<b>ok</b><table><tr><td>x</td></tr></table>`, []string{"b"})
	if strings.Contains(strings.ToLower(out), "<table") {
		t.Fatalf("未白名单的 table 应剥离: %q", out)
	}
	if !strings.Contains(out, "<b>ok</b>") {
		t.Fatalf("白名单 b 应原样保留: %q", out)
	}
}

func TestSanitizeHTMLDefaultPolicy(t *testing.T) {
	out := sanitizeHTML(`<h1>t</h1><script>x</script>`, nil)
	if strings.Contains(strings.ToLower(out), "<script") {
		t.Fatalf("默认策略也应剥离 script: %q", out)
	}
}

func TestNormalizeFunctions(t *testing.T) {
	if got := normalizeWhitespace("a\r\nb\t\tc  d\n\n"); got != "a b c d" {
		t.Errorf("空白归一失败: %q", got)
	}
	if got := normalizeWhitespace("  "); got != "" {
		t.Errorf("纯空白应归空: %q", got)
	}
	nfc := normalizeUnicode("é")
	if nfc != "é" {
		t.Errorf("NFC 归一失败: %q", nfc)
	}
}
